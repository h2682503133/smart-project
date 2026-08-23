# BearPi-HM Nano MQTT 硬件接入说明

本文提供给 BearPi-HM Nano 固件开发人员，用于将温度、土壤湿度和光照数据通过 MQTT 上报到智慧农业服务器。

## 1. 当前接入范围

当前服务端已实现遥测数据上报，设备只需要发布以下内容：

- 温度；
- 土壤湿度；
- 光照强度；
- 上述三个指标对应的设备侧告警标记。

当前版本不要求设备上报时间、在线状态、电量或信号强度。服务端以收到遥测消息的时间作为采样时间，并根据最近一次收到消息的时间判断设备在线状态。

## 2. 接入前准备

服务端人员需要向固件人员提供以下参数：

| 参数 | 示例 | 说明 |
| --- | --- | --- |
| MQTT Broker 地址 | `192.168.1.100` | EMQX 所在服务器的 IP 或域名 |
| MQTT 端口 | `1883` | 开发环境通常使用 1883；生产环境应使用 TLS 端口 |
| MQTT 用户名 | 由服务端提供 | 开发环境可能为空 |
| MQTT 密码 | 由服务端提供 | 开发环境可能为空 |
| `ownerId` | `7` | 设备所属用户 ID |
| `deviceSn` | `BEARPI-HM-NANO-001` | 设备序列号，必须与服务端绑定时填写的序列号完全一致 |

设备必须先在业务系统中完成绑定：

1. 使用设备序列号 `deviceSn` 将设备绑定到某个地块；
2. 获取该地块所属的 `ownerId`；
3. 将 `ownerId`、`deviceSn` 和 MQTT 连接参数写入设备配置；
4. 设备连接 Broker 后开始发布遥测。

如果设备未绑定、已解绑、`ownerId` 错误或 `deviceSn` 不一致，Broker 可能仍会接收消息，但业务服务器会拒绝该消息。

## 3. MQTT 连接要求

| 配置项 | 要求 |
| --- | --- |
| 协议版本 | MQTT 3.1.1 |
| Client ID | 每台设备必须唯一，建议 `bearpi-{deviceSn}` |
| Keep Alive | 建议 60 秒 |
| Clean Session | 建议 `true` |
| 遥测 QoS | `1` |
| Retain | `false` |
| Payload 编码 | UTF-8 JSON |

设备断网或 Broker 不可达时，应持续重连。建议重连间隔采用退避策略：1 秒、2 秒、4 秒、8 秒，最大不超过 30 秒。

生产环境如果启用 TLS，固件还需要烧录服务端提供的 CA 证书，并校验 Broker 域名。不要在生产网络中使用匿名、明文 MQTT。

## 4. 发布 Topic

遥测 Topic 格式：

```text
agri/{ownerId}/{deviceSn}/telemetry
```

示例：

```text
agri/7/BEARPI-HM-NANO-001/telemetry
```

注意事项：

- `agri`、`telemetry` 均为小写；
- Topic 中不能包含空格；
- `ownerId` 必须是大于 0 的整数；
- `deviceSn` 区分大小写，必须和服务端登记值一致；
- 一个 MQTT 消息只上传一组当前传感器数据。

## 5. Payload 字段

Payload 必须是一个 JSON 对象，且以下六个字段全部必填：

| 字段 | JSON 类型 | 单位 | 服务端约束 | 说明 |
| --- | --- | --- | --- | --- |
| `temperature` | number | ℃ | 必须是有效数字 | 当前温度 |
| `soilMoisture` | number | % | `0 <= value <= 100` | 当前土壤湿度百分比 |
| `light` | number | lx | `value >= 0` | 当前光照强度 |
| `temperatureWarning` | boolean | 无 | 只能为 `true` 或 `false` | 设备侧是否检测到温度告警 |
| `soilMoistureWarning` | boolean | 无 | 只能为 `true` 或 `false` | 设备侧是否检测到土壤湿度告警 |
| `lightWarning` | boolean | 无 | 只能为 `true` 或 `false` | 设备侧是否检测到光照告警 |

如果固件暂未实现本地阈值判断，三个 Warning 字段统一发送 `false`，但不能省略。

正确示例：

```json
{
  "temperature": 26.5,
  "soilMoisture": 48.0,
  "light": 920,
  "temperatureWarning": false,
  "soilMoistureWarning": false,
  "lightWarning": false
}
```

告警示例：

```json
{
  "temperature": 38.2,
  "soilMoisture": 16.4,
  "light": 1250,
  "temperatureWarning": true,
  "soilMoistureWarning": true,
  "lightWarning": false
}
```

## 6. 严格格式要求

服务端采用严格 JSON 校验。以下情况会被拒绝：

- 缺少任意一个必填字段；
- 字段名称大小写错误；
- 数值字段使用字符串，例如 `"temperature": "26.5"`；
- 布尔字段使用 `0`、`1` 或字符串，例如 `"lightWarning": "false"`；
- 土壤湿度小于 0 或大于 100；
- 光照值小于 0；
- JSON 后面拼接额外内容；
- 携带任何未定义字段。

