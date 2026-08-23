"""trace_id 贯穿工具：生成/透传/读取。

约定：跨服务调用在 HTTP 头 `X-Trace-Id` 传递；进程内用 contextvar 传播。
"""
from __future__ import annotations

import contextvars
import uuid

_current: contextvars.ContextVar[str | None] = contextvars.ContextVar("trace_id", default=None)
_request: contextvars.ContextVar[str | None] = contextvars.ContextVar("request_id", default=None)
_actor: contextvars.ContextVar[str | None] = contextvars.ContextVar("actor_id", default=None)

TRACE_HEADER = "X-Trace-Id"
REQUEST_HEADER = "X-Request-ID"
HEADER = TRACE_HEADER


def new_trace_id() -> str:
    return uuid.uuid4().hex


def set_trace_id(trace_id: str) -> None:
    _current.set(trace_id)


def get_request_id() -> str | None:
    return _request.get()


def ensure_request_id() -> str:
    request_id = get_request_id()
    if not request_id:
        request_id = new_trace_id()
        _request.set(request_id)
    return request_id


def set_actor_id(actor_id: str) -> None:
    _actor.set(str(actor_id))


def get_actor_id() -> str | None:
    return _actor.get()


def get_trace_id() -> str | None:
    return _current.get()


def ensure_trace_id() -> str:
    """当前无 trace_id 则生成并设置，返回当前值。"""
    tid = get_trace_id()
    if not tid:
        tid = new_trace_id()
        set_trace_id(tid)
    return tid


def normalize_correlation_id(value: str | None, fallback: str | None = None) -> str:
    value = (value or "").strip()
    if value and len(value) <= 64 and all(char.isascii() and (char.isalnum() or char in "._:-") for char in value):
        return value
    return fallback or new_trace_id()


def bind_correlation_ids(trace_id: str, request_id: str) -> tuple[contextvars.Token, contextvars.Token, contextvars.Token]:
    return _current.set(trace_id), _request.set(request_id), _actor.set(None)


def reset_correlation_ids(tokens: tuple[contextvars.Token, contextvars.Token, contextvars.Token]) -> None:
    trace_token, request_token, actor_token = tokens
    _current.reset(trace_token)
    _request.reset(request_token)
    _actor.reset(actor_token)
