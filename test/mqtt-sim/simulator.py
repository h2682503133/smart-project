#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
MQTT 传感器 / 水泵模拟器 + 简易 Web UI
=======================================
功能:
  - 连接 MQTT broker, 按固定间隔发布遥测到 {prefix}/{owner_id}/{device_sn}/telemetry
  - 传感器: 在 UI 设置目标值, 每 tick 变化量 = 差距 * 变化系数(差距越大变化越快),
    并叠加随机扰动, 当前值逐渐向目标值靠近
  - 水泵:  开启后每 tick 给土壤湿度增加"设定增量 + 随机扰动"
  - payload 与 Go 后端严格一致:
    {temperature, soilMoisture, light, temperatureWarning, soilMoistureWarning, lightWarning}

用法:
  python simulator.py [--port 8090] [--host 0.0.0.0]
然后浏览器打开 http://127.0.0.1:8090
"""
import argparse
import json
import random
import os
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import urlparse

try:
    sys.stdout.reconfigure(encoding="utf-8", errors="replace")
except Exception:
    pass

try:
    from paho.mqtt.client import CallbackAPIVersion, Client as MqttClient
    PAHO_V2 = True
except ImportError:  # paho-mqtt 1.x
    from paho.mqtt.client import Client as MqttClient
    PAHO_V2 = False

# ---------------------------------------------------------------- 全局状态
LOCK = threading.Lock()

# 现场状态文件：保留每个设备组的配置与当前读数，重启后恢复（可保留现场）
STATE_FILE = os.path.join(os.path.dirname(os.path.abspath(__file__)), "simulator_state.json")

# 组内 4 页：每页 = 一台设备，可独立设置 device_sn。
# 页类型：soilMoisture（土壤湿度）/ temperature（温度）/ light（光照）/ pump（水泵/阀门）
PAGES = ("soilMoisture", "temperature", "light", "pump")

# 各页设备默认序列号
PAGE_SN_DEFAULTS = {
    "soilMoisture": "SN-BEARPI-001",
    "temperature": "SN-TEMP-001",
    "light": "SN-LIGHT-001",
    "pump": "SN-VALVE-CODE-001",
}

PAGE_NAMES = {
    "soilMoisture": "土壤湿度",
    "temperature": "温度",
    "light": "光照",
    "pump": "水泵 (增加土壤湿度)",
}

# 每页设备的默认配置（单参数传感器：只上报自己指标，对齐文档 10 §5）
PAGE_SENSOR_DEFAULTS = {
    "soilMoisture": {"unit": "%",  "current": 45.0, "target": 45.0, "factor": 0.08, "noise": 0.5,
                     "min": 20, "max": 80, "phys_min": 0, "phys_max": 100},
    "temperature": {"unit": "°C", "current": 25.0, "target": 25.0, "factor": 0.08, "noise": 0.2,
                    "min": 5, "max": 40, "phys_min": -20, "phys_max": 80},
    "light":       {"unit": "lx", "current": 800.0, "target": 800.0, "factor": 0.08, "noise": 5.0,
                    "min": 200, "max": 1000, "phys_min": 0, "phys_max": 3000},
}

PUMP_DEFAULTS = {"enabled": False, "delta": 2.0, "noise": 0.5}

def _copy_page(page, device_sn):
    """构造一页（一台设备）：传感器页带自己的指标配置，水泵页带水泵配置。"""
    if page == "pump":
        return {"device_sn": device_sn, "pump": dict(PUMP_DEFAULTS)}
    return {"device_sn": device_sn, "sensor": dict(PAGE_SENSOR_DEFAULTS[page])}

def _new_group():
    """一组 = 4 页（湿度/温度/光照/水泵）的一份完整复制，每页可设置独立 device_sn。"""
    return {
        "soilMoisture": _copy_page("soilMoisture", "SN-BEARPI-001"),
        "temperature":  _copy_page("temperature", "SN-TEMP-001"),
        "light":        _copy_page("light", "SN-LIGHT-001"),
        "pump":         _copy_page("pump", "SN-VALVE-CODE-001"),
    }

# 设备组字典：{group_id: {page: {device_sn, sensor|pump}}}
# 组内每个页（每台设备）可以设置不同的 device_sn，各自向 {prefix}/{ownerId}/{deviceSn}/telemetry
# 发布只含自身指标的 payload（单参数传感器），模拟多台独立设备。
GROUPS = {"1": _new_group()}

def _default_group_id():
    return next(iter(GROUPS)) if GROUPS else None

def _group(gid):
    return GROUPS.get(str(gid))

def _page_dev(gid, page):
    g = _group(gid)
    if g is None or page not in g:
        return None
    return g[page]

def _find_by_sn(device_sn):
    """按设备序列号找到 (group_id, page, page_dev)；供命令/阈值下发定位到对应页设备。"""
    for gid, g in GROUPS.items():
        for page, dev in g.items():
            if dev.get("device_sn") == device_sn:
                return gid, page, dev
    return None, None, None

# 后端 MQTT 命令日志: 最近 20 条
COMMANDS = []

CFG = {
    "broker": "tcp://127.0.0.1:1883",
    "prefix": "agri",
    "owner_id": "2",
    "interval_ms": 1000,            # 内部采样频率：1 秒模拟一次传感器读数
    "publish_interval_ms": 30000,   # 上报频率：30 秒 publish 一次（对齐文档 10 §7）
    "connected": False,
    "running": False,
}

# 每设备组每页（传感器）最近 N 个值, 供 UI 画曲线
HISTORY = {gid: {page: [] for page in PAGES} for gid in GROUPS}
HISTORY_LIMIT = 150

STATE = {
    "publish_count": 0,
    "last_payload": None,
    "last_error": None,
    "last_ts": None,
}

# ---------------------------------------------------------------- 保留现场
def _state_payload():
    """可持久化的现场数据：全局配置（不含运行态）+ 各设备组页配置与当前读数。"""
    with LOCK:
        return {
            "version": 3,
            "config": {k: CFG[k] for k in ("broker", "prefix", "owner_id", "interval_ms", "publish_interval_ms")},
            "groups": {
                gid: {
                    page: dict(dev)
                    for page, dev in g.items()
                }
                for gid, g in GROUPS.items()
            },
        }

def save_state():
    """保存现场（配置 + 当前读数）到 STATE_FILE，供重启恢复。"""
    try:
        with open(STATE_FILE, "w", encoding="utf-8") as f:
            json.dump(_state_payload(), f, ensure_ascii=False, indent=2)
    except Exception as e:
        print(f"[state] save failed: {e}", flush=True)

def load_state():
    """启动时加载现场：文件存在则恢复各设备组页配置与读数，保留上次现场。"""
    global GROUPS
    try:
        if not os.path.exists(STATE_FILE):
            return
        with open(STATE_FILE, "r", encoding="utf-8") as f:
            data = json.load(f)
        if not isinstance(data, dict) or not data.get("groups"):
            return
        with LOCK:
            loaded = {}
            for gid, g in data["groups"].items():
                if not isinstance(g, dict):
                    continue
                group = {}
                for page in PAGES:
                    src = g.get(page)
                    if not isinstance(src, dict):
                        src = {}
                    if page == "pump":
                        group[page] = {
                            "device_sn": str(src.get("device_sn") or "SN-VALVE-CODE-001"),
                            "pump": {k: (_to_bool(v) if k == "enabled" else float(v))
                                     for k, v in {"enabled": False, "delta": 2.0, "noise": 0.5,
                                                  **dict(src.get("pump") or {})}.items()},
                        }
                    else:
                        merged = dict(PAGE_SENSOR_DEFAULTS[page])
                        s = src.get("sensor") or {}
                        for k in ("current", "target", "factor", "noise", "min", "max"):
                            if k in s and s[k] is not None:
                                merged[k] = float(s[k])
                        group[page] = {
                            "device_sn": str(src.get("device_sn") or PAGE_SN_DEFAULTS[page]),
                            "sensor": merged,
                        }
                loaded[str(gid)] = group
            if loaded:
                GROUPS = loaded
                for k in ("broker", "prefix", "owner_id", "interval_ms", "publish_interval_ms"):
                    if k in data.get("config", {}):
                        CFG[k] = data["config"][k]
                print(f"[state] restored {len(loaded)} groups from {STATE_FILE}", flush=True)
    except Exception as e:
        print(f"[state] load failed: {e}", flush=True)

_client = None          # paho client
_client_broker = None   # 当前 client 对应的 broker
_client_prefix = None   # 当前 client 订阅命令的 prefix
_stop_event = threading.Event()
_tick_thread = None

# 设备端已应用的阈值配置版本（Go 版本化下发：只应用更高版本）
_LOCAL_CFG_VERSION: dict[str, int] = {}

# ---------------------------------------------------------------- MQTT
def _on_connect(client, userdata, flags, rc, properties=None):
    if hasattr(rc, "is_failure"):
        ok = not rc.is_failure
    else:
        ok = (rc == 0)
    with LOCK:
        CFG["connected"] = ok
        if not ok:
            STATE["last_error"] = f"MQTT 连接被拒绝: {rc}"
    if ok:
        # 订阅命令下发 topic: {prefix}/+/+/command
        with LOCK:
            sub_cmd = f"{CFG['prefix']}/+/+/command"
            sub_thr = f"{CFG['prefix']}/+/+/config/thresholds/#"
        client.subscribe(sub_cmd, 1)
        client.subscribe(sub_thr, 1)
        print(f"[mqtt] subscribed {sub_cmd}", flush=True)
        print(f"[mqtt] subscribed {sub_thr}", flush=True)
    print(f"[mqtt] connected={ok}", flush=True)

def _on_disconnect(client, userdata, rc, properties=None):
    with LOCK:
        CFG["connected"] = False
        if rc != 0:
            STATE["last_error"] = f"MQTT 连接断开 (rc={rc})"
    print(f"[mqtt] disconnected rc={rc}", flush=True)

def _on_message(client, userdata, msg):
    """收到后端下发的设备消息:
    - {prefix}/{owner}/{deviceSn}/command                         灌溉 OPEN/CLOSE（兼容 SET_THRESHOLD 旧命令）
    - {prefix}/{owner}/{deviceSn}/config/thresholds/v/{version}   版本化阈值完整快照（Go 新契约）
    """
    try:
        parts = msg.topic.split("/")
        # 版本化阈值配置下发（7 段: prefix/owner/sn/config/thresholds/v/version）
        if len(parts) == 7 and parts[3] == "config" and parts[4] == "thresholds" and parts[5] == "v":
            _handle_threshold_config(client, parts, msg.payload or b"{}")
            return
        if len(parts) != 4 or parts[3] != "command":
            return
        owner_id, device_sn = parts[1], parts[2]
        payload = json.loads(msg.payload or b"{}")
        action = str(payload.get("action", "")).upper()
        with LOCK:
            # 不校验 owner（订阅 agri/+/+/command 通配）：命令按 device_sn 定位页设备处理
            gid, page, dev = _find_by_sn(device_sn)
            if dev is None:
                print(f"[cmd] no device owns {device_sn}: {msg.topic}", flush=True)
                return
        if action in ("OPEN", "IRRIGATION_ON"):
            with LOCK:
                if page == "pump":
                    dev["pump"]["enabled"] = True
                else:
                    # 传感器页收到开泵命令：作用于本组水泵页
                    GROUPS[gid]["pump"]["pump"]["enabled"] = True
            state_str = "开"
        elif action in ("CLOSE", "IRRIGATION_OFF"):
            with LOCK:
                if page == "pump":
                    dev["pump"]["enabled"] = False
                else:
                    GROUPS[gid]["pump"]["pump"]["enabled"] = False
            state_str = "关"
        elif action in ("SET_THRESHOLD", "SYNC_THRESHOLD"):
            # 云端改库后同步设备端阈值：更新该页设备传感器 min/max，告警判断立即用新阈值
            thresholds = payload.get("thresholds") or {}
            with LOCK:
                applied = []
                for metric, t in thresholds.items():
                    if page == "pump" or metric != page:
                        continue
                    if not isinstance(t, dict):
                        continue
                    if t.get("min") is not None:
                        dev["sensor"]["min"] = float(t["min"])
                    if t.get("max") is not None:
                        dev["sensor"]["max"] = float(t["max"])
                    applied.append(f"{metric}({dev['sensor']['min']}-{dev['sensor']['max']})")
            state_str = "阈值"
            if not applied:
                print(f"[cmd] SET_THRESHOLD 无有效字段: {msg.topic}", flush=True)
                return
            print(f"[cmd] 组{gid}/{page} 阈值同步: {', '.join(applied)} ({payload.get('commandId')})", flush=True)
        else:
            print(f"[cmd] unknown action: {action}", flush=True)
            return
        entry = {
            "ts": time.time(),
            "commandId": payload.get("commandId"),
            "action": action,
            "state": state_str,
            "mode": payload.get("mode"),
            "reason": payload.get("reason"),
            "durationSeconds": payload.get("durationSeconds"),
            "topic": msg.topic,
            "deviceSn": device_sn,
            "groupId": gid,
            "page": page,
        }
        with LOCK:
            COMMANDS.insert(0, entry)
            del COMMANDS[20:]
            STATE["last_command"] = entry
        # 设备回执 ack (Go 端暂未消费, 模拟真实设备行为)
        try:
            ack = {"commandId": payload.get("commandId"), "status": "ACKNOWLEDGED", "deviceSn": device_sn}
            client.publish(f"{parts[0]}/{owner_id}/{device_sn}/command/ack", json.dumps(ack), qos=1)
        except Exception:
            pass
        print(f"[cmd] 组{gid}/{page} {state_str}: {payload.get('commandId')} from {msg.topic}", flush=True)
    except Exception as e:
        print(f"[cmd] error: {e}", flush=True)

def _send_threshold_ack(client, prefix, owner_id, device_sn, message_id, version, status, reason=None):
    """设备端 ACK: {prefix}/{owner}/{sn}/config/thresholds/ack（Go 契约）。"""
    try:
        ack = {"messageId": message_id, "configVersion": version, "status": status}
        if reason:
            ack["reason"] = reason
        client.publish(f"{prefix}/{owner_id}/{device_sn}/config/thresholds/ack",
                       json.dumps(ack, ensure_ascii=False), qos=1)
    except Exception as e:
        print(f"[thr] ack send error: {e}", flush=True)


def _handle_threshold_config(client, parts, raw):
    """处理 Go 版本化阈值配置: {prefix}/{owner}/{sn}/config/thresholds/v/{version}
    只应用高于本地版本的完整快照，规则映射到传感器 min/max，应用后回 ACK。"""
    owner_id, device_sn = parts[1], parts[2]
    try:
        version = int(parts[6])
    except ValueError:
        return
    with LOCK:
        gid, page, dev = _find_by_sn(device_sn)
    try:
        cfg_msg = json.loads(raw)
        message_id = str(cfg_msg.get("messageId") or "").strip()
        rules = cfg_msg.get("rules") or []
        if not message_id or not isinstance(rules, list):
            raise ValueError("缺少 messageId 或 rules")
    except Exception as e:
        # payload 不可解析：无法回有效 ACK（ACK 需 messageId），记录日志
        print(f"[thr] {device_sn} 配置解析失败: {e}", flush=True)
        return
    # 版本单调：只应用高于本地版本的配置（避免 retained 旧消息/乱序回退）
    with LOCK:
        prev = _LOCAL_CFG_VERSION.get(device_sn, 0)
        if version <= prev:
            return
    applied = []
    if dev is not None and page != "pump":
        # 该设备是传感器页：只应用匹配自己指标的规则（单参数传感器）
        with LOCK:
            for rule in rules:
                if not rule.get("enabled", True):
                    continue
                if rule.get("metric") != page:
                    continue
                sensor = dev["sensor"]
                op = str(rule.get("operator", "")).upper()
                value = rule.get("value")
                if value is None:
                    continue
                if op in ("LT", "LTE"):
                    sensor["min"] = float(value)
                elif op in ("GT", "GTE"):
                    sensor["max"] = float(value)
                else:
                    continue
                applied.append(f"{page}[{sensor['min']}-{sensor['max']}]")
    with LOCK:
        _LOCAL_CFG_VERSION[device_sn] = version
        COMMANDS.insert(0, {
            "ts": time.time(),
            "commandId": message_id,
            "action": f"THRESHOLD v{version}",
            "state": "阈值",
            "mode": "CONFIG",
            "reason": ", ".join(applied) if applied else ("执行设备(无传感器)已应用" if page == "pump" else "无有效规则"),
            "topic": f"{parts[0]}/{owner_id}/{device_sn}/config/thresholds/v/{version}",
            "deviceSn": device_sn,
            "groupId": gid,
            "page": page,
        })
        del COMMANDS[20:]
    _send_threshold_ack(client, parts[0], owner_id, device_sn, message_id, version, "APPLIED")
    print(f"[thr] {device_sn} apply v{version}: {', '.join(applied) if applied else 'ack-only'}", flush=True)


def _parse_broker(url):
    """tcp://host:port 或 host:port → (host, port)"""
    url = url.strip()
    if "://" in url:
        url = url.split("://", 1)[1]
    host, port = url, 1883
    if ":" in url:
        host, _, port_s = url.rpartition(":")
        port = int(port_s)
    return host, port

