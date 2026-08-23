# 智慧农业 Go 服务接口与关键业务逻辑

本文面向前端、测试和业务人员，只描述 Go 服务已经提供的业务能力、关键规则、请求参数与响应数据。本文不介绍框架、数据库实现、消息模型或任何非 Go 服务内部实现。

## 1. 使用约定

### 1.1 地址与认证

- 业务接口统一以 `/api/v1` 开头。
- 除注册、登录和健康检查外，业务接口均需携带访问令牌。
- 请求头格式：`Authorization: Bearer <accessToken>`。
- 用户只能访问自己名下的地块、设备、命令、告警和会话。
- 账户停用后，旧令牌也会立即失效。

### 1.2 统一响应

成功响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {}
}
```

失败响应：

```json
{
  "code": 40001,
  "message": "参数错误",
  "data": null
}
```

常用业务错误码：

| code | 含义 |
| --- | --- |
| `40001` | 请求参数不合法 |
| `40101` | 未登录、令牌失效或账户已停用 |
| `40102` | 内部服务认证失败 |
| `40301` | 没有对应权限 |
| `40401` | 用户或地块不存在 |
| `40402` | 设备不存在 |
| `40403` | 控制命令不存在 |
| `40404` | 告警或会话不存在 |
| `40405` | 知识文档不存在 |
| `40901` | 数据重复或设备已绑定 |
| `40902` | 灌溉设备当前不可控制 |
| `40903` | 告警或会话状态冲突 |
| `40904` | 知识文档重复或状态冲突 |
| `41301` | 上传文件超过限制 |
| `50000` | 服务内部错误 |
| `50301` | 所需存储能力未启用 |

时间统一使用 ISO 8601，例如 `2026-08-22T08:23:00Z`。响应中的数值 ID 使用正整数，命令 ID 和会话 ID 使用字符串。

## 2. 认证与当前用户

### 2.1 注册

`POST /api/v1/auth/register`

关键规则：

- `username` 为 3～64 个非空白字符。
- `mobile` 为 6～15 位数字，可带前导 `+`。
- `password` 长度为 8～72 字节。
- 用户名或手机号不能重复。

请求：

```json
{
  "mobile": "13812345678",
  "username": "grower01",
  "password": "strong-password"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "user": {
      "id": 7,
      "username": "grower01",
      "mobile": "13812345678",
      "status": "ACTIVE",
      "interactionStyle": null,
      "knowledgeReliance": null,
      "createdAt": "2026-08-22T08:00:00Z",
      "updatedAt": "2026-08-22T08:00:00Z"
    }
  }
}
```

### 2.2 登录

`POST /api/v1/auth/login`

请求：

```json
{
  "username": "grower01",
  "password": "strong-password"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "accessToken": "<access-token>",
    "expiresIn": 7200,
    "user": {
      "id": 7,
      "name": "grower01",
      "role": "FARMER"
    }
  }
}
```

### 2.3 当前用户

`GET /api/v1/users/me`

请求体：无。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 7,
    "name": "grower01",
    "role": "FARMER",
    "interactionStyle": null,
    "knowledgeReliance": null
  }
}
```

## 3. 看板、地块与遥测

### 3.1 总览看板

`GET /api/v1/dashboard/overview`

关键逻辑：