不要在 Payload 中加入以下字段：

```json
{
  "ownerId": 7,
  "deviceId": 3,
  "deviceSn": "BEARPI-HM-NANO-001",
  "timestamp": 1710000000,
  "status": "ONLINE",
  "battery": 80,
  "signal": -60
}
```

这些身份和状态数据不由设备 Payload 决定。`ownerId` 和 `deviceSn` 从可信 Topic 获取，设备 ID、地块及绑定关系由服务端数据库解析。

## 7. 建议上报时机

当前服务端默认超过 2 分钟未收到遥测就把设备视为离线，因此建议：

- 正常运行时每 30 秒上报一次；
- 最长上报间隔不要超过 60 秒；
- 设备启动且传感器初始化成功后立即上报一次；
- 指标突变或 Warning 状态变化时立即补充上报一次；
- 传感器读取失败时不要使用 0 代替真实值，也不要发布格式不完整的数据。

服务端使用消息接收时间作为采样时间。设备离线期间不要积压并在重连后集中补发旧遥测，否则旧数据会被误认为当前数据。重连成功后只发送最新的一组传感器值。

## 8. 固件发送流程

```text
初始化网络
  -> 连接 MQTT Broker
  -> 等待连接成功
  -> 读取三个传感器
  -> 校验读数是否有效
  -> 计算三个 Warning 标记
  -> 生成严格 JSON
  -> QoS 1 发布到 telemetry Topic
  -> 等待下一采样周期
```

C 风格伪代码：

```c
void publish_telemetry(void)
{
    SensorData data = read_sensors();
    if (!data.valid) {
        log_error("sensor read failed");
        return;
    }

    char topic[160];
    snprintf(topic, sizeof(topic),
             "agri/%s/%s/telemetry", OWNER_ID, DEVICE_SN);

    char payload[320];
    snprintf(payload, sizeof(payload),
             "{\"temperature\":%.2f,"
             "\"soilMoisture\":%.2f,"
             "\"light\":%.2f,"
             "\"temperatureWarning\":%s,"
             "\"soilMoistureWarning\":%s,"
             "\"lightWarning\":%s}",
             data.temperature,
             data.soil_moisture,
             data.light,
             data.temperature_warning ? "true" : "false",
             data.soil_moisture_warning ? "true" : "false",
             data.light_warning ? "true" : "false");

    mqtt_publish(topic, payload, /* qos */ 1, /* retain */ false);
}
```

实际函数名需按 BearPi-HM Nano 固件所使用的 MQTT 库替换。

## 9. 发送成功的判断

使用 QoS 1 时，设备收到 PUBACK 只代表 Broker 已收到消息，不代表业务服务器一定已成功处理。

联调时应同时检查：

1. 设备日志显示 MQTT 已连接；
2. 发布 Topic 与分配值完全一致；
3. 设备收到 QoS 1 PUBACK；
4. EMQX 控制台能看到设备连接和消息；
5. 业务系统中对应地块的温度、土壤湿度和光照已更新；
6. 设备状态在一次有效遥测后显示为在线。

## 10. 软件侧联调命令

硬件上线前，可由服务端人员使用 MQTT 命令行模拟 BearPi 上报：

```bash
mosquitto_pub \
  -h 127.0.0.1 \
  -p 1883 \
  -q 1 \
  -t 'agri/7/BEARPI-HM-NANO-001/telemetry' \
  -m '{"temperature":26.5,"soilMoisture":48,"light":920,"temperatureWarning":false,"soilMoistureWarning":false,"lightWarning":false}'
```

如 Broker 已启用认证，需要增加 `-u 用户名 -P 密码`。测试前必须确保示例中的 `ownerId`、`deviceSn` 已在服务端完成有效绑定。

## 11. 当前未接入的消息

以下 Topic 属于后续扩展，当前硬件联调阶段不要依赖其业务处理结果：

- `agri/{ownerId}/{deviceSn}/heartbeat`；
- `agri/{ownerId}/{deviceSn}/command/ack`；
- `agri/{ownerId}/{deviceSn}/event`；
- 服务端下发的 `agri/{ownerId}/{deviceSn}/command`。

当前设备在线状态由遥测上报自动维持，不需要单独发送心跳包。

## 12. 硬件交付检查表

- [ ] `deviceSn` 可配置且每台设备唯一；
- [ ] MQTT Client ID 每台设备唯一；
- [ ] 支持 MQTT 3.1.1、QoS 1 和自动重连；
- [ ] Topic 使用正确的 `ownerId` 和 `deviceSn`；
- [ ] 六个 JSON 字段始终完整且类型正确；
- [ ] 正常上报间隔不超过 60 秒；
- [ ] 重连后只发送最新数据，不集中补发旧数据；
- [ ] 生产环境支持账号密码和 TLS 证书校验；
- [ ] 真机上报后，业务系统能看到遥测更新和设备在线。
