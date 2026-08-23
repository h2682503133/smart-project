"""tool-service：9 个工具实现（集中管理）+ JSON Schema 定义。

工具清单：
  1. get_user_plots        田块查询入口（Go /plots，JWT 权限）
  2. get_latest_telemetry  地块最新遥测（Go /plots/{id}/telemetry/latest）
  3. get_telemetry_history 历史趋势（Go /telemetry/history）
  4. get_farm_overview     总览（Go /dashboard/overview）
  5. get_active_alerts     活动告警（Go /alerts）
  6. get_alert_rules       阈值规则（Go /plots/{id}/thresholds）
  7. get_device_status     设备状态（Go /devices）
  8. search_knowledge      知识检索（Milvus 直连，ACTIVE 由 Go 保证）
  9. get_document_content  文档原文（MinIO 直连）

mock_go=true 时（config.yaml tool.mock_go），现场/排查工具返回契约示例数据，
便于 Go 未就绪时本地联调。
"""
from __future__ import annotations

import sys
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from shared.config import get_config  # noqa: E402
from shared.go_client import get_go_client  # noqa: E402

# ============================================================
# mock 开关与示例数据
# ============================================================

_MOCK_PLOTS = [
    {"id": "plot_a1", "code": "A1", "name": "西侧棚", "cropName": "番茄",
     "area": 300.0, "soilMoisture": 29.1, "temperature": 26.1, "deviceStatus": "ONLINE"},
    {"id": "plot_a3", "code": "A3", "name": "东侧棚", "cropName": "番茄",
     "area": 320.5, "soilMoisture": 27.8, "temperature": 26.8, "deviceStatus": "ONLINE"},
]


def _mock_enabled() -> bool:
    return bool(get_config("tool").get("mock_go", False))


def _plots_full_threshold() -> int:
    return int(get_config("tool").get("plots_full_threshold", 5))


def _top_k() -> int:
    return int(get_config("tool").get("top_k", 5))


# ============================================================
# 1. get_user_plots：田块查询入口
# ============================================================

GET_USER_PLOTS_SCHEMA = {
    "farm_id": {"type": "string", "description": "按农场过滤（可选）"},
    "crop_type": {"type": "string", "description": "按作物过滤（可选）"},
    "keyword": {"type": "string", "description": "田块名称关键词（可选）"},
    "limit": {"type": "integer", "description": "返回条数上限"},
    "offset": {"type": "integer"},
}


def get_user_plots(authorization: str, args: dict) -> dict:
    """返回用户权限范围内的田块列表；田块少（≤ 阈值）全量返回，多则分页+total。"""
    if _mock_enabled():
        return _mock_plots(args)

    query = {
        k: v for k, v in args.items()
        if k in ("farm_id", "crop_type", "keyword", "limit", "offset") and v is not None
    }
    plots = get_go_client().get_plots(authorization, **query)
    if not isinstance(plots, list):
        return {"ok": False, "error": "Go 返回格式异常"}
    return {"ok": True, "data": {"total": len(plots), "plots": plots}}


def _mock_plots(args: dict) -> dict:
    plots = _MOCK_PLOTS
    keyword = args.get("keyword")
    if keyword:
        plots = [p for p in plots if keyword in p["name"]]
    return {"ok": True, "data": {"total": len(plots), "plots": plots}}


# ============================================================
# 2. get_latest_telemetry：地块最新遥测
# ============================================================

GET_LATEST_TELEMETRY_SCHEMA = {
    "plot_id": {"type": "string", "description": "地块 ID（必填）"},
    "metrics": {"type": "array", "items": {"type": "string"},
                "description": "指标列表，如 soilMoisture/temperature；空=全部"},
}


def get_latest_telemetry(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {
            "plotId": args.get("plot_id"),
            "sampleTime": "2026-08-22T08:21:00+08:00",
            "metrics": {"soilMoisture": {"value": 27.8, "unit": "%"},
                        "temperature": {"value": 26.8, "unit": "°C"}},
        }}
    metrics = ",".join(args.get("metrics") or [])
    data = get_go_client().get_plot_telemetry_latest(
        authorization, args["plot_id"], metrics=metrics
    )
    return {"ok": True, "data": data}


# ============================================================
# 3. get_telemetry_history：历史趋势
# ============================================================

GET_TELEMETRY_HISTORY_SCHEMA = {
    "plot_id": {"type": "string"},
    "metric": {"type": "string", "enum": ["soilMoisture", "temperature"]},
    "range": {"type": "string", "enum": ["1h", "24h", "7d", "30d"]},
    "interval": {"type": "string", "description": "聚合粒度，如 5m/1h/1d"},
}


def get_telemetry_history(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {
            "plotId": args.get("plot_id"), "metric": args.get("metric"), "unit": "%",
            "points": [{"time": "2026-08-16T00:00:00+08:00", "avg": 34.2, "min": 28.5, "max": 39.1}],
        }}
    data = get_go_client().get_telemetry_history(authorization, **args)
    return {"ok": True, "data": data}


# ============================================================
# 4. get_farm_overview：总览
# ============================================================

