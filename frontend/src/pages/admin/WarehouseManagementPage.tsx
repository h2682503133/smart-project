import { Archive, Building2, ClipboardList, Edit3, Loader2, PackagePlus, RefreshCw, Trash2, Warehouse as WarehouseIcon, X } from 'lucide-react';
import { FormEvent, useCallback, useEffect, useState } from 'react';
import { api } from '../../api';
import type { Material, StockRecord, StockView, Warehouse } from '../../types';

type WarehouseTab = 'stocks' | 'materials' | 'warehouses' | 'records';
type FormErrors = Partial<Record<'name' | 'category' | 'unit' | 'warehouseId' | 'materialId' | 'plotId' | 'quantity' | 'form', string>>;

export function WarehouseManagementPage() {
  const [tab, setTab] = useState<WarehouseTab>('stocks');
  const [stockWarehouseId, setStockWarehouseId] = useState('');
  const [stockMaterialId, setStockMaterialId] = useState('');
  const [stockFilters, setStockFilters] = useState<{ warehouseId?: number; materialId?: number }>({});
  const [recordWarehouseId, setRecordWarehouseId] = useState('');
  const [recordType, setRecordType] = useState('');
  const [recordFilters, setRecordFilters] = useState<{ warehouseId?: number; type?: string }>({});
  const [materialModal, setMaterialModal] = useState<Material | 'create' | null>(null);
  const [warehouseModal, setWarehouseModal] = useState<Warehouse | 'create' | null>(null);
  const [inboundOpen, setInboundOpen] = useState(false);
  const [notice, setNotice] = useState('');
  const [noticeError, setNoticeError] = useState(false);
  const [actionBusy, setActionBusy] = useState(false);

  const materials = useWarehouseResource(() => api.materials(), []);
  const warehouses = useWarehouseResource(() => api.warehouses(), []);
  const stocks = useWarehouseResource(() => api.stocks({ ...stockFilters }), [stockFilters]);
  const records = useWarehouseResource(() => api.stockRecords({ ...recordFilters }), [recordFilters]);

  const materialItems = materials.data?.items ?? [];
  const warehouseItems = warehouses.data?.items ?? [];
  const stockItems = stocks.data?.items ?? [];
  const recordItems = records.data?.items ?? [];

  function showNotice(message: string, isError = false) {
    setNotice(message);
    setNoticeError(isError);
  }

  function refreshAll() {
    materials.refresh();
    warehouses.refresh();
    stocks.refresh();
    records.refresh();
  }

  async function removeMaterial(material: Material) {
    if (!window.confirm(`确定删除物料“${material.name}”吗？`)) return;
    setActionBusy(true);
    try {
      await api.deleteMaterial(material.id);
      showNotice('物料已删除。');
      refreshAll();
    } catch (error) {
      showNotice(errorMessage(error), true);
    } finally {
      setActionBusy(false);
    }
  }

  async function removeWarehouse(warehouse: Warehouse) {
    if (!window.confirm(`确定删除仓库“${warehouse.name}”吗？`)) return;
    setActionBusy(true);
    try {
      await api.deleteWarehouse(warehouse.id);
      showNotice('仓库已删除。');
      refreshAll();
    } catch (error) {
      showNotice(errorMessage(error), true);
    } finally {
      setActionBusy(false);
    }
  }

  const tabs: Array<{ key: WarehouseTab; label: string }> = [
    { key: 'stocks', label: '库存' },
    { key: 'materials', label: '物料' },
    { key: 'warehouses', label: '仓库' },
    { key: 'records', label: '流水' }
  ];

  return (
    <div className="admin-stack">
      {notice && <div className={`admin-notice${noticeError ? ' error' : ''}`}>{notice}</div>}

      <section className="warehouse-hero">
        <div>
          <span className="warehouse-hero-icon"><WarehouseIcon size={23} /></span>
          <div>
            <h2>成品仓储</h2>
            <p>管理作物成品、收获入库与可售库存。库存变动均可追溯。</p>
          </div>
        </div>
        <button type="button" className="admin-heading-action" onClick={() => setInboundOpen(true)} disabled={materialItems.length === 0 || warehouseItems.length === 0} title={materialItems.length === 0 || warehouseItems.length === 0 ? '请先维护物料和仓库' : undefined}>
          <PackagePlus size={16} />
          收获入库
        </button>
      </section>

      <div className="admin-stat-grid warehouse-stat-grid">
        <article className="admin-stat-card tone-green"><span>库存品种</span><strong>{stockItems.length}</strong></article>
      </div>

      <div className="admin-filter-bar warehouse-toolbar">
        <div className="admin-tabs" role="tablist" aria-label="仓储管理视图">
          {tabs.map((item) => (
            <button type="button" key={item.key} className={tab === item.key ? 'active' : ''} onClick={() => setTab(item.key)} role="tab" aria-selected={tab === item.key}>
              {item.label}
            </button>
          ))}
        </div>
        <button type="button" className="ghost" title="刷新" onClick={refreshAll}>
          <RefreshCw size={16} />
        </button>
        {tab === 'materials' && <button type="button" className="primary" onClick={() => setMaterialModal('create')}><PackagePlus size={15} />新建物料</button>}
        {tab === 'warehouses' && <button type="button" className="primary" onClick={() => setWarehouseModal('create')}><Building2 size={15} />新建仓库</button>}
      </div>

      {tab === 'stocks' && (
        <section className="admin-card">
          <div className="admin-card-heading">
            <div><h3>库存总览</h3><p className="admin-card-copy">可售数量由总库存扣减待交易订单占用后统一计算。</p></div>
          </div>
          <form className="admin-filter-bar warehouse-filter-row" onSubmit={(event) => { event.preventDefault(); setStockFilters({ warehouseId: positiveNumber(stockWarehouseId), materialId: positiveNumber(stockMaterialId) }); }}>
            <select value={stockWarehouseId} onChange={(event) => setStockWarehouseId(event.target.value)}><option value="">全部仓库</option>{warehouseItems.map((warehouse) => <option key={warehouse.id} value={warehouse.id}>{warehouse.name}</option>)}</select>
            <select value={stockMaterialId} onChange={(event) => setStockMaterialId(event.target.value)}><option value="">全部物料</option>{materialItems.map((material) => <option key={material.id} value={material.id}>{material.name}</option>)}</select>
            <button type="submit">筛选</button>
          </form>
          <ResourceState resource={stocks}>
            <StockTable items={stockItems} />
          </ResourceState>
        </section>
      )}

      {tab === 'materials' && (
        <section className="admin-card">
          <div className="admin-card-heading"><div><h3>物料主数据</h3><p className="admin-card-copy">仅维护作物成品名称、类别、单位与规格。</p></div></div>
          <ResourceState resource={materials}>
            <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>物料</th><th>类别</th><th>单位</th><th>规格</th><th>状态</th><th>操作</th></tr></thead><tbody>
              {materialItems.map((material) => <tr key={material.id}><td className="admin-strong">{material.name}</td><td>{material.category}</td><td>{material.unit}</td><td>{material.spec || '--'}</td><td><StatusBadge value={material.status} /></td><td className="admin-actions"><button type="button" className="admin-link-btn" onClick={() => setMaterialModal(material)}><Edit3 size={14} />编辑</button><button type="button" className="admin-link-btn danger" disabled={actionBusy} onClick={() => void removeMaterial(material)}><Trash2 size={14} />删除</button></td></tr>)}
              {materialItems.length === 0 && <EmptyTable colSpan={6} text="暂无物料，请先新建物料。" />}
            </tbody></table></div>
          </ResourceState>
        </section>
      )}

      {tab === 'warehouses' && (
        <section className="admin-card">
          <div className="admin-card-heading"><div><h3>仓库主数据</h3><p className="admin-card-copy">仓库停用后不能继续用于收获入库。</p></div></div>
          <ResourceState resource={warehouses}>
            <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>仓库</th><th>位置</th><th>状态</th><th>更新时间</th><th>操作</th></tr></thead><tbody>
              {warehouseItems.map((warehouse) => <tr key={warehouse.id}><td className="admin-strong">{warehouse.name}</td><td>{warehouse.location || '--'}</td><td><StatusBadge value={warehouse.status} /></td><td>{formatDateTime(warehouse.updatedAt)}</td><td className="admin-actions"><button type="button" className="admin-link-btn" onClick={() => setWarehouseModal(warehouse)}><Edit3 size={14} />编辑</button><button type="button" className="admin-link-btn danger" disabled={actionBusy} onClick={() => void removeWarehouse(warehouse)}><Trash2 size={14} />删除</button></td></tr>)}
              {warehouseItems.length === 0 && <EmptyTable colSpan={5} text="暂无仓库，请先新建仓库。" />}
            </tbody></table></div>
          </ResourceState>
        </section>
      )}

      {tab === 'records' && (
        <section className="admin-card">
          <div className="admin-card-heading"><div><h3>出入库流水</h3><p className="admin-card-copy">入库关联来源地块，出库关联订单号，历史记录不物理删除。</p></div></div>
          <form className="admin-filter-bar warehouse-filter-row" onSubmit={(event) => { event.preventDefault(); setRecordFilters({ warehouseId: positiveNumber(recordWarehouseId), type: recordType || undefined }); }}>
            <select value={recordWarehouseId} onChange={(event) => setRecordWarehouseId(event.target.value)}><option value="">全部仓库</option>{warehouseItems.map((warehouse) => <option key={warehouse.id} value={warehouse.id}>{warehouse.name}</option>)}</select>
            <select value={recordType} onChange={(event) => setRecordType(event.target.value)}><option value="">全部类型</option><option value="IN">入库</option><option value="OUT">出库</option></select>
            <button type="submit">筛选</button>
          </form>
          <ResourceState resource={records}>
            <RecordTable items={recordItems} />
          </ResourceState>
        </section>
      )}

      {materialModal && <MaterialModal material={materialModal === 'create' ? undefined : materialModal} onClose={() => setMaterialModal(null)} onSaved={async () => { setMaterialModal(null); showNotice('物料已保存。'); refreshAll(); }} />}
      {warehouseModal && <WarehouseModal warehouse={warehouseModal === 'create' ? undefined : warehouseModal} onClose={() => setWarehouseModal(null)} onSaved={async () => { setWarehouseModal(null); showNotice('仓库已保存。'); refreshAll(); }} />}
      {inboundOpen && <InboundModal materials={materialItems} warehouses={warehouseItems} onClose={() => setInboundOpen(false)} onSaved={async () => { setInboundOpen(false); showNotice('收获入库已登记。'); refreshAll(); }} />}
    </div>
  );
}

