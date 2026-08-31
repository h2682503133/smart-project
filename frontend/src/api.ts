import type {
  AdminCommand,
  AdminDeleteResult,
  AdminDevice,
  AdminKnowledgeDoc,
  AdminPlot,
  AdminPlotLatest,
  AdminUser,
  AdminWarehouse,
  AdminWarehouseItem,
  AdminWarehouseStockLog,
  AgentMessage,
  AgentSession,
  AgentStreamEvent,
  AlertItem,
  ApiResponse,
  CommandItem,
  DashboardOverview,
  Device,
  DeviceStatusDetail,
  EventNotice,
  IrrigationStatus,
  KnowledgeDocument,
  MarketMaterial,
  Material,
  Order,
  OrderDetail,
  PageResult,
  Plot,
  TelemetryHistory,
  TelemetryHistoryMetric,
  TelemetryLatest,
  ThresholdRuleCreateInput,
  ThresholdRule,
  ThresholdSync,
  ThresholdUpdateResult,
  User,
  Warehouse,
  StockRecord,
  StockView,
  RegistrationRole
} from './types';

const API_BASE = (import.meta.env.VITE_API_BASE_URL ?? '').replace(/\/$/, '');
const AGENT_BASE = (import.meta.env.VITE_AGENT_BASE_URL ?? '').replace(/\/$/, '');
export const SYSTEM_REDELIVERY_QUESTION = '【系统补发】';

export class ApiError extends Error {
  status: number;
  code?: number;

