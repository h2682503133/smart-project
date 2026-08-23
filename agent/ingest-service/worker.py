"""ingest-service：worker 主循环（无对外 HTTP，消费 3 个 Redis Stream）。

- queue:doc.process      文档向量化
- queue:session.summary  会话摘要
- queue:session.activity LRU 逐出判定
"""
from __future__ import annotations

import logging
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
sys.path.insert(0, str(ROOT))
sys.path.insert(0, str(ROOT / "ingest-service"))
sys.path.insert(0, str(ROOT / "agent-service"))  # 复用 llm 适配

from shared.config import get_config  # noqa: E402
from shared.observability import configure_logging  # noqa: E402
from shared.redis_client import get_redis  # noqa: E402
from shared.trace import bind_correlation_ids, normalize_correlation_id, reset_correlation_ids  # noqa: E402

from doc_processor import process_doc_event  # noqa: E402
from lru import handle_activity  # noqa: E402
from summarizer import build_summary  # noqa: E402

configure_logging()
log = logging.getLogger("ingest")

CONSUMER = "ingest-worker-1"

HANDLERS = {
    "doc.process": lambda ev: process_doc_event(ev),
    "session.summary": lambda ev: build_summary(ev.get("session_id", ""), ev.get("user_id", "")),
    "session.activity": lambda ev: handle_activity([ev]),
}


def run_once() -> None:
    r = get_redis()
    for stream in HANDLERS:
        try:
            events = r.xread_group(stream, CONSUMER, count=10, block_ms=100)
        except Exception as e:
            log.warning("读取 %s 失败: %s", stream, e)
            continue
        for ev in events:
            event_id = str(ev.get("id", ""))
            request_id = normalize_correlation_id(event_id)
            trace_id = normalize_correlation_id(ev.get("trace_id"), request_id)
            tokens = bind_correlation_ids(trace_id, request_id)
            started_at = time.perf_counter()
            try:
                result = HANDLERS[stream](ev)
                r.xack(stream, ev["id"])  # 处理成功才 ACK
                log.info(
                    "Stream event processed",
                    extra={
                        "event": "stream_event_processed",
                        "service": "ingest-service",
                        "stream": stream,
                        "event_id": event_id,
                        "trace_id": trace_id,
                        "request_id": request_id,
                        "result": "success",
                        "duration_ms": round((time.perf_counter() - started_at) * 1000, 3),
                    },
                )
            except Exception as e:
                # 失败重投（带重试计数，超限丢弃），失败消息不再丢失
                recovery_exception = None
                try:
                    requeued = r.retry_later(stream, ev, retry_key="retry", max_retries=3)
                    if requeued:
                        r.xack(stream, ev["id"])  # 原消息已重投，可 ACK
                        outcome = "requeued"
                    else:
                        r.xack(stream, ev["id"])
                        outcome = "discarded"
                except Exception as recovery_error:
                    recovery_exception = type(recovery_error).__name__
                    outcome = "recovery_failed"
                failure_record = {
                    "event": "stream_event_processed",
                    "service": "ingest-service",
                    "stream": stream,
                    "event_id": event_id,
                    "trace_id": trace_id,
                    "request_id": request_id,
                    "result": "failure",
                    "outcome": outcome,
                    "exception_type": type(e).__name__,
                    "duration_ms": round((time.perf_counter() - started_at) * 1000, 3),
                }
                if recovery_exception:
                    failure_record["recovery_exception_type"] = recovery_exception
                log.error(
                    "Stream event processing failed",
                    extra=failure_record,
                )
            finally:
                reset_correlation_ids(tokens)


def main() -> None:
    log.info("ingest-service 启动，消费队列: %s", list(HANDLERS))
    while True:
        run_once()
        time.sleep(0.5)


if __name__ == "__main__":
    main()