function useWarehouseResource<T>(load: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      setData(await load());
    } catch (requestError) {
      setError(errorMessage(requestError));
    } finally {
      setLoading(false);
    }
  }, deps);

  useEffect(() => { void refresh(); }, [refresh]);
  return { data, loading, error, refresh };
}

function ResourceState<T>(props: { resource: { loading: boolean; error: string }; children: React.ReactNode }) {
  if (props.resource.loading) return <div className="admin-empty"><Loader2 size={22} className="admin-spin" />加载中…</div>;
  if (props.resource.error) return <div className="admin-error">{props.resource.error}</div>;
  return <>{props.children}</>;
}

function StockTable(props: { items: StockView[] }) {
  return <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>物料</th><th>仓库</th><th>总库存</th><th>待交易占用</th><th>可售数量</th></tr></thead><tbody>
    {props.items.map((stock) => <tr key={stock.stockId}><td className="admin-strong">{stock.materialName}</td><td>{stock.warehouseName}</td><td>{formatQuantity(stock.totalQuantity)} {stock.unit}</td><td>{formatQuantity(stock.reservedQuantity)} {stock.unit}</td><td><span className="warehouse-available">{formatQuantity(stock.availableQuantity)} {stock.unit}</span></td></tr>)}
    {props.items.length === 0 && <EmptyTable colSpan={5} text="暂无库存记录。完成收获入库后会显示在这里。" />}
  </tbody></table></div>;
}

