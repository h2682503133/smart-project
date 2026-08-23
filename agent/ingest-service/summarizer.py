"""ingest-service：会话摘要（消费 queue:session.summary）。

流程：会话 closed → 消费事件
  → 源：Redis 窗口 / Go GET /agent/sessions/{id}/messages
  → ≥2 条消息才生成摘要
  → LLM 生成摘要 → embedding → 写独立记忆 collection（metadata: session_id/model_version/generated_at/plot_id）
  → 不落 SQL
"""
from __future__ import annotations

import datetime
from typing import Any

from shared.config import get_config
from shared.embedding import embed
from shared.milvus_client import ensure_collections, upsert_documents
from shared.redis_client import get_redis

_SUMMARY_PROMPT = (
    "把以下对话压缩成一段会话摘要（100 字以内），保留：用户身份相关、地块/作物、"
    "关键决策与结论、待办事项。不要添加对话中不存在的信息。\n\n对话：\n{dialogue}"
)


def _collect_dialogue(session_id: str, user_id: str) -> list[dict]:
    """摘要源：优先 Redis 窗口（按用户），其次留空（Go 拉取在 worker 层可扩展）。"""
    turns = get_redis().window_get(user_id)
    return turns


def build_summary(session_id: str, user_id: str) -> dict[str, Any]:
    """生成并写入记忆向量。返回 {session_id, status}。"""
    import asyncio

    cfg = get_config("agent")
    min_messages = int(cfg.get("min_messages_for_summary", 2))

    dialogue = _collect_dialogue(session_id, user_id)
    if len(dialogue) < min_messages:
        return {"session_id": session_id, "status": "skipped", "reason": "消息不足"}

    dialogue_text = "\n".join(f"{t.get('role')}: {t.get('content')}" for t in dialogue)
    try:
        from llm import get_llm  # ingest 复用 agent 的 llm 适配（或独立配置）

        summary = asyncio.run(get_llm().chat([{"role": "user", "content": _SUMMARY_PROMPT.format(dialogue=dialogue_text)}]))
    except Exception as e:
        return {"session_id": session_id, "status": "failed", "reason": f"摘要生成失败: {e}"}

    ensure_collections()
    vec = embed([summary])[0]
    generated_at = datetime.datetime.now(datetime.timezone.utc).isoformat()
    rows = [{
        "id": f"mem:{session_id}:{generated_at}",
        "vector": vec,
        "user_id": user_id,
        "session_id": session_id,
        "summary_text": summary,
        "generated_at": generated_at,
        "model_version": get_config("llm").get("model"),
    }]
    upsert_documents(rows, kind="memory")
    return {"session_id": session_id, "status": "ok", "summary": summary}
