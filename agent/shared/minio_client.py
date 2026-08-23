"""MinIO 客户端（知识文档原文读取；向量化写入由 ingest 负责拉取）。"""
from __future__ import annotations

from io import BytesIO
from typing import Any

from shared.config import get_config

_client: Any = None


def _get_client():
    global _client
    if _client is not None:
        return _client
    from minio import Minio

    cfg = get_config("minio")
    _client = Minio(
        cfg.get("endpoint", "localhost:9000"),
        access_key=cfg.get("access_key", "minioadmin"),
        secret_key=cfg.get("secret_key"),
        secure=bool(cfg.get("secure", False)),
    )
    return _client


def get_document(object_key: str) -> str:
    """按对象 key 读取文档原文（UTF-8 文本；二进制文档由 ingest 负责解析）。"""
    client = _get_client()
    bucket = get_config("minio").get("bucket", "knowledge")
    resp = client.get_object(bucket, object_key)
    try:
        return resp.read().decode("utf-8", errors="replace")
    finally:
        resp.close()
        resp.release_conn()


def put_bytes(object_key: str, data: bytes, content_type: str = "application/octet-stream") -> None:
    client = _get_client()
    bucket = get_config("minio").get("bucket", "knowledge")
    client.put_object(bucket, object_key, BytesIO(data), length=len(data), content_type=content_type)