function RecordTable(props: { items: StockRecord[] }) {
  return <div className="admin-table-wrap"><table className="admin-table"><thead><tr><th>时间</th><th>类型</th><th>物料</th><th>仓库</th><th>数量</th><th>关联信息</th><th>来源地块</th><th>备注</th></tr></thead><tbody>
    {props.items.map((record) => <tr key={record.id}><td>{formatDateTime(record.createdAt)}</td><td><span className={`admin-badge ${record.type === 'OUT' ? 'warn' : 'ok'}`}>{record.type === 'IN' ? '入库' : '出库'}</span></td><td className="admin-strong">{record.materialName}</td><td>{record.warehouseName}</td><td className={record.type === 'OUT' ? 'admin-stock-out' : 'admin-stock-in'}>{record.type === 'OUT' ? '-' : '+'}{formatQuantity(record.quantity)} {record.unit}</td><td>{referenceLabel(record)}</td><td>{record.plotId ? `地块 #${record.plotId}` : '--'}</td><td className="admin-ellipsis" title={record.remark || ''}>{record.remark || '--'}</td></tr>)}
    {props.items.length === 0 && <EmptyTable colSpan={8} text="暂无出入库流水。" />}
  </tbody></table></div>;
}

function EmptyTable(props: { colSpan: number; text: string }) {
  return <tr><td colSpan={props.colSpan} className="admin-empty"><Archive size={18} />{props.text}</td></tr>;
}

