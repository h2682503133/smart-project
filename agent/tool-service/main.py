"""tool-service：FastAPI 入口（端口 8002）。

GET /tools                → 工具清单（JSON Schema，agent 启动时拉取缓存）
POST /tools/{name}/execute → 执行工具（JWT 透传 + JSON Schema 校验 + trace_id）
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from fastapi import FastAPI, Header, HTTPException  # noqa: E402
from pydantic import BaseModel, Field  # noqa: E402
from jsonschema import Draft202012Validator  # noqa: E402

from registry import get_registry  # noqa: E402
from tools.impl import register_all  # noqa: E402
from shared.observability import install_observability  # noqa: E402
from shared.trace import HEADER, ensure_trace_id  # noqa: E402

app = FastAPI(title="tool-service", version="0.1.0")
install_observability(app, "tool-service")

register_all()
_registry = get_registry()


class ExecuteRequest(BaseModel):
    args: dict = Field(default_factory=dict)


class ToolInfo(BaseModel):
    name: str
    version: str
    description: str
    parameters: dict
    required: list[str]


@app.get("/healthz")
def healthz() -> dict:
    return {"ok": True}


@app.get("/tools", response_model=list[ToolInfo])
def list_tools() -> list[ToolInfo]:
    return [ToolInfo(**d.model_dump()) for d in _registry.list_defs()]


@app.post("/tools/{name}/execute")
def execute(
    name: str,
    req: ExecuteRequest,
    authorization: str = Header(default=""),
    x_trace_id: str = Header(default="", alias=HEADER),
) -> dict:
    tool_def = _registry.get_def(name)
    if tool_def is None:
        raise HTTPException(status_code=404, detail=f"未知工具: {name}")
    fn = _registry.get_fn(name)
    if fn is None:
        raise HTTPException(status_code=500, detail=f"工具未实现: {name}")

    # JSON Schema 校验入参（白名单）
    schema = {
        "type": "object",
        "properties": tool_def.parameters,
        "required": tool_def.required,
        "additionalProperties": False,
    }
    try:
        Draft202012Validator(schema).validate(req.args)
    except Exception as e:
        raise HTTPException(status_code=422, detail=f"参数校验失败: {e}") from e

    try:
        result = fn(authorization, req.args)
    except Exception as e:
        # 工具异常：返回 ok=false，不抛 HTTP 错误（agent 可向 LLM 解释）
        return {"ok": False, "error": f"工具执行失败: {e}", "trace_id": ensure_trace_id()}
    result["trace_id"] = ensure_trace_id()
    return result