- 批量读取并汇总当前用户全部地块的最新土壤湿度、温度和光照。
- 统计设备在线、离线和总数。
- `active` 统计 `ACTIVE` 告警。
- `pendingConfirm` 按当前接口契约统计 `CONFIRMED` 告警，并兼容旧的 `ACKNOWLEDGED` 数据。
- 没有遥测数据时，对应值返回 `null`。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "sampleTime": "2026-08-22T08:20:00Z",
    "avgSoilMoisture": {
      "value": 27.8,
      "unit": "%"
    },
    "avgTemperature": {
      "value": 26.3,
      "unit": "°C"
    },
    "avgLight": {
      "value": 920,
      "unit": "lx"
    },
    "deviceOnline": {
      "online": 12,
      "total": 13,
      "offline": 1
    },
    "alerts": {
      "active": 2,
      "pendingConfirm": 1
    },
    "plots": [
      {
        "id": 11,
        "code": "A1",
        "soilMoisture": 27.8,
        "temperature": 26.3,
        "light": 920,
        "status": "ACTIVE"
      }
    ]
  }
}
```

### 3.2 地块列表

`GET /api/v1/plots`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": [
    {
      "id": 11,
      "code": "A1",
      "name": "西侧棚",
      "status": "ACTIVE",
      "soilMoisture": null,
      "temperature": null,
      "deviceStatus": null,
      "alertCount": 0,
      "updatedAt": "2026-08-22T08:00:00Z"
    }
  ]
}
```

### 3.3 地块详情

`GET /api/v1/plots/{plotId}`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 11,
    "code": "A1",
    "name": "西侧棚",
    "cropName": "番茄",
    "area": 12.5,
    "status": "ACTIVE",
    "createdAt": "2026-08-01T00:00:00Z"
  }
}
```

### 3.4 单个地块最新遥测

`GET /api/v1/plots/{plotId}/telemetry/latest`

关键逻辑：先校验地块归属，再返回最新指标及该地块绑定的来源设备。暂无指标时 `metrics` 为空对象。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "plotId": 11,
    "sampleTime": "2026-08-22T08:20:00Z",
    "metrics": {
      "soilMoisture": {
        "value": 27.8,
        "unit": "%"
      },
      "temperature": {
        "value": 26.3,
        "unit": "°C"
      },
      "light": {
        "value": 920,
        "unit": "lx"
      }
    },
    "sourceDevices": [
      {
        "id": 31,
        "name": "A1 土壤传感器",
        "status": "ONLINE",
        "battery": null
      }
    ]
  }
}
```

### 3.5 多地块最新遥测

`GET /api/v1/telemetry/latest?plotId=11`

`plotId` 可选；不传时返回当前用户全部地块。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": [
    {
      "plotId": 11,
      "plotCode": "A1",
      "soilMoisture": 27.8,
      "temperature": 26.3,
      "light": 920,
      "status": "ALERT",
      "sampleTime": "2026-08-22T08:20:00Z"
    }
  ]
}
```

其中 `status` 为 `NORMAL` 或 `ALERT`，由该地块是否存在活动告警决定。

### 3.6 历史趋势

`GET /api/v1/telemetry/history?plotId=11&metric=soilMoisture&range=24h&interval=1h`

关键规则：

- `metric`：`soilMoisture`、`temperature` 或 `light`。
- 时间可使用 `range=1h|24h|7d|30d`，也可使用 `startTime` 和 `endTime`。
- `interval`：`5m`、`1h` 或 `1d`，默认 `1h`。
- 查询区间不能超过 30 天。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "plotId": 11,
    "metric": "soilMoisture",
    "unit": "%",
    "points": [
      {
        "time": "2026-08-22T07:00:00Z",
        "avg": 27.6,
        "min": 26.9,
        "max": 28.2
      }
    ]
  }
}
```

## 4. 设备管理

### 4.1 设备列表

`GET /api/v1/devices?plotId=11&status=ONLINE&type=SOIL_SENSOR&page=1&pageSize=20`