function StatusBadge(props: { value: string }) {
  const active = props.value === 'ACTIVE';
  return <span className={`admin-badge ${active ? 'ok' : props.value === 'DISABLED' ? 'warn' : 'off'}`}>{active ? '启用' : props.value === 'DISABLED' ? '停用' : '已删除'}</span>;
}

function MaterialModal(props: { material?: Material; onClose: () => void; onSaved: () => Promise<void> }) {
  const [name, setName] = useState(props.material?.name ?? '');
  const [category, setCategory] = useState(props.material?.category ?? '');
  const [unit, setUnit] = useState(props.material?.unit ?? 'kg');
  const [spec, setSpec] = useState(props.material?.spec ?? '');
  const [status, setStatus] = useState(props.material?.status ?? 'ACTIVE');
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<FormErrors>({});

  async function submit(event: FormEvent) {
    event.preventDefault();
    const next: FormErrors = {};
    if (!name.trim()) next.name = '请输入物料名称。';
    if (!category.trim()) next.category = '请输入物料类别。';
    if (!unit.trim()) next.unit = '请输入计量单位。';
    if (Object.keys(next).length) return setErrors(next);
    setBusy(true);
    try {
      const payload = { name: name.trim(), category: category.trim(), unit: unit.trim(), spec: spec.trim() || undefined, status };
      if (props.material) await api.updateMaterial(props.material.id, payload);
      else await api.createMaterial(payload);
      await props.onSaved();
    } catch (requestError) {
      setErrors({ form: errorMessage(requestError) });
    } finally { setBusy(false); }
  }

  return <Modal title={props.material ? '编辑物料' : '新建物料'} onClose={props.onClose}>
    <form onSubmit={(event) => void submit(event)} noValidate>
      <label>物料名称 *<input value={name} onChange={(event) => { setName(event.target.value); clearError(setErrors, 'name'); }} className={errors.name ? 'input-invalid' : undefined} />{errors.name && <FieldError text={errors.name} />}</label>
      <div className="admin-modal-grid"><label>类别 *<input value={category} onChange={(event) => { setCategory(event.target.value); clearError(setErrors, 'category'); }} placeholder="如 作物" className={errors.category ? 'input-invalid' : undefined} />{errors.category && <FieldError text={errors.category} />}</label><label>单位 *<input value={unit} onChange={(event) => { setUnit(event.target.value); clearError(setErrors, 'unit'); }} placeholder="如 kg" className={errors.unit ? 'input-invalid' : undefined} />{errors.unit && <FieldError text={errors.unit} />}</label></div>
      <label>规格<input value={spec} onChange={(event) => setSpec(event.target.value)} placeholder="如 一级果" /></label>
      <label>状态<select value={status} onChange={(event) => setStatus(event.target.value)}><option value="ACTIVE">启用</option><option value="DISABLED">停用</option></select></label>
      {errors.form && <FieldError text={errors.form} />}
      <ModalFooter onClose={props.onClose} busy={busy} label={props.material ? '保存修改' : '创建物料'} />
    </form>
  </Modal>;
}

