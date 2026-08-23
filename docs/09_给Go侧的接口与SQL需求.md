# 给 Go 侧的需求清单（MySQL 版）

> 依据：`智慧农业接口设计_修改后.md`、`智慧农业系统架构设计.md` 与当前 `backend/` 实现。
>
> 技术基线：Go 1.25、Gin、GORM、MySQL 8.4、Redis、TDengine、EMQX、Milvus、MinIO。
>
> 数据库决策：Go 侧业务数据库统一使用 **MySQL 8.4**；高频遥测写入 TDengine，Redis 只保存可重建的最新状态和短期缓存，Milvus 保存知识向量，MinIO 保存知识文档原文件。
>
> 状态标记：`已具备` 表示当前仓库已有对应路由和主要业务代码；`部分具备` 表示只有 HTTP/持久化骨架或仍使用模拟逻辑；`待实现` 表示当前仓库尚无完整实现。

---

## 1. Go 侧交付边界

Go 后端监听 `8080`，负责：

1. 用户认证、JWT 校验和按当前用户进行数据隔离。
2. 地块、设备、阈值、告警、控制命令、审计和知识文档元数据管理。
3. 设备 MQTT 消息接收、校验、去重、命令下发和回执处理。
4. Redis 最新遥测/设备状态维护，TDengine 遥测写入和历史聚合查询。
5. 面向前端提供 REST 与 SSE；统一前缀为 `/api/v1`。
6. 智能问答的业务数据工具、会话消息持久化、建议采纳和知识文档管理。
7. 调用独立智能体服务时负责鉴权透传、超时、熔断、审计和返回格式转换。

Go 侧不负责：

- 向量切片、Embedding 计算和 Milvus 索引实现；
- 大模型推理实现；
- 前端页面和设备固件实现。

智能体不得直连 MySQL、Redis 或 TDengine，只能通过 Go 侧受控接口读取业务数据或写入会话事实。

---

## 2. 通用接口要求

### 2.1 鉴权与权限

- 除注册、登录、健康检查和明确的内部服务接口外，全部要求 `Authorization: Bearer <access_token>`。
- Go 必须从 JWT 获取当前用户 ID，不接受客户端传入的 `userId` 作为授权依据。
- 当前仓库已经将农场关系扁平化为 `plots.owner_id`；所有地块、设备、告警、命令和智能问答上下文查询都必须通过 `owner_id` 校验归属。
- 用户访问无权资源时统一返回 `404`，避免泄露资源是否存在。
- 控制、阈值修改、设备绑定/解绑、告警确认、AI 建议采纳和知识文档状态变更必须写审计日志。

### 2.2 请求与响应

- 成功和失败统一返回 `{code, message, data}`；健康检查接口可保留 `{status}`。
- JSON 字段使用 `camelCase`，数据库字段使用 `snake_case`。
- 分页参数：`page` 从 1 开始，`pageSize` 默认 20、最大 100；分页结果包含 `items/page/pageSize/total`。
- 时间对外使用 ISO 8601；MySQL 中统一保存 UTC `DATETIME(6)`，输出时按接口约定转换。
- 每个响应带 `X-Request-ID`；服务间传递 `trace_id/request_id/message_id/command_id`。
- 参数错误、未认证、资源不存在、状态冲突、依赖不可用分别使用稳定业务错误码。

### 2.3 幂等与事务

- MQTT QoS 1 消息按 `messageId` 去重。
- 控制命令支持 `Idempotency-Key`，同一用户的重复请求返回原命令结果。
- “写业务表 + 写审计/Outbox”必须在同一 MySQL 事务中完成。
- 会话消息写入和 `chat_sessions.last_message_at` 更新必须在同一事务中完成。
- AI 建议只能成功采纳一次；重复采纳返回原 `commandId`。

---

## 3. REST 接口需求

### 3.1 认证与用户

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 已具备 | `POST /api/v1/auth/register` | `mobile/username/password` 校验，密码安全哈希，用户名和手机号唯一 |
| 已具备 | `POST /api/v1/auth/login` | 用户名密码登录，返回 JWT、有效期和用户概要 |
| 已具备 | `GET /api/v1/users/me` | 从 MySQL 返回当前用户及 `interactionStyle`、`knowledgeReliance` |

