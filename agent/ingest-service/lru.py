"""ingest-service：LRU 逐出判定（消费 queue:session.activity）。

流程：消费交互事件 → ZADD ctx:active（score=最后交互时间）
  → ZCARD > 最大活跃用户数 → 判定最久未回 K 个用户
  → 逐出：DEL ctx:{userId} + ZREM ctx:active（直接销毁，不写回）
"""
from __future__ import annotations

import time

from shared.config import get_config
from shared.redis_client import get_redis


def handle_activity(events: list[dict]) -> dict:
    """处理一批交互事件，返回 {evicted: [user_id]}。"""
    cfg = get_config("ingest")
    max_users = int(cfg.get("max_active_users", 1000))
    evict_batch = int(cfg.get("evict_batch", 5))

    r = get_redis()
    for ev in events:
        uid = ev.get("user_id")
        if uid:
            r.touch_active(uid)

    evicted: list[str] = []
    if r.active_count() > max_users:
        candidates = r.lru_evict_candidates(evict_batch)
        for uid in candidates:
            r.evict_user(uid)
            evicted.append(uid)
    return {"evicted": evicted, "active": r.active_count()}