function WarehouseModal(props: { warehouse?: Warehouse; onClose: () => void; onSaved: () => Promise<void> }) {
  const [name, setName] = useState(props.warehouse?.name ?? '');
  const [location, setLocation] = useState(props.warehouse?.location ?? '');
  const [status, setStatus] = useState(props.warehouse?.status ?? 'ACTIVE');
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<FormErrors>({});

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!name.trim()) { setErrors({ name: '请输入仓库名称。' }); return; }
    setBusy(true);
    try {
      const payload = { name: name.trim(), location: location.trim() || undefined, status };
      if (props.warehouse) await api.updateWarehouse(props.warehouse.id, payload);
      else await api.createWarehouse(payload);
      await props.onSaved();
    } catch (requestError) {
      setErrors({ form: errorMessage(requestError) });
    } finally { setBusy(false); }
  }

  return <Modal title={props.warehouse ? '编辑仓库' : '新建仓库'} onClose={props.onClose}>
    <form onSubmit={(event) => void submit(event)} noValidate>
      <label>仓库名称 *<input value={name} onChange={(event) => { setName(event.target.value); clearError(setErrors, 'name'); }} className={errors.name ? 'input-invalid' : undefined} />{errors.name && <FieldError text={errors.name} />}</label>
      <label>位置<input value={location} onChange={(event) => setLocation(event.target.value)} placeholder="如 园区北侧" /></label>
      <label>状态<select value={status} onChange={(event) => setStatus(event.target.value)}><option value="ACTIVE">启用</option><option value="DISABLED">停用</option></select></label>
      {errors.form && <FieldError text={errors.form} />}
      <ModalFooter onClose={props.onClose} busy={busy} label={props.warehouse ? '保存修改' : '创建仓库'} />
    </form>
  </Modal>;
}