### 3.2 地块

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 已具备 | `GET /api/v1/plots` | 仅返回当前用户地块；接入 Redis 后补齐最新湿度、温度、设备状态、告警数 |
| 已具备 | `GET /api/v1/plots/{plotId}` | 返回当前用户地块详情，不得跨用户读取 |

当前项目不再对外暴露农场成员关系；接口文档中的 `farmId` 过滤在 Go 侧应转换为当前用户可见地块范围，不能重新引入 `farms/farm_users` 依赖。

### 3.3 看板与遥测

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 待实现 | `GET /api/v1/dashboard/overview` | 聚合当前用户地块、Redis 最新值、在线设备和活动告警；返回统一采样时间 |
| 待实现 | `GET /api/v1/plots/{plotId}/telemetry/latest` | 先校验地块归属，再从 Redis 返回最新指标和来源设备 |
| 待实现 | `GET /api/v1/telemetry/latest` | 批量返回当前用户全部或指定地块的最新数据，避免逐地块查询 |
| 已实现 | `GET /api/v1/telemetry/history` | 从 TDengine 查询（`TDENGINE_ENABLED` 时装配 `TDengineStore`）；支持 `plotId/metric/range/startTime/endTime/interval`，`INTERVAL` 桶聚合 avg/min/max + `FILL(PREV)`；限制最大时间范围 30 天和点数 |
| 待实现 | `POST /api/v1/telemetry/import` | 仅管理员/测试环境可用；校验、批量写入、逐条返回失败原因 |

历史趋势必须按请求粒度聚合和降采样，7 天趋势查询目标为 P95 小于 2 秒。

### 3.4 设备管理

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 已具备 | `GET /api/v1/devices` | 支持 `plotId/status/type/page/pageSize`，只查当前用户设备 |
| 已具备 | `POST /api/v1/devices/bind` | 校验设备序列号、地块归属、设备未被有效绑定；写审计 |
| 已具备 | `DELETE /api/v1/devices/{deviceId}/binding` | 软解绑并保留历史数据；写审计 |
| 已具备 | `GET /api/v1/devices/{deviceId}/status` | 返回在线状态、电量、信号、最后心跳和状态说明 |

### 3.5 灌溉控制

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 部分具备 | `GET /api/v1/plots/{plotId}/irrigation/status` | 以 Redis 设备状态为主，MySQL 最近命令为兜底，并标明状态时间 |
| 部分具备 | `POST /api/v1/plots/{plotId}/irrigation/commands` | 校验归属、设备在线、安全时长和幂等键；先保存 `PENDING`，再通过 Outbox/MQTT 下发 |
| 已具备 | `GET /api/v1/commands/{commandId}` | 返回命令、请求参数、回执、错误和时间线 |
| 已具备 | `GET /api/v1/commands` | 支持 `plotId/status/page/pageSize` 并按当前用户隔离 |

现有 `Issue` 服务会在创建命令后立即把状态改为 `SUCCEEDED`，只能视为演示桩。正式实现必须按以下状态机推进：

```text
PENDING -> SENT -> SUCCEEDED
                -> FAILED
        -> TIMEOUT
```

- 创建命令后立即返回 `PENDING`，不得等待设备真实执行后才响应。
- MQTT 发布成功后改为 `SENT`；收到合法回执后改为 `SUCCEEDED/FAILED`。
- 超过 `expires_at` 未收到回执改为 `TIMEOUT`。
- 旧回执、重复回执和非目标设备回执不得覆盖终态。

### 3.6 阈值与告警

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 部分具备 | `GET /api/v1/plots/{plotId}/thresholds` | 返回阈值、持续时间、回差、级别和启用状态 |
| 部分具备 | `PUT /api/v1/plots/{plotId}/thresholds/{thresholdId}` | 校验地块归属、指标和阈值范围；更新后写审计 |
| 部分具备 | `GET /api/v1/alerts` | 支持 `plotId/status/page/pageSize`，只返回当前用户告警 |
| 部分具备 | `GET /api/v1/alerts/logs` | 支持开始/结束时间和状态筛选，包含恢复、确认、关闭记录 |
| 部分具备 | `POST /api/v1/alerts/{alertId}/confirm` | 幂等确认，保存确认人、时间和备注；写审计 |
| 已具备 | `POST /internal/alerts/trigger` | 内部密钥鉴权；活动告警去重后，同事务创建 owner 站内通知与 `ALERT_TRIGGERED` Outbox，并把原始 JSON 请求可靠转发到 Agent |