def _new_client():
    if PAHO_V2:
        c = MqttClient(CallbackAPIVersion.VERSION2, client_id="mqtt-sim-ui-001")
    else:
        c = MqttClient(client_id="mqtt-sim-ui-001")
    c.on_connect = _on_connect
    c.on_disconnect = _on_disconnect
    c.on_message = _on_message
    c.reconnect_delay_set(min_delay=1, max_delay=10)
    return c

def _ensure_connected():
    """按当前 CFG 的 broker/prefix 建立连接(自动重建), 异步重连由 paho loop 负责."""
    global _client, _client_broker, _client_prefix
    with LOCK:
        broker = CFG["broker"]
        prefix = CFG["prefix"]
    if _client is not None and _client_broker == broker and _client_prefix == prefix and _client.is_connected():
        return
    if _client is not None:
        try:
            _client.disconnect()
            _client.loop_stop()
        except Exception:
            pass
        _client = None
    try:
        host, port = _parse_broker(broker)
        c = _new_client()
        c.connect_async(host, port, keepalive=30)
        c.loop_start()
        _client, _client_broker, _client_prefix = c, broker, prefix
    except Exception as e:
        with LOCK:
            STATE["last_error"] = f"MQTT 连接失败: {e}"
        print(f"[mqtt] connect failed: {e}", flush=True)

