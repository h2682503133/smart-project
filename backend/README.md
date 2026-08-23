# 智慧农业 Gin 后端骨架

该目录是依据《智慧农业系统架构设计（修订版）》建立的 Go 模块化单体。目前包含 Gin HTTP 服务、MySQL 领域模型、GORM Repository、嵌入式数据库迁移，以及基于 JWT 的注册登录功能。

## 技术基线

- Go 1.25
- Gin
- GORM
- MySQL 8.4 LTS
- Redis 8.4
- Docker Compose

## 模块结构

```text
backend/
├─ cmd/api/                    # HTTP 服务入口与优雅停机
├─ internal/
│  ├─ http/                   # Gin 路由与 Handler
│  ├─ config/                 # 环境变量配置
│  ├─ platform/database/      # MySQL 连接与嵌入式 SQL 迁移
│  ├─ shared/persistence/     # 公共模型与泛型 Repository
│  ├─ identity/               # 用户与角色
│  ├─ plot/                   # 农户所属田块
│  ├─ device/                 # 设备与绑定关系
│  ├─ events/                 # SSE 用户订阅、隔离与断线重放
│  ├─ alert/                  # 告警规则与告警记录
│  ├─ control/                # 设备控制命令
│  ├─ notification/           # 告警通知
│  ├─ audit/                  # 操作审计
│  └─ outbox/                 # 可靠异步事件
└─ compose.yaml
```

项目不预留 Agent/RAG 模块。

## 已建立的 MySQL 表

- `users`、`roles`、`user_roles`
- `plots`（通过 `owner_id` 归属农户）
- `devices`、`device_bindings`
- `alert_rules`、`alerts`
- `device_commands`
- `notifications`
- `audit_logs`
- `outbox_events`
- `chat_sessions`、`chat_messages`、`ai_suggestions`
- `knowledge_documents`

高频温度、土壤湿度和光照遥测不进入 MySQL。最新地块快照写入 Redis；设备在线状态由服务端收到遥测的时间推导，设备不会上报状态、电量或信号。历史遥测后续由 `telemetry` 模块批量写入 TDengine。

## 本地启动

启动 MySQL、Redis 和 MinIO：

```powershell
docker compose up -d mysql redis minio
```

安装依赖并执行测试：

```powershell
go mod tidy
go test ./...
```

启动应用：

```powershell
go run ./cmd/api
```

健康检查：

```text
GET http://localhost:8080/actuator/health
GET http://localhost:8080/actuator/health/liveness
GET http://localhost:8080/actuator/health/readiness
```

认证接口：

```text
POST /api/v1/auth/register  # mobile、username、password，无手机验证码
POST /api/v1/auth/login     # username、password
GET  /api/v1/users/me       # Authorization: Bearer <access_token>
```

地块接口：

```text
GET  /api/v1/plots          # 当前用户的地块列表
GET  /api/v1/plots/{plotId} # 当前用户的地块详情
```

两个地块接口都要求 Bearer Token，并按令牌中的用户 ID 隔离数据。实时遥测接入前，列表中的 `soilMoisture`、`temperature` 和 `deviceStatus` 返回 `null`。

实时事件接口：

```text
GET  /api/v1/events/stream   # Bearer Token；支持 Last-Event-ID 断线续传
```

响应使用 `text/event-stream`，每 15 秒发送一次心跳，并在客户端断开时清理订阅。事件中心按 owner ID 隔离连接；事件生产者发布时必须提供 owner ID、资源 ID 和事件类型。

支持的实时事件为 `telemetry.updated`、`alert.created`、`alert.recovered`、`device.status.changed` 和 `command.result`。告警创建和控制命令完成已接入现有业务流程；其余类型的发布函数供 MQTT 遥测、告警恢复和设备心跳模块接入。

MQTT 客户端订阅 `agri/+/+/telemetry`，并通过 `telemetry.DecodePayload` 和 `IngestService` 接入。设备 JSON 仅允许 `temperature/soilMoisture/light` 与 `temperatureWarning/soilMoistureWarning/lightWarning` 六个必填字段；设备、owner、地块和接收时间从可信 Topic 与绑定关系补充。未知字段会被拒绝。客户端使用 QoS 1、自动重连；BearPi-HM Nano 暂未上线或 Broker 暂不可达时不会阻塞 HTTP API 启动。