筛选参数均可选；`pageSize` 最大为 100。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "items": [
      {
        "id": 31,
        "deviceSn": "SN-20260822-001",
        "name": "A1 土壤传感器",
        "type": "SOIL_SENSOR",
        "plotId": 11,
        "status": "ONLINE",
        "battery": 86,
        "lastSeenAt": "2026-08-22T08:20:00Z",
        "firmwareVersion": "1.2.0"
      }
    ],
    "page": 1,
    "pageSize": 20,
    "total": 1
  }
}
```

设备状态可为 `UNACTIVATED`、`ONLINE`、`OFFLINE`、`RECONNECTING`、`FAULT` 或 `DISABLED`。

### 4.2 绑定设备

`POST /api/v1/devices/bind`

关键逻辑：地块必须属于当前用户；一个设备同一时间只能存在一条有效绑定；已登记设备的类型不能被改成其他类型。

请求：

```json
{
  "deviceSn": "SN-20260822-001",
  "plotId": 11,
  "name": "A1 土壤传感器",
  "type": "SOIL_SENSOR"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 31,
    "deviceSn": "SN-20260822-001",
    "status": "OFFLINE"
  }
}
```

### 4.3 解绑设备

`DELETE /api/v1/devices/{deviceId}/binding`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": true
}
```

### 4.4 设备状态

`GET /api/v1/devices/{deviceId}/status`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "deviceId": 31,
    "status": "ONLINE",
    "battery": 86,
    "signal": 72,
    "lastSeenAt": "2026-08-22T08:20:00Z",
    "message": null
  }
}
```

## 5. 灌溉控制

### 5.1 查询当前灌溉状态

`GET /api/v1/plots/{plotId}/irrigation/status`

关键逻辑：状态仅由最近一条成功或已确认的命令计算，失败、超时和被拒绝的命令不会覆盖实际状态。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "plotId": 11,
    "valveDeviceId": 41,
    "state": "ON",
    "mode": "MANUAL",
    "remainingSeconds": 480,
    "maxSeconds": 1800,
    "lastCommandId": "cmd_0123456789abcdef"
  }
}
```

### 5.2 下发灌溉命令

`POST /api/v1/plots/{plotId}/irrigation/commands`

必须携带请求头：

```text
Idempotency-Key: irrigation-11-20260822-001
```

关键规则：

- 幂等键必填，长度不超过 64；相同用户使用相同幂等键重试时返回原命令。
- `action`：`OPEN` 或 `CLOSE`。
- `mode`：`MANUAL`、`AUTO` 或 `AI_SUGGESTED`。
- `OPEN` 的 `durationSeconds` 必须为 60～1800。
- `CLOSE` 的 `durationSeconds` 必须为 0 或省略。
- 地块必须绑定在线的灌溉阀门。

请求：

```json
{
  "action": "OPEN",
  "durationSeconds": 600,
  "mode": "MANUAL",
  "reason": "土壤湿度偏低"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "commandId": "cmd_0123456789abcdef",
    "plotId": 11,
    "action": "OPEN",
    "status": "SUCCESS",
    "createdAt": "2026-08-22T08:21:10Z"
  }
}
```

关闭请求：

```json
{
  "action": "CLOSE",
  "mode": "MANUAL",
  "reason": "灌溉完成"
}
```

### 5.3 查询命令结果

`GET /api/v1/commands/{commandId}`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": "cmd_0123456789abcdef",
    "plotId": 11,
    "deviceId": 41,
    "action": "OPEN",
    "status": "SUCCEEDED",
    "requestPayload": {
      "durationSeconds": 600,
      "mode": "MANUAL",
      "reason": "土壤湿度偏低"
    },
    "ackPayload": {
      "state": "ON",
      "remainingSeconds": 600
    },
    "createdAt": "2026-08-22T08:21:10Z",
    "ackAt": "2026-08-22T08:21:10Z"
  }
}
```

命令状态包括 `PENDING`、`REJECTED`、`SENT`、`ACKNOWLEDGED`、`SUCCEEDED`、`FAILED`、`TIMEOUT` 和 `EXPIRED`。

### 5.4 命令列表

`GET /api/v1/commands?plotId=11&status=SUCCEEDED&page=1&pageSize=20`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "items": [
      {
        "id": "cmd_0123456789abcdef",
        "plotCode": "A1",
        "action": "OPEN",
        "durationSeconds": 600,
        "status": "SUCCEEDED",
        "operatorName": "grower01",
        "createdAt": "2026-08-22T08:21:10Z"
      }
    ],
    "page": 1,
    "pageSize": 20,
    "total": 1
  }
}
```