function InboundModal(props: { materials: Material[]; warehouses: Warehouse[]; onClose: () => void; onSaved: () => Promise<void> }) {
  const [warehouseId, setWarehouseId] = useState(String(props.warehouses[0]?.id ?? ''));
  const [materialId, setMaterialId] = useState(String(props.materials[0]?.id ?? ''));
  const [plotId, setPlotId] = useState('');
  const [quantity, setQuantity] = useState('');
  const [remark, setRemark] = useState('');
  const [busy, setBusy] = useState(false);
  const [errors, setErrors] = useState<FormErrors>({});

  async function submit(event: FormEvent) {
    event.preventDefault();
    const next: FormErrors = {};
    const parsedQuantity = Number(quantity);
    if (!positiveNumber(warehouseId)) next.warehouseId = '请选择入库仓库。';
    if (!positiveNumber(materialId)) next.materialId = '请选择入库物料。';
    if (!positiveNumber(plotId)) next.plotId = '请输入有效的来源地块 ID。';
    if (!Number.isFinite(parsedQuantity) || parsedQuantity <= 0) next.quantity = '请输入大于 0 的入库数量。';
    if (Object.keys(next).length) return setErrors(next);
    setBusy(true);
    try {
      await api.inboundStock({ warehouseId: Number(warehouseId), materialId: Number(materialId), plotId: Number(plotId), quantity: parsedQuantity, remark: remark.trim() || undefined });
      await props.onSaved();
    } catch (requestError) {
      setErrors({ form: errorMessage(requestError) });
    } finally { setBusy(false); }
  }

  return <Modal title="登记收获入库" onClose={props.onClose}>
    <form onSubmit={(event) => void submit(event)} noValidate>
      <p className="admin-modal-hint">入库将记录来源地块并生成不可变更的库存流水。</p>
      <label>入库仓库 *<select value={warehouseId} onChange={(event) => { setWarehouseId(event.target.value); clearError(setErrors, 'warehouseId'); }} className={errors.warehouseId ? 'input-invalid' : undefined}><option value="">请选择仓库</option>{props.warehouses.filter((warehouse) => warehouse.status === 'ACTIVE').map((warehouse) => <option key={warehouse.id} value={warehouse.id}>{warehouse.name}</option>)}</select>{errors.warehouseId && <FieldError text={errors.warehouseId} />}</label>
      <label>收获物料 *<select value={materialId} onChange={(event) => { setMaterialId(event.target.value); clearError(setErrors, 'materialId'); }} className={errors.materialId ? 'input-invalid' : undefined}><option value="">请选择物料</option>{props.materials.filter((material) => material.status === 'ACTIVE').map((material) => <option key={material.id} value={material.id}>{material.name} · {material.unit}</option>)}</select>{errors.materialId && <FieldError text={errors.materialId} />}</label>
      <div className="admin-modal-grid"><label>来源地块 ID *<input inputMode="numeric" value={plotId} onChange={(event) => { setPlotId(event.target.value); clearError(setErrors, 'plotId'); }} placeholder="如 3" className={errors.plotId ? 'input-invalid' : undefined} />{errors.plotId && <FieldError text={errors.plotId} />}</label><label>入库数量 *<input type="number" min="0" step="0.001" value={quantity} onChange={(event) => { setQuantity(event.target.value); clearError(setErrors, 'quantity'); }} className={errors.quantity ? 'input-invalid' : undefined} />{errors.quantity && <FieldError text={errors.quantity} />}</label></div>
      <label>备注<input value={remark} maxLength={500} onChange={(event) => setRemark(event.target.value)} placeholder="可填写采收批次或说明" /></label>
      {errors.form && <FieldError text={errors.form} />}
      <ModalFooter onClose={props.onClose} busy={busy} label="确认入库" />
    </form>
  </Modal>;
}

function Modal(props: { title: string; onClose: () => void; children: React.ReactNode }) {
  return <div className="admin-modal-mask"><section className="admin-modal" role="dialog" aria-modal="true" aria-label={props.title}><header><h3>{props.title}</h3><button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button></header>{props.children}</section></div>;
}

function ModalFooter(props: { onClose: () => void; busy: boolean; label: string }) {
  return <footer><button type="button" onClick={props.onClose}>取消</button><button type="submit" className="primary" disabled={props.busy}>{props.busy ? '提交中…' : props.label}</button></footer>;
}

function FieldError(props: { text: string }) { return <p className="field-error" role="alert">{props.text}</p>; }

function clearError(setErrors: React.Dispatch<React.SetStateAction<FormErrors>>, field: keyof FormErrors) {
  setErrors((current) => {
    if (!current[field] && !current.form) return current;
    const next = { ...current };
    delete next[field];
    delete next.form;
    return next;
  });
}

function positiveNumber(value: string) {
  const parsed = Number(value);
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined;
}

function decimalValue(value: number | string) {
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

function formatQuantity(value: number | string) {
  return new Intl.NumberFormat('zh-CN', { maximumFractionDigits: 3 }).format(decimalValue(value));
}

function formatDateTime(value?: string | null) {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit' });
}

function referenceLabel(record: StockRecord) {
  if (record.refType === 'HARVEST') return `收获 · ${record.refId}`;
  if (record.refType === 'ORDER') return `订单 · ${record.refId}`;
  return `${record.refType} · ${record.refId}`;
}

function errorMessage(error: unknown) { return error instanceof Error ? error.message : '请求失败，请稍后重试。'; }
