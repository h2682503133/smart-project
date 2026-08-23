"""context-service：FastAPI 入口（端口 8001）。

POST /context/build
  入参: {user_id, window_turns?, knowledge_chunks?, memory_chunks?, live_data?, question, budget_tokens?}
  出参: {prompt, used_tokens, trimmed}
说明: System 段所需的用户标签由 context-service 直接调 Go `GET /users/me`（JWT 透传）。
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from fastapi import FastAPI, Header, HTTPException, Request  # noqa: E402
from pydantic import BaseModel, Field  # noqa: E402

from assemble import assemble  # noqa: E402
from shared.go_client import get_go_client  # noqa: E402
from shared.observability import install_observability  # noqa: E402
from shared.trace import set_actor_id  # noqa: E402

app = FastAPI(title="context-service", version="0.1.0")
install_observability(app, "context-service")


class BuildRequest(BaseModel):
    user_id: str
    question: str
    window_turns: list[dict] = Field(default_factory=list)
    knowledge_chunks: list[str] = Field(default_factory=list)
    memory_chunks: list[str] = Field(default_factory=list)
    live_data: list[dict] = Field(default_factory=list)
    budget_tokens: int = 4000


class BuildResponse(BaseModel):
    prompt: str
    used_tokens: int
    trimmed: list[str]


@app.get("/healthz")
def healthz() -> dict:
    return {"ok": True}


@app.post("/context/build", response_model=BuildResponse)
def build(req: BuildRequest, request: Request, authorization: str = Header(default="")) -> BuildResponse:
    if not authorization.startswith("Bearer "):
        raise HTTPException(status_code=401, detail="缺少 JWT")
    set_actor_id(req.user_id)
    request.state.actor_id = req.user_id

    # 用户标签经 Go 获取（JWT 透传），失败时用默认标签不阻塞问答
    profile = {"interaction_style": None, "knowledge_reliance": None}
    try:
        me = get_go_client().get_user_me(authorization)
        profile = {
            "interaction_style": me.get("interactionStyle"),
            "knowledge_reliance": me.get("knowledgeReliance"),
        }
    except Exception:
        pass

    result = assemble(
        profile=profile,
        window_turns=req.window_turns,
        knowledge_chunks=req.knowledge_chunks,
        memory_chunks=req.memory_chunks,
        live_data=req.live_data,
        question=req.question,
        budget_tokens=req.budget_tokens,
    )
    return BuildResponse(**result)
