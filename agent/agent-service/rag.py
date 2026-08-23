"""agent-service：RAG 知识检索 + 长期记忆召回（Milvus 直连）。"""
from __future__ import annotations

from typing import Any

from shared.config import get_config
from shared.embedding import embed
from shared.milvus_client import search_knowledge as _kb_search
from shared.milvus_client import search_memory as _mem_search


def rag_search(question: str, category: str | None = None) -> list[dict[str, Any]]:
    """知识检索：文本 → embedding → Milvus 知识 collection。"""
    top_k = int(get_config("tool").get("top_k", 5))
    vec = embed([question])[0]
    return _kb_search(vec, top_k=top_k, category=category)


def memory_recall(user_id: str, question: str) -> list[dict[str, Any]]:
    """长期记忆召回（user_id 强隔离，top_k 默认 3，时间衰减在检索侧标注）。"""
    top_k = int(get_config("agent").get("memory_top_k", 3))
    try:
        vec = embed([question])[0]
    except Exception:
        return []
    hits = _mem_search(user_id, vec, top_k=top_k)
    return [
        {
            "summary": h["summary"],
            "generated_at": h.get("generated_at"),
            "score": h.get("score"),
        }
        for h in hits
    ]