  constructor(status: number, message: string, code?: number) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export function tokenStorage() {
  return {
    get: () => localStorage.getItem('smart_access_token'),
    set: (token: string) => localStorage.setItem('smart_access_token', token),
    clear: () => localStorage.removeItem('smart_access_token')
  };
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const headers = new Headers(options.headers);
  const token = tokenStorage().get();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  if (options.body && !(options.body instanceof FormData) && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }

  let response: Response;
  try {
    response = await fetch(`${API_BASE}${path}`, { ...options, headers });
  } catch {
    throw new ApiError(0, backendUnavailableMessage());
  }
  const payload = (await response.json().catch(() => null)) as ApiResponse<T> | null;
  if (!response.ok || !payload || payload.code !== 0) {
    const message = payload?.message || (response.status >= 500 ? backendUnavailableMessage() : '请求失败');
    throw new ApiError(response.status, message, payload?.code);
  }
  return payload.data;
}

function query(params: Record<string, string | number | undefined | null>) {
  const search = new URLSearchParams();
  Object.entries(params).forEach(([key, value]) => {
    if (value !== undefined && value !== null && value !== '') {
      search.set(key, String(value));
    }
  });
  const text = search.toString();
  return text ? `?${text}` : '';
}

export const api = {
  register: (payload: { mobile: string; username: string; password: string; role?: RegistrationRole }) =>
    request<{ user: User }>('/api/v1/auth/register', { method: 'POST', body: JSON.stringify(payload) }),
  login: (payload: { username: string; password: string }) =>
    request<{ accessToken: string; expiresIn: number; user: User }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  me: () => request<User>('/api/v1/users/me'),
  dashboard: () => request<DashboardOverview>('/api/v1/dashboard/overview'),
  plots: () => request<Plot[]>('/api/v1/plots'),
  plot: (plotId: number) => request<Plot>(`/api/v1/plots/${plotId}`),
  telemetry: (plotId: number) => request<TelemetryLatest>(`/api/v1/plots/${plotId}/telemetry/latest`),
  telemetryHistory: async (plotId: number, metric: TelemetryHistoryMetric) => {
    const history = await request<TelemetryHistory>(
      `/api/v1/telemetry/history${query({ plotId, metric, range: '7d', interval: '5m' })}`
    );
    return withLocalTrendDemo(history, plotId, metric);
  },
  thresholds: (plotId: number) => request<ThresholdRule[]>(`/api/v1/plots/${plotId}/thresholds`),
  createThreshold: (plotId: number, payload: ThresholdRuleCreateInput) =>
    request<ThresholdUpdateResult>(`/api/v1/plots/${plotId}/thresholds`, {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  updateThreshold: (plotId: number, rule: ThresholdRule) =>
    request<ThresholdUpdateResult>(`/api/v1/plots/${plotId}/thresholds/${rule.id}`, {
      method: 'PUT',
      body: JSON.stringify({
        metric: rule.metric,
        operator: rule.operator,
        value: rule.value,
        hysteresis: rule.hysteresis,
        durationSeconds: rule.durationSeconds,
        level: rule.level,
        enabled: rule.enabled
      })
    }),
  thresholdSync: (plotId: number, thresholdId: number) =>
    request<ThresholdSync>(`/api/v1/plots/${plotId}/thresholds/${thresholdId}/sync`),
  devices: (params: { plotId?: number; status?: string; type?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<Device>>(`/api/v1/devices${query({ page: 1, pageSize: 50, ...params })}`),
  bindDevice: (payload: { deviceSn: string; plotId: number; name: string; type: string }) =>
    request<{ id: number; deviceSn: string; status: string }>('/api/v1/devices/bind', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  unbindDevice: (deviceId: number) => request<boolean>(`/api/v1/devices/${deviceId}/binding`, { method: 'DELETE' }),
  deviceStatus: (deviceId: number) => request<DeviceStatusDetail>(`/api/v1/devices/${deviceId}/status`),
  irrigationStatus: (plotId: number) => request<IrrigationStatus>(`/api/v1/plots/${plotId}/irrigation/status`),
  issueIrrigation: (plotId: number, payload: { action: string; durationSeconds: number; mode: string; reason: string }) =>
    request<{ commandId: string; plotId: number; action: string; status: string; createdAt: string }>(
      `/api/v1/plots/${plotId}/irrigation/commands`,
      {
        method: 'POST',
        headers: { 'Idempotency-Key': newIdempotencyKey() },
        body: JSON.stringify(payload)
      }
    ),
  commands: (params: { plotId?: number; status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<CommandItem>>(`/api/v1/commands${query({ page: 1, pageSize: 20, ...params })}`),
  alerts: (params: { plotId?: number; status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AlertItem>>(`/api/v1/alerts${query({ page: 1, pageSize: 30, ...params })}`),
  confirmAlert: (alertId: number, remark: string) =>
    request<{ id: number; status: string; confirmedAt: string }>(`/api/v1/alerts/${alertId}/confirm`, {
      method: 'POST',
      body: JSON.stringify({ remark })
    }),
  knowledge: (category?: string) => request<KnowledgeDocument[]>(`/api/v1/knowledge/docs${query({ category })}`),
  uploadKnowledge: (form: FormData) =>
    request<KnowledgeDocument>('/api/v1/knowledge/docs', {
      method: 'POST',
      body: form
    }),
  createSession: (plotId?: number) =>
    request<AgentSession>('/api/v1/ai/sessions', {
      method: 'POST',
      body: JSON.stringify(plotId ? { plotId } : {})
    }),
  messages: (sessionId: string) =>
    request<PageResult<AgentMessage>>(`/api/v1/ai/sessions/${sessionId}/messages?page=1&pageSize=50`),
  closeSession: (sessionId: string) =>
    request<{ sessionId: string; status: string }>(`/api/v1/ai/sessions/${sessionId}/close`, { method: 'POST' }),
  closeAgentChat: (sessionId: string) => closeAgentChat(sessionId),
  // ---------------- 管理后台（SYSTEM_ADMIN） ----------------
  adminUsers: (params: { keyword?: string; role?: string; status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminUser>>(`/api/v1/admin/users${query({ page: 1, pageSize: 50, ...params })}`),
  adminDevices: (params: { plotId?: number; status?: string; type?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminDevice>>(`/api/v1/admin/devices${query({ page: 1, pageSize: 100, ...params })}`),
  adminAlerts: (params: { plotId?: number; status?: string; startTime?: string; endTime?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AlertItem>>(`/api/v1/admin/alerts${query({ page: 1, pageSize: 50, ...params })}`),
  adminTelemetryLatest: (params: { plotId?: number } = {}) =>
    request<AdminPlotLatest[]>(`/api/v1/admin/telemetry/latest${query(params)}`),
  adminDevicesStatus: (params: { plotId?: number; status?: string; type?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminDevice>>(`/api/v1/admin/devices/status${query({ page: 1, pageSize: 100, ...params })}`),
  adminBindDevice: (payload: { deviceSn: string; plotId: number; name: string; type: string }) =>
    request<{ id: number; deviceSn: string; status: string }>('/api/v1/admin/devices/bind', {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  adminUnbindDevice: (deviceId: number) =>
    request<boolean>(`/api/v1/admin/devices/${deviceId}/binding`, { method: 'DELETE' }),
  adminDeleteDevice: (deviceId: number) =>
    request<{ id: number; deleted: boolean }>(`/api/v1/admin/devices/${deviceId}`, { method: 'DELETE' }),
  adminPlots: (params: { keyword?: string; ownerId?: number; status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminPlot>>(`/api/v1/admin/plots${query({ page: 1, pageSize: 100, ...params })}`),
  adminCreatePlot: (payload: { code: string; name: string; area?: number | null; location?: string | null; ownerId: number }) =>
    request<AdminPlot>('/api/v1/admin/plots', { method: 'POST', body: JSON.stringify(payload) }),
  adminAssignPlot: (plotId: number, ownerId: number) =>
    request<AdminPlot>(`/api/v1/admin/plots/${plotId}/owner`, {
      method: 'PUT',
      body: JSON.stringify({ ownerId })
    }),
  adminKnowledgeDocs: (params: { status?: string; category?: string; keyword?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminKnowledgeDoc>>(`/api/v1/admin/knowledge/docs${query({ page: 1, pageSize: 50, ...params })}`),
  adminDeleteKnowledgeDoc: (docId: number) =>
    request<AdminDeleteResult>(`/api/v1/admin/knowledge/docs/${docId}`, { method: 'DELETE' }),
  approveKnowledgeDoc: (docId: number) =>
    request<KnowledgeDocument>(`/api/v1/knowledge/docs/${docId}/approve`, { method: 'POST' }),
  publishKnowledgeDoc: (docId: number) =>
    request<KnowledgeDocument>(`/api/v1/knowledge/docs/${docId}/publish`, { method: 'POST' }),
  adminWarehouses: (params: { keyword?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminWarehouse>>(`/api/v1/admin/warehouses${query({ page: 1, pageSize: 100, ...params })}`),
  adminCreateWarehouse: (payload: { name: string; location?: string; managerName?: string; remark?: string }) =>
    request<AdminWarehouse>('/api/v1/admin/warehouses', { method: 'POST', body: JSON.stringify(payload) }),
  adminWarehouseItems: (params: { warehouseId?: number; keyword?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminWarehouseItem>>(`/api/v1/admin/warehouse-items${query({ page: 1, pageSize: 100, ...params })}`),
  adminCreateWarehouseItem: (payload: {
    warehouseId: number;
    name: string;
    category: string;
    unit: string;
    initialQuantity?: number;
    safetyStock?: number;
    remark?: string;
  }) =>
    request<AdminWarehouseItem>('/api/v1/admin/warehouse-items', { method: 'POST', body: JSON.stringify(payload) }),
  adminAdjustWarehouseStock: (itemId: number, payload: { action: 'IN' | 'OUT' | 'SET'; quantity: number; reason?: string }) =>
    request<AdminWarehouseStockLog>(`/api/v1/admin/warehouse-items/${itemId}/stock`, {
      method: 'POST',
      body: JSON.stringify(payload)
    }),
  adminWarehouseStockLogs: (params: { warehouseId?: number; itemId?: number; page?: number; pageSize?: number } = {}) =>
    request<PageResult<AdminWarehouseStockLog>>(`/api/v1/admin/warehouse-stock-logs${query({ page: 1, pageSize: 30, ...params })}`),
  adminCommands: (params: {
    plotId?: number;
    deviceId?: number;
    status?: string;
    action?: string;
    startAt?: string;
    endAt?: string;
    page?: number;
    pageSize?: number;
  } = {}) =>
    request<PageResult<AdminCommand>>(`/api/v1/admin/commands${query({ page: 1, pageSize: 50, ...params })}`),
  adminCommand: (commandId: string) => request<AdminCommand>(`/api/v1/admin/commands/${encodeURIComponent(commandId)}`),

  // ---------------- 二阶段仓储与采购意向 ----------------
  materials: (params: { keyword?: string; status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<Material>>(`/api/v1/materials${query({ page: 1, pageSize: 100, ...params })}`),
  createMaterial: (payload: { name: string; category: string; unit: string; spec?: string; status?: string }) =>
    request<Material>('/api/v1/materials', { method: 'POST', body: JSON.stringify(payload) }),
  updateMaterial: (materialId: number, payload: { name: string; category: string; unit: string; spec?: string; status?: string }) =>
    request<Material>(`/api/v1/materials/${materialId}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteMaterial: (materialId: number) => request<{ id: number }>(`/api/v1/materials/${materialId}`, { method: 'DELETE' }),
  warehouses: (params: { keyword?: string; status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<Warehouse>>(`/api/v1/warehouses${query({ page: 1, pageSize: 100, ...params })}`),
  createWarehouse: (payload: { name: string; location?: string; status?: string }) =>
    request<Warehouse>('/api/v1/warehouses', { method: 'POST', body: JSON.stringify(payload) }),
  updateWarehouse: (warehouseId: number, payload: { name: string; location?: string; status?: string }) =>
    request<Warehouse>(`/api/v1/warehouses/${warehouseId}`, { method: 'PUT', body: JSON.stringify(payload) }),
  deleteWarehouse: (warehouseId: number) => request<{ id: number }>(`/api/v1/warehouses/${warehouseId}`, { method: 'DELETE' }),
  stocks: (params: { warehouseId?: number; materialId?: number; page?: number; pageSize?: number } = {}) =>
    request<PageResult<StockView>>(`/api/v1/stocks${query({ page: 1, pageSize: 100, ...params })}`),
  stockRecords: (params: {
    warehouseId?: number;
    materialId?: number;
    plotId?: number;
    type?: string;
    startAt?: string;
    endAt?: string;
    page?: number;
    pageSize?: number;
  } = {}) =>
    request<PageResult<StockRecord>>(`/api/v1/stock-records${query({ page: 1, pageSize: 50, ...params })}`),
  inboundStock: (payload: { warehouseId: number; materialId: number; quantity: number; plotId: number; remark?: string }) =>
    request<{ recordId: number; stockQuantity: string | number }>('/api/v1/stocks/inbound', {
      method: 'POST',
      headers: { 'Idempotency-Key': newIdempotencyKey() },
      body: JSON.stringify(payload)
    }),
  marketMaterials: (params: { page?: number; pageSize?: number } = {}) =>
    request<PageResult<MarketMaterial>>(`/api/v1/market/materials${query({ page: 1, pageSize: 100, ...params })}`),
  createMarketMaterial: (payload: { name: string; unit: 'kg' | 't' }) =>
    request<Material>('/api/v1/market/materials', { method: 'POST', body: JSON.stringify(payload) }),
  orders: (params: { status?: string; page?: number; pageSize?: number } = {}) =>
    request<PageResult<Order>>(`/api/v1/orders${query({ page: 1, pageSize: 50, ...params })}`),
  order: (orderId: number) => request<OrderDetail>(`/api/v1/orders/${orderId}`),
  createOrder: (payload: { items: Array<{ materialId: number; quantity: number }>; expectedTime?: string; remark?: string }) =>
    request<Order>('/api/v1/orders', { method: 'POST', body: JSON.stringify(payload) }),
  reviewOrder: (orderId: number, action: 'approve' | 'reject') =>
    request<Order>(`/api/v1/orders/${orderId}/review`, { method: 'POST', body: JSON.stringify({ action }) }),
  terminateOrder: (orderId: number, action: 'cancel' | 'close') =>
    request<Order>(`/api/v1/orders/${orderId}/terminate`, { method: 'POST', body: JSON.stringify({ action }) }),
  confirmOrder: (orderId: number, payload: { items: Array<{ materialId: number; quantity: number }> }) =>
    request<Order>(`/api/v1/orders/${orderId}/confirm`, { method: 'POST', body: JSON.stringify(payload) })
};

export async function streamAgentChat(
  payload: { sessionId?: string; plotId?: number | ''; question: string },
  onEvent: (event: AgentStreamEvent) => void,
  signal: AbortSignal
) {
  const headers = new Headers({ 'Content-Type': 'application/json' });
  const token = tokenStorage().get();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }

  let response: Response;
  try {
    response = await fetch(`${AGENT_BASE}/agent/chat`, {
      method: 'POST',
      headers,
      signal,
      body: JSON.stringify({
        session_id: payload.sessionId,
        plot_id: payload.plotId == null ? undefined : String(payload.plotId),
        question: payload.question
      })
    });
  } catch (error) {
    if (isAbortError(error)) throw error;
    throw new ApiError(0, 'AI 服务不可达，请确认 agent-service 已启动。');
  }
  if (!response.ok || !response.body) {
    throw new ApiError(response.status, await agentErrorMessage(response));
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let eventType = 'message';
  let data = '';

  const flush = () => {
    if (!data) {
      eventType = 'message';
      return;
    }
    try {
      const event = JSON.parse(data) as AgentStreamEvent;
      onEvent({ ...event, type: typeof event.type === 'string' ? event.type : eventType });
    } catch {
      onEvent({ type: 'error', message: 'AI 返回的数据格式无效。' });
    }
    eventType = 'message';
    data = '';
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split(/\r?\n/);
    buffer = lines.pop() ?? '';
    for (const line of lines) {
      if (line === '') {
        flush();
      } else if (line.startsWith('event:')) {
        eventType = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        data += line.slice(5).trim();
      }
    }
  }
}

export function streamOfflineBacklog(
  onEvent: (event: AgentStreamEvent) => void,
  signal: AbortSignal
) {
  return streamAgentChat(
    { sessionId: '', plotId: '', question: SYSTEM_REDELIVERY_QUESTION },
    onEvent,
    signal
  );
}

async function closeAgentChat(sessionId: string) {
  const headers = new Headers();
  const token = tokenStorage().get();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  let response: Response;
  try {
    response = await fetch(`${AGENT_BASE}/agent/chat/sessions/${encodeURIComponent(sessionId)}/close`, {
      method: 'POST',
      headers
    });
  } catch (error) {
    if (isAbortError(error)) throw error;
    throw new ApiError(0, 'AI 服务不可达，请确认 agent-service 已启动。');
  }
  if (!response.ok) {
    throw new ApiError(response.status, await agentErrorMessage(response));
  }
}

async function agentErrorMessage(response: Response) {
  const payload = (await response.json().catch(() => null)) as { detail?: unknown } | null;
  return typeof payload?.detail === 'string' ? payload.detail : 'AI 对话请求失败。';
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError';
}

export async function streamEvents(onEvent: (event: EventNotice) => void, signal: AbortSignal) {
  const headers = new Headers();
  const token = tokenStorage().get();
  if (token) {
    headers.set('Authorization', `Bearer ${token}`);
  }
  const response = await fetch(`${API_BASE}/api/v1/events/stream`, { headers, signal });
  if (!response.ok || !response.body) {
    throw new ApiError(response.status, '事件流连接失败');
  }
  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  let eventId = '';
  let eventType = 'message';
  let data = '';

  const flush = () => {
    if (!data) {
      eventId = '';
      eventType = 'message';
      return;
    }
    try {
      onEvent({
        id: eventId || `${Date.now()}`,
        type: eventType,
        data: JSON.parse(data),
        receivedAt: new Date().toISOString()
      });
    } catch {
      onEvent({
        id: eventId || `${Date.now()}`,
        type: eventType,
        data: { raw: data },
        receivedAt: new Date().toISOString()
      });
    }
    eventId = '';
    eventType = 'message';
    data = '';
  };

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const lines = buffer.split(/\r?\n/);
    buffer = lines.pop() ?? '';
    for (const line of lines) {
      if (line === '') {
        flush();
      } else if (line.startsWith('id:')) {
        eventId = line.slice(3).trim();
      } else if (line.startsWith('event:')) {
        eventType = line.slice(6).trim();
      } else if (line.startsWith('data:')) {
        data += line.slice(5).trim();
      }
    }
  }
}

function newIdempotencyKey() {
  if ('randomUUID' in crypto) {
    return crypto.randomUUID().slice(0, 64);
  }
  return `cmd_${Date.now()}_${Math.random().toString(16).slice(2)}`.slice(0, 64);
}

function withLocalTrendDemo(
  history: TelemetryHistory,
  plotId: number,
  metric: TelemetryHistoryMetric
): TelemetryHistory {
  const demoPlotId = Number(new URLSearchParams(window.location.search).get('trendDemoPlotId') ?? 0);
  if (!import.meta.env.DEV || demoPlotId !== plotId || history.points.length > 0) return history;

  const interval = 5 * 60 * 1000;
  const pointCount = 7 * 24 * 12;
  const end = Math.floor(Date.now() / interval) * interval;
  const start = end - (pointCount - 1) * interval;
  const points = Array.from({ length: pointCount }, (_, index) => {
    const dayCycle = (index % (24 * 12)) / (24 * 12);
    const longWave = Math.sin(index / 72);
    const value = metric === 'soilMoisture'
      ? 43 + Math.sin(dayCycle * Math.PI * 2) * 2.8 + longWave * 2.1 + (index % 96 < 12 ? 3.6 : 0)
      : 25.4 + Math.sin((dayCycle - 0.24) * Math.PI * 2) * 2.7 + longWave * 0.7;
    const spread = metric === 'soilMoisture' ? 0.45 : 0.18;
    return {
      time: new Date(start + index * interval).toISOString(),
      avg: Number(value.toFixed(2)),
      min: Number((value - spread).toFixed(2)),
      max: Number((value + spread).toFixed(2))
    };
  });

  return {
    plotId,
    metric,
    unit: metric === 'soilMoisture' ? '%' : '°C',
    points
  };
}

function backendUnavailableMessage() {
  return API_BASE
    ? `后端服务不可达，请检查 ${API_BASE}`
    : '后端服务不可达，请确认项目 API 服务已启动后重试';
}
