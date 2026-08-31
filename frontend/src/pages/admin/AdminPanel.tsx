import {
  AlertTriangle,
  BookOpen,
  CheckCircle2,
  ClipboardList,
  FileText,
  LayoutDashboard,
  Loader2,
  Map,
  Plus,
  RefreshCw,
  Search,
  ShoppingCart,
  Trash2,
  Users,
  Warehouse,
  X
} from 'lucide-react';
import { FormEvent, useCallback, useEffect, useMemo, useState } from 'react';
import { api, ApiError } from '../../api';
import { CommandLibraryPage } from './CommandLibraryPage';
import { OrderManagementPage } from './OrderManagementPage';
import { WarehouseManagementPage } from './WarehouseManagementPage';
import type {
  AdminDevice,
  AdminKnowledgeDoc,
  AdminPlot,
  AdminPlotLatest,
  AdminUser,
  AdminWarehouseItem
} from '../../types';

type AdminSection = 'overview' | 'users' | 'plots' | 'knowledge' | 'devices' | 'alerts' | 'warehouse' | 'orders' | 'commands';

const ADMIN_SECTIONS: Array<{ key: AdminSection; label: string; icon: React.ReactNode }> = [
  { key: 'overview', label: '总览', icon: <LayoutDashboard size={17} /> },
  { key: 'users', label: '用户管理', icon: <Users size={17} /> },
  { key: 'plots', label: '地块管理', icon: <Map size={17} /> },
  { key: 'alerts', label: '报警记录', icon: <AlertTriangle size={17} /> },
  { key: 'warehouse', label: '仓储管理', icon: <Warehouse size={17} /> },
  { key: 'orders', label: '订单管理', icon: <ShoppingCart size={17} /> },
  { key: 'commands', label: '命令库', icon: <ClipboardList size={17} /> },
  { key: 'knowledge', label: '文件审批', icon: <BookOpen size={17} /> },
  { key: 'devices', label: '设备管理', icon: <FileText size={17} /> }
];

const WAREHOUSE_MANAGER_SECTIONS = ADMIN_SECTIONS.filter((item) => item.key === 'warehouse' || item.key === 'orders');

const DOC_STATUS_NAMES: Record<string, string> = {
  DRAFT: '待审核',
  APPROVED: '已审核',
  ACTIVE: '已发布',
  ARCHIVED: '已归档'
};

type PlotFormErrors = Partial<Record<'code' | 'name' | 'ownerId' | 'area' | 'form', string>>;
type DeviceBindErrors = Partial<Record<'sn' | 'name' | 'type' | 'plotId' | 'form', string>>;
type WarehouseFormErrors = Partial<Record<
  'warehouseId' | 'name' | 'location' | 'managerName' | 'category' | 'unit' | 'initialQuantity' | 'safetyStock' | 'quantity' | 'reason' | 'form',
  string
>>;

export function AdminPanel(props: {
  user: { id: number; name: string; role?: string } | null;
  onLogout: () => void;
}) {
  const isWarehouseManager = props.user?.role === 'WAREHOUSE_MANAGER';
  const [section, setSection] = useState<AdminSection>(isWarehouseManager ? 'warehouse' : 'overview');
  const isAdmin = props.user?.role === 'SYSTEM_ADMIN';
  const visibleSections = isWarehouseManager ? WAREHOUSE_MANAGER_SECTIONS : ADMIN_SECTIONS;

  useEffect(() => {
    if (!visibleSections.some((item) => item.key === section)) {
      setSection(visibleSections[0]?.key ?? 'overview');
    }
  }, [section, visibleSections]);

  const roleLabel = isWarehouseManager
    ? '仓库管理员控制面板'
    : isAdmin
      ? '系统管理员控制面板'
      : '技术员控制面板';

  return (
    <div className="admin-shell">
      <aside className="admin-sidebar">
        <div className="admin-brand">
          <strong>智慧农田管理台</strong>
          <span>{props.user?.name ?? '管理员'}</span>
        </div>
        <nav className="admin-nav">
          {visibleSections.map((item) => (
            <button
              key={item.key}
              className={section === item.key ? 'active' : ''}
              onClick={() => setSection(item.key)}
            >
              {item.icon}
              {item.label}
            </button>
          ))}
        </nav>
        <div className="admin-sidebar-footer">
          <button onClick={props.onLogout}>退出登录</button>
        </div>
      </aside>
      <main className="admin-main">
        <header className="admin-topbar">
          <h1>{visibleSections.find((item) => item.key === section)?.label}</h1>
          <span className="admin-topbar-sub">{roleLabel}</span>
        </header>
        <div className="admin-content">
          {section === 'overview' && <AdminOverview />}
          {section === 'users' && <AdminUsers />}
          {section === 'plots' && <AdminPlots isAdmin={isAdmin} />}
          {section === 'alerts' && <AdminAlerts />}
          {section === 'warehouse' && <WarehouseManagementPage />}
          {section === 'orders' && <OrderManagementPage />}
          {section === 'commands' && isAdmin && <CommandLibraryPage />}
          {section === 'knowledge' && <AdminKnowledge isAdmin={isAdmin} />}
          {section === 'devices' && <AdminDevices isAdmin={isAdmin} />}
        </div>
      </main>
    </div>
  );
}

