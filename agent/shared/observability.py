"""Shared JSON logging and one-record-per-request HTTP observability."""
from __future__ import annotations

import asyncio
import json
import logging
import sys
import time
from datetime import datetime, timezone
from typing import Any

from shared.trace import (
    REQUEST_HEADER,
    TRACE_HEADER,
    bind_correlation_ids,
    get_actor_id,
    normalize_correlation_id,
    reset_correlation_ids,
)


class _JSONFormatter(logging.Formatter):
    _standard = set(logging.makeLogRecord({}).__dict__) | {"message", "asctime"}

    def format(self, record: logging.LogRecord) -> str:
        payload: dict[str, Any] = {
            "timestamp": datetime.fromtimestamp(record.created, timezone.utc).isoformat(),
            "level": record.levelname.lower(),
            "message": record.getMessage(),
        }
        for key, value in record.__dict__.items():
            if key not in self._standard and not key.startswith("_"):
                payload[key] = value
        return json.dumps(payload, ensure_ascii=False, default=str, separators=(",", ":"))


def configure_logging() -> None:
    """Use JSON on stdout and disable Uvicorn's duplicate access record."""
    handler = logging.StreamHandler(sys.stdout)
    handler.setFormatter(_JSONFormatter())
    handler._smart_observability = True  # type: ignore[attr-defined]
    logging.basicConfig(level=logging.INFO, handlers=[handler], force=True)
    logging.getLogger("uvicorn.access").disabled = True


def install_observability(app: Any, service: str) -> None:
    configure_logging()
    app.add_middleware(ObservabilityMiddleware, service=service)


class ObservabilityMiddleware:
    def __init__(self, app: Any, service: str) -> None:
        self.app = app
        self.service = service
        self.log = logging.getLogger("observability.http")

    async def __call__(self, scope: dict, receive: Any, send: Any) -> None:
        if scope.get("type") != "http":
            await self.app(scope, receive, send)
            return

        request_headers = {
            key.decode("latin-1").lower(): value.decode("latin-1")
            for key, value in scope.get("headers", [])
        }
        request_id = normalize_correlation_id(request_headers.get(REQUEST_HEADER.lower()))
        trace_id = normalize_correlation_id(request_headers.get(TRACE_HEADER.lower()), request_id)
        tokens = bind_correlation_ids(trace_id, request_id)
        started_at = time.perf_counter()
        status = 500
        response_bytes = 0
        response_started = False
        caught: BaseException | None = None

        async def send_with_correlation(message: dict) -> None:
            nonlocal status, response_bytes, response_started
            if message["type"] == "http.response.start":
                status = int(message["status"])
                response_started = True
                headers = [
                    (key, value)
                    for key, value in message.get("headers", [])
                    if key.lower() not in {REQUEST_HEADER.lower().encode(), TRACE_HEADER.lower().encode()}
                ]
                headers.extend([
                    (REQUEST_HEADER.encode(), request_id.encode()),
                    (TRACE_HEADER.encode(), trace_id.encode()),
                ])
                message["headers"] = headers
            elif message["type"] == "http.response.body":
                response_bytes += len(message.get("body", b""))
            await send(message)

        try:
            await self.app(scope, receive, send_with_correlation)
        except asyncio.CancelledError as exc:
            caught = exc
            raise
        except Exception as exc:
            caught = exc
            if response_started:
                raise
            body = b'{"detail":"Internal Server Error"}'
            await send_with_correlation({
                "type": "http.response.start",
                "status": 500,
                "headers": [
                    (b"content-type", b"application/json"),
                    (b"content-length", str(len(body)).encode()),
                ],
            })
            await send_with_correlation({"type": "http.response.body", "body": body})
        finally:
            route = getattr(scope.get("route"), "path", None) or "unmatched"
            content_length = request_headers.get("content-length", "0")
            try:
                request_bytes = max(0, int(content_length))
            except ValueError:
                request_bytes = 0
            result = "success" if status < 400 and caught is None else "failure"
            source = scope.get("client") or ("", 0)
            record: dict[str, Any] = {
                "event": "http_request_completed",
                "service": self.service,
                "request_id": request_id,
                "trace_id": trace_id,
                "method": scope.get("method", ""),
                "route": route,
                "path": scope.get("path", ""),
                "status": status,
                "result": result,
                "duration_ms": round((time.perf_counter() - started_at) * 1000, 3),
                "request_bytes": request_bytes,
                "response_bytes": response_bytes,
                "client_ip": source[0],
            }
            actor_id = (scope.get("state") or {}).get("actor_id") or get_actor_id()
            if actor_id:
                record["actor_id"] = actor_id
            if caught is not None:
                record["exception_type"] = type(caught).__name__
            level = (
                logging.ERROR
                if status >= 500 or caught is not None and not isinstance(caught, asyncio.CancelledError)
                else logging.WARNING
                if status >= 400 or caught is not None
                else logging.INFO
            )
            self.log.log(level, "HTTP request completed", extra=record)
            reset_correlation_ids(tokens)