## 6. 阈值与告警

### 6.1 查询阈值规则

`GET /api/v1/plots/{plotId}/thresholds`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": [
    {
      "id": 2,
      "plotId": 11,
      "metric": "soilMoisture",
      "operator": "LT",
      "value": 28,
      "hysteresis": 2,
      "unit": "%",
      "durationSeconds": 300,
      "enabled": true,
      "level": "MEDIUM"
    }
  ]
}
```

### 6.2 新增或更新阈值规则

`PUT /api/v1/plots/{plotId}/thresholds/{thresholdId}`

关键规则：

- `operator`：`LT`、`LTE`、`GT` 或 `GTE`。
- `level`：`LOW`、`MEDIUM` 或 `HIGH`。
- `durationSeconds` 范围为 0～86400。
- `hysteresis` 必须大于等于 0；更新时不传该字段会保留已有回差值，传入时才覆盖。

请求：

```json
{
  "metric": "soilMoisture",
  "operator": "LT",
  "value": 28,
  "hysteresis": 2,
  "durationSeconds": 300,
  "level": "MEDIUM",
  "enabled": true
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 2,
    "updatedAt": "2026-08-22T08:22:00Z"
  }
}
```

### 6.3 告警列表

`GET /api/v1/alerts?plotId=11&status=ACTIVE&page=1&pageSize=20`

告警状态包括 `ACTIVE`、`CONFIRMED`、`RESOLVED` 和 `CLOSED`；旧的 `ACKNOWLEDGED` 数据对外按 `CONFIRMED` 返回。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "items": [
      {
        "id": 101,
        "plotId": 11,
        "plotCode": "A1",
        "metric": "soilMoisture",
        "level": "MEDIUM",
        "status": "ACTIVE",
        "title": "A1 地块湿度偏低",
        "content": "26.5% 触发持续阈值告警",
        "currentValue": 26.5,
        "thresholdValue": 28,
        "startedAt": "2026-08-22T08:20:00Z"
      }
    ],
    "page": 1,
    "pageSize": 20,
    "total": 1
  }
}
```

### 6.4 告警日志

`GET /api/v1/alerts/logs?plotId=11&startTime=2026-08-01T00:00:00Z&endTime=2026-08-22T23:59:59Z&page=1&pageSize=20`

响应结构与告警列表一致，可附加时间范围和状态筛选。

### 6.5 确认告警

`POST /api/v1/alerts/{alertId}/confirm`

关键逻辑：确认操作幂等。客户端因超时而重试时仍返回成功，并保持第一次确认的时间和备注。

请求：

```json
{
  "remark": "已开启灌溉，继续观察"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 101,
    "status": "CONFIRMED",
    "confirmedAt": "2026-08-22T08:23:00Z"
  }
}
```

## 7. 实时事件

`GET /api/v1/events/stream`

该接口返回事件流，不使用统一 JSON 响应外壳。断线重连时可携带 `Last-Event-ID` 请求头。

事件示例：

```text
id: 18
event: command.result
data: {"commandId":"cmd_0123456789abcdef","status":"SUCCEEDED","plotId":11,"ackAt":"2026-08-22T08:21:10Z","eventTime":"2026-08-22T08:21:10Z","resourceId":"cmd_0123456789abcdef"}
```

Go 服务定义的事件类型：

- `telemetry.updated`
- `alert.created`
- `alert.recovered`
- `device.status.changed`
- `command.result`

事件按当前用户隔离，不会把其他用户的数据推送到本连接。

## 8. 会话数据管理（Go 侧）

本节只描述 Go 服务对会话和消息记录的管理，不涉及回答生成过程。

### 8.1 创建会话

`POST /api/v1/ai/sessions`

请求中的 `plotId` 可选：