def _publish():
    """按组×页发布：每页设备只发自己指标的 payload（单参数传感器，对齐文档 10 §5）到自己的 topic。"""
    global _client
    with LOCK:
        broker = CFG["broker"]
        prefix = CFG["prefix"]
        owner = CFG["owner_id"]
    if _client is None or not _client.is_connected():
        with LOCK:
            STATE["last_error"] = "MQTT 未连接"
        return
    targets = []
    with LOCK:
        for gid, g in GROUPS.items():
            for page, dev in g.items():
                sn = dev.get("device_sn")
                if not sn:
                    continue
                if page == "pump":
                    # 水泵/阀门：无传感器指标，发全空 payload 保活（warning 恒 false）
                    p = {"temperatureWarning": False, "soilMoistureWarning": False, "lightWarning": False}
                else:
                    s = dev["sensor"]
                    key = page
                    warning_key = page + "Warning"
                    if page == "soilMoisture":
                        p = {"soilMoisture": round(s["current"], 2),
                             "soilMoistureWarning": bool(s["current"] < s["min"] or s["current"] > s["max"])}
                    elif page == "temperature":
                        p = {"temperature": round(s["current"], 2),
                             "temperatureWarning": bool(s["current"] < s["min"] or s["current"] > s["max"])}
                    else:  # light
                        p = {"light": round(s["current"], 1),
                             "lightWarning": bool(s["current"] < s["min"] or s["current"] > s["max"])}
                targets.append((f"{prefix}/{owner}/{sn}/telemetry", p, gid, page))
    try:
        ok_count = 0
        last_payload = None
        for topic, p, gid, page in targets:
            info = _client.publish(topic, json.dumps(p, ensure_ascii=False), qos=1)
            if info.rc == 0:
                ok_count += 1
                last_payload = p
        if ok_count == 0:
            with LOCK:
                STATE["last_error"] = f"publish rc={info.rc}"
            return
        with LOCK:
            STATE["publish_count"] += ok_count
            STATE["last_payload"] = last_payload
            STATE["last_error"] = None
            STATE["last_ts"] = time.time()
    except Exception as e:
        with LOCK:
            STATE["last_error"] = f"publish 异常: {e}"