GET_FARM_OVERVIEW_SCHEMA = {
    "farm_id": {"type": "string"},
}


def get_farm_overview(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {
            "farmId": args.get("farm_id"), "farmName": "张家湾温室群",
            "avgSoilMoisture": {"value": 28.9, "unit": "%"},
            "deviceOnline": {"online": 12, "total": 13, "offline": 1},
            "alerts": {"active": 2},
        }}
    data = get_go_client().get_dashboard_overview(authorization, farmId=args.get("farm_id"))
    return {"ok": True, "data": data}


# ============================================================
# 5. get_active_alerts：活动告警
# ============================================================

GET_ACTIVE_ALERTS_SCHEMA = {
    "plot_id": {"type": "string", "description": "按地块过滤（可选）"},
    "status": {"type": "string", "description": "ACTIVE/CONFIRMED 等（可选）"},
}


def get_active_alerts(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": [{
            "id": "alert_001", "plotId": args.get("plot_id") or "plot_a3", "plotCode": "A3",
            "metric": "soilMoisture", "level": "MEDIUM", "status": "ACTIVE",
            "title": "A3 地块湿度偏低", "currentValue": 28.6, "thresholdValue": 30,
            "startedAt": "2026-08-22T08:20:00+08:00",
        }]}
    data = get_go_client().get_alerts(authorization, **{k: v for k, v in args.items() if v})
    return {"ok": True, "data": data}


# ============================================================
# 6. get_alert_rules：阈值规则
# ============================================================

GET_ALERT_RULES_SCHEMA = {
    "plot_id": {"type": "string"},
}


def get_alert_rules(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": [{
            "id": "thr_001", "plotId": args.get("plot_id"), "metric": "soilMoisture",
            "operator": "LT", "value": 28, "unit": "%", "durationSeconds": 300,
            "level": "MEDIUM", "enabled": True,
        }]}
    data = get_go_client().get_thresholds(authorization, args["plot_id"])
    return {"ok": True, "data": data}


# ============================================================
# 7. get_device_status：设备状态
# ============================================================

GET_DEVICE_STATUS_SCHEMA = {
    "device_id": {"type": "string"},
    "plot_id": {"type": "string", "description": "按地块查设备列表（可选）"},
}


def get_device_status(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {
            "deviceId": args.get("device_id") or "dev_gateway_01",
            "status": "ONLINE", "battery": 87, "signal": 76,
            "lastSeenAt": "2026-08-22T08:21:00+08:00",
        }}
    query = {"plotId": args["plot_id"]} if args.get("plot_id") else {}
    data = get_go_client().get_devices(authorization, **query)
    return {"ok": True, "data": data}


# ============================================================
# 8. search_knowledge：知识检索（Milvus 直连）
# ============================================================

SEARCH_KNOWLEDGE_SCHEMA = {
    "query": {"type": "string", "description": "检索问题"},
    "category": {"type": "string", "description": "分类过滤（可选）"},
    "top_k": {"type": "integer"},
}


def search_knowledge(authorization: str, args: dict) -> dict:
    """Milvus 知识 collection 检索（ACTIVE 可用性由 Go 保证，无需状态过滤）。"""
    if _mock_enabled():
        return {"ok": True, "data": [
            {"docId": "doc_001", "title": "番茄温室灌溉建议", "version": 2,
             "source": "knowledge/tomato-irrigation.pdf", "score": 0.91,
             "content": "见干见湿原则：土壤湿度低于 30% 时建议灌溉……"},
        ]}
    from shared.milvus_client import search_knowledge as _milvus_search  # 延迟导入
    top_k = args.get("top_k") or _top_k()
    results = _milvus_search(args["query"], category=args.get("category"), top_k=top_k)
    return {"ok": True, "data": results}


# ============================================================
# 9. get_document_content：文档原文（MinIO 直连）
# ============================================================

GET_DOCUMENT_CONTENT_SCHEMA = {
    "doc_id": {"type": "string", "description": "文档 ID（经 Go 清单映射 file_url）"},
    "file_url": {"type": "string", "description": "MinIO 对象 key（可直接拉取）"},
}


def get_document_content(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {"docId": args.get("doc_id") or args.get("file_url"), "content": "（mock）番茄灌溉手册原文……"}}
    from shared.minio_client import get_document  # 延迟导入

    object_key = args.get("file_url")
    if not object_key:
        # doc_id → 从 Go 可用文档清单取 file_url
        docs = get_go_client().get_knowledge_docs(authorization)
        matched = next((d for d in docs if str(d.get("id")) == str(args["doc_id"])), None)
        if not matched or not matched.get("file_url"):
            return {"ok": False, "error": f"未找到文档 {args.get('doc_id')}"}
        object_key = matched["file_url"]
    content = get_document(object_key)
    return {"ok": True, "data": {"docId": args.get("doc_id"), "fileUrl": object_key, "content": content}}


# ============================================================
# 10. get_irrigation_status：灌溉状态
# ============================================================

GET_IRRIGATION_STATUS_SCHEMA = {
    "plot_id": {"type": "string", "description": "地块 ID（必填）"},
}


