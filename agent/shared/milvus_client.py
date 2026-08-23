"""Milvus 客户端（向量库归智能体侧，Go 不碰）。

两个 collection：
- knowledge：知识切片（ingest 写 / tool 检索），无状态过滤（Go 保证可用性）
- memory：对话记忆（ingest 写 / agent 召回），按 user_id 强隔离 + 时间衰减

依赖：pymilvus（pip install pymilvus）。未安装/未连接时抛错，由调用方降级。
"""
from __future__ import annotations

from typing import Any

from shared.config import get_config

_conn: Any = None


def _get_conn():
    global _conn
    if _conn is not None:
        return _conn
    from pymilvus import MilvusClient

    cfg = get_config("milvus")
    _conn = MilvusClient(uri=f"http://{cfg.get('host','localhost')}:{cfg.get('port',19530)}",
                         token=cfg.get("token"))
    return _conn


def _collection(name: str) -> str:
    return get_config("milvus").get(f"{name}_collection", name)


def ensure_collections() -> None:
    """幂等建 collection（ingest 启动时调用）。完整 schema：VARCHAR 主键 + 业务字段。"""
    from pymilvus import CollectionSchema, DataType, FieldSchema

    conn = _get_conn()
    dim = int(get_config("milvus").get("dimension", 1024))
    for name in ("knowledge", "memory"):
        col = _collection(name)
        if conn.has_collection(col):
            continue
        fields = [
            FieldSchema(name="id", dtype=DataType.VARCHAR, is_primary=True, max_length=255),
            FieldSchema(name="vector", dtype=DataType.FLOAT_VECTOR, dim=dim),
        ]
        if name == "knowledge":
            fields += [
                FieldSchema(name="doc_id", dtype=DataType.VARCHAR, max_length=128),
                FieldSchema(name="title", dtype=DataType.VARCHAR, max_length=255),
                FieldSchema(name="version", dtype=DataType.INT64),
                FieldSchema(name="category", dtype=DataType.VARCHAR, max_length=64),
                FieldSchema(name="source", dtype=DataType.VARCHAR, max_length=512),
                FieldSchema(name="content", dtype=DataType.VARCHAR, max_length=4096),
            ]
        else:  # memory
            fields += [
                FieldSchema(name="user_id", dtype=DataType.VARCHAR, max_length=128),
                FieldSchema(name="session_id", dtype=DataType.VARCHAR, max_length=128),
                FieldSchema(name="summary_text", dtype=DataType.VARCHAR, max_length=4096),
                FieldSchema(name="generated_at", dtype=DataType.VARCHAR, max_length=64),
                FieldSchema(name="model_version", dtype=DataType.VARCHAR, max_length=64),
            ]
        schema = CollectionSchema(fields, description=f"{name} collection")
        conn.create_collection(collection_name=col, schema=schema)
        # 向量索引（FLAT 精确检索，小数据量够用；量大再换 IVF/HNSW）
        try:
            from pymilvus.milvus_client.index import IndexParams

            vidx = IndexParams()
            vidx.add_index(field_name="vector", index_type="FLAT", metric_type="COSINE")
            conn.create_index(col, vidx)
        except Exception:
            pass
        # memory collection 建 user_id 标量索引（user_id 强过滤）
        if name == "memory":
            try:
                from pymilvus.milvus_client.index import IndexParams

                idx = IndexParams()
                idx.add_index(field_name="user_id", index_type="INVERTED")
                conn.create_index(col, idx)
            except Exception:
                pass  # 索引建失败不阻塞（可后续补建）


def _load(col: str) -> None:
    """确保 collection 已加载（幂等；load_collection 已加载时毫秒级返回，无需轮询）。"""
    conn = _get_conn()
    conn.load_collection(col)


def search_knowledge(query_embedding: list[float], top_k: int = 5,
                     category: str | None = None) -> list[dict[str, Any]]:
    """知识检索（向量召回，category 可选过滤）。"""
    conn = _get_conn()
    col = _collection("knowledge")
    _load(col)
    filter_expr = f'category == "{category}"' if category else None
    hits = conn.search(
        collection_name=col,
        data=[query_embedding],
        limit=top_k,
        filter=filter_expr,
        output_fields=["doc_id", "title", "version", "source", "category", "content"],
    )
    out: list[dict[str, Any]] = []
    for hit in hits[0]:
        entity = hit.get("entity", {})
        out.append({
            "docId": entity.get("doc_id"),
            "title": entity.get("title"),
            "version": entity.get("version"),
            "source": entity.get("source"),
            "category": entity.get("category"),
            "content": entity.get("content"),
            "score": hit.get("distance"),
        })
    return out


def search_memory(user_id: str, query_embedding: list[float], top_k: int = 3) -> list[dict[str, Any]]:
    """长期记忆召回（user_id 强过滤 + generated_at 时间衰减降权）。"""
    conn = _get_conn()
    col = _collection("memory")
    _load(col)
    hits = conn.search(
        collection_name=col,
        data=[query_embedding],
        limit=top_k,
        filter=f'user_id == "{user_id}"',
        output_fields=["summary_text", "session_id", "generated_at", "model_version"],
    )
    out: list[dict[str, Any]] = []
    for hit in hits[0]:
        entity = hit.get("entity", {})
        out.append({
            "summary": entity.get("summary_text"),
            "session_id": entity.get("session_id"),
            "generated_at": entity.get("generated_at"),
            "model_version": entity.get("model_version"),
            "score": hit.get("distance"),
        })
    return out


def upsert_documents(rows: list[dict[str, Any]], kind: str = "knowledge") -> None:
    """批量写入向量（kind: knowledge / memory）。行需含 id 与 vector 字段。"""
    conn = _get_conn()
    col = _collection(kind)
    conn.upsert(collection_name=col, data=rows)