```json
{
  "plotId": 11
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": "chat_0123456789abcdef",
    "userId": 7,
    "plotId": 11,
    "status": "ACTIVE",
    "createdAt": "2026-08-22T08:30:00Z",
    "updatedAt": "2026-08-22T08:30:00Z"
  }
}
```

### 8.2 查询会话消息

`GET /api/v1/ai/sessions/{sessionId}/messages?page=1&pageSize=20`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "items": [
      {
        "id": 501,
        "sessionId": "chat_0123456789abcdef",
        "role": "ASSISTANT",
        "content": "建议继续观察当前湿度变化。",
        "citations": [],
        "plotId": 11,
        "modelVersion": "model-v1",
        "traceId": "trace-001",
        "createdAt": "2026-08-22T08:31:00Z"
      }
    ],
    "page": 1,
    "pageSize": 20,
    "total": 1
  }
}
```

### 8.3 关闭会话

`POST /api/v1/ai/sessions/{sessionId}/close`

关闭操作幂等。

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "sessionId": "chat_0123456789abcdef",
    "status": "CLOSED"
  }
}
```

### 8.4 Python 智能体回写会话消息

`POST /api/v1/agent/sessions/{sessionId}/messages`

该接口供 Python `agent-service` 在完成一轮问答后，将用户问题或模型回答写入 Go 服务。Python 透传当前用户的访问令牌，Go 根据令牌校验会话归属；会话不存在、不属于当前用户或已经关闭时，不允许写入。

请求头：

```text
Authorization: Bearer <accessToken>
Content-Type: application/json
X-Trace-Id: trace-001
```

其中 `X-Trace-Id` 可选；请求体未提供 `trace_id` 时，Go 使用该请求头记录链路 ID。

路径参数：

| 参数 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `sessionId` | string | 是 | 已由 `POST /api/v1/ai/sessions` 创建的会话 ID，最长 64 个字符 |

请求 JSON：

```json
{
  "role": "assistant",
  "content": "根据当前土壤湿度，建议继续观察，暂不灌溉。",
  "citations": [
    {
      "docId": "61",
      "title": "番茄灌溉规范",
      "version": 1
    }
  ],
  "plot_id": "11",
  "model_version": "deepseek-v4-flash",
  "trace_id": "trace-001"
}
```

请求字段：

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| `role` | string | 是 | 消息角色，不区分大小写；允许 `USER`、`ASSISTANT`、`SYSTEM`、`TOOL` |
| `content` | string | 是 | 消息正文；去除首尾空白后不能为空，最大 100000 个字符 |
| `citations` | array/object/null | 否 | 模型回答引用的结构化 JSON，最大 64 KiB |
| `plot_id` | string/null | 否 | Python 使用的 snake_case 字段；值必须是十进制正整数形式，如 `"11"`，并且必须与会话绑定地块一致 |
| `model_version` | string | 否 | Python 使用的 snake_case 字段；模型名称或版本，最长 64 个字符 |
| `trace_id` | string | 否 | Python 使用的 snake_case 字段；链路 ID，最长 64 个字符；优先级高于 `X-Trace-Id` 请求头 |

成功响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "messageId": 501,
    "createdAt": "2026-08-22T08:31:00Z"
  }
}
```

失败响应示例：

```json
{
  "code": 40001,
  "message": "参数错误：plot_id 必须是正整数",
  "data": null
}
```

可能返回的业务错误：

| HTTP 状态 | code | 场景 |
| --- | --- | --- |
| `400` | `40001` | JSON 无法解析、缺少 `role/content`、字段超长、引用 JSON 无效、地块 ID 非法或与会话不一致 |
| `401` | `40101` | 未携带用户访问令牌、令牌无效或账户已停用 |
| `404` | `40404` | 会话不存在，或者会话不属于当前用户 |
| `409` | `40903` | 会话已经关闭，禁止继续写入消息 |

## 9. 知识文档管理（Go 侧）

### 9.1 查询已发布文档

`GET /api/v1/knowledge/docs?category=irrigation`

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": [
    {
      "id": 61,
      "title": "番茄灌溉规范",
      "category": "irrigation",
      "version": 1,
      "source": "农业技术手册",
      "downloadUrl": "<temporary-download-url>",
      "publishedAt": "2026-08-22T09:00:00Z",
      "updatedAt": "2026-08-22T09:00:00Z"
    }
  ]
}
```