def get_irrigation_status(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {
            "plotId": args.get("plot_id"), "valveDeviceId": "dev_valve_01",
            "state": "OFF", "mode": "MANUAL", "remainingSeconds": 0, "maxSeconds": 900,
        }}
    data = get_go_client().get_irrigation_status(authorization, args["plot_id"])
    return {"ok": True, "data": data}


# ============================================================
# 11. send_irrigation_command：下发灌溉命令（控制类，复用 Go §6.2）
# ============================================================

SEND_IRRIGATION_COMMAND_SCHEMA = {
    "plot_id": {"type": "string", "description": "地块 ID（必填）"},
    "action": {"type": "string", "enum": ["OPEN", "CLOSE"], "description": "OPEN 开启 / CLOSE 关闭"},
    "duration_seconds": {"type": "integer", "minimum": 60, "maximum": 1800,
                         "description": "开启时长（OPEN 时必填，60-1800 秒）"},
    "reason": {"type": "string", "description": "命令原因（审计用，agent 填建议依据）"},
}


def send_irrigation_command(authorization: str, args: dict) -> dict:
    """下发灌溉命令（JWT 权限由 Go 校验；agent 侧生成幂等键防重复）。"""
    import uuid

    if _mock_enabled():
        return {"ok": True, "data": {
            "commandId": f"cmd_mock_{uuid.uuid4().hex[:10]}",
            "plotId": args.get("plot_id"), "action": args.get("action"),
            "status": "PENDING", "createdAt": "2026-08-22T08:21:10+08:00",
        }}
    body = {
        "action": args["action"],
        "reason": args.get("reason", "agent 建议"),
        "idempotencyKey": uuid.uuid4().hex,
    }
    if args.get("duration_seconds"):
        body["durationSeconds"] = args["duration_seconds"]
    data = get_go_client().post_irrigation_command(authorization, args["plot_id"], body)
    return {"ok": True, "data": data}


# ============================================================
# 12. get_command_result：命令执行结果（复用 Go §6.3）
# ============================================================

GET_COMMAND_RESULT_SCHEMA = {
    "command_id": {"type": "string", "description": "命令 ID（必填）"},
}


def get_command_result(authorization: str, args: dict) -> dict:
    if _mock_enabled():
        return {"ok": True, "data": {
            "id": args.get("command_id"), "plotId": "plot_a3",
            "action": "OPEN", "status": "SUCCEEDED",
            "ackPayload": {"state": "ON", "remainingSeconds": 600},
            "createdAt": "2026-08-22T08:21:10+08:00", "ackAt": "2026-08-22T08:21:12+08:00",
        }}
    data = get_go_client().get_command_result(authorization, args["command_id"])
    return {"ok": True, "data": data}


# ============================================================
# 注册表（启动时调用）
# ============================================================

def register_all() -> None:
    from registry import get_registry

    reg = get_registry()
    reg.register("get_user_plots", "1.0", "获取当前用户权限范围内的田块列表", GET_USER_PLOTS_SCHEMA, [], get_user_plots)
    reg.register("get_latest_telemetry", "1.0", "查询地块最新遥测值（带采样时间）", GET_LATEST_TELEMETRY_SCHEMA, ["plot_id"], get_latest_telemetry)
    reg.register("get_telemetry_history", "1.0", "查询历史趋势（支持聚合粒度）", GET_TELEMETRY_HISTORY_SCHEMA, ["plot_id", "metric"], get_telemetry_history)
    reg.register("get_farm_overview", "1.0", "查询农场/地块总览汇总", GET_FARM_OVERVIEW_SCHEMA, [], get_farm_overview)
    reg.register("get_active_alerts", "1.0", "查询活动告警", GET_ACTIVE_ALERTS_SCHEMA, [], get_active_alerts)
    reg.register("get_alert_rules", "1.0", "查询地块阈值规则（含 operator/时长）", GET_ALERT_RULES_SCHEMA, ["plot_id"], get_alert_rules)
    reg.register("get_device_status", "1.0", "查询设备在线状态", GET_DEVICE_STATUS_SCHEMA, [], get_device_status)
    reg.register("search_knowledge", "1.0", "检索农业知识库（RAG）", SEARCH_KNOWLEDGE_SCHEMA, ["query"], search_knowledge)
    reg.register("get_document_content", "1.0", "获取知识文档原文片段", GET_DOCUMENT_CONTENT_SCHEMA, [], get_document_content)
    # 控制类（组内已允许 agent 直接发送命令；复用 Go 已有控制接口）
    reg.register("get_irrigation_status", "1.0", "查询地块灌溉状态（ON/OFF、剩余时长）", GET_IRRIGATION_STATUS_SCHEMA, ["plot_id"], get_irrigation_status)
    reg.register("send_irrigation_command", "1.0", "下发灌溉命令（OPEN/CLOSE，需用户明确意图）", SEND_IRRIGATION_COMMAND_SCHEMA, ["plot_id", "action"], send_irrigation_command)
    reg.register("get_command_result", "1.0", "查询命令执行结果（SUCCEEDED/FAILED/TIMEOUT）", GET_COMMAND_RESULT_SCHEMA, ["command_id"], get_command_result)
