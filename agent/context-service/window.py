"""context-service：短期窗口读取（Redis ctx:{userId}）。"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from shared.redis_client import get_redis  # noqa: E402


def read_window(user_id: str, max_turns: int = 8) -> list[dict]:
    """读取用户短期窗口，裁剪到最近 max_turns 轮。"""
    turns = get_redis().window_get(user_id)
    if len(turns) > max_turns:
        turns = turns[-max_turns:]
    return turns
