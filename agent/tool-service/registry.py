"""tool-service：工具注册表（9 个工具，JSON Schema 白名单 + 版本号）。

agent-service 不认识工具实现，只认识"工具名 + 入参"；
本服务注册全部工具定义（含 JSON Schema），并执行工具实现。
"""
from __future__ import annotations

from typing import Any, Callable, Optional

from pydantic import BaseModel, Field


class ToolDef(BaseModel):
    name: str
    version: str
    description: str
    parameters: dict[str, Any]  # JSON Schema（object）
    required: list[str] = Field(default_factory=list)


# 工具实现签名：fn(authorization: str, args: dict) -> dict（返回统一 {ok, data/error}）
ToolFn = Callable[[str, dict[str, Any]], dict[str, Any]]


class _Registry:
    def __init__(self) -> None:
        self._defs: dict[str, ToolDef] = {}
        self._fns: dict[str, ToolFn] = {}

    def register(self, name: str, version: str, description: str,
                 parameters: dict[str, Any], required: list[str], fn: ToolFn) -> None:
        self._defs[name] = ToolDef(
            name=name, version=version, description=description,
            parameters=parameters, required=required,
        )
        self._fns[name] = fn

    def list_defs(self) -> list[ToolDef]:
        return list(self._defs.values())

    def get_def(self, name: str) -> Optional[ToolDef]:
        return self._defs.get(name)

    def get_fn(self, name: str) -> Optional[ToolFn]:
        return self._fns.get(name)


_registry = _Registry()


def get_registry() -> _Registry:
    return _registry
