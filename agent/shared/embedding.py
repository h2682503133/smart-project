"""Embedding 封装（双模式兼容，由 config 的 embedding.mode 切换）。

mode: "multimodal"（默认）→ 火山方舟 doubao-embedding-vision
    接口：POST /api/v3/embeddings/multimodal
    请求：{"model": "<ep-xxx>", "input": [{"type": "text", "text": ...}], "dimensions": 1024}
    响应：data.embedding（每次 1 个向量，逐条调用）
mode: "standard" → OpenAI 兼容标准文本 embedding
    接口：POST <base_url>/embeddings（批量字符串数组，如 doubao-embedding-text / bge-m3 服务）
    响应：data[] 每个元素一个向量

纯文本向量场景（标准接口）切 mode: "standard" 即可，无需改代码。
"""
from __future__ import annotations

from typing import Any

import httpx

from shared.config import get_config

_client: httpx.Client | None = None


def _get_client() -> httpx.Client:
    global _client
    if _client is None:
        _client = httpx.Client(timeout=60)
    return _client


def _embed_cfg() -> dict[str, Any]:
    cfg = get_config("embedding")
    return {
        "mode": cfg.get("mode", "multimodal"),
        "base_url": cfg.get("base_url", "https://ark.cn-beijing.volces.com/api/v3"),
        "api_key": cfg.get("api_key"),
        "model": cfg.get("model"),
        "dimensions": cfg.get("dimensions"),
    }


def _embed_multimodal(texts: list[str], cfg: dict[str, Any]) -> list[list[float]]:
    """火山多模态 embedding：逐条调用（每次 1 个向量）。"""
    url = cfg["base_url"].rstrip("/") + "/embeddings/multimodal"
    headers = {"Authorization": f"Bearer {cfg['api_key']}", "Content-Type": "application/json"}
    out: list[list[float]] = []
    client = _get_client()
    for text in texts:
        payload: dict[str, Any] = {"model": cfg["model"], "input": [{"type": "text", "text": text}]}
        if cfg.get("dimensions"):
            payload["dimensions"] = int(cfg["dimensions"])
        resp = client.post(url, headers=headers, json=payload)
        resp.raise_for_status()
        out.append(resp.json()["data"]["embedding"])
    return out


def _embed_standard(texts: list[str], cfg: dict[str, Any]) -> list[list[float]]:
    """OpenAI 兼容标准文本 embedding：批量字符串数组。"""
    from openai import OpenAI

    client = OpenAI(base_url=cfg["base_url"], api_key=cfg["api_key"])
    kwargs: dict[str, Any] = {"model": cfg["model"], "input": texts}
    if cfg.get("dimensions"):
        kwargs["dimensions"] = int(cfg["dimensions"])
    resp = client.embeddings.create(**kwargs)
    # 响应按 input 顺序返回（某些服务可能乱序，按 index 排序保证对应）
    ordered = sorted(resp.data, key=lambda d: d.index)
    return [d.embedding for d in ordered]


def embed(texts: list[str]) -> list[list[float]]:
    """批量文本向量化（按 mode 分派）。"""
    cfg = _embed_cfg()
    if cfg["mode"] == "standard":
        return _embed_standard(texts, cfg)
    return _embed_multimodal(texts, cfg)
