"""agent-service：问答编排主流程。

链路：JWT → 会话状态（惰性超时检查）→ 三路取数（知识/现场工具/记忆）
     → context/build 组装 → LLM 流式 → 落库 chat_messages → 更新窗口+activity
     → 收尾意图（can_close → 追问"还有其他问题吗"）
"""
from __future__ import annotations

import json
from typing import Any, AsyncIterator

import httpx

from shared.config import get_config
from shared.go_client import get_go_client
from shared.redis_client import get_redis
from shared.trace import REQUEST_HEADER, ensure_request_id, ensure_trace_id
from llm import get_llm
from rag import memory_recall, rag_search
from session import (
    STATUS_ACTIVE,
    STATUS_CLOSED,
    STATUS_WAITING_CLOSE,
    check_lazy_timeout,
    close,
    create_session,
    get_session,
    llm_intent,
    mark_waiting_close,
    rule_close_hit,
    touch,
)

_context_url = None
_tool_url = None


def _svc_url(service: str) -> str:
    cfg = get_config(service)
    return cfg.get("base_url", f"http://{service}:{cfg.get('port', 8000)}")


async def _call_context(payload: dict, authorization: str) -> dict:
    async with httpx.AsyncClient(timeout=15, trust_env=False) as client:
        resp = await client.post(
            f"{_context_url}/context/build",
            json=payload,
            headers={
                "Authorization": authorization,
                "X-Trace-Id": ensure_trace_id(),
                REQUEST_HEADER: ensure_request_id(),
            },
        )
        resp.raise_for_status()
        return resp.json()


async def _call_tool(name: str, args: dict, authorization: str) -> dict:
    async with httpx.AsyncClient(timeout=15, trust_env=False) as client:
        resp = await client.post(
            f"{_tool_url}/tools/{name}/execute",
            json={"args": args},
            headers={
                "Authorization": authorization,
                "X-Trace-Id": ensure_trace_id(),
                REQUEST_HEADER: ensure_request_id(),
            },
        )
        resp.raise_for_status()
        return resp.json()


async def _tool_schemas() -> list[dict]:
    """拉取工具清单（启动缓存，失败返回空）。"""
    async with httpx.AsyncClient(timeout=10, trust_env=False) as client:
        resp = await client.get(
            f"{_tool_url}/tools",
            headers={"X-Trace-Id": ensure_trace_id(), REQUEST_HEADER: ensure_request_id()},
        )
        resp.raise_for_status()
        defs = resp.json()
    return [
        {
            "type": "function",
            "function": {
                "name": d["name"],
                "description": d["description"],
                "parameters": {"type": "object", "properties": d["parameters"], "required": d["required"]},
            },
        }
        for d in defs
    ]


