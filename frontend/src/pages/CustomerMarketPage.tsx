import { CalendarDays, Leaf, Loader2, LogOut, PackageCheck, RefreshCw, ShoppingBag, XCircle } from 'lucide-react';
import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { api } from '../api';
import type { MarketMaterial, Material, Order } from '../types';

type QuantityUnit = 'kg' | 't';
type FieldErrors = Partial<Record<'productName' | 'quantity' | 'form', string>>;

const ORDER_STATUS_NAMES: Record<string, string> = {
  PENDING: '待审批',
  APPROVED: '已批准',
  TRADING: '待交易',
  CONFIRMED: '已成交',
  CLOSED: '未成交',
  REJECTED: '已拒绝',
  DELETED: '已结束'
};

export function CustomerMarketPage(props: { user: { name: string }; onLogout: () => void }) {
  const [materials, setMaterials] = useState<MarketMaterial[]>([]);
  const [orders, setOrders] = useState<Order[]>([]);
  const [loading, setLoading] = useState(true);
  const [refreshing, setRefreshing] = useState(false);
  const [busy, setBusy] = useState(false);
  const [creatingProduct, setCreatingProduct] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [productName, setProductName] = useState('');
  const [quantity, setQuantity] = useState('');
  const [quantityUnit, setQuantityUnit] = useState<QuantityUnit>('kg');
  const [expectedTime, setExpectedTime] = useState('');
  const [remark, setRemark] = useState('');
  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});

  const load = useCallback(async (showSpinner = true) => {
    if (showSpinner) setLoading(true);
    setError('');
    try {
      const [market, nextOrders] = await Promise.all([
        api.marketMaterials(),
        api.orders()
      ]);
      setMaterials(market.items);
      setOrders(nextOrders.items);
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
      setRefreshing(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  const selectedMaterial = useMemo(
    () => materials.find((material) => materialNameKey(material.name) === materialNameKey(productName)),
    [materials, productName]
  );
  const missingProductName = useMemo(() => {
    const name = productName.trim();
    return name && !selectedMaterial ? name : '';
  }, [productName, selectedMaterial]);

  function clearFieldError(field: keyof FieldErrors) {
    setFieldErrors((current) => {
      if (!current[field] && !current.form) return current;
      const next = { ...current };
      delete next[field];
      delete next.form;
      return next;
    });
  }

  async function createMissingProduct() {
    const name = productName.trim();
    if (!name || selectedMaterial) return;
    setCreatingProduct(true);
    setNotice('');
    try {
      const material = await api.createMarketMaterial({ name, unit: quantityUnit });
      setMaterials((current) => mergeMarketMaterial(current, material));
      setProductName(material.name);
      setFieldErrors((current) => {
        const next = { ...current };
        delete next.productName;
        delete next.form;
        return next;
      });
      setNotice(`“${material.name}”已新增至物料库，完成入库后即可提交采购意向。`);
    } catch (requestError) {
      setFieldErrors((current) => ({ ...current, productName: errorMessage(requestError) }));
    } finally {
      setCreatingProduct(false);
    }
  }

  async function submitIntent(event: FormEvent) {
    event.preventDefault();
    const parsedQuantity = Number(quantity);
    const typedProductName = productName.trim();
    const quantityInMaterialUnit = selectedMaterial && Number.isFinite(parsedQuantity)
      ? convertMassQuantity(parsedQuantity, quantityUnit, selectedMaterial.unit)
      : null;
    const nextErrors: FieldErrors = {};

    if (!typedProductName) {
      nextErrors.productName = '请填写需要采购的农产品。';
    } else if (!selectedMaterial) {
      nextErrors.productName = `未找到“${typedProductName}”，请填写在售农产品。`;
    }
    if (!Number.isFinite(parsedQuantity) || parsedQuantity <= 0) {
      nextErrors.quantity = '请输入大于 0 的意向数量。';
    } else if (selectedMaterial && quantityInMaterialUnit == null) {
      nextErrors.quantity = `当前农产品库存单位为 ${selectedMaterial.unit}，暂不支持换算。`;
    }
    if (remark.trim().length > 500) nextErrors.form = '备注不能超过 500 个字符。';
    if (Object.keys(nextErrors).length > 0) {
      setFieldErrors(nextErrors);
      return;
    }
    if (!selectedMaterial || quantityInMaterialUnit == null) return;

    setBusy(true);
    setNotice('');
    try {
      await api.createOrder({
        items: [{ materialId: selectedMaterial.id, quantity: quantityInMaterialUnit }],
        expectedTime: expectedTime ? new Date(expectedTime).toISOString() : undefined,
        remark: remark.trim() || undefined
      });
      setProductName('');
      setQuantity('');
      setQuantityUnit('kg');
      setExpectedTime('');
      setRemark('');
      setFieldErrors({});
      setNotice('采购意向已提交，等待仓库管理员审核。');
      await load(false);
    } catch (requestError) {
      setFieldErrors({ form: errorMessage(requestError) });
    } finally {
      setBusy(false);
    }
  }

  async function cancelOrder(order: Order) {
    if (!window.confirm(`确定取消意向单 ${order.orderNo} 吗？`)) return;
    setBusy(true);
    setNotice('');
    try {
      await api.terminateOrder(order.id, 'cancel');
      setNotice('采购意向已取消。');
      await load(false);
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="customer-shell">
      <header className="customer-topbar">
        <div>
          <span className="customer-eyebrow">智慧农业交易市场</span>
          <h1>农产品采购意向</h1>
          <p>{props.user.name}，意向数量仅记录采购需求，成交时再核实库存。</p>
        </div>
        <div className="customer-actions">
          <button type="button" className="customer-icon-button" title="刷新市场数据" onClick={() => { setRefreshing(true); void load(false); }} disabled={refreshing || busy}>
            <RefreshCw size={19} className={refreshing ? 'admin-spin' : undefined} />
          </button>
          <button type="button" className="customer-icon-button" title="退出登录" onClick={props.onLogout}>
            <LogOut size={19} />
          </button>
        </div>
      </header>

      {error && <div className="customer-notice error">{error}</div>}
      {notice && <div className="customer-notice">{notice}</div>}

      <section className="customer-band">
        <div className="customer-section-heading">
          <div>
            <span className="customer-section-icon"><ShoppingBag size={20} /></span>
            <div>
              <h2>交易市场</h2>
              <p>填写农产品、意向数量与期望时间。</p>
            </div>
          </div>
        </div>

        {loading ? (
          <div className="customer-empty"><Loader2 size={22} className="admin-spin" />加载市场数据…</div>
        ) : (
          <div className="customer-market-grid">
            {materials.map((material) => {
              const selected = materialNameKey(material.name) === materialNameKey(productName);
              return (
                <button
                  type="button"
                  className={`market-material${selected ? ' selected' : ''}`}
                  key={material.id}
                  onClick={() => {
                    setProductName(material.name);
                    clearFieldError('productName');
                    clearFieldError('quantity');
                  }}
                >
                  <span>{material.category || '农产品'}</span>
                  <strong>{material.name}</strong>
                  <small>{material.spec || '规格待定'}</small>
                  <em>可售 {formatQuantity(material.availableQuantity)} {material.unit}</em>
                </button>
              );
            })}
            {materials.length === 0 && <div className="customer-empty"><Leaf size={20} />暂无可售农产品。</div>}
          </div>
        )}
      </section>

      <section className="customer-band customer-intent-band">
        <div className="customer-section-heading">
          <div>
            <span className="customer-section-icon"><PackageCheck size={20} /></span>
            <div>
              <h2>提交采购意向</h2>
              <p>提交后由仓库管理员审批，系统不包含价格或支付环节。</p>
            </div>
          </div>
        </div>
        <form className="customer-intent-form" onSubmit={(event) => void submitIntent(event)} noValidate>
          <div className="customer-field">
            <label htmlFor="market-product-name">农产品</label>
            <input id="market-product-name" list="market-material-options" value={productName} onChange={(event) => { setProductName(event.target.value); clearFieldError('productName'); }} placeholder="请输入农产品名称" autoComplete="off" className={fieldErrors.productName ? 'input-invalid' : undefined} />
            <datalist id="market-material-options">{materials.map((material) => <option key={material.id} value={material.name} label={`可售 ${formatQuantity(material.availableQuantity)} ${material.unit}`} />)}</datalist>
            {missingProductName ? <div className="customer-product-create"><p className="field-error" role="alert">{fieldErrors.productName || `未找到“${missingProductName}”。`}</p><button type="button" title={`新建“${missingProductName}”`} onClick={() => void createMissingProduct()} disabled={creatingProduct || busy}>{creatingProduct ? '新建中…' : '新建品种'}</button></div> : fieldErrors.productName && <p className="field-error" role="alert">{fieldErrors.productName}</p>}
          </div>
          <label className="customer-field">
            意向数量
            <div className={`customer-quantity-input${fieldErrors.quantity ? ' input-invalid' : ''}`}>
              <input type="number" min="0" step="0.001" value={quantity} onChange={(event) => { setQuantity(event.target.value); clearFieldError('quantity'); }} placeholder="请输入数量" />
              <select aria-label="意向数量单位" value={quantityUnit} onChange={(event) => { setQuantityUnit(event.target.value as QuantityUnit); clearFieldError('quantity'); }}>
                <option value="kg">kg</option>
                <option value="t">t</option>
              </select>
            </div>
            {fieldErrors.quantity && <p className="field-error" role="alert">{fieldErrors.quantity}</p>}
          </label>
          <label className="customer-field">
            期望时间
            <div className="customer-date-input">
              <CalendarDays size={17} />
              <input type="datetime-local" value={expectedTime} onChange={(event) => setExpectedTime(event.target.value)} />
            </div>
          </label>
          <label className="customer-field customer-field-wide">
            备注
            <input value={remark} maxLength={500} onChange={(event) => { setRemark(event.target.value); clearFieldError('form'); }} placeholder="可填写规格、批次或沟通说明" />
          </label>
          {fieldErrors.form && <p className="field-error customer-form-error" role="alert">{fieldErrors.form}</p>}
          <button type="submit" className="customer-submit" disabled={busy || creatingProduct}>
            {busy ? '提交中…' : '提交采购意向'}
          </button>
        </form>
      </section>

      <section className="customer-band">
        <div className="customer-section-heading">
          <div>
            <span className="customer-section-icon"><PackageCheck size={20} /></span>
            <div>
              <h2>我的意向</h2>
              <p>待审批意向可自行取消。</p>
            </div>
          </div>
        </div>
        {loading ? (
          <div className="customer-empty"><Loader2 size={22} className="admin-spin" />加载意向记录…</div>
        ) : (
          <div className="customer-order-list">
            {orders.map((order) => (
              <article className="customer-order-row" key={order.id}>
                <div>
                  <strong>{order.orderNo}</strong>
                  <p>{orderItemsText(order)}</p>
                  <small>{order.expectedTime ? `期望 ${formatDateTime(order.expectedTime)}` : '未填写期望时间'}</small>
                </div>
                <div className="customer-order-actions">
                  <span className={`customer-status ${statusTone(order.status)}`}>{ORDER_STATUS_NAMES[order.status] ?? order.status}</span>
                  {order.status === 'PENDING' && (
                    <button type="button" className="customer-cancel" onClick={() => void cancelOrder(order)} disabled={busy}>
                      <XCircle size={16} />取消
                    </button>
                  )}
                </div>
              </article>
            ))}
            {orders.length === 0 && <div className="customer-empty"><Leaf size={20} />暂未提交采购意向。</div>}
          </div>
        )}
      </section>
    </main>
  );
}

function materialNameKey(value: string) {
  return value.trim().toLocaleLowerCase('zh-CN');
}

function mergeMarketMaterial(current: MarketMaterial[], material: Material) {
  const next: MarketMaterial = {
    id: material.id,
    name: material.name,
    category: material.category,
    unit: material.unit,
    spec: material.spec,
    availableQuantity: 0,
    totalQuantity: 0
  };
  const existing = current.find((item) => materialNameKey(item.name) === materialNameKey(material.name));
  if (!existing) return [...current, next];
  return current.map((item) => materialNameKey(item.name) === materialNameKey(material.name)
    ? { ...next, availableQuantity: item.availableQuantity, totalQuantity: item.totalQuantity }
    : item);
}

function massUnitFactor(unit: string) {
  switch (unit.trim().toLowerCase()) {
    case 'kg':
    case '公斤':
    case '千克':
      return 1;
    case 't':
    case '吨':
      return 1000;
    default:
      return null;
  }
}

function convertMassQuantity(quantity: number, fromUnit: string, toUnit: string) {
  const fromFactor = massUnitFactor(fromUnit);
  const toFactor = massUnitFactor(toUnit);
  return fromFactor == null || toFactor == null ? null : quantity * fromFactor / toFactor;
}

function quantityValue(value: number | string) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatQuantity(value: number | string) {
  const parsed = quantityValue(value);
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(parsed);
}

function orderItemsText(order: Order) {
  return order.items.map((item) => `${item.materialName ?? `物料 #${item.materialId}`} ${formatQuantity(item.quantity)} ${item.unit ?? ''}`).join('、') || '暂无明细';
}

function formatDateTime(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function statusTone(status: string) {
  if (status === 'PENDING') return 'pending';
  if (status === 'TRADING' || status === 'APPROVED') return 'trading';
  if (status === 'CONFIRMED') return 'confirmed';
  return 'closed';
}

function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : '请求失败，请稍后重试。';
}