告警触发已按规则行锁实现活动告警去重；告警引擎仍需实现持续时间、回差和恢复逻辑。Agent 转发由 `AGENT_ALERT_URL` 启用，失败采用指数退避，不能回滚已经提交的告警和 owner 通知。

### 3.7 实时推送 SSE

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 已具备 | `GET /api/v1/events/stream` | JWT 鉴权、心跳、断线清理、按当前用户隔离，支持 `Last-Event-ID` 断线续传；事件生产者按资源所属 owner 发布即可完成地块过滤 |

事件类型：

- `telemetry.updated`
- `alert.created`
- `alert.recovered`
- `device.status.changed`
- `command.result`

每条事件至少包含事件 ID、事件时间和资源 ID；禁止向连接推送其他用户的地块数据。

五类事件的类型化发布契约均已具备，统一校验 owner、资源和事件时间并自动生成事件 ID。`alert.created` 已接入告警事务提交后的新告警触发点，`command.result` 已接入控制命令完成点；`telemetry.updated`、`alert.recovered` 和 `device.status.changed` 由后续 MQTT 遥测接入、告警恢复引擎和设备心跳调度器调用同一发布契约。

### 3.8 智能问答与知识文档

Go 作为前端业务网关，保留 `/api/v1/ai/*` 公共契约；模型推理、向量检索可委托独立智能体服务。

| 状态 | 方法与路径 | Go 侧要求 |
|---|---|---|
| 待实现 | `POST /api/v1/ai/chat` | 校验会话和地块归属；组装 Redis 最新值、MySQL 阈值、TDengine 趋势；调用智能体；落用户问题、回答、引用和建议 |
| 已具备 | `POST /api/v1/ai/sessions` | 创建当前用户会话，可绑定当前用户地块 |
| 已具备 | `GET /api/v1/ai/sessions/{sessionId}/messages` | 仅会话所有者可读，按创建时间稳定排序并支持分页 |
| 已具备 | `POST /api/v1/ai/sessions/{sessionId}/close` | 当前用户幂等关闭会话，关闭后禁止继续写入 |
| 待实现 | `POST /api/v1/ai/suggestions/{suggestionId}/accept` | 必须人工确认；事务内标记建议已采纳并创建控制命令，返回 `commandId` |
| 已具备 | `POST /internal/agent/sessions/{sessionId}/messages` | 供智能体实时写入单轮事实；使用内部服务认证，不接受普通用户 JWT |
| 已具备 | `GET /api/v1/knowledge/docs` | 只返回 `ACTIVE` 文档并支持 `category`；MinIO 启用时返回短期签名下载地址 |
| 已具备 | `POST /api/v1/knowledge/docs` | `multipart/form-data` 上传 MinIO，校验大小并计算 SHA-256，事务写元数据、审计和 Outbox |
| 已具备 | `POST /api/v1/knowledge/docs/{docId}/approve` | 管理员审核，状态 `DRAFT -> APPROVED` |
| 已具备 | `POST /api/v1/knowledge/docs/{docId}/publish` | 状态 `APPROVED -> ACTIVE`；事务内归档同标题旧活动版本 |
| 已具备 | `POST /api/v1/knowledge/docs/{docId}/archive` | 状态转为 `ARCHIVED` 并写智能体通知 Outbox |

智能体内部通知约定：

```http
POST http://agent-service:8000/internal/knowledge/notify
X-Internal-Service-Key: <shared-secret>
Content-Type: application/json
```

```json
{
  "docId": 1001,
  "event": "UPLOADED",
  "version": 1,
  "traceId": "trace_01J..."
}
```

通知事件为 `UPLOADED/UPDATED/ARCHIVED`。Go 使用 Outbox 异步重试，不因智能体短暂不可用回滚已成功的文件上传。

当前实现通过 `KNOWLEDGE_NOTIFY_URL` 启用派发器；多实例使用带租约的 `PROCESSING` 认领，失败采用指数退避。未配置通知 URL 时仅积累 Outbox，不发送网络请求。

---

## 4. MQTT 与后台任务

### 4.1 Topic