BearPi-HM Nano 发布示例：

```text
Topic: agri/{ownerId}/{deviceSn}/telemetry
Payload: {"temperature":26.5,"soilMoisture":48,"light":920,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":false}
```

智能问答会话接口：

```text
POST /api/v1/ai/sessions                       # 创建当前用户会话
GET  /api/v1/ai/sessions/{id}/messages         # 分页读取当前用户会话消息
POST /api/v1/ai/sessions/{id}/close            # 幂等关闭会话
POST /internal/agent/sessions/{id}/messages    # 智能体实时写入消息，使用内部服务密钥
POST /internal/alerts/trigger                  # 告警引擎触发告警；自动通知 owner 并转发原请求到 Agent
```

告警触发接口在一个 MySQL 事务中完成活动告警去重、告警落库、owner 站内通知和 `ALERT_TRIGGERED` Outbox 写入。Agent 转发使用收到的原始 JSON 请求体；Agent 不可用时按指数退避重试，不影响 owner 告警和通知落库。

知识文档元数据接口：

```text
GET  /api/v1/knowledge/docs                    # 登录用户读取 ACTIVE 文档
POST /api/v1/knowledge/docs                    # SYSTEM_ADMIN 上传文件并创建 DRAFT 元数据
POST /api/v1/knowledge/docs/{id}/approve       # DRAFT -> APPROVED
POST /api/v1/knowledge/docs/{id}/publish       # APPROVED -> ACTIVE
POST /api/v1/knowledge/docs/{id}/archive       # ACTIVE -> ARCHIVED
```

知识文档上传接口使用 `multipart/form-data`，字段为 `file/title/category/source?/version?`。文件写入 MinIO 后，元数据、审计日志和 Outbox 事件在同一 MySQL 事务内落库；数据库事务失败时会回滚已上传对象。活动文档列表返回短期签名下载地址。

## 配置

本地默认连接：

```text
mysql://localhost:3307/smart_agriculture
username: smart_agriculture
password: smart_agriculture
```