# ---------------------------------------------------------------- 模拟 tick
def _clamp(v, lo, hi):
    return max(lo, min(hi, v))

def _to_bool(v):
    """严格布尔解析：兼容 JSON 布尔与字符串 'true'/'false'/'1'/'0'。"""
    if isinstance(v, bool):
        return v
    return str(v).strip().lower() in ("true", "1", "yes", "on")

def _step():
    """内部采样：每 tick 更新各设备组每页设备读数（高频），并记录曲线（不发布）。"""
    with LOCK:
        for gid, g in GROUPS.items():
            pump = g["pump"].get("pump", {})
            pump_on = bool(pump.get("enabled"))
            # 1) 水泵先作用于本组土壤湿度页: 设定增量 + 扰动
            sm = g["soilMoisture"].get("sensor")
            if pump_on and sm is not None:
                sm["current"] += pump.get("delta", 0) + random.uniform(-pump.get("noise", 0), pump.get("noise", 0))
            # 2) 各传感器页向目标值逼近: 变化量 = 差距 * 系数 + 扰动 (clamp 用物理范围)
            for page in ("soilMoisture", "temperature", "light"):
                s = g[page].get("sensor")
                if s is None:
                    continue
                diff = s["target"] - s["current"]
                s["current"] += diff * s["factor"] + random.uniform(-s["noise"], s["noise"])
                s["current"] = _clamp(s["current"], s["phys_min"], s["phys_max"])
                HISTORY.setdefault(gid, {}).setdefault(page, []).append(round(s["current"], 3))
                h = HISTORY[gid][page]
                if len(h) > HISTORY_LIMIT:
                    HISTORY[gid][page] = h[-HISTORY_LIMIT:]

def _tick_loop():
    last_publish = 0.0
    last_save = 0.0
    while not _stop_event.is_set():
        with LOCK:
            interval = CFG["interval_ms"] / 1000.0
            publish_interval = CFG["publish_interval_ms"] / 1000.0
            running = CFG["running"]
        if running:
            _ensure_connected()
            _step()  # 高频采样（1s）
            now = time.time()
            if now - last_publish >= publish_interval:  # 低频上报（30s）
                _publish()
                last_publish = now
            if now - last_save >= 5:  # 定期保留现场（当前读数）
                save_state()
                last_save = now
        _stop_event.wait(interval)

def _start():
    with LOCK:
        CFG["running"] = True
        STATE["last_error"] = None
    save_state()
    print("[sim] started", flush=True)

def _stop():
    with LOCK:
        CFG["running"] = False
    save_state()
    print("[sim] stopped", flush=True)

# ---------------------------------------------------------------- HTTP
def _state_snapshot():
    with LOCK:
        return {
            "config": {k: CFG[k] for k in ("broker", "prefix", "owner_id", "interval_ms", "publish_interval_ms", "connected", "running")},
            "groups": {
                gid: {
                    page: dict(dev)
                    for page, dev in g.items()
                }
                for gid, g in GROUPS.items()
            },
            "commands": list(COMMANDS),
            "history": {gid: {page: list(v) for page, v in pages.items()} for gid, pages in HISTORY.items()},
            "state": {k: STATE[k] for k in ("publish_count", "last_payload", "last_error", "last_ts")},
        }

def _apply_sensor(patch):
    """更新某设备组某传感器页: {group_id, id, target/factor/noise/min/max}（id 为页类型）。"""
    gid = str(patch.get("group_id") or _default_group_id())
    page = str(patch.get("id") or "")
    with LOCK:
        dev = _page_dev(gid, page)
        if dev is None or "sensor" not in dev:
            return False
        s = dev["sensor"]
        for k in ("target", "factor", "noise", "min", "max"):
            if k in patch and patch[k] is not None:
                s[k] = float(patch[k])
        return True

def _apply_pump(patch):
    """更新某设备组水泵页: {group_id, enabled/delta/noise}。"""
    gid = str(patch.get("group_id") or _default_group_id())
    with LOCK:
        dev = _page_dev(gid, "pump")
        if dev is None or "pump" not in dev:
            return False
        for k in ("enabled", "delta", "noise"):
            if k in patch and patch[k] is not None:
                dev["pump"][k] = bool(patch[k]) if k == "enabled" else float(patch[k])
        return True

def _apply_page_sn(patch):
    """设置某设备组某页的 device_sn: {group_id, page, device_sn}。"""
    gid = str(patch.get("group_id") or _default_group_id())
    page = str(patch.get("page") or "")
    sn = str(patch.get("device_sn") or "").strip()
    if page not in PAGES or not sn:
        return False
    with LOCK:
        dev = _page_dev(gid, page)
        if dev is None:
            return False
        dev["device_sn"] = sn
        return True

def _add_group():
    with LOCK:
        # 找一个空闲组号
        n = 1
        while str(n) in GROUPS:
            n += 1
        GROUPS[str(n)] = _new_group()
        HISTORY[str(n)] = {page: [] for page in PAGES}
        return str(n)

def _remove_group(gid):
    with LOCK:
        gid = str(gid)
        if gid not in GROUPS:
            return False
        del GROUPS[gid]
        HISTORY.pop(gid, None)
        return True