async def handle_question(
    user_id: str,
    question: str,
    session_id: str | None,
    plot_id: str | None,
    authorization: str,
) -> AsyncIterator[dict[str, Any]]:
    """处理一轮问答，SSE 事件流：{type: answer/done/error, ...}。"""
    global _context_url, _tool_url
    _context_url = _context_url or _svc_url("context")
    _tool_url = _tool_url or _svc_url("tool")

    # ---------- 会话状态 ----------
    state = get_session(session_id) if session_id else None
    if state is None:
        session_id = session_id or "sess_" + ensure_trace_id()[:12]
        # 在 Go 侧同步建会话（落库需要 Go 的 session_id；失败则降级用本地 id）
        try:
            go_sess = get_go_client().create_session(authorization, plot_id)
            if go_sess.get("id"):
                session_id = go_sess["id"]
        except Exception:
            pass  # Go 建会话失败降级本地 id（落库会继续失败，但不阻塞回答）
        state = create_session(user_id, session_id, plot_id)
    if state["status"] == STATUS_CLOSED:
        yield {"type": "error", "message": "会话已结束，请发起新会话"}
        return
    if state["status"] == STATUS_WAITING_CLOSE and check_lazy_timeout(state):
        close(session_id, state)
        yield {"type": "error", "message": "会话已超时结束，请发起新会话"}
        return

    # waiting_close 下的回复：先规则判定（省一轮 LLM）
    if state["status"] == STATUS_WAITING_CLOSE:
        if rule_close_hit(question):
            close(session_id, state)
            yield {"type": "done", "sessionId": session_id, "canClose": True, "closed": True,
                   "message": "好的，有需要随时找我！"}
            return
        intent = await llm_intent(question)
        if intent == "close":
            close(session_id, state)
            yield {"type": "done", "sessionId": session_id, "canClose": True, "closed": True,
                   "message": "好的，有需要随时找我！"}
            return
        state["status"] = STATUS_ACTIVE  # 新问题/继续 → 回到 active
        get_redis().session_set(session_id, state)

    # ---------- 三路取数（知识/记忆并行，同步 embedding 走线程池） ----------
    knowledge = []
    memory = []

    def _safe_rag():
        try:
            return rag_search(question)
        except Exception:
            return []

    def _safe_memory():
        try:
            return memory_recall(user_id, question)
        except Exception:
            return []

    import asyncio

    knowledge, memory = await asyncio.gather(
        asyncio.to_thread(_safe_rag),
        asyncio.to_thread(_safe_memory),
    )

    # ---------- 组装（context-service，不含 live_data：工具结果走对话上下文） ----------
    window_turns = get_redis().window_get(user_id)
    prompt_result = await _call_context(
        {
            "user_id": user_id,
            "question": question,
            "window_turns": window_turns[-8:],
            "knowledge_chunks": [k.get("content", "") for k in knowledge[:5]],
            "memory_chunks": [m["summary"] for m in memory[:3]],
            "live_data": [],
        },
        authorization,
    )
    prompt = prompt_result["prompt"]

    messages = [{"role": "system", "content": prompt}]
    for t in window_turns[-6:]:
        messages.append({"role": t["role"], "content": t["content"]})
    messages.append({"role": "user", "content": question})

    # ---------- Agent 循环：LLM 全程控制工具调用，中间回复直出，最终回复前标注 ----------
    answer = ""
    max_rounds = int(get_config("agent").get("max_tool_rounds", 5))
    tool_log: list[dict] = []
    try:
        tools = await _tool_schemas()
    except Exception:
        tools = []

    for _ in range(max_rounds):
        round_parts: list[str] = []
        tool_calls_acc: dict[int, dict] = {}

        stream = await get_llm()._get_client().chat.completions.create(
            model=get_config("llm").get("model"),
            messages=messages,
            tools=tools or None,
            tool_choice="auto" if tools else None,
            stream=True,
            temperature=get_config("llm").get("temperature", 0.3),
        )
        # 本轮先收（不直出），轮结束后按"中间回复 / 最终回复"统一发出
        async for chunk in stream:
            if not chunk.choices:
                continue
            delta = chunk.choices[0].delta
            if delta is None:
                continue
            if delta.content:
                round_parts.append(delta.content)
            if delta.tool_calls:
                for tc in delta.tool_calls:
                    acc = tool_calls_acc.setdefault(tc.index, {"id": "", "name": "", "arguments": ""})
                    if tc.id:
                        acc["id"] = tc.id
                    if tc.function:
                        if tc.function.name:
                            acc["name"] += tc.function.name
                        if tc.function.arguments:
                            acc["arguments"] += tc.function.arguments

        if not tool_calls_acc:
            # 最终回复轮（代码判定）：内容前加"【最终回复】"前缀
            answer = "".join(round_parts)
            yield {"type": "answer", "delta": "【最终回复】"}
            for part in round_parts:
                yield {"type": "answer", "delta": part}
            break

        # 中间回复（本轮要调工具）：内容直出，再执行工具回填，继续循环
        for part in round_parts:
            yield {"type": "answer", "delta": part}
        tcs = [tool_calls_acc[i] for i in sorted(tool_calls_acc)]
        yield {
            "type": "tool_call",
            "tool_calls": [{"name": t["name"], "arguments": t["arguments"]} for t in tcs],
        }
        messages.append({
            "role": "assistant",
            "content": "".join(round_parts) or None,
            "tool_calls": [
                {
                    "id": t["id"],
                    "type": "function",
                    "function": {"name": t["name"], "arguments": t["arguments"] or "{}"},
                }
                for t in tcs
            ],
        })
        for t in tcs:
            try:
                args = json.loads(t["arguments"] or "{}")
            except json.JSONDecodeError:
                args = {}
            result = await _call_tool(t["name"], args, authorization)
            tool_log.append({"tool": t["name"], "args": args, "ok": result.get("ok")})
            messages.append({
                "role": "tool",
                "tool_call_id": t["id"],
                "content": json.dumps(result, ensure_ascii=False)[:2000],
            })
    else:
        # 达到最大轮数仍未收尾（防御）
        if not answer:
            answer = "已尽力查询，但信息有限，建议到系统查看最新数据。"

    can_close = _judge_can_close(question, answer)

    # ---------- 落库 + 窗口 + activity ----------
    # 会话创建未带 plot（Go 按 JWT 归属校验），落库也不传 plot_id，避免与会话 plot 不一致被拒
    try:
        get_go_client().post_message(authorization, session_id, {
            "role": "user", "content": question, "model_version": get_config("llm").get("model"),
        })
        get_go_client().post_message(authorization, session_id, {
            "role": "assistant", "content": answer,
            "citations": [{"docId": k.get("docId"), "title": k.get("title"), "version": k.get("version")} for k in knowledge[:5]] or None,
            "model_version": get_config("llm").get("model"),
        })
    except Exception as e:
        print(f"[落库失败] session={session_id}: {e}", flush=True)  # 不阻塞回答，但记录便于排查

    # 更新短期窗口（只存文字对话，现场数据不进窗口）
    window = get_redis().window_get(user_id)
    window.append({"role": "user", "content": question})
    window.append({"role": "assistant", "content": answer})
    get_redis().window_set(user_id, window[-16:])
    get_redis().touch_active(user_id)
    get_redis().xadd("session.activity", {"user_id": user_id, "ts": __import__("time").time()})

    touch(session_id, state)
    if can_close:
        mark_waiting_close(session_id, state)
        answer_note = "\n\n还有其他问题吗？"
        yield {"type": "answer", "delta": answer_note}
        yield {"type": "done", "sessionId": session_id, "canClose": True,
               "sources": _sources(knowledge), "closed": False}
    else:
        yield {"type": "done", "sessionId": session_id, "canClose": False,
               "sources": _sources(knowledge), "closed": False}


def _judge_can_close(question: str, answer: str) -> bool:
    """收尾意图启发式：问题简短 + 回答给出建议 → 视为可收尾（生产可换 LLM 判定）。"""
    return len(question) < 40


def _sources(knowledge: list[dict]) -> list[dict]:
    return [
        {"type": "KNOWLEDGE_DOC", "title": k.get("title"), "docId": k.get("docId"),
         "version": k.get("version"), "score": k.get("score")}
        for k in knowledge[:5]
    ]
