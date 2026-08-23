"""Go 主后端 HTTP 客户端（JWT 透传 + trace_id）。

所有智能体侧对 Go 的调用都经此客户端：
- 工具取数接口（GET /plots、/telemetry/...、/alerts、/thresholds、/devices 等，带 JWT）
- 用户标签 GET /users/me（带 JWT）
- 消息落库 POST /agent/sessions/{id}/messages（带 JWT）
- 文档清单 GET /knowledge/docs（带 JWT）
- 摘要落库 POST /agent/sessions/{id}/summaries（内部密钥）
"""
from __future__ import annotations

from typing import Any

import httpx

from shared.config import get_config
from shared.trace import HEADER, REQUEST_HEADER, ensure_request_id, ensure_trace_id


class GoClient:
    def __init__(self) -> None:
        cfg = get_config("go")
        self.base_url = cfg.get("base_url", "http://go-backend:8080/api/v1")
        self.timeout = float(cfg.get("timeout_seconds", 10))
        self.internal_key = cfg.get("internal_key")  # ${GO_INTERNAL_KEY}
        # connect 短超时：Go 不可达（SYN 被丢弃/拒绝）时快速降级，避免拖慢问答
        self._timeout = httpx.Timeout(
            connect=1.0, read=self.timeout, write=self.timeout, pool=self.timeout
        )

    def _headers(self, authorization: str = "", internal: bool = False) -> dict[str, str]:
        h = {
            HEADER: ensure_trace_id(),
            REQUEST_HEADER: ensure_request_id(),
            "Content-Type": "application/json",
        }
        if internal:
            if not self.internal_key:
                raise RuntimeError("未配置 GO_INTERNAL_KEY")
            h["X-Internal-Key"] = self.internal_key
        elif authorization:
            h["Authorization"] = authorization
        return h

    def _request(
        self, method: str, path: str, authorization: str = "", internal: bool = False, **kwargs: Any
    ) -> dict[str, Any] | list[Any]:
        url = f"{self.base_url}{path}"
        with httpx.Client(timeout=self._timeout, trust_env=False) as client:
            resp = client.request(method, url, headers=self._headers(authorization, internal), **kwargs)
            resp.raise_for_status()
            body = resp.json()
        # Go 统一响应 {code, message, data}
        if isinstance(body, dict) and "data" in body:
            return body["data"]
        return body

    # ---------- 用户 ----------
    def get_user_me(self, authorization: str) -> dict[str, Any]:
        data = self._request("GET", "/users/me", authorization=authorization)
        return data if isinstance(data, dict) else {}

    # ---------- 工具取数（JWT） ----------
    def get_plots(self, authorization: str, **query: Any) -> list[Any]:
        return self._request("GET", "/plots", authorization=authorization, params=query)

    def get_plot_telemetry_latest(self, authorization: str, plot_id: str, metrics: str = "") -> dict[str, Any]:
        params = {"metrics": metrics} if metrics else {}
        data = self._request(
            "GET", f"/plots/{plot_id}/telemetry/latest", authorization=authorization, params=params
        )
        return data if isinstance(data, dict) else {}

    def get_telemetry_history(self, authorization: str, **query: Any) -> dict[str, Any]:
        data = self._request("GET", "/telemetry/history", authorization=authorization, params=query)
        return data if isinstance(data, dict) else {}

    def get_alerts(self, authorization: str, **query: Any) -> list[Any]:
        return self._request("GET", "/alerts", authorization=authorization, params=query)

    def get_thresholds(self, authorization: str, plot_id: str) -> list[Any]:
        return self._request("GET", f"/plots/{plot_id}/thresholds", authorization=authorization)

    def get_devices(self, authorization: str, **query: Any) -> list[Any]:
        return self._request("GET", "/devices", authorization=authorization, params=query)

    def get_dashboard_overview(self, authorization: str, **query: Any) -> dict[str, Any]:
        data = self._request("GET", "/dashboard/overview", authorization=authorization, params=query)
        return data if isinstance(data, dict) else {}

    # ---------- 会话（JWT） ----------
    def create_session(self, authorization: str, plot_id: str | None = None) -> dict[str, Any]:
        """在 Go 侧创建会话（返回 Go 的 session_id，落库需用它）。

        plot_id 不随创建传入：Go 按 JWT 归属校验 plot 所有权，
        传错会 404；会话创建后落库消息时再带 plot_id。
        """
        data = self._request(
            "POST", "/ai/sessions", authorization=authorization, json={}
        )
        return data if isinstance(data, dict) else {}

    def post_message(self, authorization: str, session_id: str, body: dict[str, Any]) -> dict[str, Any]:
        data = self._request(
            "POST", f"/agent/sessions/{session_id}/messages", authorization=authorization, json=body
        )
        return data if isinstance(data, dict) else {}

    def close_session(self, authorization: str, session_id: str) -> dict[str, Any]:
        """关闭 Go 侧会话（幂等；本地会话不存在时 Go 返回 404，调用方容忍）。"""
        data = self._request(
            "POST", f"/ai/sessions/{session_id}/close", authorization=authorization
        )
        return data if isinstance(data, dict) else {}

    # ---------- 控制（JWT，复用 Go 已有接口） ----------
    def get_irrigation_status(self, authorization: str, plot_id: str) -> dict[str, Any]:
        data = self._request(
            "GET", f"/plots/{plot_id}/irrigation/status", authorization=authorization
        )
        return data if isinstance(data, dict) else {}

    def post_irrigation_command(self, authorization: str, plot_id: str,
                                body: dict[str, Any]) -> dict[str, Any]:
        data = self._request(
            "POST", f"/plots/{plot_id}/irrigation/commands", authorization=authorization, json=body
        )
        return data if isinstance(data, dict) else {}

    def get_command_result(self, authorization: str, command_id: str) -> dict[str, Any]:
        data = self._request(
            "GET", f"/commands/{command_id}", authorization=authorization
        )
        return data if isinstance(data, dict) else {}

    # ---------- 知识库（JWT） ----------
    def get_knowledge_docs(self, authorization: str = "", **query: Any) -> list[Any]:
        return self._request("GET", "/knowledge/docs", authorization=authorization, params=query)

    # ---------- 摘要落库（内部密钥） ----------
    def post_summary(self, session_id: str, body: dict[str, Any]) -> dict[str, Any]:
        data = self._request(
            "POST", f"/agent/sessions/{session_id}/summaries", internal=True, json=body
        )
        return data if isinstance(data, dict) else {}


_inst: GoClient | None = None


def get_go_client() -> GoClient:
    global _inst
    if _inst is None:
        _inst = GoClient()
    return _inst