### 9.2 上传文档

`POST /api/v1/knowledge/docs`

仅 `SYSTEM_ADMIN` 可调用。该请求实际使用文件表单；业务字段等价表示如下：

```json
{
  "file": "<binary-file>",
  "title": "番茄灌溉规范",
  "category": "irrigation",
  "source": "农业技术手册",
  "version": 1
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 61,
    "title": "番茄灌溉规范",
    "category": "irrigation",
    "objectKey": "knowledge/20687/abcdef-manual.pdf",
    "fileHash": "<sha256>",
    "source": "农业技术手册",
    "status": "DRAFT",
    "version": 1,
    "uploadedBy": 1,
    "createdAt": "2026-08-22T08:50:00Z",
    "updatedAt": "2026-08-22T08:50:00Z"
  }
}
```

### 9.3 审批、发布与归档

仅 `SYSTEM_ADMIN` 可调用，请求体均为空：

- `POST /api/v1/knowledge/docs/{docId}/approve`：`DRAFT` → `APPROVED`
- `POST /api/v1/knowledge/docs/{docId}/publish`：`APPROVED` → `ACTIVE`
- `POST /api/v1/knowledge/docs/{docId}/archive`：`ACTIVE` → `ARCHIVED`

同标题的新版本发布后，旧的活动版本会自动归档。

响应示例：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 61,
    "title": "番茄灌溉规范",
    "category": "irrigation",
    "objectKey": "knowledge/20687/abcdef-manual.pdf",
    "fileHash": "<sha256>",
    "source": "农业技术手册",
    "status": "ACTIVE",
    "version": 1,
    "uploadedBy": 1,
    "approvedBy": 1,
    "publishedAt": "2026-08-22T09:00:00Z",
    "createdAt": "2026-08-22T08:50:00Z",
    "updatedAt": "2026-08-22T09:00:00Z"
  }
}
```

## 10. Go 服务内部接口

内部接口不面向浏览器或普通用户，使用 `X-Internal-Service-Key` 请求头。

### 10.1 写入会话消息

`POST /internal/agent/sessions/{sessionId}/messages`

请求：

```json
{
  "role": "ASSISTANT",
  "content": "建议继续观察当前湿度变化。",
  "citations": [],
  "plotId": 11,
  "modelVersion": "model-v1",
  "traceId": "trace-001"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "messageId": 501,
    "createdAt": "2026-08-22T08:31:00Z"
  }
}
```

### 10.2 触发告警

`POST /internal/alerts/trigger`

关键逻辑：同一规则已有未结束告警时不会重复创建；首次创建会同时生成站内通知，并记录待后续处理的可靠事件。

请求：

```json
{
  "ruleId": 2,
  "deviceId": 31,
  "triggerValue": 26.5,
  "triggeredAt": "2026-08-22T08:20:00Z",
  "traceId": "trace-alert-001"
}
```

响应：

```json
{
  "code": 0,
  "message": "OK",
  "data": {
    "id": 101,
    "status": "ACTIVE",
    "created": true,
    "triggeredAt": "2026-08-22T08:20:00Z"
  }
}
```

重复触发时 `created` 为 `false`，并返回已有告警 ID。

## 11. 健康检查

无需认证：

- `GET /actuator/health/liveness`：进程是否存活。
- `GET /actuator/health/readiness`：服务是否可以接收业务请求。
- `GET /actuator/health`：与 readiness 相同。

正常响应：

```json
{
  "status": "UP"
}
```

不可用响应：

```json
{
  "status": "DOWN"
}
```
