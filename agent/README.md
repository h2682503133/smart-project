# 智慧农业 · 智能体（agent）侧实现

> 智能体相关部分的 Python 实现：RAG 检索、上下文组装、工具调用、大模型编排、SSE 流式问答、异步加工。
> 技术栈：**Python 3.11 + FastAPI**（Go 主后端之外的独立服务，见 `docs/08_智能体接口契约.md`、`docs/09_给Go侧的接口与SQL需求.md`）。

---

## 一、架构

```
┌─ 前端（Vue/H5，未完成）───────────────────────────────┐
│                                                       │
│  ┌─ agent-service (8000) ──────────────────────────┐  │
│  │  POST /agent/chat（SSE 流式）                   │  │
│  │  POST /agent/chat/sessions/{id}/close           │  │
│  │  POST /internal/knowledge/notify（Go 文档通知）  │  │
│  │  编排：三路取数 → 组装 → LLM 流式 → 落库/窗口    │  │
│  └──────────┬──────────────────┬───────────────────┘  │
│             │ HTTP             │ HTTP                 │
│  ┌──────────▼─────────┐  ┌─────▼──────────┐          │
│  │ context-service    │  │ tool-service   │          │
│  │ (8001) 组装/裁剪    │  │ (8002) 9 工具   │          │
│  │ prompts 硬编码+case │  │ JSON Schema 校验│          │
│  └──────────┬─────────┘  │ Go 取数+mock    │          │
│             │            └─────┬──────────┘          │
│  ┌──────────▼──────────────────▼──────────────────┐   │
│  │ ingest-service (8003，无对外 HTTP)             │   │
│  │ queue:doc.process 文档向量化                   │   │
│  │ queue:session.summary 会话摘要→记忆            │   │
│  │ queue:session.activity LRU 逐出判定            │   │
│  └───────────────────────────────────────────────┘   │
│                                                       │
│  外部依赖：Redis（窗口/队列/会话，agent 侧独立实例 agent-redis）·  │
│  Milvus（知识/记忆向量）· MinIO（知识原文）· DeepSeek（LLM）·    │
│  火山方舟（embedding）                                          │
└───────────────────────────────────────────────────────┘
```

服务间经 **compose 内网 DNS + 端口**通信；JWT 透传（agent → tool/context → Go）；Go → agent 通知用内部密钥；`trace_id` 贯穿。

## 二、目录结构

```
agent/
├── config.yaml            # 非敏感配置；敏感项 ${VAR} 占位（环境变量注入）
├── .env.example           # 密钥模板（不入库）
├── requirements.txt       # 依赖
├── Dockerfile / docker-compose.yml
├── shared/                # 4 服务共享
│   ├── config.py          # yaml + 环境变量解析
│   ├── trace.py           # trace_id
│   ├── redis_client.py    # Redis + Stream（消费组/重投）+ ctx 窗口 + LRU
│   ├── go_client.py       # Go 接口客户端（JWT 透传 / 内部密钥 / 短连接超时）
│   ├── jwt.py             # JWT 校验（与 Go 共享 secret）
│   ├── schemas.py         # pydantic 模型
│   ├── embedding.py       # 向量化（multimodal 火山 / standard OpenAI 兼容，config 切换）
│   ├── milvus_client.py   # Milvus（知识/记忆两 collection，完整 schema）
│   └── minio_client.py    # MinIO 知识原文
├── agent-service/         # 8000：问答编排（SSE）、会话状态机、工具循环、RAG/记忆
├── context-service/       # 8001：System 段组装（prompts 硬编码 + case）、预算裁剪
├── tool-service/          # 8002：9 工具注册表（JSON Schema）+ 执行
├── ingest-service/        # 8003：三队列消费者（文档向量化/摘要/LRU）
├── scripts/               # 测试脚本（冒烟/端到端/耗时）
└── tests/                 # 单元测试（prompts/assemble）
```

## 三、快速开始

### 1. 依赖与配置

```powershell
cd agent
python -m pip install --only-binary=:all: -r requirements.txt
copy .env.example .env    # 填写密钥（或直接设环境变量）
```

`config.yaml` 敏感占位（必须提供，否则启动报错）：

| 环境变量 | 用途 |
|---|---|
| `LLM_API_KEY` | DeepSeek 对话模型 |
| `JWT_SECRET` | 与 Go 侧一致的 JWT 密钥 |
| `GO_INTERNAL_KEY` | Go→agent 通知的内部密钥 |
| `MINIO_SECRET_KEY` | MinIO 密钥 |
| `REDIS_PASSWORD` / `MILVUS_TOKEN` | 生产环境（本地可空） |

LLM/embedding 配置在 `config.yaml`：