function useAdminLoader<T>(load: () => Promise<T>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refresh = useCallback(() => {
    setLoading(true);
    setError('');
    load()
      .then(setData)
      .catch((err) => setError(errorMessage(err)))
      .finally(() => setLoading(false));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  useEffect(() => {
    refresh();
  }, [refresh]);

  return { data, loading, error, refresh };
}

function AdminState(props: { loading: boolean; error: string; children: React.ReactNode }) {
  if (props.loading) {
    return (
      <div className="admin-empty">
        <Loader2 size={22} className="admin-spin" />
        加载中…
      </div>
    );
  }
  if (props.error) {
    return <div className="admin-error">{props.error}</div>;
  }
  return <>{props.children}</>;
}

// ---------------- 总览 ----------------

function AdminOverview() {
  const [users, setUsers] = useState<number | null>(null);
  const [plots, setPlots] = useState<AdminPlot[]>([]);
  const [pending, setPending] = useState<number | null>(null);
  const [alerts, setAlerts] = useState<number | null>(null);
  const [latestByPlot, setLatestByPlot] = useState<Record<number, AdminPlotLatest>>({});
  const [error, setError] = useState('');

  useEffect(() => {
    void Promise.all([
      api.adminUsers({ pageSize: 1 }).then((page) => page.total).catch(() => null),
      api.adminPlots({ pageSize: 100 }).then((page) => page.items).catch(() => [] as AdminPlot[]),
      api.adminKnowledgeDocs({ status: 'DRAFT', pageSize: 1 }).then((page) => page.total).catch(() => null),
      api.adminKnowledgeDocs({ status: 'APPROVED', pageSize: 1 }).then((page) => page.total).catch(() => null),
      api.adminAlerts({ pageSize: 1 }).then((page) => page.total).catch(() => null),
      api.adminTelemetryLatest().then((items) => items).catch(() => [] as AdminPlotLatest[])
    ]).then(([userTotal, plotItems, draftTotal, approvedTotal, alertTotal, latest]) => {
      setUsers(userTotal);
      setPlots(plotItems);
      setPending((draftTotal ?? 0) + (approvedTotal ?? 0));
      setAlerts(alertTotal);
      const byPlot: Record<number, AdminPlotLatest> = {};
      for (const item of latest) {
        byPlot[item.plotId] = item;
      }
      setLatestByPlot(byPlot);
    }).catch((err) => setError(errorMessage(err)));
  }, []);

  const deviceCount = plots.reduce((sum, plot) => sum + plot.deviceCount, 0);
  const cards = [
    { label: '用户总数', value: users ?? '--', tone: 'green' },
    { label: '地块总数', value: plots.length, tone: 'blue' },
    { label: '绑定设备', value: deviceCount, tone: 'amber' },
    { label: '待审批文件', value: pending ?? '--', tone: 'purple' },
    { label: '告警总数', value: alerts ?? '--', tone: 'red' }
  ];

  return (
    <div className="admin-stack">
      {error && <div className="admin-error">{error}</div>}
      <div className="admin-stat-grid">
        {cards.map((card) => (
          <div className={`admin-stat-card tone-${card.tone}`} key={card.label}>
            <span>{card.label}</span>
            <strong>{card.value}</strong>
          </div>
        ))}
      </div>
      <section className="admin-card">
        <h3>地块归属一览</h3>
        {plots.length === 0 && <div className="admin-empty">暂无地块数据。</div>}
        <div className="admin-table-wrap">
          <table className="admin-table">
            <thead>
              <tr>
                <th>编码</th>
                <th>名称</th>
                <th>归属用户</th>
                <th>设备</th>
                <th>土壤湿度</th>
                <th>温度</th>
              </tr>
            </thead>
            <tbody>
              {plots.slice(0, 10).map((plot) => {
                const latest = latestByPlot[plot.id];
                return (
                  <tr key={plot.id}>
                    <td>{plot.code}</td>
                    <td>{plot.name}</td>
                    <td>{plot.ownerName || `#${plot.ownerId}`}</td>
                    <td>{plot.deviceCount}</td>
                    <td>{latest?.soilMoisture != null ? `${latest.soilMoisture.toFixed(1)}%` : '--'}</td>
                    <td>{latest?.temperature != null ? `${latest.temperature.toFixed(1)}℃` : '--'}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

// ---------------- 用户管理 ----------------

function AdminUsers() {
  const [keyword, setKeyword] = useState('');
  const [role, setRole] = useState('');
  const [query, setQuery] = useState<{ keyword?: string; role?: string }>({});
  const { data, loading, error, refresh } = useAdminLoader(
    () => api.adminUsers({ ...query, pageSize: 100 }),
    [query]
  );

  function submitSearch(event: FormEvent) {
    event.preventDefault();
    setQuery({ keyword: keyword.trim() || undefined, role: role || undefined });
  }

  return (
    <div className="admin-stack">
      <form className="admin-filter-bar" onSubmit={submitSearch}>
        <div className="admin-search">
          <Search size={16} />
          <input value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="用户名 / 手机号" />
        </div>
        <select value={role} onChange={(event) => setRole(event.target.value)}>
          <option value="">全部角色</option>
          <option value="FARMER">农户</option>
          <option value="TECHNICIAN">技术员</option>
          <option value="SYSTEM_ADMIN">系统管理员</option>
        </select>
        <button type="submit">查询</button>
        <button type="button" className="ghost" onClick={refresh} title="刷新">
          <RefreshCw size={15} />
        </button>
      </form>
      <AdminState loading={loading} error={error}>
        <section className="admin-card">
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>用户名</th>
                  <th>手机号</th>
                  <th>角色</th>
                  <th>状态</th>
                  <th>地块数</th>
                  <th>注册时间</th>
                </tr>
              </thead>
              <tbody>
                {(data?.items ?? []).map((user) => (
                  <tr key={user.id}>
                    <td>{user.id}</td>
                    <td className="admin-strong">{user.username}</td>
                    <td>{user.mobile}</td>
                    <td>
                      <span className={`admin-badge ${user.role === 'SYSTEM_ADMIN' ? 'admin' : ''}`}>
                        {user.role === 'SYSTEM_ADMIN' ? '管理员' : user.role === 'TECHNICIAN' ? '技术员' : '农户'}
                      </span>
                    </td>
                    <td>
                      <span className={`admin-badge ${user.status === 'ACTIVE' ? 'ok' : 'off'}`}>
                        {user.status === 'ACTIVE' ? '正常' : '禁用'}
                      </span>
                    </td>
                    <td>{user.plotCount}</td>
                    <td>{formatDate(user.createdAt)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </AdminState>
    </div>
  );
}

// ---------------- 地块管理 ----------------

function AdminPlots({ isAdmin }: { isAdmin: boolean }) {
  const [keyword, setKeyword] = useState('');
  const [query, setQuery] = useState<{ keyword?: string }>({});
  const { data, loading, error, refresh } = useAdminLoader(
    () => api.adminPlots({ ...query, pageSize: 100 }),
    [query]
  );
  const users = useAdminLoader(() => api.adminUsers({ pageSize: 100 }), []);
  const [createOpen, setCreateOpen] = useState(false);
  const [assignPlot, setAssignPlot] = useState<AdminPlot | null>(null);
  const [notice, setNotice] = useState('');

  const farmerOptions = useMemo(() => (users.data?.items ?? []).filter((user) => user.role === 'FARMER'), [users.data]);

  function submitSearch(event: FormEvent) {
    event.preventDefault();
    setQuery({ keyword: keyword.trim() || undefined });
  }

  return (
    <div className="admin-stack">
      {notice && <div className="admin-notice">{notice}</div>}
      <form className="admin-filter-bar" onSubmit={submitSearch}>
        <div className="admin-search">
          <Search size={16} />
          <input value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="编码 / 名称" />
        </div>
        <button type="submit">查询</button>
        <button type="button" className="ghost" onClick={refresh} title="刷新">
          <RefreshCw size={15} />
        </button>
        {isAdmin && (
          <button type="button" className="primary" onClick={() => setCreateOpen(true)}>
            <Plus size={15} />
            新建地块
          </button>
        )}
      </form>
      <AdminState loading={loading} error={error}>
        <section className="admin-card">
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>编码</th>
                  <th>名称</th>
                  <th>归属用户</th>
                  <th>面积</th>
                  <th>位置</th>
                  <th>设备</th>
                  <th>状态</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {(data?.items ?? []).map((plot) => (
                  <tr key={plot.id}>
                    <td className="admin-strong">{plot.code}</td>
                    <td>{plot.name}</td>
                    <td>{plot.ownerName || `#${plot.ownerId}`}</td>
                    <td>{plot.area != null ? `${plot.area} 亩` : '--'}</td>
                    <td>{plot.location || '--'}</td>
                    <td>{plot.deviceCount}</td>
                    <td>
                      <span className={`admin-badge ${plot.status === 'ACTIVE' ? 'ok' : 'off'}`}>
                        {plot.status === 'ACTIVE' ? '启用' : '停用'}
                      </span>
                    </td>
                    <td>
                      {isAdmin && (
                        <button className="admin-link-btn" onClick={() => setAssignPlot(plot)}>
                          分配/转移
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </AdminState>

      {createOpen && (
        <CreatePlotModal
          farmers={farmerOptions}
          onClose={() => setCreateOpen(false)}
          onCreated={async () => {
            setCreateOpen(false);
            setNotice('地块已创建');
            refresh();
          }}
        />
      )}
      {assignPlot && (
        <AssignPlotModal
          plot={assignPlot}
          farmers={farmerOptions}
          onClose={() => setAssignPlot(null)}
          onAssigned={async (ownerId) => {
            await api.adminAssignPlot(assignPlot.id, ownerId);
            setAssignPlot(null);
            setNotice(`地块已分配给用户 #${ownerId}`);
            refresh();
          }}
        />
      )}
    </div>
  );
}

function CreatePlotModal(props: {
  farmers: AdminUser[];
  onClose: () => void;
  onCreated: () => Promise<void>;
}) {
  const [code, setCode] = useState('');
  const [name, setName] = useState('');
  const [area, setArea] = useState('');
  const [location, setLocation] = useState('');
  const [ownerId, setOwnerId] = useState('');
  const [busy, setBusy] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<PlotFormErrors>({});

  function clearFieldError(field: keyof PlotFormErrors) {
    setFieldErrors((current) => {
      if (!current[field] && !current.form) return current;
      const next = { ...current };
      delete next[field];
      delete next.form;
      return next;
    });
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const parsedArea = area.trim() ? Number(area) : null;
    const nextErrors: PlotFormErrors = {};
    if (!code.trim()) nextErrors.code = '请输入地块编码。';
    if (!name.trim()) nextErrors.name = '请输入地块名称。';
    if (!ownerId) nextErrors.ownerId = '请选择归属用户。';
    if (parsedArea !== null && (!Number.isFinite(parsedArea) || parsedArea < 0)) {
      nextErrors.area = '请输入不小于 0 的面积。';
    }
    if (Object.keys(nextErrors).length > 0) {
      setFieldErrors(nextErrors);
      return;
    }

    setFieldErrors({});
    setBusy(true);
    try {
      await api.adminCreatePlot({
        code: code.trim(),
        name: name.trim(),
        area: parsedArea,
        location: location.trim() || null,
        ownerId: Number(ownerId)
      });
      await props.onCreated();
    } catch (err) {
      setFieldErrors(plotFormErrorFor(errorMessage(err)));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-modal-mask">
      <form className="admin-modal" onSubmit={submit} noValidate>
        <header>
          <h3>新建地块</h3>
          <button type="button" onClick={props.onClose}>
            <X size={18} />
          </button>
        </header>
        <label>
          地块编码 *
          <input
            value={code}
            onChange={(event) => {
              setCode(event.target.value);
              clearFieldError('code');
            }}
            placeholder="如 B2"
            className={fieldErrors.code ? 'input-invalid' : undefined}
            aria-invalid={Boolean(fieldErrors.code)}
            aria-describedby={fieldErrors.code ? 'plot-code-error' : undefined}
          />
          {fieldErrors.code && <p className="field-error" id="plot-code-error" role="alert">{fieldErrors.code}</p>}
        </label>
        <label>
          地块名称 *
          <input
            value={name}
            onChange={(event) => {
              setName(event.target.value);
              clearFieldError('name');
            }}
            placeholder="如 B2 西瓜试验田"
            className={fieldErrors.name ? 'input-invalid' : undefined}
            aria-invalid={Boolean(fieldErrors.name)}
            aria-describedby={fieldErrors.name ? 'plot-name-error' : undefined}
          />
          {fieldErrors.name && <p className="field-error" id="plot-name-error" role="alert">{fieldErrors.name}</p>}
        </label>
        <label>
          归属用户 *
          <select
            value={ownerId}
            onChange={(event) => {
              setOwnerId(event.target.value);
              clearFieldError('ownerId');
            }}
            className={fieldErrors.ownerId ? 'input-invalid' : undefined}
            aria-invalid={Boolean(fieldErrors.ownerId)}
            aria-describedby={fieldErrors.ownerId ? 'plot-owner-error' : undefined}
          >
            <option value="">请选择</option>
            {props.farmers.map((user) => (
              <option value={user.id} key={user.id}>
                {user.username}（#id {user.id}）
              </option>
            ))}
          </select>
          {fieldErrors.ownerId && <p className="field-error" id="plot-owner-error" role="alert">{fieldErrors.ownerId}</p>}
        </label>
        <label>
          面积（亩）
          <input
            value={area}
            onChange={(event) => {
              setArea(event.target.value);
              clearFieldError('area');
            }}
            type="number"
            min={0}
            step="0.1"
            placeholder="可选"
            className={fieldErrors.area ? 'input-invalid' : undefined}
            aria-invalid={Boolean(fieldErrors.area)}
            aria-describedby={fieldErrors.area ? 'plot-area-error' : undefined}
          />
          {fieldErrors.area && <p className="field-error" id="plot-area-error" role="alert">{fieldErrors.area}</p>}
        </label>
        <label>
          位置
          <input value={location} onChange={(event) => setLocation(event.target.value)} placeholder="可选" />
        </label>
        {fieldErrors.form && <p className="field-error admin-form-error" role="alert">{fieldErrors.form}</p>}
        <footer>
          <button type="button" onClick={props.onClose}>取消</button>
          <button type="submit" className="primary" disabled={busy}>
            {busy ? '创建中…' : '创建地块'}
          </button>
        </footer>
      </form>
    </div>
  );
}

function AssignPlotModal(props: {
  plot: AdminPlot;
  farmers: AdminUser[];
  onClose: () => void;
  onAssigned: (ownerId: number) => Promise<void>;
}) {
  const [ownerId, setOwnerId] = useState(String(props.plot.ownerId));
  const [busy, setBusy] = useState(false);
  const [ownerError, setOwnerError] = useState('');

  async function submit(event: FormEvent) {
    event.preventDefault();
    if (!ownerId) {
      setOwnerError('请选择归属用户。');
      return;
    }
    setBusy(true);
    setOwnerError('');
    try {
      await props.onAssigned(Number(ownerId));
    } catch (err) {
      setOwnerError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-modal-mask">
      <form className="admin-modal" onSubmit={submit} noValidate>
        <header>
          <h3>分配地块 {props.plot.code}</h3>
          <button type="button" onClick={props.onClose}>
            <X size={18} />
          </button>
        </header>
        <p className="admin-modal-hint">将地块 {props.plot.name} 分配给以下用户（转移归属）。</p>
        <label>
          归属用户 *
          <select
            value={ownerId}
            onChange={(event) => {
              setOwnerId(event.target.value);
              setOwnerError('');
            }}
            className={ownerError ? 'input-invalid' : undefined}
            aria-invalid={Boolean(ownerError)}
            aria-describedby={ownerError ? 'assign-owner-error' : undefined}
          >
            {props.farmers.map((user) => (
              <option value={user.id} key={user.id}>
                {user.username}（#id {user.id}，当前 {user.plotCount} 块）
              </option>
            ))}
          </select>
          {ownerError && <p className="field-error" id="assign-owner-error" role="alert">{ownerError}</p>}
        </label>
        <footer>
          <button type="button" onClick={props.onClose}>取消</button>
          <button type="submit" className="primary" disabled={busy}>
            {busy ? '保存中…' : '确认分配'}
          </button>
        </footer>
      </form>
    </div>
  );
}

// ---------------- 文件审批 ----------------

function AdminKnowledge({ isAdmin }: { isAdmin: boolean }) {
  const [status, setStatus] = useState('');
  const [keyword, setKeyword] = useState('');
  const [query, setQuery] = useState<{ status?: string }>({});
  const { data, loading, error, refresh } = useAdminLoader(
    () => api.adminKnowledgeDocs({ ...query, pageSize: 50 }),
    [query]
  );
  const [preview, setPreview] = useState<AdminKnowledgeDoc | null>(null);
  const [notice, setNotice] = useState('');
  const [busy, setBusy] = useState(false);
  const [uploadOpen, setUploadOpen] = useState(false);

  const tabs = [
    { key: '', label: '全部' },
    { key: 'DRAFT', label: '待审核' },
    { key: 'APPROVED', label: '已审核' },
    { key: 'ACTIVE', label: '已发布' },
    { key: 'ARCHIVED', label: '已归档' }
  ];

  async function runAction(doc: AdminKnowledgeDoc, action: 'approve' | 'publish' | 'delete') {
    if (action === 'delete') {
      const ok = window.confirm(`确认物理删除「${doc.title}」？\n将同时删除 MinIO 文件并清理向量索引，不可恢复。`);
      if (!ok) return;
    }
    setBusy(true);
    setNotice('');
    try {
      if (action === 'approve') {
        await api.approveKnowledgeDoc(doc.id);
        setNotice('已审核通过');
      } else if (action === 'publish') {
        await api.publishKnowledgeDoc(doc.id);
        setNotice('已发布');
      } else {
        await api.adminDeleteKnowledgeDoc(doc.id);
        setNotice('已删除，向量索引异步清理中');
      }
      refresh();
    } catch (err) {
      setNotice(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-stack">
      {notice && <div className={`admin-notice ${notice.startsWith('已删除') || notice.startsWith('已') ? '' : 'error'}`}>{notice}</div>}
      <div className="admin-filter-bar">
        <div className="admin-tabs">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              className={query.status === tab.key ? 'active' : ''}
              onClick={() => setQuery({ status: tab.key })}
            >
              {tab.label}
            </button>
          ))}
        </div>
        <button type="button" className="ghost" onClick={refresh} title="刷新">
          <RefreshCw size={15} />
        </button>
        <button type="button" className="primary" onClick={() => setUploadOpen(true)}>
          <Plus size={15} />
          上传文档
        </button>
      </div>
      <AdminState loading={loading} error={error}>
        <section className="admin-card">
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>标题</th>
                  <th>分类</th>
                  <th>状态</th>
                  <th>版本</th>
                  <th>上传人</th>
                  <th>更新时间</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {(data?.items ?? []).map((doc) => (
                  <tr key={doc.id}>
                    <td>{doc.id}</td>
                    <td className="admin-strong">{doc.title}</td>
                    <td>{doc.category}</td>
                    <td>
                      <span className={`admin-badge ${doc.status === 'ACTIVE' ? 'ok' : doc.status === 'DRAFT' ? 'warn' : ''}`}>
                        {DOC_STATUS_NAMES[doc.status] ?? doc.status}
                      </span>
                    </td>
                    <td>v{doc.version}</td>
                    <td>{doc.uploaderName}</td>
                    <td>{formatDate(doc.updatedAt)}</td>
                    <td className="admin-actions">
                      {doc.downloadUrl && (
                        <button className="admin-link-btn" onClick={() => setPreview(doc)}>
                          预览
                        </button>
                      )}
                      {doc.status === 'DRAFT' && (
                        <button className="admin-link-btn" disabled={busy} onClick={() => void runAction(doc, 'approve')}>
                          通过
                        </button>
                      )}
                      {isAdmin && doc.status === 'APPROVED' && (
                        <button className="admin-link-btn" disabled={busy} onClick={() => void runAction(doc, 'publish')}>
                          发布
                        </button>
                      )}
                      <button className="admin-link-btn danger" disabled={busy} onClick={() => void runAction(doc, 'delete')}>
                        <Trash2 size={14} />
                        删除
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </AdminState>

      {preview && (
        <PreviewDrawer doc={preview} onClose={() => setPreview(null)} />
      )}
    </div>
  );
}

function PreviewDrawer(props: { doc: AdminKnowledgeDoc; onClose: () => void }) {
  const [content, setContent] = useState('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    if (!props.doc.downloadUrl) {
      setError('该文档没有可预览的下载地址');
      setLoading(false);
      return;
    }
    const url = props.doc.downloadUrl.replace('http://minio:9000', '/minio-download');
    fetch(url)
      .then((response) => {
        if (!response.ok) throw new Error(`下载失败（${response.status}）`);
        return response.text();
      })
      .then((text) => {
        if (cancelled) return;
        setContent(text.length > 200000 ? `${text.slice(0, 200000)}\n\n……（内容过长，已截断）` : text);
      })
      .catch((err) => {
        if (!cancelled) setError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [props.doc]);

  return (
    <div className="admin-drawer-mask">
      <aside className="admin-drawer">
        <header>
          <div>
            <h3>{props.doc.title}</h3>
            <span>
              {DOC_STATUS_NAMES[props.doc.status] ?? props.doc.status} · v{props.doc.version} · {props.doc.uploaderName}
            </span>
          </div>
          <button onClick={props.onClose}>
            <X size={18} />
          </button>
        </header>
        <div className="admin-preview-body">
          {loading && (
            <div className="admin-empty">
              <Loader2 size={22} className="admin-spin" />
              正在加载文件内容…
            </div>
          )}
          {error && <div className="admin-error">{error}</div>}
          {!loading && !error && <pre className="admin-preview-text">{content}</pre>}
        </div>
        <footer>
          {props.doc.downloadUrl && (
            <a
              href={props.doc.downloadUrl.replace('http://minio:9000', '/minio-download')}
              target="_blank"
              rel="noreferrer"
            >
              打开原文件
            </a>
          )}
          <button onClick={props.onClose}>关闭</button>
        </footer>
      </aside>
    </div>
  );
}

// ---------------- 设备管理 ----------------

function AdminDevices({ isAdmin }: { isAdmin: boolean }) {
  const { data, loading, error, refresh } = useAdminLoader(() => api.adminDevicesStatus({ pageSize: 100 }), []);
  const plots = useAdminLoader(() => api.adminPlots({ pageSize: 100 }), []);
  const [sn, setSn] = useState('');
  const [name, setName] = useState('');
  const [type, setType] = useState('');
  const [plotId, setPlotId] = useState('');
  const [busy, setBusy] = useState(false);
  const [notice, setNotice] = useState('');
  const [bindErrors, setBindErrors] = useState<DeviceBindErrors>({});

  function clearBindError(field: keyof DeviceBindErrors) {
    setBindErrors((current) => {
      if (!current[field] && !current.form) return current;
      const next = { ...current };
      delete next[field];
      delete next.form;
      return next;
    });
  }

  async function bind(event: FormEvent) {
    event.preventDefault();
    setNotice('');
    const nextErrors: DeviceBindErrors = {};
    if (!sn.trim()) nextErrors.sn = '请输入设备序列号。';
    if (!name.trim()) nextErrors.name = '请输入设备名称。';
    if (!type.trim()) nextErrors.type = '请输入设备类型。';
    if (Object.keys(nextErrors).length > 0) {
      setBindErrors(nextErrors);
      return;
    }

    setBindErrors({});
    setBusy(true);
    try {
      const onlyAdd = !plotId;
      await api.adminBindDevice({ deviceSn: sn.trim(), plotId: onlyAdd ? 0 : Number(plotId), name: name.trim(), type: type.trim() });
      setNotice(onlyAdd ? '设备已添加（未绑定地块）' : '设备已绑定');
      setSn('');
      setName('');
      setType('');
      setPlotId('');
      refresh();
    } catch (err) {
      setBindErrors(deviceBindErrorFor(errorMessage(err)));
    } finally {
      setBusy(false);
    }
  }

  async function unbind(device: AdminDevice) {
    const ok = window.confirm(`确认解绑设备「${device.name}」（${device.deviceSn}）？`);
    if (!ok) return;
    setBusy(true);
    setNotice('');
    try {
      await api.adminUnbindDevice(device.id);
      setNotice('设备已解绑');
      refresh();
    } catch (err) {
      setNotice(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  async function removeDevice(device: AdminDevice) {
    const ok = window.confirm(
      `确认删除设备「${device.name}」（${device.deviceSn}）？\n将同时删除绑定记录，不可恢复。`
    );
    if (!ok) return;
    setBusy(true);
    setNotice('');
    try {
      await api.adminDeleteDevice(device.id);
      setNotice('设备已删除');
      refresh();
    } catch (err) {
      setNotice(errorMessage(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-stack">
      {notice && <div className="admin-notice">{notice}</div>}
      {isAdmin && (
        <section className="admin-card">
          <h3>添加 / 绑定设备</h3>
          <form className="admin-bind-form" onSubmit={bind} noValidate>
            <label className="admin-input-field">
              <span>设备序列号</span>
              <input
                value={sn}
                onChange={(event) => {
                  setSn(event.target.value);
                  clearBindError('sn');
                }}
                placeholder="deviceSn（不存在则自动创建）"
                className={bindErrors.sn ? 'input-invalid' : undefined}
                aria-invalid={Boolean(bindErrors.sn)}
                aria-describedby={bindErrors.sn ? 'device-sn-error' : undefined}
              />
              {bindErrors.sn && <p className="field-error" id="device-sn-error" role="alert">{bindErrors.sn}</p>}
            </label>
            <label className="admin-input-field">
              <span>设备名称</span>
              <input
                value={name}
                onChange={(event) => {
                  setName(event.target.value);
                  clearBindError('name');
                }}
                placeholder="请输入设备名称"
                className={bindErrors.name ? 'input-invalid' : undefined}
                aria-invalid={Boolean(bindErrors.name)}
                aria-describedby={bindErrors.name ? 'device-name-error' : undefined}
              />
              {bindErrors.name && <p className="field-error" id="device-name-error" role="alert">{bindErrors.name}</p>}
            </label>
            <label className="admin-input-field">
              <span>设备类型</span>
              <input
                value={type}
                onChange={(event) => {
                  setType(event.target.value);
                  clearBindError('type');
                }}
                placeholder="如 SOIL_SENSOR / VALVE"
                className={bindErrors.type ? 'input-invalid' : undefined}
                aria-invalid={Boolean(bindErrors.type)}
                aria-describedby={bindErrors.type ? 'device-type-error' : undefined}
              />
              {bindErrors.type && <p className="field-error" id="device-type-error" role="alert">{bindErrors.type}</p>}
            </label>
            <label className="admin-input-field">
              <span>绑定地块</span>
              <select
                value={plotId}
                onChange={(event) => {
                  setPlotId(event.target.value);
                  clearBindError('plotId');
                }}
                className={bindErrors.plotId ? 'input-invalid' : undefined}
                aria-invalid={Boolean(bindErrors.plotId)}
                aria-describedby={bindErrors.plotId ? 'device-plot-error' : undefined}
              >
                <option value="">暂不绑定（仅添加设备）</option>
                {(plots.data?.items ?? []).map((plot) => (
                  <option value={plot.id} key={plot.id}>
                    {plot.code} {plot.name}（{plot.ownerName || `#${plot.ownerId}`}）
                  </option>
                ))}
              </select>
              {bindErrors.plotId && <p className="field-error" id="device-plot-error" role="alert">{bindErrors.plotId}</p>}
            </label>
            <button type="submit" className="primary" disabled={busy}>
              {busy ? '处理中…' : plotId ? '绑定设备' : '添加设备'}
            </button>
            {bindErrors.form && <p className="field-error admin-form-error" role="alert">{bindErrors.form}</p>}
          </form>
        </section>
      )}
      <AdminState loading={loading} error={error}>
        <section className="admin-card">
          <h3>全部设备</h3>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>序列号</th>
                  <th>名称</th>
                  <th>类型</th>
                  <th>状态</th>
                  <th>绑定地块</th>
                  <th>归属用户</th>
                  <th>操作</th>
                </tr>
              </thead>
              <tbody>
                {(data?.items ?? []).map((device) => (
                  <tr key={device.id}>
                    <td className="admin-strong">{device.deviceSn}</td>
                    <td>{device.name}</td>
                    <td>{device.type}</td>
                    <td>
                      <span className={`admin-badge ${device.status === 'ONLINE' ? 'ok' : device.status === 'OFFLINE' ? 'off' : ''}`}>
                        {deviceStatusName(device.status)}
                      </span>
                      {device.lastSeenAt && (
                        <small className="admin-device-seen">心跳 {formatDateTime(device.lastSeenAt)}</small>
                      )}
                    </td>
                    <td>
                      {device.plotId > 0 ? (
                        `${device.plotCode ?? '#' + device.plotId} ${device.plotName ?? ''}`
                      ) : (
                        <span className="admin-badge off">未绑定</span>
                      )}
                    </td>
                    <td>{device.ownerName || '--'}</td>
                    <td className="admin-actions">
                      {isAdmin && device.plotId > 0 && (
                        <button className="admin-link-btn danger" disabled={busy} onClick={() => void unbind(device)}>
                          解绑
                        </button>
                      )}
                      {isAdmin && (
                        <button className="admin-link-btn danger" disabled={busy} onClick={() => void removeDevice(device)}>
                          <Trash2 size={14} />
                          删除
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      </AdminState>
    </div>
  );
}

// ---------------- 仓储管理 ----------------

type StockAction = 'IN' | 'OUT' | 'SET';

const STOCK_ACTION_NAMES: Record<StockAction, string> = {
  IN: '入库',
  OUT: '出库',
  SET: '盘点'
};

function AdminWarehouse({ isAdmin }: { isAdmin: boolean }) {
  const [keyword, setKeyword] = useState('');
  const [warehouseId, setWarehouseId] = useState('');
  const [query, setQuery] = useState<{ keyword?: string; warehouseId?: number }>({});
  const warehouses = useAdminLoader(() => api.adminWarehouses({ pageSize: 100 }), []);
  const items = useAdminLoader(() => api.adminWarehouseItems({ ...query, pageSize: 100 }), [query]);
  const stockLogs = useAdminLoader(
    () => api.adminWarehouseStockLogs({ warehouseId: query.warehouseId, pageSize: 20 }),
    [query.warehouseId]
  );
  const [createWarehouseOpen, setCreateWarehouseOpen] = useState(false);
  const [createItemOpen, setCreateItemOpen] = useState(false);
  const [adjusting, setAdjusting] = useState<{ item: AdminWarehouseItem; action: StockAction } | null>(null);
  const [notice, setNotice] = useState('');
  const [noticeError, setNoticeError] = useState(false);

  const warehouseOptions = warehouses.data?.items ?? [];

  function refreshAll() {
    warehouses.refresh();
    items.refresh();
    stockLogs.refresh();
  }

  function submitSearch(event: FormEvent) {
    event.preventDefault();
    setQuery({
      keyword: keyword.trim() || undefined,
      warehouseId: warehouseId ? Number(warehouseId) : undefined
    });
  }

  function showNotice(message: string, isError = false) {
    setNotice(message);
    setNoticeError(isError);
  }

  return (
    <div className="admin-stack">
      {notice && <div className={`admin-notice${noticeError ? ' error' : ''}`}>{notice}</div>}
      <section className="admin-card">
        <div className="admin-card-heading">
          <div>
            <h3>仓库概览</h3>
            <p className="admin-card-copy">按仓库查看物资数量与低库存提醒。</p>
          </div>
          {isAdmin && (
            <button type="button" className="admin-heading-action" onClick={() => setCreateWarehouseOpen(true)}>
              <Plus size={15} />
              新建仓库
            </button>
          )}
        </div>
        <AdminState loading={warehouses.loading} error={warehouses.error}>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>仓库</th>
                  <th>位置</th>
                  <th>负责人</th>
                  <th>物资种类</th>
                  <th>低库存</th>
                  <th>更新时间</th>
                </tr>
              </thead>
              <tbody>
                {warehouseOptions.map((warehouse) => (
                  <tr key={warehouse.id}>
                    <td className="admin-strong">{warehouse.name}</td>
                    <td>{warehouse.location || '--'}</td>
                    <td>{warehouse.managerName || '--'}</td>
                    <td>{warehouse.itemCount}</td>
                    <td>
                      <span className={`admin-badge ${warehouse.lowStockCount > 0 ? 'warn' : 'ok'}`}>
                        {warehouse.lowStockCount > 0 ? `${warehouse.lowStockCount} 项需关注` : '库存正常'}
                      </span>
                    </td>
                    <td>{formatDateTime(warehouse.updatedAt)}</td>
                  </tr>
                ))}
                {warehouseOptions.length === 0 && (
                  <tr>
                    <td colSpan={6} className="admin-empty">暂无仓库。{isAdmin ? '请先新建仓库。' : ''}</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </AdminState>
      </section>

      <form className="admin-filter-bar" onSubmit={submitSearch}>
        <div className="admin-search">
          <Search size={16} />
          <input value={keyword} onChange={(event) => setKeyword(event.target.value)} placeholder="物资名称 / 分类" />
        </div>
        <select value={warehouseId} onChange={(event) => setWarehouseId(event.target.value)}>
          <option value="">全部仓库</option>
          {warehouseOptions.map((warehouse) => (
            <option key={warehouse.id} value={warehouse.id}>{warehouse.name}</option>
          ))}
        </select>
        <button type="submit">查询</button>
        <button type="button" className="ghost" onClick={refreshAll} title="刷新">
          <RefreshCw size={15} />
        </button>
        {isAdmin && (
          <button
            type="button"
            className="primary"
            onClick={() => setCreateItemOpen(true)}
            disabled={warehouseOptions.length === 0}
            title={warehouseOptions.length === 0 ? '请先新建仓库' : undefined}
          >
            <Plus size={15} />
            登记物资
          </button>
        )}
      </form>

      <AdminState loading={items.loading} error={items.error}>
        <section className="admin-card">
          <h3>库存明细</h3>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>物资</th>
                  <th>仓库</th>
                  <th>分类</th>
                  <th>当前库存</th>
                  <th>安全库存</th>
                  <th>状态</th>
                  <th>更新时间</th>
                  {isAdmin && <th>操作</th>}
                </tr>
              </thead>
              <tbody>
                {(items.data?.items ?? []).map((item) => {
                  const isLowStock = item.safetyStock != null && item.quantity <= item.safetyStock;
                  return (
                    <tr key={item.id}>
                      <td className="admin-strong">{item.name}</td>
                      <td>{item.warehouseName}</td>
                      <td>{item.category}</td>
                      <td>{formatStock(item.quantity)} {item.unit}</td>
                      <td>{item.safetyStock == null ? '--' : `${formatStock(item.safetyStock)} ${item.unit}`}</td>
                      <td>
                        <span className={`admin-badge ${isLowStock ? 'warn' : 'ok'}`}>
                          {item.safetyStock == null ? '未设置预警' : isLowStock ? '库存偏低' : '充足'}
                        </span>
                      </td>
                      <td>{formatDateTime(item.updatedAt)}</td>
                      {isAdmin && (
                        <td className="admin-actions">
                          <button className="admin-link-btn" onClick={() => setAdjusting({ item, action: 'IN' })}>入库</button>
                          <button className="admin-link-btn" onClick={() => setAdjusting({ item, action: 'OUT' })}>出库</button>
                          <button className="admin-link-btn" onClick={() => setAdjusting({ item, action: 'SET' })}>盘点</button>
                        </td>
                      )}
                    </tr>
                  );
                })}
                {(items.data?.items ?? []).length === 0 && (
                  <tr>
                    <td colSpan={isAdmin ? 8 : 7} className="admin-empty">暂无符合条件的库存物资。</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </AdminState>

      <AdminState loading={stockLogs.loading} error={stockLogs.error}>
        <section className="admin-card">
          <h3>最近库存流水</h3>
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>时间</th>
                  <th>仓库</th>
                  <th>物资</th>
                  <th>操作</th>
                  <th>变动</th>
                  <th>操作后库存</th>
                  <th>操作人</th>
                  <th>备注</th>
                </tr>
              </thead>
              <tbody>
                {(stockLogs.data?.items ?? []).map((log) => (
                  <tr key={log.id}>
                    <td>{formatDateTime(log.createdAt)}</td>
                    <td>{log.warehouseName}</td>
                    <td className="admin-strong">{log.itemName}</td>
                    <td><span className={`admin-badge ${log.action === 'OUT' ? 'warn' : log.action === 'SET' ? 'admin' : 'ok'}`}>{STOCK_ACTION_NAMES[log.action as StockAction] ?? log.action}</span></td>
                    <td className={log.changeQuantity < 0 ? 'admin-stock-out' : 'admin-stock-in'}>{formatStockChange(log.changeQuantity)}</td>
                    <td>{formatStock(log.afterQuantity)}</td>
                    <td>{log.operatorName || '--'}</td>
                    <td className="admin-ellipsis" title={log.reason || ''}>{log.reason || '--'}</td>
                  </tr>
                ))}
                {(stockLogs.data?.items ?? []).length === 0 && (
                  <tr>
                    <td colSpan={8} className="admin-empty">暂无库存变动记录。</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </section>
      </AdminState>

      {createWarehouseOpen && (
        <CreateWarehouseModal
          onClose={() => setCreateWarehouseOpen(false)}
          onCreated={async () => {
            setCreateWarehouseOpen(false);
            showNotice('仓库已创建');
            refreshAll();
          }}
        />
      )}
      {createItemOpen && (
        <CreateWarehouseItemModal
          warehouses={warehouseOptions}
          onClose={() => setCreateItemOpen(false)}
          onCreated={async () => {
            setCreateItemOpen(false);
            showNotice('物资已登记');
            refreshAll();
          }}
        />
      )}
      {adjusting && (
        <AdjustWarehouseStockModal
          item={adjusting.item}
          action={adjusting.action}
          onClose={() => setAdjusting(null)}
          onAdjusted={async () => {
            setAdjusting(null);
            showNotice(`${STOCK_ACTION_NAMES[adjusting.action]}已完成`);
            refreshAll();
          }}
        />
      )}
    </div>
  );
}

function CreateWarehouseModal(props: { onClose: () => void; onCreated: () => Promise<void> }) {
  const [name, setName] = useState('');
  const [location, setLocation] = useState('');
  const [managerName, setManagerName] = useState('');
  const [remark, setRemark] = useState('');
  const [busy, setBusy] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<WarehouseFormErrors>({});

  function clearFieldError(field: keyof WarehouseFormErrors) {
    setFieldErrors((current) => {
      if (!current[field] && !current.form) return current;
      const next = { ...current };
      delete next[field];
      delete next.form;
      return next;
    });
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const errors: WarehouseFormErrors = {};
    if (!name.trim()) errors.name = '请输入仓库名称。';
    if (location.trim().length > 255) errors.location = '仓库位置不能超过 255 个字符。';
    if (managerName.trim().length > 64) errors.managerName = '负责人不能超过 64 个字符。';
    if (remark.trim().length > 500) errors.form = '备注不能超过 500 个字符。';
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }
    setFieldErrors({});
    setBusy(true);
    try {
      await api.adminCreateWarehouse({
        name: name.trim(),
        location: location.trim() || undefined,
        managerName: managerName.trim() || undefined,
        remark: remark.trim() || undefined
      });
      await props.onCreated();
    } catch (error) {
      setFieldErrors(warehouseFormErrorFor(errorMessage(error)));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-modal-mask">
      <form className="admin-modal" onSubmit={submit} noValidate>
        <header>
          <h3>新建仓库</h3>
          <button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button>
        </header>
        <label>
          仓库名称 *
          <input value={name} onChange={(event) => { setName(event.target.value); clearFieldError('name'); }} placeholder="如 一号农资仓" className={fieldErrors.name ? 'input-invalid' : undefined} />
          {fieldErrors.name && <p className="field-error" role="alert">{fieldErrors.name}</p>}
        </label>
        <label>
          仓库位置
          <input value={location} onChange={(event) => { setLocation(event.target.value); clearFieldError('location'); }} placeholder="如 园区北侧" className={fieldErrors.location ? 'input-invalid' : undefined} />
          {fieldErrors.location && <p className="field-error" role="alert">{fieldErrors.location}</p>}
        </label>
        <label>
          负责人
          <input value={managerName} onChange={(event) => { setManagerName(event.target.value); clearFieldError('managerName'); }} placeholder="请输入负责人姓名" className={fieldErrors.managerName ? 'input-invalid' : undefined} />
          {fieldErrors.managerName && <p className="field-error" role="alert">{fieldErrors.managerName}</p>}
        </label>
        <label>
          备注
          <input value={remark} onChange={(event) => { setRemark(event.target.value); clearFieldError('form'); }} placeholder="选填" />
        </label>
        {fieldErrors.form && <p className="field-error admin-form-error" role="alert">{fieldErrors.form}</p>}
        <footer>
          <button type="button" onClick={props.onClose}>取消</button>
          <button type="submit" className="primary" disabled={busy}>{busy ? '创建中…' : '创建仓库'}</button>
        </footer>
      </form>
    </div>
  );
}

function CreateWarehouseItemModal(props: {
  warehouses: Array<{ id: number; name: string }>;
  onClose: () => void;
  onCreated: () => Promise<void>;
}) {
  const [warehouseId, setWarehouseId] = useState(String(props.warehouses[0]?.id ?? ''));
  const [name, setName] = useState('');
  const [category, setCategory] = useState('');
  const [unit, setUnit] = useState('');
  const [initialQuantity, setInitialQuantity] = useState('0');
  const [safetyStock, setSafetyStock] = useState('');
  const [remark, setRemark] = useState('');
  const [busy, setBusy] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<WarehouseFormErrors>({});

  function clearFieldError(field: keyof WarehouseFormErrors) {
    setFieldErrors((current) => {
      if (!current[field] && !current.form) return current;
      const next = { ...current };
      delete next[field];
      delete next.form;
      return next;
    });
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const initial = Number(initialQuantity || 0);
    const safety = safetyStock.trim() ? Number(safetyStock) : undefined;
    const errors: WarehouseFormErrors = {};
    if (!warehouseId) errors.warehouseId = '请选择仓库。';
    if (!name.trim()) errors.name = '请输入物资名称。';
    if (!category.trim()) errors.category = '请输入物资分类。';
    if (!unit.trim()) errors.unit = '请输入计量单位。';
    if (!Number.isFinite(initial) || initial < 0) errors.initialQuantity = '请输入不小于 0 的初始库存。';
    if (safety !== undefined && (!Number.isFinite(safety) || safety < 0)) errors.safetyStock = '请输入不小于 0 的安全库存。';
    if (remark.trim().length > 500) errors.form = '备注不能超过 500 个字符。';
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }
    setFieldErrors({});
    setBusy(true);
    try {
      await api.adminCreateWarehouseItem({
        warehouseId: Number(warehouseId), name: name.trim(), category: category.trim(), unit: unit.trim(),
        initialQuantity: initial, safetyStock: safety, remark: remark.trim() || undefined
      });
      await props.onCreated();
    } catch (error) {
      setFieldErrors(warehouseFormErrorFor(errorMessage(error)));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-modal-mask">
      <form className="admin-modal" onSubmit={submit} noValidate>
        <header>
          <h3>登记物资</h3>
          <button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button>
        </header>
        <label>
          所属仓库 *
          <select value={warehouseId} onChange={(event) => { setWarehouseId(event.target.value); clearFieldError('warehouseId'); }} className={fieldErrors.warehouseId ? 'input-invalid' : undefined}>
            <option value="">请选择仓库</option>
            {props.warehouses.map((warehouse) => <option key={warehouse.id} value={warehouse.id}>{warehouse.name}</option>)}
          </select>
          {fieldErrors.warehouseId && <p className="field-error" role="alert">{fieldErrors.warehouseId}</p>}
        </label>
        <label>
          物资名称 *
          <input value={name} onChange={(event) => { setName(event.target.value); clearFieldError('name'); }} placeholder="如 水溶肥" className={fieldErrors.name ? 'input-invalid' : undefined} />
          {fieldErrors.name && <p className="field-error" role="alert">{fieldErrors.name}</p>}
        </label>
        <div className="admin-modal-grid">
          <label>
            分类 *
            <input value={category} onChange={(event) => { setCategory(event.target.value); clearFieldError('category'); }} placeholder="如 肥料" className={fieldErrors.category ? 'input-invalid' : undefined} />
            {fieldErrors.category && <p className="field-error" role="alert">{fieldErrors.category}</p>}
          </label>
          <label>
            单位 *
            <input value={unit} onChange={(event) => { setUnit(event.target.value); clearFieldError('unit'); }} placeholder="如 kg" className={fieldErrors.unit ? 'input-invalid' : undefined} />
            {fieldErrors.unit && <p className="field-error" role="alert">{fieldErrors.unit}</p>}
          </label>
        </div>
        <div className="admin-modal-grid">
          <label>
            初始库存
            <input type="number" min="0" step="0.01" value={initialQuantity} onChange={(event) => { setInitialQuantity(event.target.value); clearFieldError('initialQuantity'); }} className={fieldErrors.initialQuantity ? 'input-invalid' : undefined} />
            {fieldErrors.initialQuantity && <p className="field-error" role="alert">{fieldErrors.initialQuantity}</p>}
          </label>
          <label>
            安全库存
            <input type="number" min="0" step="0.01" value={safetyStock} onChange={(event) => { setSafetyStock(event.target.value); clearFieldError('safetyStock'); }} placeholder="选填" className={fieldErrors.safetyStock ? 'input-invalid' : undefined} />
            {fieldErrors.safetyStock && <p className="field-error" role="alert">{fieldErrors.safetyStock}</p>}
          </label>
        </div>
        <label>
          备注
          <input value={remark} onChange={(event) => { setRemark(event.target.value); clearFieldError('form'); }} placeholder="选填" />
        </label>
        {fieldErrors.form && <p className="field-error admin-form-error" role="alert">{fieldErrors.form}</p>}
        <footer>
          <button type="button" onClick={props.onClose}>取消</button>
          <button type="submit" className="primary" disabled={busy}>{busy ? '登记中…' : '登记物资'}</button>
        </footer>
      </form>
    </div>
  );
}

function AdjustWarehouseStockModal(props: {
  item: AdminWarehouseItem;
  action: StockAction;
  onClose: () => void;
  onAdjusted: () => Promise<void>;
}) {
  const isSet = props.action === 'SET';
  const [quantity, setQuantity] = useState(isSet ? String(props.item.quantity) : '');
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [fieldErrors, setFieldErrors] = useState<WarehouseFormErrors>({});

  function clearFieldError(field: keyof WarehouseFormErrors) {
    setFieldErrors((current) => {
      if (!current[field] && !current.form) return current;
      const next = { ...current };
      delete next[field];
      delete next.form;
      return next;
    });
  }

  async function submit(event: FormEvent) {
    event.preventDefault();
    const parsedQuantity = Number(quantity);
    const errors: WarehouseFormErrors = {};
    if (!Number.isFinite(parsedQuantity) || parsedQuantity < 0 || (!isSet && parsedQuantity === 0)) {
      errors.quantity = isSet ? '请输入不小于 0 的盘点后库存。' : '请输入大于 0 的数量。';
    }
    if (reason.trim().length > 255) errors.reason = '备注不能超过 255 个字符。';
    if (Object.keys(errors).length > 0) {
      setFieldErrors(errors);
      return;
    }
    setFieldErrors({});
    setBusy(true);
    try {
      await api.adminAdjustWarehouseStock(props.item.id, {
        action: props.action, quantity: parsedQuantity, reason: reason.trim() || undefined
      });
      await props.onAdjusted();
    } catch (error) {
      setFieldErrors(warehouseFormErrorFor(errorMessage(error)));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="admin-modal-mask">
      <form className="admin-modal" onSubmit={submit} noValidate>
        <header>
          <h3>{STOCK_ACTION_NAMES[props.action]}：{props.item.name}</h3>
          <button type="button" onClick={props.onClose} title="关闭"><X size={18} /></button>
        </header>
        <p className="admin-modal-hint">当前库存：{formatStock(props.item.quantity)} {props.item.unit}</p>
        <label>
          {isSet ? '盘点后库存 *' : `${STOCK_ACTION_NAMES[props.action]}数量 *`}
          <input type="number" min="0" step="0.01" value={quantity} onChange={(event) => { setQuantity(event.target.value); clearFieldError('quantity'); }} className={fieldErrors.quantity ? 'input-invalid' : undefined} />
          {fieldErrors.quantity && <p className="field-error" role="alert">{fieldErrors.quantity}</p>}
        </label>
        <label>
          备注
          <input value={reason} onChange={(event) => { setReason(event.target.value); clearFieldError('reason'); }} placeholder={isSet ? '如 月末盘点' : '如 生产领用'} className={fieldErrors.reason ? 'input-invalid' : undefined} />
          {fieldErrors.reason && <p className="field-error" role="alert">{fieldErrors.reason}</p>}
        </label>
        {fieldErrors.form && <p className="field-error admin-form-error" role="alert">{fieldErrors.form}</p>}
        <footer>
          <button type="button" onClick={props.onClose}>取消</button>
          <button type="submit" className="primary" disabled={busy}>{busy ? '提交中…' : `确认${STOCK_ACTION_NAMES[props.action]}`}</button>
        </footer>
      </form>
    </div>
  );
}

// ---------------- 报警记录 ----------------

const ALERT_STATUS_NAMES: Record<string, string> = {
  ACTIVE: '告警中',
  ACKNOWLEDGED: '已确认',
  CONFIRMED: '已处理',
  RESOLVED: '已恢复'
};

const ALERT_LEVEL_NAMES: Record<string, string> = {
  LOW: '低',
  MEDIUM: '中',
  HIGH: '高',
  CRITICAL: '严重'
};

const ALERT_METRIC_NAMES: Record<string, string> = {
  soilMoisture: '土壤湿度',
  temperature: '温度',
  light: '光照',
  humidity: '空气湿度'
};

function AdminAlerts() {
  const [status, setStatus] = useState('');
  const [query, setQuery] = useState<{ status?: string }>({});
  const { data, loading, error, refresh } = useAdminLoader(
    () => api.adminAlerts({ ...query, pageSize: 100 }),
    [query]
  );

  // 自动轮询：告警状态（ACTIVE→RESOLVED）由设备侧异步变化，30s 自动刷新保持页面不过期
  useEffect(() => {
    const timer = window.setInterval(() => refresh(), 30_000);
    return () => window.clearInterval(timer);
  }, [refresh]);

  const tabs = [
    { key: '', label: '全部' },
    { key: 'ACTIVE', label: '告警中' },
    { key: 'CONFIRMED', label: '已处理' },
    { key: 'RESOLVED', label: '已恢复' }
  ];

  return (
    <div className="admin-stack">
      <div className="admin-filter-bar">
        <div className="admin-tabs">
          {tabs.map((tab) => (
            <button
              key={tab.key}
              className={query.status === tab.key ? 'active' : ''}
              onClick={() => setQuery({ status: tab.key })}
            >
              {tab.label}
            </button>
          ))}
        </div>
        <button type="button" className="ghost" onClick={refresh} title="刷新">
          <RefreshCw size={15} />
        </button>
      </div>
      <AdminState loading={loading} error={error}>
        <section className="admin-card">
          <div className="admin-table-wrap">
            <table className="admin-table">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>地块</th>
                  <th>指标</th>
                  <th>级别</th>
                  <th>告警内容</th>
                  <th>触发值</th>
                  <th>状态</th>
                  <th>触发时间</th>
                  <th>处理备注</th>
                </tr>
              </thead>
              <tbody>
                {(data?.items ?? []).map((alert) => (
                  <tr key={alert.id}>
                    <td>{alert.id}</td>
                    <td className="admin-strong">
                      {alert.plotCode || `#${alert.plotId}`}
                    </td>
                    <td>{ALERT_METRIC_NAMES[alert.metric] ?? alert.metric}</td>
                    <td>
                      <span className={`admin-badge ${alert.level === 'HIGH' || alert.level === 'CRITICAL' ? 'danger' : 'warn'}`}>
                        {ALERT_LEVEL_NAMES[alert.level] ?? alert.level}
                      </span>
                    </td>
                    <td className="admin-ellipsis" title={alert.title}>{alert.title}</td>
                    <td>{alert.currentValue != null ? alert.currentValue : '--'}</td>
                    <td>
                      <span className={`admin-badge ${alert.status === 'ACTIVE' ? 'danger' : alert.status === 'RESOLVED' ? 'ok' : ''}`}>
                        {ALERT_STATUS_NAMES[alert.status] ?? alert.status}
                      </span>
                    </td>
                    <td>{formatDateTime(alert.startedAt)}</td>
                    <td className="admin-ellipsis" title={alert.confirmRemark ?? ''}>
                      {alert.confirmRemark || '--'}
                    </td>
                  </tr>
                ))}
                {(data?.items ?? []).length === 0 && (
                  <tr>
                    <td colSpan={9} className="admin-empty">暂无报警记录。</td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
          {data && data.total > (data.items ?? []).length && (
            <div className="admin-pager">
              共 {data.total} 条记录（当前显示最近 {data.items.length} 条，可按状态筛选）
            </div>
          )}
        </section>
      </AdminState>
    </div>
  );
}

function formatDateTime(value?: string | null) {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '--';
  return date.toLocaleString('zh-CN', {
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit'
  });
}

function deviceStatusName(status: string) {
  const names: Record<string, string> = {
    ONLINE: '在线',
    OFFLINE: '离线',
    UNACTIVATED: '未激活',
    RECONNECTING: '重连中',
    FAULT: '故障',
    DISABLED: '禁用'
  };
  return names[status] ?? status;
}

// ---------------- 工具函数 ----------------

function formatDate(value?: string | null) {
  if (!value) return '--';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '--';
  return date.toLocaleDateString('zh-CN', { year: 'numeric', month: '2-digit', day: '2-digit' });
}

function errorMessage(error: unknown) {
  if (error instanceof ApiError) return error.message;
  if (error instanceof Error) return error.message;
  return '操作失败，请稍后重试';
}

function plotFormErrorFor(message: string): PlotFormErrors {
  const normalized = message.toLowerCase();
  if (/编码|code/.test(normalized)) return { code: message };
  if (/名称|name/.test(normalized)) return { name: message };
  if (/归属|用户|owner/.test(normalized)) return { ownerId: message };
  if (/面积|area/.test(normalized)) return { area: message };
  return { form: message };
}

function deviceBindErrorFor(message: string): DeviceBindErrors {
  const normalized = message.toLowerCase();
  if (/序列号|devicesn|device sn|serial/.test(normalized)) return { sn: message };
  if (/名称|name/.test(normalized)) return { name: message };
  if (/类型|type/.test(normalized)) return { type: message };
  if (/地块|plot/.test(normalized)) return { plotId: message };
  return { form: message };
}

function warehouseFormErrorFor(message: string): WarehouseFormErrors {
  const normalized = message.toLowerCase();
  if (/warehouse|仓库/.test(normalized)) return { warehouseId: message };
  if (/物资|名称|name/.test(normalized)) return { name: message };
  if (/分类|category/.test(normalized)) return { category: message };
  if (/单位|unit/.test(normalized)) return { unit: message };
  if (/安全库存|初始库存|库存|数量|quantity/.test(normalized)) return { quantity: message };
  if (/备注|reason|remark/.test(normalized)) return { reason: message };
  return { form: message };
}

function formatStock(value: number) {
  return value.toLocaleString('zh-CN', { maximumFractionDigits: 2 });
}

function formatStockChange(value: number) {
  const prefix = value > 0 ? '+' : '';
  return `${prefix}${formatStock(value)}`;
}