| 方向 | Topic | Go 侧处理 |
|---|---|---|
| 设备 -> Go | `agri/{ownerId}/{deviceSn}/telemetry` | 验证设备和绑定、去重、写 TDengine、更新 Redis、触发告警、推送 SSE |
| 设备 -> Go | `agri/{ownerId}/{deviceSn}/heartbeat` | 更新 Redis 在线状态和 MySQL `last_seen_at`，推送状态变化 |
| 设备 -> Go | `agri/{ownerId}/{deviceSn}/command/ack` | 校验命令与设备，幂等更新命令终态，推送结果 |
| Go -> 设备 | `agri/{ownerId}/{deviceSn}/command` | 从 Outbox 发布命令，QoS 1，记录发送结果 |
| 设备 -> Go | `agri/{ownerId}/{deviceSn}/event` | 记录设备异常并按规则生成通知 |

### 4.2 必须实现的任务

- `TelemetryMessageHandler`：Schema 校验、时间戳校验、去重、批量写 TDengine、更新最新值。
- `HeartbeatMessageHandler`：在线状态和电量/信号维护。
- `CommandPublisher`：扫描 Outbox 并发布 MQTT，失败指数退避。
- `CommandAckHandler`：回执状态机、设备匹配和重复回执处理。
- `DeviceOfflineScheduler`：心跳过期转离线并发 SSE。
- `CommandTimeoutScheduler`：过期命令转 `TIMEOUT`。
- `AlertEvaluator`：持续时间、回差、去重和恢复。
- `OutboxDispatcher`：可靠投递 MQTT、SSE/通知和智能体文档事件。

---

## 5. MySQL 需求

### 5.1 MySQL 规范

- 版本：MySQL 8.4 LTS。
- 引擎与字符集：`InnoDB`、`utf8mb4`、`utf8mb4_0900_ai_ci`。
- 时间：统一 `DATETIME(6)`，应用层按 UTC 写入。
- 金额/测量阈值使用 `DECIMAL`，禁止用 `FLOAT` 做等值业务判断。
- 可变结构使用原生 `JSON`；写入前在 Go DTO 层校验 Schema。
- 外键字段类型必须与被引用主键完全一致。
- 迁移脚本版本化执行，不依赖 GORM `AutoMigrate` 修改生产结构。
- 高频遥测不得写入 MySQL。

### 5.2 当前已有业务表

当前迁移已经建立：

- `users`、`roles`、`user_roles`
- `plots`
- `devices`、`device_bindings`
- `alert_rules`、`alerts`
- `device_commands`
- `notifications`
- `audit_logs`
- `outbox_events`

后续代码和文档统一沿用这些复数表名，不再使用 `sys_user/plot/device/control_command` 等另一套命名。

### 5.3 users 个性化字段

建议迁移：`008_add_user_agent_preferences.sql`。

```sql
ALTER TABLE users
    ADD COLUMN interaction_style VARCHAR(16) NULL
        COMMENT '语言风格：plain/casual/professional' AFTER status,
    ADD COLUMN knowledge_reliance VARCHAR(16) NULL
        COMMENT '决策依据：experience/document/data' AFTER interaction_style;
```

Go 侧同时修改用户模型和 `GET /api/v1/users/me` DTO；写入时只接受注释中列出的值或 `NULL`。

### 5.4 智能问答表

建议迁移：`009_create_ai_chat_schema.sql`。

