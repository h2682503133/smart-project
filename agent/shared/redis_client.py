"""Redis 客户端与 Stream 封装。

职责：
- 连接（共用实例，key 前缀隔离）；
- Redis Stream 生产/消费（XADD / XREADGROUP / XACK），支持消费组与重试；
- 短期窗口 ctx:{userId} 读写。
"""
from __future__ import annotations

import json
import time
from typing import Any

import redis

from shared.config import get_config
from shared.trace import ensure_trace_id


class RedisClient:
    def __init__(self) -> None:
        cfg = get_config("redis")
        self._pool = redis.ConnectionPool(
            host=cfg.get("host", "localhost"),
            port=int(cfg.get("port", 6379)),
            db=int(cfg.get("db", 0)),
            password=cfg.get("password"),  # 来自 ${REDIS_PASSWORD}，无则 None
            decode_responses=True,
        )
        self._r = redis.Redis(connection_pool=self._pool)
        self.stream_prefix = cfg.get("stream_prefix", "queue:")
        self.group = cfg.get("consumer_group", "ingest")

    # ---------- 基础 ----------
    def client(self) -> redis.Redis:
        return self._r

    # ---------- Stream 生产 ----------
    def xadd(self, stream: str, fields: dict[str, Any], maxlen: int = 10000) -> str:
        payload = {"payload": json.dumps(fields, ensure_ascii=False), "trace_id": ensure_trace_id()}
        return self._r.xadd(self.stream_prefix + stream, payload, maxlen=maxlen)

    # ---------- Stream 消费（消费组） ----------
    def ensure_group(self, stream: str) -> None:
        full = self.stream_prefix + stream
        try:
            self._r.xgroup_create(full, self.group, id="0", mkstream=True)
        except redis.exceptions.ResponseError as e:
            if "BUSYGROUP" not in str(e):
                raise

    def xread_group(
        self, stream: str, consumer: str, count: int = 10, block_ms: int = 5000
    ) -> list[dict[str, Any]]:
        """读取一批消息（**不自动 ACK**，由调用方成功后 xack，失败可重投）。"""
        full = self.stream_prefix + stream
        self.ensure_group(stream)
        try:
            raw = self._r.xreadgroup(
                self.group, consumer, {full: ">"}, count=count, block=block_ms
            )
        except redis.exceptions.ResponseError:
            # 流不存在/组未建（并发创建竞态）→ 重建后重试一次
            self.ensure_group(stream)
            raw = self._r.xreadgroup(
                self.group, consumer, {full: ">"}, count=count, block=block_ms
            )
        out: list[dict[str, Any]] = []
        for _stream, entries in raw or []:
            for msg_id, fields in entries:
                out.append(
                    {
                        "id": msg_id,
                        "stream": _stream,
                        **json.loads(fields.get("payload", "{}")),
                        "trace_id": fields.get("trace_id"),
                    }
                )
        return out

    def xack(self, stream: str, msg_id: str) -> None:
        """处理成功后确认（删除消费组消息）。"""
        full = self.stream_prefix + stream
        self._r.xack(full, self.group, msg_id)

    def retry_later(self, stream: str, fields: dict[str, Any], retry_key: str | None = None,
                    max_retries: int = 3, delay_ms: int = 1000) -> bool:
        """失败消息重投：带重试计数，超限丢弃。返回是否已重投。"""
        import time

        retries = int(fields.pop(retry_key, 0)) if retry_key else 0
        if retries >= max_retries:
            return False  # 超限丢弃
        payload = dict(fields)
        if retry_key:
            payload[retry_key] = retries + 1
        self.xadd(stream, payload, maxlen=10000)
        if delay_ms:
            time.sleep(delay_ms / 1000)
        return True

    # ---------- 短期窗口 ctx:{userId} ----------
    def window_get(self, user_id: str) -> list[dict[str, Any]]:
        raw = self._r.get(f"ctx:{user_id}")
        if not raw:
            return []
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return []

    def window_set(self, user_id: str, turns: list[dict[str, Any]], ttl: int = 86400) -> None:
        self._r.set(f"ctx:{user_id}", json.dumps(turns, ensure_ascii=False), ex=ttl)

    # ---------- LRU 活跃索引 ----------
    def touch_active(self, user_id: str) -> None:
        """更新 ctx:active ZSET（score=最后交互时间）。"""
        self._r.zadd("ctx:active", {user_id: time.time()})

    def active_count(self) -> int:
        return self._r.zcard("ctx:active")

    def lru_evict_candidates(self, k: int) -> list[str]:
        """取最久未回 k 个用户（score 升序）。"""
        return self._r.zrange("ctx:active", 0, k - 1)

    def evict_user(self, user_id: str) -> None:
        self._r.delete(f"ctx:{user_id}")
        self._r.zrem("ctx:active", user_id)

    # ---------- 会话状态 ----------
    def session_get(self, session_id: str) -> dict[str, Any] | None:
        raw = self._r.get(f"agent:session:{session_id}")
        if not raw:
            return None
        try:
            return json.loads(raw)
        except json.JSONDecodeError:
            return None

    def session_set(self, session_id: str, data: dict[str, Any], ttl: int | None = None) -> None:
        self._r.set(
            f"agent:session:{session_id}",
            json.dumps(data, ensure_ascii=False),
            ex=ttl,
        )


_inst: RedisClient | None = None


def get_redis() -> RedisClient:
    global _inst
    if _inst is None:
        _inst = RedisClient()
    return _inst
