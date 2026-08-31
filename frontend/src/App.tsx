import { Bell, Bot, Check, Grid2X2, Inbox, LogOut, Map, MessageSquare, Pencil, Plus, RefreshCw, Send, Settings, Sprout, Square, X } from 'lucide-react';
import { FormEvent, MouseEvent, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { api, ApiError, streamAgentChat, streamEvents, streamOfflineBacklog, tokenStorage } from './api';
import { AdminPanel } from './pages/admin/AdminPanel';
import { AlarmCenterPage } from './pages/AlarmCenterPage';
import { ControlPanelPage } from './pages/ControlPanelPage';
import { DashboardPage } from './pages/DashboardPage';
import { DeviceListPage } from './pages/DeviceListPage';
import { KnowledgePage } from './pages/KnowledgePage';
import { CustomerMarketPage } from './pages/CustomerMarketPage';
import { AuthMode, defaultCredentials, LoginPage } from './pages/LoginPage';
import type {
  AgentChatMessage,
  AlertItem,
  CommandItem,
  DashboardOverview,
  Device,
  EventNotice,
  IrrigationStatus,
  KnowledgeDocument,
  Plot,
  TelemetryHistory,
  TelemetryHistoryMetric,
  TelemetryLatest,
  ThresholdRuleCreateInput,
  ThresholdRule,
  User
} from './types';

type View = 'overview' | 'plots' | 'ask' | 'manage';

const telemetryUpdatedEvent = 'telemetry.updated';
const celsiusUnit = String.fromCharCode(176) + 'C';

const adminRoles = ['SYSTEM_ADMIN', 'TECHNICIAN', 'WAREHOUSE_MANAGER'];

function canAccessAdmin(role?: string) {
  return role != null && adminRoles.includes(role);
}

export default function App() {
  const [authMode, setAuthMode] = useState<AuthMode>('login');
  const [credentials, setCredentials] = useState(defaultCredentials);
  const [user, setUser] = useState<User | null>(null);
  const [view, setView] = useState<View>('overview');
  const [initialLoading, setInitialLoading] = useState(() => Boolean(tokenStorage().get()));
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [data, setData] = useState<AppData>(emptyData);
  const [selectedPlotId, setSelectedPlotId] = useState<number | null>(null);
  const [overviewPlotId, setOverviewPlotId] = useState<number | null>(null);
  const [trendMetric, setTrendMetric] = useState<TelemetryHistoryMetric>('soilMoisture');
  const [trendHistory, setTrendHistory] = useState<TelemetryHistory | null>(null);
  const [trendLoading, setTrendLoading] = useState(false);
  const [plotDetailLoading, setPlotDetailLoading] = useState(false);
  const [events, setEvents] = useState<EventNotice[]>([]);
  const agentControllerRef = useRef<AbortController | null>(null);
  const offlineBacklogControllerRef = useRef<AbortController | null>(null);
  const refreshRequestRef = useRef(0);
  const trendRequestRef = useRef(0);
  const plotDetailRequestRef = useRef(0);
  const [agentSessionId, setAgentSessionId] = useState<string | null>(null);
  const [agentMessages, setAgentMessages] = useState<AgentChatMessage[]>([]);
  const [agentStreaming, setAgentStreaming] = useState(false);
  const [agentError, setAgentError] = useState('');
  const [offlineBacklog, setOfflineBacklog] = useState('');
  const [offlineBacklogLoading, setOfflineBacklogLoading] = useState(false);
  const [offlineBacklogError, setOfflineBacklogError] = useState('');

  const pulseButton = useCallback((button: HTMLButtonElement | null) => {
    if (!button || !button.isConnected) return;
    button.classList.remove('button-feedback');
    void button.offsetWidth;
    button.classList.add('button-feedback');
  }, []);

  const handleButtonInteraction = useCallback((event: MouseEvent<HTMLElement>) => {
    const button = (event.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (!button || button.disabled) return;
    pulseButton(button);
  }, [pulseButton]);

  const selectedPlot = useMemo(
    () => data.plots.find((plot) => plot.id === selectedPlotId) ?? data.plots[0],
    [data.plots, selectedPlotId]
  );
  const overviewSelectedPlot = useMemo(
    () => data.plots.find((plot) => plot.id === overviewPlotId),
    [data.plots, overviewPlotId]
  );

  const selectedTelemetry = selectedPlot ? data.telemetry[selectedPlot.id] : undefined;
  const selectedIrrigation = selectedPlot ? data.irrigation[selectedPlot.id] : undefined;
  const selectedRules = selectedPlot ? data.thresholds[selectedPlot.id] ?? [] : [];
  const overviewSelectedIrrigation = overviewSelectedPlot ? data.irrigation[overviewSelectedPlot.id] : undefined;
  const headerPlot = view === 'overview' ? overviewSelectedPlot : selectedPlot;

  const selectPlot = useCallback((plotId: number) => {
    setSelectedPlotId(plotId);
    setOverviewPlotId(plotId);
  }, []);

  const selectTrendMetric = useCallback((metric: TelemetryHistoryMetric) => {
    setTrendMetric(metric);
  }, []);

  const requestOfflineBacklog = useCallback(async () => {
    if (offlineBacklogControllerRef.current) return;

    const controller = new AbortController();
    offlineBacklogControllerRef.current = controller;
    let content = '';
    let streamError = '';
    setOfflineBacklogError('');
    setOfflineBacklogLoading(true);

    try {
      await streamOfflineBacklog((event) => {
        if (event.type === 'answer' && event.delta) {
          content += event.delta;
          setOfflineBacklog(content);
          return;
        }
        if (event.type === 'error') {
          streamError = event.message || '补发信息获取失败，请稍后重试。';
        }
      }, controller.signal);
      if (!streamError && !controller.signal.aborted) {
        setOfflineBacklog(content);
      }
    } catch (error) {
      if (!isAbortError(error)) {
        streamError = errorMessage(error);
      }
    } finally {
      if (offlineBacklogControllerRef.current === controller) {
        offlineBacklogControllerRef.current = null;
        if (streamError && !controller.signal.aborted) {
          setOfflineBacklogError(streamError);
        }
        setOfflineBacklogLoading(false);
      }
    }
  }, []);

  const loadData = useCallback(async () => {
    const requestId = refreshRequestRef.current + 1;
    refreshRequestRef.current = requestId;
    if (!tokenStorage().get()) {
      setInitialLoading(false);
      return;
    }
    setRefreshing(true);
    try {
      const profile = await api.me();
      if (profile.role === 'CUSTOMER' || canAccessAdmin(profile.role)) {
        if (requestId !== refreshRequestRef.current) return;
        setUser(profile);
        setData(emptyData);
        setSelectedPlotId(null);
        setOverviewPlotId(null);
        setNotice('');
        return;
      }
      const [dashboard, plots, devices, alerts, commands, knowledge] = await Promise.all([
        api.dashboard().catch(() => null),
        api.plots(),
        api.devices().catch(() => null),
        api.alerts().catch(() => null),
        api.commands().catch(() => null),
        api.knowledge().catch(() => null)
      ]);

      const nextTelemetry: Record<number, TelemetryLatest> = {};
      const nextIrrigation: Record<number, IrrigationStatus> = {};
      const nextThresholds: Record<number, ThresholdRule[]> = {};
      await Promise.all(
        plots.map(async (plot) => {
          const [telemetry, irrigation, thresholds] = await Promise.all([
            api.telemetry(plot.id).catch(() => undefined),
            api.irrigationStatus(plot.id).catch(() => undefined),
            api.thresholds(plot.id).catch(() => undefined)
          ]);
          if (telemetry) nextTelemetry[plot.id] = telemetry;
          if (irrigation) nextIrrigation[plot.id] = irrigation;
          if (thresholds) nextThresholds[plot.id] = thresholds;
        })
      );

      if (requestId !== refreshRequestRef.current) return;

      setUser((current) => (current?.id === profile.id ? current : profile));
      setData((current) => ({
        dashboard: dashboard ?? current.dashboard,
        plots,
        devices: devices?.items ?? current.devices,
        alerts: alerts?.items ?? current.alerts,
        commands: commands?.items ?? current.commands,
        knowledge: knowledge ?? current.knowledge,
        telemetry: { ...current.telemetry, ...nextTelemetry },
        irrigation: { ...current.irrigation, ...nextIrrigation },
        thresholds: { ...current.thresholds, ...nextThresholds }
      }));
      setSelectedPlotId((current) => current ?? plots[0]?.id ?? null);
      setOverviewPlotId((current) => current && plots.some((plot) => plot.id === current) ? current : null);
      setNotice('');
    } catch (error) {
      if (requestId !== refreshRequestRef.current) return;
      if (error instanceof ApiError && error.status === 401) {
        tokenStorage().clear();
        setUser(null);
        setNotice('登录已过期，请重新登录');
      } else {
        setNotice(errorMessage(error));
      }
    } finally {
      if (requestId === refreshRequestRef.current) {
        setInitialLoading(false);
        setRefreshing(false);
      }
    }
  }, []);

  useEffect(() => {
    void loadData();
  }, [loadData]);

  useEffect(() => {
    const plotId = overviewSelectedPlot?.id;
    const requestId = trendRequestRef.current + 1;
    trendRequestRef.current = requestId;

    if (!user || !plotId) {
      setTrendHistory(null);
      setTrendLoading(false);
      return;
    }

    setTrendLoading(true);
    void api.telemetryHistory(plotId, trendMetric)
      .then((history) => {
        if (requestId === trendRequestRef.current) {
          setTrendHistory(history);
        }
      })
      .catch(() => {
        if (requestId === trendRequestRef.current) {
          setTrendHistory(null);
        }
      })
      .finally(() => {
        if (requestId === trendRequestRef.current) {
          setTrendLoading(false);
        }
      });
  }, [overviewSelectedPlot?.id, trendMetric, user]);

  useEffect(() => {
    const plotId = selectedPlot?.id;
    const requestId = plotDetailRequestRef.current + 1;
    plotDetailRequestRef.current = requestId;

    if (!user || view !== 'plots' || !plotId) {
      setPlotDetailLoading(false);
      return;
    }

    setPlotDetailLoading(true);
    void api.plot(plotId)
      .then((detail) => {
        if (requestId !== plotDetailRequestRef.current) return;
        setData((current) => ({
          ...current,
          plots: current.plots.map((plot) => (plot.id === detail.id ? { ...plot, ...detail } : plot))
        }));
      })
      .catch(() => undefined)
      .finally(() => {
        if (requestId === plotDetailRequestRef.current) {
          setPlotDetailLoading(false);
        }
      });
  }, [selectedPlot?.id, user, view]);

  useEffect(() => {
    if (!user) return;
    const controller = new AbortController();
    // 告警/命令状态变化事件 → 后台刷新告警列表（避免页面停留时状态过期）
    const alertStateEvents = new Set(['alert.created', 'alert.recovered', 'command.result']);
    streamEvents((event) => {
      if (event.type === telemetryUpdatedEvent) {
        setData((current) => applyTelemetryEvent(current, event));
        return;
      }
      if (alertStateEvents.has(event.type)) {
        void api.alerts()
          .then((page) => setData((current) => ({ ...current, alerts: page.items })))
          .catch(() => undefined);
      }
      setEvents((current) => [event, ...current].slice(0, 5));
    }, controller.signal).catch(() => undefined);
    return () => controller.abort();
  }, [user]);

  async function handleAuth(event: FormEvent) {
    event.preventDefault();
    setBusy(true);
    setNotice('');
    try {
      if (authMode === 'register') {
        await api.register(credentials);
      }
      const result = await api.login(credentials);
      tokenStorage().set(result.accessToken);
      setUser(result.user);
      if (result.user.role === 'CUSTOMER' || canAccessAdmin(result.user.role)) {
        setData(emptyData);
        setInitialLoading(false);
        return;
      }
      await loadData();
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  function logout() {
    agentControllerRef.current?.abort();
    offlineBacklogControllerRef.current?.abort();
    offlineBacklogControllerRef.current = null;
    tokenStorage().clear();
    setUser(null);
    setData(emptyData);
    setSelectedPlotId(null);
    setOverviewPlotId(null);
    trendRequestRef.current += 1;
    setTrendMetric('soilMoisture');
    setTrendHistory(null);
    setTrendLoading(false);
    plotDetailRequestRef.current += 1;
    setPlotDetailLoading(false);
    setEvents([]);
    setAgentSessionId(null);
    setAgentMessages([]);
    setAgentStreaming(false);
    setAgentError('');
    setOfflineBacklog('');
    setOfflineBacklogLoading(false);
    setOfflineBacklogError('');
  }

  async function issueIrrigation(action: 'OPEN' | 'CLOSE', durationSeconds = 600) {
    if (!selectedPlot) return;
    setBusy(true);
    setNotice('');
    try {
      await api.issueIrrigation(selectedPlot.id, {
        action,
        durationSeconds: action === 'OPEN' ? durationSeconds : 0,
        mode: 'MANUAL',
        reason: action === 'OPEN' ? '前端手动开启灌溉' : '前端手动关闭灌溉'
      });
      const [irrigation, commands] = await Promise.all([
        api.irrigationStatus(selectedPlot.id),
        api.commands({ plotId: selectedPlot.id })
      ]);
      setData((current) => ({
        ...current,
        irrigation: { ...current.irrigation, [selectedPlot.id]: irrigation },
        commands: commands.items
      }));
      setNotice(action === 'OPEN' ? '灌溉命令已下发' : '关闭命令已下发');
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function confirmAlert(alertId: number) {
    const remark = window.prompt('确认说明', '已查看并安排处理');
    if (!remark) return;
    setBusy(true);
    try {
      await api.confirmAlert(alertId, remark);
      const alerts = await api.alerts();
      setData((current) => ({ ...current, alerts: alerts.items }));
      setNotice('告警已确认');
    } catch (error) {
      setNotice(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  async function saveThresholdRule(rule: ThresholdRule) {
    if (!selectedPlot) return false;
    setBusy(true);
    try {
      const update = await api.updateThreshold(selectedPlot.id, { ...rule, enabled: !rule.enabled });
      const thresholds = await api.thresholds(selectedPlot.id);
      setData((current) => ({
        ...current,
        thresholds: { ...current.thresholds, [selectedPlot.id]: thresholds }
      }));
      setNotice(
        update.targetCount > 0
          ? `阈值规则已保存，第 ${update.configVersion} 版正在下发至 ${update.targetCount} 台机器`
          : `阈值规则已保存，第 ${update.configVersion} 版当前无绑定机器需要下发`
      );
      return true;
    } catch (error) {
      throw error;
    } finally {
      setBusy(false);
    }
  }

  async function createThresholdRule(rule: ThresholdRuleCreateInput) {
    if (!selectedPlot) return false;
    setBusy(true);
    try {
      await api.createThreshold(selectedPlot.id, rule);
      const thresholds = await api.thresholds(selectedPlot.id);
      setData((current) => ({
        ...current,
        thresholds: { ...current.thresholds, [selectedPlot.id]: thresholds }
      }));
      setNotice('阈值规则已添加');
      return true;
    } catch (error) {
      throw error;
    } finally {
      setBusy(false);
    }
  }

  async function uploadKnowledge(form: FormData) {
    const file = form.get('file');
    if (!(file instanceof File) || !file.name || file.size === 0) {
      throw new Error('请选择可用的文档文件。');
    }

    setBusy(true);
    setNotice('');
    try {
      const uploaded = await api.uploadKnowledge(form);
      const knowledge = await api.knowledge();
      setData((current) => ({
        ...current,
        knowledge: [uploaded, ...knowledge.filter((document) => document.id !== uploaded.id)]
      }));
      setNotice('知识文档已上传');
    } finally {
      setBusy(false);
    }
  }

  async function sendAgentQuestion(question: string) {
    const content = question.trim();
    if (!content || agentStreaming) return;

    const userMessage: AgentChatMessage = {
      id: newChatMessageId('user'),
      role: 'USER',
      content,
      status: 'COMPLETE'
    };
    const assistantMessageId = newChatMessageId('assistant');
    setAgentMessages((current) => [
      ...current,
      userMessage,
      { id: assistantMessageId, role: 'ASSISTANT', content: '', status: 'STREAMING' }
    ]);
    setAgentError('');
    setAgentStreaming(true);

    const controller = new AbortController();
    agentControllerRef.current = controller;

    const runAttempt = async (sessionId?: string) => {
      let completed = false;
      let failed = false;
      let receivedAnswer = false;
      let needsNewSession = false;

      try {
        await streamAgentChat(
          { sessionId, plotId: selectedPlot?.id, question: content },
          (event) => {
            if (needsNewSession) return;

            if (event.type === 'answer' && event.delta) {
              receivedAnswer = true;
              setAgentMessages((current) =>
                current.map((message) =>
                  message.id === assistantMessageId
                    ? { ...message, content: `${message.content}${event.delta}` }
                    : message
                )
              );
              return;
            }
            if (event.type === 'done') {
              completed = true;
              if (event.sessionId) setAgentSessionId(event.sessionId);
              setAgentMessages((current) =>
                current.map((message) =>
                  message.id === assistantMessageId
                    ? {
                        ...message,
                        content: message.content || event.message || 'AI 未返回内容，请重试。',
                        status: 'COMPLETE',
                        sources: event.sources
                      }
                    : message
                )
              );
              return;
            }
            if (event.type === 'error') {
              const message = event.message || 'AI 响应失败，请稍后重试。';
              if (sessionId && !receivedAnswer && shouldRenewAgentSession(message)) {
                needsNewSession = true;
                return;
              }

              failed = true;
              setAgentError(message);
              setAgentMessages((current) =>
                current.map((item) =>
                  item.id === assistantMessageId
                    ? { ...item, content: item.content || message, status: 'ERROR' }
                    : item
                )
              );
            }
          },
          controller.signal
        );

        if (needsNewSession) return true;
        if (!completed && !failed) {
          setAgentMessages((current) =>
            current.map((message) =>
              message.id === assistantMessageId
                ? { ...message, content: message.content || 'AI 未返回内容，请重试。', status: 'COMPLETE' }
                : message
            )
          );
        }
      } catch (error) {
        if (isAbortError(error)) {
          setAgentMessages((current) =>
            current.map((message) =>
              message.id === assistantMessageId
                ? { ...message, content: message.content || '已停止生成。', status: 'COMPLETE' }
                : message
            )
          );
          return false;
        }

        const message = errorMessage(error);
        if (sessionId && !receivedAnswer && shouldRenewAgentSession(message)) {
          return true;
        }

        setAgentError(message);
        setAgentMessages((current) =>
          current.map((item) =>
            item.id === assistantMessageId
              ? { ...item, content: item.content || message, status: 'ERROR' }
              : item
          )
        );
      }

      return false;
    };

    try {
      const needsNewSession = await runAttempt(agentSessionId ?? undefined);
      if (needsNewSession && !controller.signal.aborted) {
        setAgentSessionId(null);
        setAgentError('');
        await runAttempt(undefined);
      }
    } finally {
      if (agentControllerRef.current === controller) {
        agentControllerRef.current = null;
      }
      setAgentStreaming(false);
    }
  }

  async function startNewAgentSession() {
    agentControllerRef.current?.abort();
    const previousSessionId = agentSessionId;
    setAgentSessionId(null);
    setAgentMessages([]);
    setAgentError('');
    setAgentStreaming(false);
    if (!previousSessionId) return;
    try {
      await api.closeAgentChat(previousSessionId);
    } catch (error) {
      setAgentError(errorMessage(error));
    }
  }

  function stopAgentResponse() {
    agentControllerRef.current?.abort();
  }

  if (!user && !initialLoading) {
    return (
      <div onClickCapture={handleButtonInteraction}>
        <LoginPage
          authMode={authMode}
          credentials={credentials}
          busy={busy}
          notice={notice}
          onAuthModeChange={(mode) => {
            setAuthMode(mode);
            setNotice('');
          }}
          onCredentialsChange={(nextCredentials) => {
            setCredentials(nextCredentials);
            setNotice('');
          }}
          onSubmit={handleAuth}
        />
      </div>
    );
  }

  if (user?.role === 'CUSTOMER') {
    return <CustomerMarketPage user={user} onLogout={logout} />;
  }

  if (user && canAccessAdmin(user.role)) {
    return (
      <AdminPanel
        user={user}
        onLogout={logout}
      />
    );
  }

  return (
    <main className="page-shell" onClickCapture={handleButtonInteraction}>
      <section className="intro">
        <p className="eyebrow">Smart Agriculture</p>
        <h1>移动端 A3 实时看板</h1>
        <p>偏白背景、高对比、扁平化、适老化</p>
      </section>

      <section className="phone" aria-busy={refreshing || busy}>
        <header className="app-header">
          <div>
            <strong>智慧农田</strong>
            <span>{user?.name ?? '农户'} · {headerPlot?.name ?? (data.plots.length > 0 ? '请选择地块' : '未绑定地块')}</span>
          </div>
          <div className="icon-actions">
            <button
              title="刷新"
              aria-label="刷新数据"
              className={refreshing ? 'is-refreshing' : undefined}
              onClick={() => void loadData()}
              disabled={refreshing || busy}
            >
              <RefreshCw size={20} />
            </button>
            <button title="退出登录" aria-label="退出登录" onClick={logout}>
              <LogOut size={20} />
            </button>
          </div>
        </header>

        {notice && <div className="toast">{notice}</div>}

        {view === 'overview' && (
          <DashboardPage
            dashboard={data.dashboard}
            plots={data.plots}
            devices={data.devices}
            alerts={data.alerts}
            telemetryByPlot={data.telemetry}
            irrigation={overviewSelectedIrrigation}
            selectedPlot={overviewSelectedPlot}
            busy={busy}
            onSelectPlot={selectPlot}
            trendMetric={trendMetric}
            trendHistory={trendHistory}
            trendLoading={trendLoading}
            onSelectTrendMetric={selectTrendMetric}
            onIrrigate={issueIrrigation}
          />
        )}

        {view === 'plots' && (
          <PlotsView
            plots={data.plots}
            selectedPlot={selectedPlot}
            telemetry={selectedTelemetry}
            devices={data.devices}
            rules={selectedRules}
            busy={busy}
            detailLoading={plotDetailLoading}
            onSelect={selectPlot}
            onSaveRule={saveThresholdRule}
            onCreateRule={createThresholdRule}
          />
        )}

        {view === 'ask' && (
          <AskView
            selectedPlot={selectedPlot}
            events={events}
            sessionId={agentSessionId}
            messages={agentMessages}
            streaming={agentStreaming}
            error={agentError}
            offlineBacklog={offlineBacklog}
            offlineBacklogLoading={offlineBacklogLoading}
            offlineBacklogError={offlineBacklogError}
            onSend={sendAgentQuestion}
            onNewSession={startNewAgentSession}
            onStop={stopAgentResponse}
            onRequestOfflineBacklog={requestOfflineBacklog}
          />
        )}

        {view === 'manage' && (
          <div className="screen-content">
            <AlarmCenterPage
              plots={data.plots}
              alerts={data.alerts}
              busy={busy}
              onConfirmAlert={confirmAlert}
            />
            <ControlPanelPage
              plots={data.plots}
              selectedPlot={selectedPlot}
              irrigation={selectedIrrigation}
              commands={data.commands}
              busy={busy}
              onSelectPlot={selectPlot}
              onIrrigate={issueIrrigation}
            />
            <KnowledgePage
              user={user}
              knowledge={data.knowledge}
              busy={busy}
              onUploadKnowledge={uploadKnowledge}
            />
            <DeviceListPage plots={data.plots} devices={data.devices} embedded />
          </div>
        )}

        <BottomNav view={view} onChange={setView} />
      </section>
    </main>
  );
}

function PlotsView(props: {
  plots: Plot[];
  selectedPlot?: Plot;
  telemetry?: TelemetryLatest;
  devices: Device[];
  rules: ThresholdRule[];
  busy: boolean;
  detailLoading: boolean;
  onSelect: (plotId: number) => void;
  onSaveRule: (rule: ThresholdRule) => Promise<boolean>;
  onCreateRule: (rule: ThresholdRuleCreateInput) => Promise<boolean>;
}) {
  const [editingRuleId, setEditingRuleId] = useState<number | null>(null);
  const [creatingRule, setCreatingRule] = useState(false);

  useEffect(() => {
    setEditingRuleId(null);
    setCreatingRule(false);
  }, [props.selectedPlot?.id]);

  return (
    <div className="screen-content">
      <section className="section-head">
        <div>
          <h2>地块</h2>
          <p>地块、传感器与阈值规则</p>
        </div>
        <Map size={24} />
      </section>
      <div className="plot-list">
        {props.plots.length === 0 && <EmptyState text="暂无地块数据，后端初始化数据后会显示在这里。" />}
        {props.plots.map((plot) => (
          <button
            key={plot.id}
            className={`plot-row ${props.selectedPlot?.id === plot.id ? 'active' : ''}`}
            onClick={() => props.onSelect(plot.id)}
          >
            <span>
              <strong>{plot.name}</strong>
              <small>{plot.code} · 作物：{plot.cropName || '未设置'} · 种植：{formatPlantingDate(plot.plantingTime)}</small>
            </span>
            <em>{plot.status === 'ACTIVE' ? '启用' : '停用'}</em>
          </button>
        ))}
      </div>

      <section className="detail-band" aria-busy={props.detailLoading}>
        <h3>{props.selectedPlot?.name ?? '地块详情'}</h3>
        <div className="detail-pills">
          <span>作物 {props.selectedPlot?.cropName || '未设置'}</span>
          <span>种植 {formatPlantingDate(props.selectedPlot?.plantingTime)}</span>
          <span>湿度 {formatMetric(props.telemetry?.metrics.soilMoisture)}</span>
          <span>温度 {formatMetric(props.telemetry?.metrics.temperature)}</span>
          <span>光照 {formatMetric(props.telemetry?.metrics.light)}</span>
          <span>{props.devices.filter((device) => device.plotId === props.selectedPlot?.id).length} 台设备</span>
        </div>
      </section>

      <section className="list-card">
        <div className="list-card-heading">
          <h3>阈值规则</h3>
          <button
            type="button"
            className="rule-add-button"
            title={creatingRule ? '取消新增阈值规则' : '新增阈值规则'}
            aria-label={creatingRule ? '取消新增阈值规则' : '新增阈值规则'}
            onClick={() => {
              setEditingRuleId(null);
              setCreatingRule((current) => !current);
            }}
            disabled={!props.selectedPlot || props.busy}
          >
            {creatingRule ? <X size={17} /> : <Plus size={17} />}
            <span>{creatingRule ? '取消' : '新增规则'}</span>
          </button>
        </div>
        {props.rules.length === 0 && !creatingRule && <EmptyState text="当前地块暂无阈值规则。" />}
        {creatingRule && (
          <div className="rule-row editing rule-row-create">
            <span>
              <strong>新增阈值规则</strong>
              <small>规则将保存到当前地块</small>
            </span>
            <ThresholdRuleEditor
              busy={props.busy}
              onCancel={() => setCreatingRule(false)}
              onSave={props.onSaveRule}
              onCreate={props.onCreateRule}
            />
          </div>
        )}
        {props.rules.map((rule) => {
          const editing = editingRuleId === rule.id;
          return (
            <div className={`rule-row ${editing ? 'editing' : ''}`} key={rule.id}>
              <span>
                <strong>{metricName(rule.metric)} {rule.operator} {rule.value}{rule.unit}</strong>
                <small>{rule.durationSeconds}s · {levelName(rule.level)}</small>
              </span>
              <div className="rule-actions">
                <button
                  type="button"
                  className="rule-edit-button"
                  title="编辑阈值"
                  aria-label={`编辑${metricName(rule.metric)}阈值`}
                  onClick={() => {
                    setCreatingRule(false);
                    setEditingRuleId(editing ? null : rule.id);
                  }}
                  disabled={props.busy}
                >
                  <Pencil size={17} />
                </button>
              </div>
              {editing && (
                <ThresholdRuleEditor
                  rule={rule}
                  busy={props.busy}
                  onCancel={() => setEditingRuleId(null)}
                  onSave={props.onSaveRule}
                  onCreate={props.onCreateRule}
                />
              )}
            </div>
          );
        })}
      </section>
    </div>
  );
}

function ThresholdRuleEditor(props: {
  rule?: ThresholdRule;
  busy: boolean;
  onCancel: () => void;
  onSave: (rule: ThresholdRule) => Promise<boolean>;
  onCreate: (rule: ThresholdRuleCreateInput) => Promise<boolean>;
}) {
  const creating = !props.rule;
  const [metric, setMetric] = useState<ThresholdRuleCreateInput['metric']>('soilMoisture');
  const [value, setValue] = useState(String(props.rule?.value ?? '30'));
  const [operator, setOperator] = useState<ThresholdRuleCreateInput['operator']>(() => {
    const current = props.rule?.operator;
    return current === 'LTE' || current === 'GT' || current === 'GTE' ? current : 'LT';
  });
  const [valueError, setValueError] = useState('');
  const [formError, setFormError] = useState('');
  const ruleMetric = props.rule?.metric ?? metric;
  const limits = thresholdMetricLimits(ruleMetric);
  const unit = props.rule?.unit ?? thresholdMetricUnit(metric);

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const nextValue = Number(value);
    if (!value.trim() || !Number.isFinite(nextValue)) {
      setValueError('请输入有效阈值。');
      return;
    }
    if (limits && (nextValue < limits.min || nextValue > limits.max)) {
      setValueError(`阈值范围为 ${limits.min} 至 ${limits.max}${unit}。`);
      return;
    }

    setValueError('');
    setFormError('');
    try {
      const saved = props.rule
        ? await props.onSave({ ...props.rule, value: nextValue, operator })
        : await props.onCreate({ metric, value: nextValue, operator, hysteresis: 0, enabled: true });
      if (saved) {
        props.onCancel();
      } else {
        setFormError('保存失败，请检查后重试。');
      }
    } catch (error) {
      const message = errorMessage(error);
      if (/阈值|threshold|value|范围/.test(message.toLowerCase())) {
        setValueError(message);
      } else {
        setFormError(message);
      }
    }
  }

  return (
    <form className="rule-editor" onSubmit={(event) => void submit(event)} noValidate>
      {creating && (
        <label className="rule-editor-metric">
          监测指标
          <select value={metric} onChange={(event) => setMetric(event.target.value as ThresholdRuleCreateInput['metric'])}>
            <option value="soilMoisture">土壤湿度</option>
            <option value="temperature">环境温度</option>
            <option value="light">光照</option>
          </select>
        </label>
      )}
      <label>
        阈值 ({unit})
        <input
          type="number"
          inputMode="decimal"
          step="any"
          min={limits?.min}
          max={limits?.max}
          value={value}
          onChange={(event) => {
            setValue(event.target.value);
            setValueError('');
          }}
          className={valueError ? 'input-invalid' : undefined}
          aria-invalid={Boolean(valueError)}
          aria-describedby={valueError ? 'threshold-value-error' : undefined}
        />
        {valueError && <p className="field-error" id="threshold-value-error" role="alert">{valueError}</p>}
      </label>
      <label>
        触发条件
        <select
          value={operator}
          onChange={(event) => setOperator(event.target.value as ThresholdRuleCreateInput['operator'])}
        >
          <option value="LT">低于 (&lt;)</option>
          <option value="LTE">低于等于 (&le;)</option>
          <option value="GT">高于 (&gt;)</option>
          <option value="GTE">高于等于 (&ge;)</option>
        </select>
      </label>
      {formError && <p className="rule-editor-error" role="alert">{formError}</p>}
      <div className="rule-editor-actions">
        <button type="submit" className="rule-save-button" disabled={props.busy}>
          <Check size={17} />
          保存
        </button>
        <button type="button" className="rule-cancel-button" onClick={props.onCancel} disabled={props.busy}>
          <X size={17} />
          取消
        </button>
      </div>
    </form>
  );
}

function AskView(props: {
  selectedPlot?: Plot;
  events: EventNotice[];
  sessionId: string | null;
  messages: AgentChatMessage[];
  streaming: boolean;
  error: string;
  offlineBacklog: string;
  offlineBacklogLoading: boolean;
  offlineBacklogError: string;
  onSend: (question: string) => Promise<void>;
  onNewSession: () => Promise<void>;
  onStop: () => void;
  onRequestOfflineBacklog: () => Promise<void>;
}) {
  const [draft, setDraft] = useState('');

  function submitQuestion(question = draft) {
    const content = question.trim();
    if (!content || props.streaming) return;
    setDraft('');
    void props.onSend(content);
  }

  return (
    <div className="screen-content">
      <section className="section-head ai-chat-page-head">
        <div>
          <h2>AI 农事助手</h2>
          <p>{props.selectedPlot?.name ?? '当前地块'} · 在线问答</p>
        </div>
        <Bot size={25} />
      </section>
      <section className="ai-chat">
        <header className="chat-header">
          <div>
            <strong>农事对话</strong>
            <span>{props.sessionId ? '当前会话已连接' : '发起问题即可开始新会话'}</span>
          </div>
          {props.messages.length > 0 && (
            <button
              type="button"
              className="chat-header-action"
              title="新建会话"
              aria-label="新建会话"
              onClick={() => void props.onNewSession()}
              disabled={props.streaming}
            >
              <Plus size={19} />
            </button>
          )}
        </header>
        <div className="chat-transcript" aria-live="polite">
          {props.messages.length === 0 && (
            <div className="chat-welcome">
              <Bot size={28} />
              <strong>你好，我是农事助手。</strong>
              <span>想了解当前地块的什么情况？</span>
              <div className="chat-suggestions">
                {['现在需要灌溉吗？', '查看当前告警', '给出今日巡检建议'].map((question) => (
                  <button type="button" key={question} onClick={() => submitQuestion(question)}>
                    {question}
                  </button>
                ))}
              </div>
            </div>
          )}
          {props.messages.map((message) => (
            <article className={`chat-message ${message.role === 'USER' ? 'user' : 'assistant'}`} key={message.id}>
              <span className="chat-message-role">{message.role === 'USER' ? '我' : '农事助手'}</span>
              <div className="chat-bubble">
                {message.content || <span className="chat-typing">正在思考</span>}
              </div>
              {message.sources && message.sources.length > 0 && (
                <div className="chat-sources">
                  {message.sources.map((source, index) => (
                    <span key={`${source.docId ?? source.title ?? 'source'}-${index}`}>
                      {source.title || '知识库参考'}
                    </span>
                  ))}
                </div>
              )}
            </article>
          ))}
        </div>
        <form
          className="chat-composer"
          onSubmit={(event) => {
            event.preventDefault();
            submitQuestion();
          }}
        >
          <textarea
            value={draft}
            maxLength={2000}
            placeholder="输入农事问题"
            aria-label="输入农事问题"
            disabled={props.streaming}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if ((event.ctrlKey || event.metaKey) && event.key === 'Enter') {
                event.preventDefault();
                submitQuestion();
              }
            }}
          />
          {props.streaming ? (
            <button type="button" className="chat-send-button stop" title="停止生成" aria-label="停止生成" onClick={props.onStop}>
              <Square size={18} fill="currentColor" />
            </button>
          ) : (
            <button
              type="submit"
              className="chat-send-button"
              title="发送"
              aria-label="发送"
              disabled={!draft.trim()}
            >
              <Send size={19} />
            </button>
          )}
        </form>
        {props.error && <p className="chat-error">{props.error}</p>}
      </section>
      <section className="list-card">
        <h3>系统实时事件</h3>
        {props.events.length === 0 && <EmptyState text="事件流连接后，告警与命令结果会出现在这里。" />}
        {props.events.map((event) => (
          <div className="plain-row" key={`${event.id}-${event.receivedAt}`}>
            <span>
              <strong>{event.type}</strong>
              <small>{formatTime(event.receivedAt)}</small>
            </span>
            <Bell size={18} />
          </div>
        ))}
      </section>
      <section className="offline-backlog" aria-busy={props.offlineBacklogLoading}>
        <header className="offline-backlog-header">
          <div className="offline-backlog-title">
            <Inbox size={19} />
            <strong>离线补发</strong>
          </div>
          <button
            type="button"
            className={`offline-backlog-action${props.offlineBacklogLoading ? ' is-refreshing' : ''}`}
            onClick={() => void props.onRequestOfflineBacklog()}
            disabled={props.offlineBacklogLoading}
          >
            <RefreshCw size={16} />
            <span>{props.offlineBacklogLoading ? '获取中' : '获取补发'}</span>
          </button>
        </header>
        <div className="offline-backlog-content" aria-live="polite">
          {props.offlineBacklog ? (
            <p>{props.offlineBacklog}</p>
          ) : (
            <span className="offline-backlog-empty">
              {props.offlineBacklogLoading ? '正在获取补发信息' : '暂无补发信息'}
            </span>
          )}
          {props.offlineBacklogError && <p className="offline-backlog-error" role="alert">{props.offlineBacklogError}</p>}
        </div>
      </section>
    </div>
  );
}

function BottomNav(props: { view: View; onChange: (view: View) => void }) {
  const items: Array<{ view: View; label: string; icon: React.ReactNode }> = [
    { view: 'overview', label: '总览', icon: <Grid2X2 size={20} /> },
    { view: 'plots', label: '地块', icon: <Map size={20} /> },
    { view: 'ask', label: '问答', icon: <MessageSquare size={20} /> },
    { view: 'manage', label: '管理', icon: <Settings size={20} /> }
  ];
  return (
    <nav className="bottom-nav">
      {items.map((item) => (
        <button
          key={item.view}
          className={props.view === item.view ? 'active' : ''}
          onClick={() => props.onChange(item.view)}
        >
          {item.icon}
          <span>{item.label}</span>
        </button>
      ))}
    </nav>
  );
}

function EmptyState(props: { text: string }) {
  return (
    <div className="empty-state">
      <Sprout size={20} />
      <span>{props.text}</span>
    </div>
  );
}

type AppData = {
  dashboard: DashboardOverview | null;
  plots: Plot[];
  devices: Device[];
  alerts: AlertItem[];
  commands: CommandItem[];
  knowledge: KnowledgeDocument[];
  telemetry: Record<number, TelemetryLatest>;
  irrigation: Record<number, IrrigationStatus>;
  thresholds: Record<number, ThresholdRule[]>;
};

const emptyData: AppData = {
  dashboard: null,
  plots: [],
  devices: [],
  alerts: [],
  commands: [],
  knowledge: [],
  telemetry: {},
  irrigation: {},
  thresholds: {}
};

function newChatMessageId(prefix: string) {
  if ('randomUUID' in crypto) {
    return `${prefix}-${crypto.randomUUID()}`;
  }
  return `${prefix}-${Date.now()}-${Math.random().toString(16).slice(2)}`;
}

function isAbortError(error: unknown) {
  return error instanceof DOMException && error.name === 'AbortError';
}

function shouldRenewAgentSession(message: string) {
  return message.includes('会话已超时') || message.includes('会话已结束') || message.includes('会话不存在');
}

function applyTelemetryEvent(current: AppData, event: EventNotice): AppData {
  const plotId = eventNumber(event.data.plotId);
  const soilMoisture = eventNumber(event.data.soilMoisture);
  const temperature = eventNumber(event.data.temperature);
  const light = eventNumber(event.data.light);
  if (plotId == null || (soilMoisture == null && temperature == null && light == null)) {
    return current;
  }

  const existing = current.telemetry[plotId];
  const sampleTime = eventTime(event) ?? existing?.sampleTime ?? null;
  if (isOlderSample(sampleTime, existing?.sampleTime)) {
    return current;
  }

  const metrics = {
    ...existing?.metrics,
    ...(soilMoisture == null
      ? {}
      : { soilMoisture: { value: soilMoisture, unit: existing?.metrics.soilMoisture?.unit ?? '%' } }),
    ...(temperature == null
      ? {}
      : {
          temperature: {
            value: temperature,
            unit: existing?.metrics.temperature?.unit ?? current.dashboard?.avgTemperature?.unit ?? celsiusUnit
          }
        }),
    ...(light == null
      ? {}
      : { light: { value: light, unit: existing?.metrics.light?.unit ?? 'lx' } })
  };
  const telemetry: Record<number, TelemetryLatest> = {
    ...current.telemetry,
    [plotId]: {
      plotId,
      sampleTime,
      metrics,
      sourceDevices: existing?.sourceDevices ?? []
    }
  };

  const plots = current.plots.map((plot) =>
    plot.id === plotId
      ? {
          ...plot,
          ...(soilMoisture == null ? {} : { soilMoisture }),
          ...(temperature == null ? {} : { temperature })
        }
      : plot
  );

  return {
    ...current,
    telemetry,
    plots,
    dashboard: updateDashboardTelemetry(current.dashboard, plotId, soilMoisture, temperature, sampleTime)
  };
}

function updateDashboardTelemetry(
  dashboard: DashboardOverview | null,
  plotId: number,
  soilMoisture: number | undefined,
  temperature: number | undefined,
  sampleTime: string | null
) {
  if (!dashboard) return dashboard;

  let updated = false;
  const plots = dashboard.plots.map((plot) => {
    if (plot.id !== plotId) return plot;
    updated = true;
    return {
      ...plot,
      ...(soilMoisture == null ? {} : { soilMoisture }),
      ...(temperature == null ? {} : { temperature })
    };
  });
  if (!updated) return dashboard;

  return {
    ...dashboard,
    sampleTime: sampleTime ?? dashboard.sampleTime,
    avgSoilMoisture: averageMetric(plots, 'soilMoisture', '%'),
    avgTemperature: averageMetric(plots, 'temperature', dashboard.avgTemperature?.unit ?? celsiusUnit),
    plots
  };
}

function averageMetric(
  plots: DashboardOverview['plots'],
  key: 'soilMoisture' | 'temperature',
  unit: string
) {
  const values = plots
    .map((plot) => plot[key])
    .filter((value): value is number => typeof value === 'number' && Number.isFinite(value));
  if (values.length === 0) return null;
  return { value: values.reduce((sum, value) => sum + value, 0) / values.length, unit };
}

function eventNumber(value: unknown) {
  return typeof value === 'number' && Number.isFinite(value) ? value : undefined;
}

function eventTime(event: EventNotice) {
  const sampleTime = event.data.sampleTime;
  if (typeof sampleTime === 'string' && !Number.isNaN(new Date(sampleTime).getTime())) {
    return sampleTime;
  }
  return event.receivedAt;
}

function isOlderSample(incoming: string | null, current?: string | null) {
  if (!incoming || !current) return false;
  return new Date(incoming).getTime() < new Date(current).getTime();
}

function errorMessage(error: unknown) {
  if (error instanceof Error) return error.message;
  return '操作失败，请稍后重试';
}

function formatMetric(metric?: { value: number; unit: string }) {
  if (!metric) return '--';
  return `${metric.value.toFixed(1)}${metric.unit}`;
}

function formatTime(value?: string | null) {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '--';
  return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

function formatPlantingDate(value?: string | null) {
  if (!value) return '未记录';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '未记录';
  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });
}

function metricName(metric: string) {
  if (metric === 'soilMoisture') return '土壤湿度';
  if (metric.toLowerCase().includes('temperature')) return '温度';
  if (metric === 'light') return '光照';
  return metric;
}

function thresholdMetricUnit(metric: string) {
  if (metric === 'soilMoisture') return '%';
  if (metric === 'temperature') return celsiusUnit;
  if (metric === 'light') return 'lx';
  return '';
}

function thresholdMetricLimits(metric: string) {
  if (metric === 'soilMoisture') return { min: 0, max: 100 };
  if (metric === 'temperature') return { min: -50, max: 100 };
  if (metric === 'light') return { min: 0, max: 200000 };
  return undefined;
}

function levelName(level: string) {
  const names: Record<string, string> = { LOW: '低', MEDIUM: '中', HIGH: '高' };
  return names[level] ?? level;
}
