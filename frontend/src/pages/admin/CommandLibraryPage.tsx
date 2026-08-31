import { ClipboardList, Loader2, RefreshCw, ScanSearch, SlidersHorizontal, X } from 'lucide-react';
import { FormEvent, useCallback, useEffect, useState } from 'react';
import { api } from '../../api';
import type { AdminCommand } from '../../types';

const COMMAND_STATUS_NAMES: Record<string, string> = {
  PENDING: '待执行',
  SENT: '已下发',
  RUNNING: '执行中',
  SUCCESS: '成功',
  SUCCEEDED: '成功',
  FAILED: '失败',
  TIMEOUT: '超时',
  CANCELED: '已取消'
};

const COMMAND_ACTION_NAMES: Record<string, string> = {
  OPEN: '开启灌溉',
  CLOSE: '关闭灌溉'
};

type Query = { plotId?: number; deviceId?: number; status?: string; startAt?: string; endAt?: string };

export function CommandLibraryPage() {
  const [form, setForm] = useState({ plotId: '', deviceId: '', status: '', startAt: '', endAt: '' });
  const [query, setQuery] = useState<Query>({});
  const [commands, setCommands] = useState<AdminCommand[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [fieldError, setFieldError] = useState('');
  const [detail, setDetail] = useState<AdminCommand | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState('');

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const result = await api.adminCommands(query);
      setCommands(result.items);
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally { setLoading(false); }
  }, [query]);

  useEffect(() => { void load(); }, [load]);

  function submitFilter(event: FormEvent) {
    event.preventDefault();
    const plotId = positiveInteger(form.plotId);
    const deviceId = positiveInteger(form.deviceId);
    if ((form.plotId && !plotId) || (form.deviceId && !deviceId)) {
      setFieldError('地块 ID 和设备 ID 必须为正整数。');
      return;
    }
    setFieldError('');
    setQuery({
      plotId,
      deviceId,
      status: form.status || undefined,
      startAt: toIso(form.startAt),
      endAt: toIso(form.endAt)
    });
  }

  async function openDetail(commandId: string) {
    setDetail(null);
    setDetailError('');
    setDetailLoading(true);
    try {
      setDetail(await api.adminCommand(commandId));
    } catch (requestError) {
      setDetailError(errorMessage(requestError));
    } finally { setDetailLoading(false); }
  }

  return <div className="admin-stack">
    {error && <div className="admin-error">{error}</div>}
    <section className="commands-hero">
      <div><span className="commands-hero-icon"><ClipboardList size={23} /></span><div><h2>设备命令库</h2><p>集中审计所有灌溉控制指令、回执与失败原因。</p></div></div>
      <button type="button" className="admin-icon-refresh" onClick={() => void load()} title="刷新命令记录"><RefreshCw size={18} /></button>
    </section>
    <section className="admin-card">
      <div className="admin-card-heading"><div><h3>筛选命令</h3><p className="admin-card-copy">此页面仅用于查看审计记录，不提供重发或直接控制。</p></div></div>
      <form className="command-filter-grid" onSubmit={submitFilter} noValidate>
        <label>地块 ID<input inputMode="numeric" value={form.plotId} onChange={(event) => { setForm((current) => ({ ...current, plotId: event.target.value })); setFieldError(''); }} placeholder="全部" /></label>
        <label>设备 ID<input inputMode="numeric" value={form.deviceId} onChange={(event) => { setForm((current) => ({ ...current, deviceId: event.target.value })); setFieldError(''); }} placeholder="全部" /></label>
        <label>执行状态<select value={form.status} onChange={(event) => setForm((current) => ({ ...current, status: event.target.value }))}><option value="">全部状态</option><option value="SUCCESS">成功</option><option value="FAILED">失败</option><option value="TIMEOUT">超时</option><option value="PENDING">待执行</option></select></label>
        <label>开始时间<input type="datetime-local" value={form.startAt} onChange={(event) => setForm((current) => ({ ...current, startAt: event.target.value }))} /></label>
        <label>结束时间<input type="datetime-local" value={form.endAt} onChange={(event) => setForm((current) => ({ ...current, endAt: event.target.value }))} /></label>
        <button type="submit" className="primary"><SlidersHorizontal size={15} />应用筛选</button>
      </form>
      {fieldError && <p className="field-error command-filter-error" role="alert">{fieldError}</p>}
    </section>
    <section className="admin-card">
      <div className="admin-card-heading"><div><h3>命令记录</h3><p className="admin-card-copy">按创建时间倒序展示，可查看下发参数和设备回执。</p></div></div>
      {loading ? <div className="admin-empty"><Loader2 size={22} className="admin-spin" />加载中…</div> : <div className="admin-table-wrap"><table className="admin-table command-table"><thead><tr><th>命令编号</th><th>地块</th><th>设备</th><th>动作</th><th>状态</th><th>操作人</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
        {commands.map((command) => <tr key={command.id}><td className="admin-strong command-id">{command.id}</td><td>{command.plotCode || (command.plotId ? `#${command.plotId}` : '--')}</td><td>{command.deviceName || (command.deviceId ? `设备 #${command.deviceId}` : '--')}</td><td>{COMMAND_ACTION_NAMES[command.action] ?? command.action}</td><td><CommandStatus status={command.status} /></td><td>{command.operatorName || '--'}</td><td>{formatDateTime(command.createdAt)}</td><td><button type="button" className="admin-link-btn" onClick={() => void openDetail(command.id)}><ScanSearch size={14} />查看</button></td></tr>)}
        {commands.length === 0 && <tr><td colSpan={8} className="admin-empty"><ClipboardList size={18} />暂无符合条件的命令记录。</td></tr>}
      </tbody></table></div>}
    </section>
    {(detail || detailLoading || detailError) && <CommandDrawer command={detail} loading={detailLoading} error={detailError} onClose={() => { setDetail(null); setDetailError(''); }} />}
  </div>;
}