class Handler(BaseHTTPRequestHandler):
    def log_message(self, fmt, *args):  # 静默访问日志
        pass

    def _send(self, code, body, ctype="application/json; charset=utf-8"):
        if isinstance(body, (dict, list)):
            body = json.dumps(body, ensure_ascii=False).encode("utf-8")
        elif isinstance(body, str):
            body = body.encode("utf-8")
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Cache-Control", "no-store")
        self.send_header("Access-Control-Allow-Origin", "*")
        self.end_headers()
        self.wfile.write(body)

    def _read_json(self):
        try:
            n = int(self.headers.get("Content-Length", "0"))
            return json.loads(self.rfile.read(n) or b"{}")
        except Exception:
            return {}

    def do_GET(self):
        path = urlparse(self.path).path
        if path == "/" or path == "/index.html":
            self._send(200, _HTML, "text/html; charset=utf-8")
        elif path == "/api/state":
            self._send(200, _state_snapshot())
        else:
            self._send(404, {"error": "not found"})

    def do_POST(self):
        path = urlparse(self.path).path
        body = self._read_json()
        if path == "/api/start":
            _start()
            self._send(200, {"ok": True})
        elif path == "/api/stop":
            _stop()
            self._send(200, {"ok": True})
        elif path == "/api/config":
            with LOCK:
                for k in ("broker", "prefix", "owner_id"):
                    if k in body and body[k]:
                        CFG[k] = str(body[k]).strip()
                if "interval_ms" in body and body["interval_ms"]:
                    CFG["interval_ms"] = max(50, int(body["interval_ms"]))
                if "publish_interval_ms" in body and body["publish_interval_ms"]:
                    CFG["publish_interval_ms"] = max(1000, int(body["publish_interval_ms"]))
            save_state()
            self._send(200, {"ok": True})
        elif path == "/api/sensor":
            ok = _apply_sensor(body)
            if ok:
                save_state()
            self._send(200, {"ok": ok})
        elif path == "/api/pump":
            ok = _apply_pump(body)
            if ok:
                save_state()
            self._send(200, {"ok": ok})
        elif path == "/api/device":
            ok = _apply_page_sn(body)
            if ok:
                save_state()
            self._send(200, {"ok": ok})
        elif path == "/api/group":
            action = str(body.get("action") or "add")
            if action == "add":
                gid = _add_group()
                save_state()
                self._send(200, {"ok": True, "group_id": gid})
            elif action == "remove":
                ok = _remove_group(body.get("group_id"))
                save_state()
                self._send(200, {"ok": ok})
            else:
                self._send(400, {"ok": False, "error": "unknown action"})
        else:
            self._send(404, {"error": "not found"})

