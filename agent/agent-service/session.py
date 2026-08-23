"""agent-service：会话状态机 + 结束判定（三层）。

状态：active / waiting_close / closed（Redis agent:session:{sessionId}）
结束判定：
  1. 显式按钮（前端调 close 接口）
  2. 意图判定（规则优先 → LLM 意图）
  3. waiting_close 后 5 分钟超时（惰性检查）
"""
from __future__ import annotations

import datetime
import re
from typing import Any

from shared.config import get_config
from shared.redis_client import get_redis

STATUS_ACTIVE = "active"
STATUS_WAITING_CLOSE = "waiting_close"
STATUS_CLOSED = "closed"

# 规则优先：命中即结束意图（省一轮 LLM 调用）
_CLOSE_PATTERNS = [
    r"没(有|什么|啥|别|其他)?(问题|事|了)?$",
    r"不(用|需要|要)(了|啦)?$",
    r"谢(谢|啦)",
    r"就?(这些|这样|这点)(了|吧)?$",
    r"^(end|no|thanks|bye|done)$",
]

_close_re = re.compile("|".join(_CLOSE_PATTERNS), re.IGNORECASE)


def _now_iso() -> str:
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def get_session(session_id: str) -> dict[str, Any] | None:
    return get_redis().session_get(session_id)


def create_session(user_id: str, session_id: str, plot_id: str | None = None) -> dict[str, Any]:
    state = {"session_id": session_id, "user_id": user_id, "status": STATUS_ACTIVE,
             "last_message_at": _now_iso(), "plot_id": plot_id, "message_count": 0}
    get_redis().session_set(session_id, state)
    return state


def touch(session_id: str, state: dict[str, Any]) -> None:
    """更新最后交互时间与消息计数。"""
    state["last_message_at"] = _now_iso()
    state["message_count"] = int(state.get("message_count", 0)) + 1
    get_redis().session_set(session_id, state)


def mark_waiting_close(session_id: str, state: dict[str, Any]) -> None:
    state["status"] = STATUS_WAITING_CLOSE
    get_redis().session_set(session_id, state)


def close(session_id: str, state: dict[str, Any]) -> None:
    state["status"] = STATUS_CLOSED
    get_redis().session_set(session_id, state)


# ---------- 结束判定 ----------

def rule_close_hit(text: str) -> bool:
    """规则优先：正则命中即视为结束意图。"""
    return bool(_close_re.search(text.strip()))


async def llm_intent(text: str) -> str:
    """LLM 意图分类：close / new_question / continue_topic。"""
    from llm import get_llm

    prompt = (
        "判断用户这句话的意图，只输出一个词：\n"
        "- close：表示对话结束（如：没有了、不用了、谢谢、就这些）\n"
        "- new_question：提出新的问题（如：那B地块呢？帮我看看温度）\n"
        "- continue_topic：继续追问当前话题\n"
        f"用户: {text}\n"
        "意图:"
    )
    answer = (await get_llm().chat([{"role": "user", "content": prompt}])).strip().lower()
    if "close" in answer:
        return "close"
    if "new" in answer:
        return "new_question"
    return "continue_topic"


def check_lazy_timeout(state: dict[str, Any], timeout_minutes: int | None = None) -> bool:
    """惰性检查：waiting_close 且 last_message_at 距今超时 → 应结束。"""
    if state.get("status") != STATUS_WAITING_CLOSE:
        return False
    if timeout_minutes is None:
        timeout_minutes = int(get_config("agent").get("close_timeout_minutes", 5))
    try:
        last = datetime.datetime.fromisoformat(state["last_message_at"])
    except (KeyError, ValueError):
        return False
    elapsed = (datetime.datetime.now(datetime.timezone.utc) - last).total_seconds()
    return elapsed > timeout_minutes * 60