function CommandDrawer(props: { command: AdminCommand | null; loading: boolean; error: string; onClose: () => void }) {
  return <div className="admin-drawer-mask"><aside className="admin-drawer" role="dialog" aria-modal="true" aria-label="命令详情"><header><div><h3>命令详情</h3><span>{props.command?.id ?? '正在读取命令信息'}</span></div><button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button></header><div className="admin-preview-body">
    {props.loading && <div className="admin-empty"><Loader2 size={22} className="admin-spin" />加载中…</div>}
    {props.error && <div className="admin-error">{props.error}</div>}
    {props.command && <div className="command-detail">
      <div className="order-detail-meta"><span>动作：{COMMAND_ACTION_NAMES[props.command.action] ?? props.command.action}</span><span>状态：{COMMAND_STATUS_NAMES[props.command.status] ?? props.command.status}</span><span>地块：{props.command.plotCode || (props.command.plotId ? `#${props.command.plotId}` : '--')}</span><span>设备：{props.command.deviceName || (props.command.deviceId ? `#${props.command.deviceId}` : '--')}</span><span>操作人：{props.command.operatorName || '--'}</span><span>创建：{formatDateTime(props.command.createdAt)}</span></div>
      <h4>下发参数</h4><pre className="admin-preview-text">{prettyPayload(props.command.requestPayload)}</pre>
      <h4>设备回执</h4><pre className="admin-preview-text">{prettyPayload(props.command.ackPayload)}</pre>
      {props.command.errorMessage && <><h4>失败原因</h4><p className="admin-error">{props.command.errorMessage}</p></>}
    </div>}
  </div></aside></div>;
}

function CommandStatus(props: { status: string }) {
  const tone = props.status === 'SUCCESS' || props.status === 'SUCCEEDED' ? 'ok' : props.status === 'FAILED' || props.status === 'TIMEOUT' ? 'danger' : 'warn';
  return <span className={`admin-badge ${tone}`}>{COMMAND_STATUS_NAMES[props.status] ?? props.status}</span>;
}

function positiveInteger(value: string) { const parsed = Number(value); return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined; }
function toIso(value: string) { return value ? new Date(value).toISOString() : undefined; }
function formatDateTime(value?: string | null) { if (!value) return '--'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }
function prettyPayload(value: unknown) { if (value == null) return '暂无数据'; try { return JSON.stringify(value, null, 2); } catch { return String(value); } }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : '请求失败，请稍后重试。'; }