# ---------------------------------------------------------------- UI (内嵌)
_HTML = """<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>MQTT 模拟器（多组设备）</title>
<style>
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body { background: #0f172a; color: #e2e8f0; font-family: "Segoe UI", "Microsoft YaHei", sans-serif; padding: 16px; }
  h1 { font-size: 18px; margin-bottom: 12px; }
  h1 small { color: #64748b; font-weight: normal; margin-left: 8px; }
  .bar { display: flex; flex-wrap: wrap; gap: 10px; align-items: center; background: #1e293b;
         border: 1px solid #334155; border-radius: 10px; padding: 10px 14px; margin-bottom: 14px; }
  .bar label { color: #94a3b8; font-size: 12px; }
  .bar input, .bar select { background: #0f172a; color: #e2e8f0; border: 1px solid #475569;
         border-radius: 6px; padding: 5px 8px; font-size: 13px; width: 150px; }
  .bar input[type=number] { width: 90px; }
  .dot { display: inline-block; width: 10px; height: 10px; border-radius: 50%; margin-right: 6px; }
  .dot.on { background: #22c55e; box-shadow: 0 0 6px #22c55e; }
  .dot.off { background: #ef4444; box-shadow: 0 0 6px #ef4444; }
  .btn { border: none; border-radius: 8px; padding: 8px 18px; font-size: 14px; cursor: pointer; font-weight: 600; }
  .btn.start { background: #22c55e; color: #052e16; }
  .btn.stop { background: #ef4444; color: #450a0a; }
  .btn.ghost { background: #334155; color: #e2e8f0; }
  .btn:disabled { opacity: .5; cursor: not-allowed; }
  .tabs { display: flex; flex-wrap: wrap; gap: 6px; margin-bottom: 14px; align-items: center; }
  .tab { background: #1e293b; border: 1px solid #334155; color: #94a3b8; border-radius: 8px;
         padding: 6px 14px; cursor: pointer; font-size: 13px; }
  .tab.active { background: #38bdf8; color: #052e16; border-color: #38bdf8; font-weight: 700; }
  .tab .del { margin-left: 8px; color: #f87171; font-weight: 700; }
  .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; }
  .card { background: #1e293b; border: 1px solid #334155; border-radius: 12px; padding: 14px; }
  .card .head { display: flex; justify-content: space-between; align-items: baseline; margin-bottom: 6px; }
  .card .name { font-size: 14px; color: #94a3b8; }
  .card .big { font-size: 34px; font-weight: 700; font-variant-numeric: tabular-nums; }
  .card .unit { font-size: 14px; color: #64748b; margin-left: 4px; }
  .warn { color: #f97316; font-size: 12px; margin-left: 8px; }
  .sn { display: block; margin: 4px 0 8px; background: #0f172a; color: #7dd3fc; border: 1px solid #475569;
        border-radius: 6px; padding: 4px 8px; font-size: 12px; width: 100%; font-family: monospace; }
  .row { display: flex; align-items: center; gap: 8px; margin-top: 8px; font-size: 13px; color: #cbd5e1; }
  .row label { width: 56px; color: #94a3b8; flex-shrink: 0; }
  .row input[type=range] { flex: 1; accent-color: #38bdf8; }
  .row input[type=number] { width: 72px; background: #0f172a; color: #e2e8f0;
         border: 1px solid #475569; border-radius: 6px; padding: 3px 6px; }
  canvas { width: 100%; height: 70px; margin-top: 10px; background: #0f172a;
          border-radius: 8px; border: 1px solid #334155; display: block; }
  .pump-btn { width: 100%; padding: 12px; border-radius: 10px; border: none; font-size: 15px;
          font-weight: 700; cursor: pointer; margin: 8px 0 4px; }
  .pump-btn.on { background: #22c55e; color: #052e16; }
  .pump-btn.off { background: #334155; color: #94a3b8; }
  .foot { margin-top: 14px; background: #1e293b; border: 1px solid #334155; border-radius: 10px; padding: 10px 14px; font-size: 12px; color: #94a3b8; }
  .foot pre { margin-top: 6px; color: #7dd3fc; white-space: pre-wrap; word-break: break-all; font-size: 12px; }
  .err { color: #f87171; }
</style>
</head>
<body>
<h1>MQTT 模拟器 <small>多组设备：4 页为一组，每页可独立设置 deviceSn</small></h1>

<div class="bar">
  <label>broker <input id="cfg-broker" value="tcp://127.0.0.1:1883"></label>
  <label>prefix <input id="cfg-prefix" value="agri" style="width:70px"></label>
  <label>ownerId <input id="cfg-owner" value="2" style="width:60px"></label>
  <label>采样ms <input id="cfg-interval" type="number" min="50" value="1000"></label>
  <label>上报ms <input id="cfg-publish" type="number" min="1000" value="30000"></label>
  <span id="conn"><span class="dot off"></span>未连接</span>
  <button id="btn-start" class="btn start">▶ 启动</button>
  <button id="btn-stop" class="btn stop" disabled>⏹ 停止</button>
  <button id="btn-add-group" class="btn ghost">+ 新增组</button>
</div>

<div class="tabs" id="tabs"></div>
<div class="grid" id="grid"></div>

<div class="foot">
  已发布 <b id="pub-count">0</b> 条 &nbsp;|&nbsp; <span id="pub-err"></span>
  <pre id="last-payload">(等待发布…)</pre>
  <div style="margin-top:8px;color:#94a3b8">后端命令 (<span id="cmd-count">0</span>)</div>
  <div id="cmd-log" style="margin-top:4px;max-height:140px;overflow-y:auto;font-size:11px;color:#7dd3fc;background:#0f172a;border:1px solid #334155;border-radius:6px;padding:6px"></div>
</div>

<script>
const $ = id => document.getElementById(id);
let state = null;
let curGroup = null;
// 每个修改目标独立的防抖 timer：不同控件互不取消（避免一次操作吞掉另一次提交）
const postTimers = {};

const PAGE_NAMES = { soilMoisture: '土壤湿度', temperature: '温度', light: '光照', pump: '水泵 (增加土壤湿度)' };
const PAGE_UNITS = { soilMoisture: '%', temperature: '°C', light: 'lx' };
const PAGE_LIMITS = {
  soilMoisture: { target: [0, 100, 0.5], factor: [0.005, 0.5, 0.005], noise: [0, 10, 0.1] },
  temperature: { target: [-10, 60, 0.5], factor: [0.005, 0.5, 0.005], noise: [0, 5, 0.1] },
  light: { target: [0, 2000, 10], factor: [0.005, 0.5, 0.005], noise: [0, 100, 1] }
};

function schedulePost(url, body) {
  const key = url + '|' + JSON.stringify(body);
  clearTimeout(postTimers[key]);
  postTimers[key] = setTimeout(() => {
    delete postTimers[key];
    fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
  }, 120);
}

// 本地输入覆盖：输入即记录，轮询合并时优先本地值，服务器同步后再清除
let localOverrides = {};

function setOverride(gid, page, field, value) {
  localOverrides[`${gid}:${page}:${field}`] = value;
}

function mergeOverrides(fresh) {
  for (const key in localOverrides) {
    if (key.startsWith('config:')) {
      const field = key.slice('config:'.length);
      if (!(field in fresh.config)) { delete localOverrides[key]; continue; }
      if (String(fresh.config[field]) === String(localOverrides[key])) {
        delete localOverrides[key];
      } else {
        fresh.config[field] = localOverrides[key];
      }
      continue;
    }
    const [gid, page, field] = key.split(':');
    const g = fresh.groups && fresh.groups[gid];
    const dev = g && g[page];
    if (!dev) continue;
    let serverVal = dev.device_sn;
    if (dev.sensor && field in dev.sensor) serverVal = dev.sensor[field];
    else if (dev.pump && field in dev.pump) serverVal = dev.pump[field];
    if (String(serverVal) === String(localOverrides[key])) {
      delete localOverrides[key];  // 服务器已同步，恢复正常回显
    } else if (field === 'device_sn') {
      dev.device_sn = localOverrides[key];
    } else if (dev.sensor && field in dev.sensor) {
      dev.sensor[field] = localOverrides[key];
    } else if (dev.pump && field in dev.pump) {
      dev.pump[field] = localOverrides[key];
    }
  }
}

// 顶栏配置乐观更新（override key 用 "config:字段"）
function applyLocalConfig(field, value) {
  if (!state || !state.config) return;
  state.config[field] = value;
  localOverrides['config:' + field] = value;
}

// 乐观更新本地 state：输入即写本地，轮询回显读本地最新值，避免被服务器旧值退回
function applyLocal(gid, page, patch) {
  const g = state && state.groups && state.groups[gid];
  if (!g || !g[page]) return;
  const dev = g[page];
  if (patch.device_sn !== undefined) {
    dev.device_sn = patch.device_sn;
    setOverride(gid, page, 'device_sn', patch.device_sn);
  }
  if (patch.sensor) {
    Object.assign(dev.sensor, patch.sensor);
    for (const k in patch.sensor) setOverride(gid, page, k, patch.sensor[k]);
  }
  if (patch.pump) {
    Object.assign(dev.pump, patch.pump);
    for (const k in patch.pump) setOverride(gid, page, k, patch.pump[k]);
  }
}

function makeCard(gid, page) {
  const card = document.createElement('div');
  card.className = 'card';
  card.id = `card-${gid}-${page}`;
  if (page === 'pump') {
    card.innerHTML = `
      <div class="head"><span class="name">${PAGE_NAMES[page]}</span><span id="pump-effect-${gid}" class="warn"></span></div>
      <input class="sn" data-sn placeholder="deviceSn">
      <button id="pump-btn-${gid}" class="pump-btn off">水泵 关</button>
      <div class="row"><label>每次增量</label><input type="range" data-k="delta" min="0" max="10" step="0.1"><input type="number" data-num="delta" step="0.1"></div>
      <div class="row"><label>扰动</label><input type="range" data-k="noise" min="0" max="5" step="0.1"><input type="number" data-num="noise" step="0.1"></div>
      <div style="margin-top:8px;font-size:12px;color:#64748b">开启后每个 tick 给本组土壤湿度增加 <b>增量+扰动</b>；湿度传感器仍会向自己的目标值逼近，两者可形成对抗。</div>`;
    const sn = card.querySelector('[data-sn]');
    sn.addEventListener('input', () => {
      applyLocal(gid, 'pump', { device_sn: sn.value });
      schedulePost('/api/device', { group_id: gid, page: 'pump', device_sn: sn.value.trim() });
    });
    card.querySelectorAll('input[data-k]').forEach(el => {
      el.addEventListener('input', () => {
        const k = el.dataset.k, num = card.querySelector(`input[data-num="${k}"]`);
        if (num) num.value = el.value;
        applyLocal(gid, 'pump', { pump: { [k]: parseFloat(el.value) } });
        schedulePost('/api/pump', { group_id: gid, [k]: parseFloat(el.value) });
      });
    });
    card.querySelectorAll('input[data-num]').forEach(el => {
      el.addEventListener('input', () => {
        const k = el.dataset.k, range = card.querySelector(`input[data-k="${k}"]`);
        if (range) range.value = el.value;
        applyLocal(gid, 'pump', { pump: { [k]: parseFloat(el.value) } });
        schedulePost('/api/pump', { group_id: gid, [k]: parseFloat(el.value) });
      });
    });
    const pumpBtn = card.querySelector('.pump-btn');
    pumpBtn.addEventListener('click', () => {
      const on = state && state.groups[gid] && state.groups[gid].pump.pump.enabled ? false : true;
      applyLocal(gid, 'pump', { pump: { enabled: on } });
      schedulePost('/api/pump', { group_id: gid, enabled: on });
      updatePumpBtn(gid, on);
    });
  } else {
    const L = PAGE_LIMITS[page];
    const units = PAGE_UNITS[page];
    card.innerHTML = `
      <div class="head"><span class="name">${PAGE_NAMES[page]}</span><span id="big-${gid}-${page}" class="big">-</span><span class="unit">${units}</span><span class="warn" id="warn-${gid}-${page}"></span></div>
      <input class="sn" data-sn placeholder="deviceSn">
      <div class="row"><label>目标</label><input type="range" data-k="target" min="${L.target[0]}" max="${L.target[1]}" step="${L.target[2]}"><input type="number" data-num="target" step="${L.target[2]}"></div>
      <div class="row"><label>变化系数</label><input type="range" data-k="factor" min="${L.factor[0]}" max="${L.factor[1]}" step="${L.factor[2]}"><input type="number" data-num="factor" step="${L.factor[2]}"></div>
      <div class="row"><label>扰动</label><input type="range" data-k="noise" min="${L.noise[0]}" max="${L.noise[1]}" step="${L.noise[2]}"><input type="number" data-num="noise" step="${L.noise[2]}"></div>
      <div class="row"><label>告警范围</label><input type="number" data-k="min" style="width:60px"><span>~</span><input type="number" data-k="max" style="width:60px"></div>
      <canvas id="cv-${gid}-${page}"></canvas>`;
    const sn = card.querySelector('[data-sn]');
    sn.addEventListener('input', () => {
      applyLocal(gid, page, { device_sn: sn.value });
      schedulePost('/api/device', { group_id: gid, page, device_sn: sn.value.trim() });
    });
    const localSensor = (k, v) => applyLocal(gid, page, { sensor: { [k]: v } });
    card.querySelectorAll('input[data-k]').forEach(el => {
      el.addEventListener('input', () => {
        const k = el.dataset.k, num = card.querySelector(`input[data-num="${k}"]`);
        if (num) num.value = el.value;
        const v = parseFloat(el.value);
        localSensor(k, v);
        schedulePost('/api/sensor', { group_id: gid, id: page, [k]: v });
      });
    });
    card.querySelectorAll('input[data-num]').forEach(el => {
      el.addEventListener('input', () => {
        const k = el.dataset.k, range = card.querySelector(`input[data-k="${k}"]`);
        if (range) range.value = el.value;
        const v = parseFloat(el.value);
        localSensor(k, v);
        schedulePost('/api/sensor', { group_id: gid, id: page, [k]: v });
      });
    });
  }
  return card;
}

function renderTabs() {
  const tabs = $('tabs');
  tabs.innerHTML = '';
  Object.keys(state.groups).forEach(gid => {
    const t = document.createElement('button');
    t.className = 'tab' + (gid === curGroup ? ' active' : '');
    t.innerHTML = `组 ${gid} <span class="del">×</span>`;
    t.onclick = (e) => {
      if (e.target.classList.contains('del')) {
        if (!confirm(`删除组 ${gid}？`)) return;
        Object.keys(localOverrides).forEach(k => { if (k.startsWith(gid + ':')) delete localOverrides[k]; });
        fetch('/api/group', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'remove', group_id: gid }) });
        return;
      }
      curGroup = gid;
      renderGroup();
    };
    tabs.appendChild(t);
  });
}

function renderGroup() {
  const grid = $('grid');
  grid.innerHTML = '';
  const g = state.groups[curGroup];
  if (!g) return;
  ['soilMoisture', 'temperature', 'light', 'pump'].forEach(page => {
    const card = makeCard(curGroup, page);
    grid.appendChild(card);
    applyCard(curGroup, page);
  });
}

function applyCard(gid, page) {
  const g = state.groups[gid];
  if (!g) return;
  const dev = g[page];
  const card = $(`card-${gid}-${page}`);
  if (!card) return;
  const sn = card.querySelector('[data-sn]');
  if (sn && document.activeElement !== sn) sn.value = dev.device_sn || '';
  if (page === 'pump') {
    const p = dev.pump || {};
    updatePumpBtn(gid, p.enabled);
    [['delta', 'delta'], ['noise', 'noise']].forEach(([k, sk]) => {
      const range = card.querySelector(`input[data-k="${k}"]`);
      const num = card.querySelector(`input[data-num="${k}"]`);
      if (range && document.activeElement !== range && document.activeElement !== num) { range.value = p[sk]; if (num) num.value = p[sk]; }
    });
    $(`pump-effect-${gid}`).textContent = p.enabled ? '正在增加湿度' : '';
    return;
  }
  const s = dev.sensor || {};
  const big = $(`big-${gid}-${page}`);
  if (big) big.textContent = Number(s.current || 0).toFixed(page === 'light' ? 0 : 1);
  const warn = $(`warn-${gid}-${page}`);
  const over = s.current < s.min || s.current > s.max;
  if (warn) { warn.textContent = over ? '⚠ 告警' : ''; warn.style.visibility = over ? 'visible' : 'hidden'; }
  [['target', 'target'], ['factor', 'factor'], ['noise', 'noise'], ['min', 'min'], ['max', 'max']].forEach(([k, sk]) => {
    const range = card.querySelector(`input[data-k="${k}"]`);
    const num = card.querySelector(`input[data-num="${k}"]`);
    if (range && document.activeElement !== range && document.activeElement !== num) { range.value = s[sk]; if (num) num.value = s[sk]; }
  });
  const cv = $(`cv-${gid}-${page}`);
  if (cv) {
    cv.width = cv.clientWidth || 280; cv.height = 70;
    draw(cv, (state.history[gid] || {})[page] || [], s.min, s.max);
  }
}

function updatePumpBtn(gid, on) {
  const b = $(`pump-btn-${gid}`);
  if (!b) return;
  b.className = 'pump-btn ' + (on ? 'on' : 'off');
  b.textContent = on ? '水泵 开 (增加湿度中)' : '水泵 关';
}

function draw(cv, values, lo, hi) {
  const ctx = cv.getContext('2d');
  const W = cv.width, H = cv.height;
  ctx.clearRect(0, 0, W, H);
  if (!values || values.length < 2) return;
  const span = (hi - lo) || 1;
  ctx.strokeStyle = '#38bdf8';
  ctx.lineWidth = 1.5;
  ctx.beginPath();
  for (let i = 0; i < values.length; i++) {
    const x = (i / (values.length - 1)) * (W - 2) + 1;
    const y = H - 2 - ((values[i] - lo) / span) * (H - 4);
    i === 0 ? ctx.moveTo(x, y) : ctx.lineTo(x, y);
  }
  ctx.stroke();
}

function bindConfig() {
  const bind = (id, key, transform) => {
    $(id).addEventListener('input', () => {
      const v = transform ? transform($(id).value) : $(id).value;
      applyLocalConfig(key, v);
      schedulePost('/api/config', { [key]: v });
    });
  };
  bind('cfg-broker', 'broker', v => v.trim());
  bind('cfg-prefix', 'prefix', v => v.trim());
  bind('cfg-owner', 'owner_id', v => v.trim());
  bind('cfg-interval', 'interval_ms', v => parseInt(v) || 1000);
  bind('cfg-publish', 'publish_interval_ms', v => parseInt(v) || 30000);
  $('btn-start').addEventListener('click', () => fetch('/api/start', { method: 'POST' }));
  $('btn-stop').addEventListener('click', () => fetch('/api/stop', { method: 'POST' }));
  $('btn-add-group').addEventListener('click', async () => {
    const r = await fetch('/api/group', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ action: 'add' }) });
    const d = await r.json();
    if (d.ok) curGroup = d.group_id;
  });
}
bindConfig();

function render(s) {
  const cfg = s.config;
  // 配置回显：全部输入框仅在未聚焦时回显（防止轮询打断输入）
  const syncVal = (id, v) => { const el = $(id); if (el && document.activeElement !== el) el.value = v; };
  syncVal('cfg-broker', cfg.broker);
  syncVal('cfg-prefix', cfg.prefix);
  syncVal('cfg-owner', cfg.owner_id);
  syncVal('cfg-interval', cfg.interval_ms);
  syncVal('cfg-publish', cfg.publish_interval_ms);
  const conn = $('conn');
  conn.innerHTML = cfg.connected ? '<span class="dot on"></span>MQTT 已连接' : '<span class="dot off"></span>MQTT 未连接';
  $('btn-start').disabled = cfg.running;
  $('btn-stop').disabled = !cfg.running;

  const gids = Object.keys(s.groups);
  if (!gids.length) return;
  if (!curGroup || !s.groups[curGroup]) curGroup = gids[0];
  renderTabs();
  if (!document.querySelector(`#card-${curGroup}-soilMoisture`)) renderGroup();
  ['soilMoisture', 'temperature', 'light', 'pump'].forEach(page => applyCard(curGroup, page));

  $('cmd-count').textContent = (s.commands || []).length;
  const log = $('cmd-log');
  if (!(s.commands || []).length) {
    log.textContent = '(等待后端下发命令…)';
  } else {
    log.innerHTML = s.commands.map(c => {
      const t = new Date(c.ts * 1000).toLocaleTimeString('zh-CN', { hour12: false });
      const d = c.durationSeconds ? ` · ${c.durationSeconds}s` : '';
      const who = `组${c.groupId || '?'}/${c.page || '?'}`;
      return `<div style="margin:2px 0">[${t}] <b style="color:${c.state === '开' ? '#22c55e' : '#f87171'}">${who} ${c.state}</b> ${c.action}${d} · ${c.commandId}<br><span style="color:#64748b">${c.topic}${c.reason ? ' · ' + c.reason : ''}</span></div>`;
    }).join('');
  }

  $('pub-count').textContent = s.state.publish_count;
  const err = $('pub-err');
  if (s.state.last_error) { err.className = 'err'; err.textContent = '⚠ ' + s.state.last_error; }
  else { err.className = ''; err.textContent = ''; }
  $('last-payload').textContent = s.state.last_payload ? JSON.stringify(s.state.last_payload, null, 2) : '(等待发布…)';
}

async function refresh() {
  try {
    const r = await fetch('/api/state');
    const fresh = await r.json();
    mergeOverrides(fresh);  // 合并本地输入覆盖，防止服务器旧值退回
    state = fresh;
    render(state);
  } catch (e) { /* 服务重启中 */ }
}
setInterval(refresh, 500);
refresh();
</script>
</body>
</html>
"""

# ---------------------------------------------------------------- main
def main():
    ap = argparse.ArgumentParser(description="MQTT 传感器/水泵模拟器")
    ap.add_argument("--host", default="0.0.0.0")
    ap.add_argument("--port", type=int, default=8090)
    args = ap.parse_args()

    load_state()  # 启动恢复现场（保留上次各组配置与读数）

    global _tick_thread
    _tick_thread = threading.Thread(target=_tick_loop, daemon=True)
    _tick_thread.start()

    server = ThreadingHTTPServer((args.host, args.port), Handler)
    print(f"[sim] UI: http://{args.host}:{args.port}", flush=True)
    with LOCK:
        print(f"[sim] {len(GROUPS)} 组设备，发布到 {CFG['prefix']}/{CFG['owner_id']}/<每页deviceSn>/telemetry @ {CFG['broker']}", flush=True)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        save_state()
        _stop_event.set()
        if _client is not None:
            try:
                _client.disconnect()
            except Exception:
                pass
        server.server_close()

if __name__ == "__main__":
    main()
