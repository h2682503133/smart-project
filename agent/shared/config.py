"""agent 服务共享配置：yaml + 环境变量加载。

规则：
- 非敏感配置放 config.yaml（模型名/top_k/阈值/端口/地址）；
- 敏感配置（API Key/密码/token）在 yaml 里写 `${VAR_NAME}` 占位，
  启动时从环境变量解析，缺则报错退出；
- 密钥不进入代码仓库。
"""
from __future__ import annotations

import os
import re
from functools import lru_cache
from pathlib import Path
from typing import Any

import yaml

_ENV_PATTERN = re.compile(r"\$\{([A-Z0-9_]+)\}")


class ConfigError(RuntimeError):
    pass


def _resolve(value: Any, base_dir: Path) -> Any:
    """递归解析配置中的 ${VAR} 占位符。"""
    if isinstance(value, dict):
        return {k: _resolve(v, base_dir) for k, v in value.items()}
    if isinstance(value, list):
        return [_resolve(v, base_dir) for v in value]
    if isinstance(value, str):
        def repl(m: re.Match) -> str:
            name = m.group(1)
            val = os.environ.get(name)
            if val is None:
                raise ConfigError(
                    f"缺少环境变量 {name}（在 {base_dir.name}/config.yaml 中声明），"
                    "请在 .env 或系统环境变量中配置"
                )
            return val

        return _ENV_PATTERN.sub(repl, value)
    return value


@lru_cache(maxsize=1)
def load_config() -> dict[str, Any]:
    """加载 config.yaml 并解析环境变量占位符（进程内缓存一次）。"""
    base_dir = Path(__file__).resolve().parent.parent  # agent/
    cfg_path = base_dir / "config.yaml"
    if not cfg_path.exists():
        raise ConfigError(f"找不到配置文件: {cfg_path}")
    with open(cfg_path, "r", encoding="utf-8") as f:
        raw = yaml.safe_load(f) or {}
    return _resolve(raw, base_dir)


def get_config(service: str | None = None) -> dict[str, Any]:
    """取配置；service 指定时返回该服务段。"""
    cfg = load_config()
    if service:
        return cfg.get("services", {}).get(service, {}) or {}
    return cfg