支持以下环境变量：

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `SERVER_PORT` | `8080` | HTTP 端口 |
| `GIN_MODE` | `debug` | Gin 运行模式 |
| `DB_DSN` | 空 | 完整 Go MySQL DSN，设置后优先级最高 |
| `DB_URL` | 原 JDBC MySQL URL | 兼容原 Spring 配置 |
| `DB_USERNAME` | `smart_agriculture` | 数据库用户名 |
| `DB_PASSWORD` | `smart_agriculture` | 数据库密码 |
| `DB_POOL_MAX_SIZE` | `10` | 最大连接数 |
| `DB_POOL_MIN_IDLE` | `2` | 最大空闲连接数 |
| `DB_MIGRATE` | `true` | 启动时执行嵌入式 SQL 迁移 |
| `REDIS_ENABLED` | `true` | 是否启用 Redis；启用后启动阶段必须连接成功 |
| `REDIS_URL` | `redis://localhost:6379/0` | Redis URL，可包含认证信息和 DB 编号 |
| `REDIS_POOL_SIZE` | `10` | Redis 连接池大小 |
| `REDIS_DIAL_TIMEOUT` | `2s` | Redis 建连超时 |
| `REDIS_READ_TIMEOUT` | `1s` | Redis 读超时 |
| `REDIS_WRITE_TIMEOUT` | `1s` | Redis 写超时 |
| `REDIS_QUERY_CACHE_TTL` | `1m` | 地块和设备查询缓存时长 |
| `REDIS_ALERT_CACHE_TTL` | `10s` | 活动告警查询缓存时长 |
| `REDIS_TELEMETRY_TTL` | `5m` | 最新地块遥测快照时长 |
| `MQTT_ENABLED` | `true` | 是否启动 MQTT 客户端 |
| `MQTT_BROKER_URL` | `tcp://localhost:1883` | EMQX MQTT 地址 |
| `MQTT_CLIENT_ID` | `smart-agriculture-api` | 服务端 MQTT Client ID，多实例部署时必须唯一 |
| `MQTT_USERNAME` / `MQTT_PASSWORD` | 空 | Broker 认证信息 |
| `MQTT_TOPIC_PREFIX` | `agri` | 遥测 Topic 前缀 |
| `MQTT_CONNECT_TIMEOUT` | `5s` | 单次连接/订阅等待时间 |
| `MQTT_RECONNECT_BACKOFF` | `5s` | 断线重连间隔 |
| `MQTT_MESSAGE_TIMEOUT` | `10s` | 单条遥测处理超时 |
| `REDIS_IRRIGATION_TTL` | `35m` | 灌溉状态快照时长 |
| `REDIS_DEVICE_ACTIVITY_TTL` | `24h` | 设备最后接收时间保留时长 |
| `DEVICE_OFFLINE_AFTER` | `2m` | 超过该时间未收到遥测则推导为离线 |
| `JWT_SECRET` | 仅供本地开发的默认密钥 | HS256 签名密钥，至少 32 个字符 |
| `JWT_ISSUER` | `smart-agriculture-api` | JWT 签发方 |
| `JWT_TTL` | `2h` | 访问令牌有效期（Go duration） |
| `INTERNAL_SERVICE_KEY` | 仅供本地开发的默认密钥 | 智能体调用 `/internal/*` 的共享密钥，至少 32 个字符 |
| `OBJECT_STORAGE_ENABLED` | `false` | 是否启用 MinIO；启用时启动阶段检查并创建 Bucket |
| `MINIO_ENDPOINT` | `localhost:9000` | MinIO S3 API 地址，不带协议 |
| `MINIO_ACCESS_KEY` | `minioadmin` | MinIO Access Key |
| `MINIO_SECRET_KEY` | `minioadmin` | MinIO Secret Key |
| `MINIO_BUCKET` | `knowledge` | 知识文档私有 Bucket |
| `MINIO_REGION` | `us-east-1` | Bucket Region |
| `MINIO_SECURE` | `false` | 是否通过 HTTPS 访问 MinIO |
| `KNOWLEDGE_MAX_UPLOAD_BYTES` | `20971520` | 单个知识文档最大字节数 |
| `MINIO_SIGNED_URL_TTL` | `15m` | 下载签名地址有效期 |
| `KNOWLEDGE_NOTIFY_URL` | 空 | 智能体通知完整 URL；为空时不启动 Outbox 派发器 |
| `AGENT_ALERT_URL` | 空 | 告警原始请求转发到 Agent 的完整 URL；为空时事件保留在 Outbox |
| `OUTBOX_DISPATCH_INTERVAL` | `2s` | Outbox 扫描周期 |
| `OUTBOX_BATCH_SIZE` | `50` | 单批认领数量，范围 1-500 |
| `AGENT_HTTP_TIMEOUT` | `5s` | 调用智能体通知接口的超时 |

已有 Flyway 数据库可直接升级：启动时会读取 `flyway_schema_history`，把成功版本导入新的 `schema_migrations`，不会重复建表。

生产环境不得使用示例密码，也不得将密钥提交到仓库。

## 可观测性日志

API 日志输出到标准输出，格式为单行 JSON。每个 HTTP 请求都会生成一条 `http_request_completed` 记录，并在响应中返回 `X-Request-ID` 和 `X-Trace-ID`；合法的上游 ID 会原样透传，缺失或非法时自动生成。记录包含 `method/route/path/status/result/duration_ms/request_bytes/response_bytes/client_ip`，认证成功后还包含 `actor_id`。

日志只记录 URL path，不记录查询参数、请求体、JWT、密码、设备密钥、内部服务密钥或文档内容。可用 `request_id` 定位单次请求，用 `trace_id` 串联 Agent、Context、Tool 和 Go 后端调用。

## 下一步

1. 接入 TDengine，将遥测历史批量落库。
2. 将控制命令演示桩替换为 MQTT 下发/回执状态机。
3. 配置智能体服务并联调知识文档通知与告警转发。
4. 实现 `/api/v1/ai/chat` 的智能体编排和 AI 建议采纳流程。