```sql
CREATE TABLE chat_sessions (
    id VARCHAR(64) NOT NULL,
    user_id BIGINT NOT NULL,
    plot_id BIGINT NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'ACTIVE'
        COMMENT 'ACTIVE/CLOSED',
    summary TEXT NULL,
    last_message_at DATETIME(6) NULL,
    closed_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_chat_sessions_user_updated (user_id, updated_at),
    KEY idx_chat_sessions_plot_updated (plot_id, updated_at),
    CONSTRAINT fk_chat_sessions_user
        FOREIGN KEY (user_id) REFERENCES users (id),
    CONSTRAINT fk_chat_sessions_plot
        FOREIGN KEY (plot_id) REFERENCES plots (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE chat_messages (
    id BIGINT NOT NULL AUTO_INCREMENT,
    session_id VARCHAR(64) NOT NULL,
    role VARCHAR(16) NOT NULL
        COMMENT 'USER/ASSISTANT/SYSTEM/TOOL',
    content LONGTEXT NOT NULL,
    citations_json JSON NULL,
    plot_id BIGINT NULL,
    model_version VARCHAR(64) NULL,
    trace_id VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_chat_messages_session_created (session_id, created_at, id),
    KEY idx_chat_messages_plot_created (plot_id, created_at),
    CONSTRAINT fk_chat_messages_session
        FOREIGN KEY (session_id) REFERENCES chat_sessions (id),
    CONSTRAINT fk_chat_messages_plot
        FOREIGN KEY (plot_id) REFERENCES plots (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;

CREATE TABLE ai_suggestions (
    id VARCHAR(64) NOT NULL,
    session_id VARCHAR(64) NOT NULL,
    plot_id BIGINT NOT NULL,
    action VARCHAR(32) NOT NULL,
    duration_seconds INT NULL,
    confidence DECIMAL(5, 4) NULL,
    reason VARCHAR(500) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'PENDING'
        COMMENT 'PENDING/ACCEPTED/REJECTED/EXPIRED',
    accepted_by BIGINT NULL,
    accepted_at DATETIME(6) NULL,
    command_id VARCHAR(64) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    KEY idx_ai_suggestions_session_created (session_id, created_at),
    KEY idx_ai_suggestions_plot_status (plot_id, status),
    CONSTRAINT fk_ai_suggestions_session
        FOREIGN KEY (session_id) REFERENCES chat_sessions (id),
    CONSTRAINT fk_ai_suggestions_plot
        FOREIGN KEY (plot_id) REFERENCES plots (id),
    CONSTRAINT fk_ai_suggestions_accept_user
        FOREIGN KEY (accepted_by) REFERENCES users (id),
    CONSTRAINT fk_ai_suggestions_command
        FOREIGN KEY (command_id) REFERENCES device_commands (command_id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
```

`citations_json` 保存结构化引用，不保存向量；会话消息是事实源，Redis 会话窗口可随时重建。

### 5.5 知识文档表

建议迁移：`010_create_knowledge_documents.sql`。

```sql
CREATE TABLE knowledge_documents (
    id BIGINT NOT NULL AUTO_INCREMENT,
    title VARCHAR(255) NOT NULL,
    category VARCHAR(64) NOT NULL,
    object_key VARCHAR(512) NOT NULL
        COMMENT 'MinIO 对象键，不保存永久公开 URL',
    file_hash CHAR(64) NOT NULL
        COMMENT '文件 SHA-256，用于防重复上传',
    source VARCHAR(255) NULL,
    status VARCHAR(16) NOT NULL DEFAULT 'DRAFT'
        COMMENT 'DRAFT/APPROVED/ACTIVE/ARCHIVED',
    version INT NOT NULL DEFAULT 1,
    uploaded_by BIGINT NOT NULL,
    approved_by BIGINT NULL,
    published_at DATETIME(6) NULL,
    archived_at DATETIME(6) NULL,
    created_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6),
    updated_at DATETIME(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
        ON UPDATE CURRENT_TIMESTAMP(6),
    PRIMARY KEY (id),
    CONSTRAINT uk_knowledge_doc_title_version UNIQUE (title, version),
    CONSTRAINT uk_knowledge_doc_file_hash UNIQUE (file_hash),
    KEY idx_knowledge_doc_status_category (status, category, version),
    CONSTRAINT fk_knowledge_doc_uploader
        FOREIGN KEY (uploaded_by) REFERENCES users (id),
    CONSTRAINT fk_knowledge_doc_approver
        FOREIGN KEY (approved_by) REFERENCES users (id)
) ENGINE = InnoDB DEFAULT CHARSET = utf8mb4 COLLATE = utf8mb4_0900_ai_ci;
```

状态规则：

- `DRAFT -> APPROVED -> ACTIVE -> ARCHIVED`；禁止逆向修改。
- 同一标题同一时间最多一个 `ACTIVE` 版本，由 Go 在事务中锁定同标题记录后保证。
- 对外列表只返回 `ACTIVE`；MinIO 下载地址按请求生成短期签名 URL。
- 文件上传成功、事务提交后写 Outbox；智能体加工失败不改变文档业务状态，但必须可查询加工失败并重试。

---

## 6. Redis、TDengine、MinIO 与 Milvus 边界

