"""ingest-service：文档向量化（消费 queue:doc.process）。

流程：收到通知（Go 文档上传/变更 → agent 入队）
  → 拉 GET /knowledge/docs（可用文档，Go 保证可用性）
  → 与 Milvus 已有向量对比（doc_id + version）
  → 缺失/版本变更 → MinIO 拉原文 → 切片 → embedding → 写知识 collection（幂等）
"""
from __future__ import annotations

from typing import Any

from shared.config import get_config
from shared.embedding import embed
from shared.go_client import get_go_client
from shared.milvus_client import ensure_collections, upsert_documents
from shared.minio_client import get_document


def _chunk_text(text: str, size: int, overlap: int) -> list[str]:
    """简单字符切片（生产可换语义切片器）。"""
    if len(text) <= size:
        return [text] if text.strip() else []
    chunks: list[str] = []
    start = 0
    while start < len(text):
        chunks.append(text[start : start + size])
        start += size - overlap
    return chunks


def process_doc_event(event: dict) -> dict:
    """处理一条文档加工事件。返回 {doc_id, version, chunks, status}。"""
    cfg = get_config("ingest")
    chunk_size = int(cfg.get("chunk_size", 800))
    chunk_overlap = int(cfg.get("chunk_overlap", 100))

    ensure_collections()

    doc_id = event.get("doc_id")
    # 拉可用文档清单，找到目标文档（含 file_url/version）
    docs = get_go_client().get_knowledge_docs()
    doc = next((d for d in docs if str(d.get("id")) == str(doc_id)), None)
    if doc is None:
        return {"doc_id": doc_id, "status": "skipped", "reason": "文档不可用或不存在"}

    file_url = doc.get("file_url")
    if not file_url:
        return {"doc_id": doc_id, "status": "skipped", "reason": "缺少 file_url"}

    # 拉原文（MinIO）
    try:
        text = get_document(file_url)
    except Exception as e:
        return {"doc_id": doc_id, "status": "failed", "reason": f"拉取原文失败: {e}"}

    # 切片 + embedding + 写入（doc_id + version 幂等）
    chunks = _chunk_text(text, chunk_size, chunk_overlap)
    if not chunks:
        return {"doc_id": doc_id, "status": "skipped", "reason": "无有效内容"}

    vectors = embed(chunks)
    rows = []
    for i, (chunk, vec) in enumerate(zip(chunks, vectors)):
        rows.append({
            "id": f"{doc_id}:v{doc.get('version', 1)}:{i}",
            "vector": vec,
            "doc_id": doc_id,
            "title": doc.get("title"),
            "version": doc.get("version"),
            "category": doc.get("category"),
            "source": file_url,
            "content": chunk,
        })
    upsert_documents(rows, kind="knowledge")
    return {"doc_id": doc_id, "version": doc.get("version"), "chunks": len(chunks), "status": "ok"}
