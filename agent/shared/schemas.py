"""共享 pydantic 数据模型。"""
from __future__ import annotations

from typing import Any, Optional

from pydantic import BaseModel, Field


class LiveData(BaseModel):
    """现场数据统一形态（回答必须带采样时间）。"""

    plot_id: str
    metric: str
    value: float
    unit: str = ""
    sampled_at: str


class Citation(BaseModel):
    """引用片段（落 chat_messages.citations_json）。"""

    doc_id: str
    chunk_id: Optional[str] = None
    title: str = ""
    version: Optional[int] = None
    source: str = ""


class ToolResult(BaseModel):
    """工具执行结果。"""

    ok: bool = True
    data: Any = None
    error: Optional[str] = None
    trace_id: Optional[str] = None


class SessionState(BaseModel):
    """会话状态（Redis agent:session:{sessionId}）。"""

    user_id: str
    status: str = "active"  # active / waiting_close / closed
    last_message_at: Optional[str] = None
    plot_id: Optional[str] = None
    message_count: int = 0