| 存储 | 数据 | Go 侧要求 |
|---|---|---|
| MySQL | 用户、地块、设备、阈值、告警、命令、会话、消息、知识元数据、审计、Outbox | 业务事实源、事务一致性、备份恢复 |
| Redis | 最新遥测、在线状态、灌溉快照、SSE 会话、去重键、短期会话窗口 | 设置 TTL；缓存丢失后可由 MySQL/TDengine/MQTT 重建 |
| TDengine | 原始和聚合遥测 | 批量写入、按用户地块过滤、保留策略和降采样 |
| MinIO | 知识原文和附件 | 私有 Bucket、短期签名 URL、对象键与 MySQL 元数据一致 |
| Milvus | 文档向量和检索元数据 | 归智能体侧管理；Go 不直连、不保存向量 |

建议 Redis Key：

```text
agri:telemetry:latest:{plotId}
agri:device:status:{deviceId}
agri:irrigation:status:{plotId}
agri:command:pending:{commandId}
agri:mqtt:dedup:{messageId}
agri:sse:user:{userId}
```

---

## 7. 非功能与安全验收

### 7.1 性能目标

- 普通 API：P95 小于 500 ms。
- 实时遥测端到端：P95 小于 3 秒。
- 7 天趋势：P95 小于 2 秒，返回经过聚合的有限数据点。
- 控制命令平台下发：P95 小于 1 秒，不含设备执行时间。
- 试点容量：持续 200 条遥测/秒，峰值 1,000 条/秒，1,000 台在线设备。

### 7.2 可靠性与安全

- MySQL 每日全量备份并启用 binlog，定期做恢复演练。
- MySQL、Redis、TDengine、EMQX 和 MinIO 不暴露到公网。
- Web/API 使用 HTTPS，MQTT 使用 TLS，生产环境禁用匿名连接。
- 密码使用 bcrypt/Argon2id；JWT 密钥、数据库密码、MQTT 凭据和内部服务密钥不得提交仓库。
- 对登录、控制、上传和内部服务接口限流；上传校验类型、大小、Hash 和恶意内容。
- 智能体只有只读业务工具权限，不能绕过“建议采纳”接口直接控制设备。
- 日志不得记录密码、JWT、设备密钥、内部服务密钥或完整敏感文档内容。

### 7.3 测试要求

- 单元测试：权限隔离、阈值持续时间/回差、命令状态机、参数校验、状态转换。
- HTTP 契约测试：路由、统一响应、错误码、分页、JWT 和越权场景。
- MySQL 集成测试：迁移从空库执行、升级执行、事务回滚、唯一键与外键。
- 依赖集成测试：Redis、TDengine、EMQX、MinIO、智能体通知重试。
- 端到端测试：遥测上报到 SSE、低湿告警到确认、控制下发到回执、问答到建议采纳。
- 异常测试：重复/乱序 MQTT、Broker 重连、数据库短暂不可用、设备离线、回执丢失、智能体超时。

---

## 8. 实施优先级与完成定义

| 优先级 | 工作包 | 完成定义 |
|---|---|---|
| P0 | MySQL 基线、认证、地块权限 | 迁移可从空库执行；JWT 和跨用户隔离测试通过 |
| P0 | MQTT、Redis 最新值、TDengine、看板/遥测接口 | 模拟设备数据 3 秒内展示，历史趋势满足查询目标 |
| P0 | 控制命令真实状态机 | 不再模拟成功；下发、回执、超时、幂等、审计全链路通过 |
| P0 | 阈值和告警闭环 | 持续时间、回差、去重、恢复、确认与 SSE 通过测试 |
| P1 | 设备管理和离线检测 | 绑定/解绑可追溯，心跳超时可正确转离线 |
| P1 | SSE 与 Outbox | 断线清理、权限过滤、依赖失败重试和积压监控可用 |
| P2 | 智能问答会话和建议采纳 | 消息实时落 MySQL，有引用、有数据时间，建议必须人工确认 |
| P2 | 知识文档管理 | 上传、审核、发布、归档、通知、失败重试和版本切换完整 |

每个工作包均需同时交付：

- Go Handler、DTO、Service、Repository；
- MySQL 版本化迁移和必要索引；
- 单元测试、HTTP 契约测试和关键集成测试；
- OpenAPI/接口示例、环境变量和运行说明；
- 关键指标、结构化日志和健康检查。

最终验收以“真实依赖链路可运行”为准；只有路由或仅返回模拟数据不视为完成。
