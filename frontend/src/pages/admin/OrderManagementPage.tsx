import { Check, ClipboardList, Loader2, RefreshCw, ScanSearch, X, XCircle } from 'lucide-react';
import { FormEvent, useCallback, useEffect, useState } from 'react';
import { api } from '../../api';
import type { Order, OrderDetail, OrderItem } from '../../types';

const STATUS_NAMES: Record<string, string> = {
  PENDING: '待审批',
  APPROVED: '已批准',
  TRADING: '待交易',
  CONFIRMED: '已成交',
  CLOSED: '未成交',
  REJECTED: '已拒绝',
  DELETED: '已结束'
};

export function OrderManagementPage() {
  const [status, setStatus] = useState('');
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [detail, setDetail] = useState<OrderDetail | null>(null);
  const [detailLoading, setDetailLoading] = useState(false);
  const [detailError, setDetailError] = useState('');
  const [confirming, setConfirming] = useState<OrderDetail | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const result = await api.orders({ status: status || undefined });
      setOrders(result.items);
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }, [status]);

  useEffect(() => { void load(); }, [load]);

  async function openDetail(orderId: number) {
    setDetail(null);
    setDetailError('');
    setDetailLoading(true);
    try {
      setDetail(await api.order(orderId));
    } catch (requestError) {
      setDetailError(errorMessage(requestError));
    } finally {
      setDetailLoading(false);
    }
  }

  async function review(order: Order, action: 'approve' | 'reject') {
    const label = action === 'approve' ? '通过审批并进入待交易' : '拒绝该采购意向';
    if (!window.confirm(`确定${label}吗？`)) return;
    setBusy(true);
    setNotice('');
    try {
      await api.reviewOrder(order.id, action);
      setNotice(action === 'approve' ? '意向已批准，库存占用将按待交易规则计算。' : '意向已拒绝。');
      await load();
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally { setBusy(false); }
  }

  async function closeOrder(order: Order) {
    if (!window.confirm(`确定关闭意向单 ${order.orderNo} 吗？关闭后将释放全部待交易占用。`)) return;
    setBusy(true);
    setNotice('');
    try {
      await api.terminateOrder(order.id, 'close');
      setNotice('意向已关闭，可售数量已释放。');
      await load();
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally { setBusy(false); }
  }

  async function openConfirm(order: Order) {
    setDetailLoading(true);
    setDetailError('');
    try {
      setConfirming(await api.order(order.id));
    } catch (requestError) {
      setDetailError(errorMessage(requestError));
    } finally { setDetailLoading(false); }
  }

  const tabs = [
    { key: '', label: '全部' },
    { key: 'PENDING', label: '待审批' },
    { key: 'TRADING', label: '待交易' },
    { key: 'REJECTED', label: '已拒绝' },
    { key: 'CLOSED', label: '未成交' }
  ];

  return (
    <div className="admin-stack">
      {error && <div className="admin-error">{error}</div>}
      {notice && <div className="admin-notice">{notice}</div>}
      <section className="orders-hero">
        <div>
          <span className="orders-hero-icon"><ClipboardList size={23} /></span>
          <div><h2>采购意向管理</h2><p>审批、待交易占用、实际成交和关闭均以订单状态为准。</p></div>
        </div>
        <button type="button" className="admin-icon-refresh" onClick={() => void load()} title="刷新订单"><RefreshCw size={18} /></button>
      </section>
      <div className="admin-filter-bar">
        <div className="admin-tabs" role="tablist" aria-label="订单状态">
          {tabs.map((tab) => <button type="button" key={tab.key} className={status === tab.key ? 'active' : ''} onClick={() => setStatus(tab.key)}>{tab.label}</button>)}
        </div>
      </div>
      <section className="admin-card">
        <div className="admin-card-heading"><div><h3>采购意向列表</h3><p className="admin-card-copy">确认成交时按实际数量扣库存；订单完成后从列表中软删除。</p></div></div>
        {loading ? <div className="admin-empty"><Loader2 size={22} className="admin-spin" />加载中…</div> : (
          <div className="admin-table-wrap"><table className="admin-table orders-table"><thead><tr><th>意向单号</th><th>顾客</th><th>采购明细</th><th>期望时间</th><th>状态</th><th>创建时间</th><th>操作</th></tr></thead><tbody>
            {orders.map((order) => <tr key={order.id}>
              <td className="admin-strong">{order.orderNo}</td>
              <td>{order.customerName || `用户 #${order.customerId}`}</td>
              <td className="admin-ellipsis" title={itemsText(order.items)}>{itemsText(order.items)}</td>
              <td>{formatDateTime(order.expectedTime)}</td>
              <td><StatusBadge status={order.status} /></td>
              <td>{formatDateTime(order.createdAt)}</td>
              <td className="admin-actions orders-actions">
                <button type="button" className="admin-link-btn" onClick={() => void openDetail(order.id)}><ScanSearch size={14} />详情</button>
                {order.status === 'PENDING' && <><button type="button" className="admin-link-btn" disabled={busy} onClick={() => void review(order, 'approve')}><Check size={14} />通过</button><button type="button" className="admin-link-btn danger" disabled={busy} onClick={() => void review(order, 'reject')}><X size={14} />拒绝</button></>}
                {(order.status === 'TRADING' || order.status === 'APPROVED') && <><button type="button" className="admin-link-btn" disabled={busy} onClick={() => void openConfirm(order)}>成交</button><button type="button" className="admin-link-btn danger" disabled={busy} onClick={() => void closeOrder(order)}><XCircle size={14} />关闭</button></>}
              </td>
            </tr>)}
            {orders.length === 0 && <tr><td colSpan={7} className="admin-empty"><ClipboardList size={18} />暂无符合条件的采购意向。</td></tr>}
          </tbody></table></div>
        )}
      </section>

      {(detail || detailLoading || detailError) && <OrderDrawer detail={detail} loading={detailLoading} error={detailError} onClose={() => { setDetail(null); setDetailError(''); }} />}
      {confirming && <ConfirmOrderModal detail={confirming} onClose={() => setConfirming(null)} onConfirmed={async () => { setConfirming(null); setNotice('订单已按实际成交数量扣库并结束。'); await load(); }} />}
    </div>
  );
}

function OrderDrawer(props: { detail: OrderDetail | null; loading: boolean; error: string; onClose: () => void }) {
  return <div className="admin-drawer-mask"><aside className="admin-drawer" role="dialog" aria-modal="true" aria-label="采购意向详情"><header><div><h3>采购意向详情</h3><span>{props.detail?.orderNo ?? '正在读取订单信息'}</span></div><button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button></header><div className="admin-preview-body">
    {props.loading && <div className="admin-empty"><Loader2 size={22} className="admin-spin" />加载中…</div>}
    {props.error && <div className="admin-error">{props.error}</div>}
    {props.detail && <div className="order-detail">
      <div className="order-detail-meta"><span>顾客：{props.detail.customerName || `用户 #${props.detail.customerId}`}</span><span>状态：{STATUS_NAMES[props.detail.status] ?? props.detail.status}</span><span>期望：{formatDateTime(props.detail.expectedTime)}</span></div>
      <h4>物料明细与可用数量</h4>
      <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>物料</th><th>意向数量</th><th>当前可用</th></tr></thead><tbody>{props.detail.items.map((item) => <tr key={`${item.materialId}-${item.id ?? ''}`}><td className="admin-strong">{item.materialName || `物料 #${item.materialId}`}</td><td>{formatQuantity(item.quantity)} {item.unit || ''}</td><td><span className="warehouse-available">{item.availableQuantity == null ? '--' : `${formatQuantity(item.availableQuantity)} ${item.unit || ''}`}</span></td></tr>)}</tbody></table></div>
      {props.detail.remark && <><h4>备注</h4><p className="admin-preview-text">{props.detail.remark}</p></>}
    </div>}
  </div></aside></div>;
}

function ConfirmOrderModal(props: { detail: OrderDetail; onClose: () => void; onConfirmed: () => Promise<void> }) {
  const [quantities, setQuantities] = useState<Record<number, string>>(() => Object.fromEntries(props.detail.items.map((item) => [item.materialId, String(item.quantity)])));
  const [errors, setErrors] = useState<Record<number, string>>({});
  const [formError, setFormError] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit(event: FormEvent) {
    event.preventDefault();
    const nextErrors: Record<number, string> = {};
    const items = props.detail.items.map((item) => {
      const quantity = Number(quantities[item.materialId]);
      if (!Number.isFinite(quantity) || quantity <= 0) nextErrors[item.materialId] = '请输入大于 0 的实际成交数量。';
      if (item.availableQuantity != null && quantity > numeric(item.availableQuantity)) nextErrors[item.materialId] = `当前可用 ${formatQuantity(item.availableQuantity)} ${item.unit || ''}。`;
      return { materialId: item.materialId, quantity };
    });
    if (Object.keys(nextErrors).length > 0) { setErrors(nextErrors); return; }
    setBusy(true);
    try {
      await api.confirmOrder(props.detail.id, { items });
      await props.onConfirmed();
    } catch (requestError) {
      setFormError(errorMessage(requestError));
    } finally { setBusy(false); }
  }

  return <div className="admin-modal-mask"><form className="admin-modal" onSubmit={(event) => void submit(event)} noValidate><header><h3>确认面谈成交</h3><button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button></header><p className="admin-modal-hint">请录入实际成交数量。系统会在同一事务中校验库存、写出库流水并结束该订单。</p>
    {props.detail.items.map((item) => <label key={item.materialId}>{item.materialName || `物料 #${item.materialId}`}<small className="order-field-help">意向 {formatQuantity(item.quantity)} {item.unit || ''}，当前可用 {item.availableQuantity == null ? '--' : `${formatQuantity(item.availableQuantity)} ${item.unit || ''}`}</small><input type="number" min="0" step="0.001" value={quantities[item.materialId] ?? ''} onChange={(event) => { setQuantities((current) => ({ ...current, [item.materialId]: event.target.value })); setErrors((current) => ({ ...current, [item.materialId]: '' })); setFormError(''); }} className={errors[item.materialId] ? 'input-invalid' : undefined} />{errors[item.materialId] && <p className="field-error" role="alert">{errors[item.materialId]}</p>}</label>)}
    {formError && <p className="field-error admin-form-error" role="alert">{formError}</p>}
    <footer><button type="button" onClick={props.onClose}>取消</button><button type="submit" className="primary" disabled={busy}>{busy ? '成交处理中…' : '确认成交并扣库'}</button></footer>
  </form></div>;
}

function StatusBadge(props: { status: string }) {
  const tone = props.status === 'PENDING' ? 'warn' : props.status === 'TRADING' || props.status === 'APPROVED' ? 'admin' : props.status === 'CONFIRMED' ? 'ok' : 'off';
  return <span className={`admin-badge ${tone}`}>{STATUS_NAMES[props.status] ?? props.status}</span>;
}

function itemsText(items: OrderItem[]) { return items.map((item) => `${item.materialName || `物料 #${item.materialId}`} ${formatQuantity(item.quantity)} ${item.unit || ''}`).join('、') || '--'; }
function numeric(value: number | string) { const parsed = Number(value); return Number.isFinite(parsed) ? parsed : 0; }
function formatQuantity(value: number | string) { return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(numeric(value)); }
function formatDateTime(value?: string | null) { if (!value) return '--'; const date = new Date(value); return Number.isNaN(date.getTime()) ? value : date.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' }); }
function errorMessage(error: unknown) { return error instanceof Error ? error.message : '请求失败，请稍后重试。'; }