```yaml
llm:
  base_url: "https://api.deepseek.com/v1"
  model: "deepseek-v4-flash"
embedding:
  mode: "multimodal"    # 火山 doubao-embedding-vision（/embeddings/multimodal，dimensions=1024）
  base_url: "https://ark.cn-beijing.volces.com/api/v3"
```

### 2. 依赖服务（Docker）

```powershell
docker compose -p smart_agriculture up -d redis minio milvus-etcd milvus-minio milvus
```

### 3. 启动 4 个服务

```powershell
# 各开一个终端，设好环境变量后：
python -m uvicorn main:app --port 8000 --app-dir agent-service     # agent
python -m uvicorn main:app --port 8001 --app-dir context-service   # context
python -m uvicorn main:app --port 8002 --app-dir tool-service      # tool（mock_go=true 时无需 Go）
python worker.py           # ingest（workdir=ingest-service）
```

### 4. 测试

测试脚本已移除（测试期产物）；联调验证走 Go 后端 + 前端页面直连。

## 四、核心设计要点

| 项 | 设计 |
|---|---|
| **工具（9 个）** | 田块查询 / 最新遥测 / 历史趋势 / 总览 / 告警 / 阈值 / 设备 / 知识检索 / 文档原文；JSON Schema 白名单 + 版本号；`mock_go=true` 时返回契约示例（Go 未就绪联调用） |
| **上下文组装** | System 段 = 硬编码提示词按 `interaction_style` / `knowledge_reliance` case 选取（`context-service/prompts.py`）；三路取数（知识/现场/记忆）分隔符隔离；**预算裁剪：现场>知识>短期>记忆** |
| **会话状态机** | Redis `agent:session:{id}`：active / waiting_close / closed；结束判定=显式按钮 → 规则正则 → LLM 意图 → 询问后 5 分钟超时（惰性检查） |
| **短期窗口** | Redis `ctx:{userId}`（按用户单会话），只存文字；LRU 最大活跃用户数（`ctx:active` ZSET），超限由 ingest 判定逐出、直接销毁（不写回） |
| **消息落库** | 每轮经 Go 接口写 `chat_messages`（事实源，复用 Go 表）；Redis 窗口为可重建缓存 |
| **知识库** | 元数据状态机归 Go（可用性 Go 保证）；向量化归智能体（Go 通知 → ingest → 火山 embedding → Milvus knowledge collection，无状态过滤） |
| **长期记忆** | 会话 closed → ingest 摘要（LLM）→ Milvus memory collection（`user_id` 强隔离，`source_type=memory`）→ 每轮自动召回 |
| **SSE** | 先发 `started` 占位事件（响应头立即返回），再逐 delta 流式 answer，`done` 带 canClose/sources |
| **并发控制** | `agent.max_concurrency`（默认 4）限制同时处理的对话数；`concurrency_wait_seconds=0`（当前）并发满立即返回"系统繁忙"不排队；调 >0 可恢复排队；`/healthz` 暴露 in_flight/waiting/available |
| **失败重投** | ingest 消费"成功才 ACK"；失败重投（retry 计数，超 3 次丢弃） |
| **可观测性** | 三个 HTTP 服务每次请求输出一条 JSON 完成记录并回传 `X-Request-ID`/`X-Trace-Id`；ingest 每次消费输出一条成功或失败记录；跨服务同时透传两个 ID |

HTTP 完成记录包含服务、路由、状态、耗时、请求/响应字节数和关联 ID；已识别用户时包含 `actor_id`。Stream 记录包含 stream、event_id、处理结果、耗时及重投/丢弃结果。日志不记录查询参数、请求体、JWT、内部密钥或完整业务内容。

## 五、与 Go 主后端的边界

| 职责 | 归属 |
|---|---|
| 用户/农场/地块/设备/遥测/告警/控制/审计 | Go（组内已测） |
| 会话/消息表（`chat_sessions`/`chat_messages`） | Go 建表，智能体经接口读写（不直连 PG） |
| 知识文档元数据（`knowledge_documents`） | Go 管状态机；智能体只读 |
| Milvus 向量库（知识+记忆） | **智能体侧**（独立部署、直连，Go 不碰） |
| 问答编排 / RAG / 上下文 / LLM | 智能体侧 |

接口契约详见：`docs/08_智能体接口契约.md`（复用 Go 已有 9 个 + Go 新增 2 个 + agent 提供）、`docs/09_给Go侧的接口与SQL需求.md`（PostgreSQL 方言）。

## 六、注意事项

- `config.yaml` 中 API key 为**测试 key**，项目完成后统一删除并改回 `${VAR}` 占位；
- `tool.mock_go: true` 为测试期开关，Go 就绪后改回 `false`；
- Windows 控制台输出中文乱码不影响功能（SSE 内容为 UTF-8）；
- 测试环境 8080 端口 SYN 被环境丢弃导致 Go 连接超时 2s 降级——生产 Go 可达时毫秒级。
