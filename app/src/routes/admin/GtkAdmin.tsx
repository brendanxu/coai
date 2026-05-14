// GtkAdmin — /admin/gtk
// Unified admin panel. Standalone full-page (NOT nested in CoAI AdminPage).
// Six-tab sidebar: Hub / Channels / Users / Ratios / Orders / Settings

import { useState, useEffect, useCallback, useRef } from "react";
import axios from "axios";
import { toast } from "sonner";
import { useSelector } from "react-redux";
import { selectAdmin } from "@/store/auth.ts";
import { listAdminOrders, markOrderPaid, refundOrder } from "@/api/adminOrders.ts";
import type { AdminOrderRow, AdminOrderListParams } from "@/api/adminOrders.ts";
import type { OrderStatus } from "@/api/orders.ts";
import { listUserRouting, updateUserRouting, SUGGESTED_GROUPS } from "@/api/userRouting.ts";
import type { UserRoutingRow } from "@/api/userRouting.ts";
import "./GtkAdmin.css";

// ── Types ──────────────────────────────────────────────────────────────────
type Tab = "hub" | "channels" | "users" | "ratios" | "orders" | "settings";

interface Channel {
  id: number;
  name: string;
  type: number;
  status: number;
  models: string;
  balance?: number;
  used_quota?: number;
  response_time?: number;
  priority?: number;
  weight?: number;
  key?: string;
  base_url?: string;
  other?: string;
}

interface User {
  id: number;
  username: string;
  email?: string;
  role: number;
  status: number;
  quota: number;
  used_quota: number;
  created_time?: number;
}

interface RatioRow { model: string; ratio: number }

const CHANNEL_TYPE_MAP: Record<number, string> = {
  1: "OpenAI", 14: "Anthropic", 17: "Aliyun",
  24: "Moonshot", 36: "SunoAPI", 43: "DeepSeek",
  45: "Sub2API", 99: "Custom",
};

const TYPE_OPTIONS = [1, 14, 17, 24, 36, 43, 45, 99];

// ── Helpers ────────────────────────────────────────────────────────────────
const q2c = (q: number) => Math.round(q / 500);
const c2q = (c: number) => c * 500;

function chStatusLabel(s: number) {
  return s === 1 ? "启用" : s === 2 ? "降级" : "禁用";
}
function chStatusClass(s: number) {
  return s === 1 ? "" : s === 2 ? "warn" : "off";
}

function orderStatusClass(s: OrderStatus): string {
  if (s === "paid" || s === "completed") return "";
  if (s === "pending_payment" || s === "running") return "warn";
  if (s === "refunded" || s === "refunded_post_delivery" || s === "failed" || s === "canceled_mid_flight") return "off";
  return "neutral";
}

function orderStatusLabel(s: OrderStatus): string {
  const map: Record<OrderStatus, string> = {
    pending_payment: "待付款",
    paid: "已付款",
    running: "进行中",
    completed: "已完成",
    refunded: "已退款",
    refunded_post_delivery: "交付后退款",
    failed: "失败",
    canceled_mid_flight: "已取消",
  };
  return map[s] ?? s;
}

function tierClass(r: number) {
  if (r >= 10) return "premium";
  if (r >= 1) return "standard";
  return "light";
}
function tierLabel(r: number) {
  if (r >= 10) return "Premium";
  if (r >= 1) return "Standard";
  return "Light";
}

// ── SVG Icons ──────────────────────────────────────────────────────────────
const IconLeaf = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
    <path d="M11 20A7 7 0 0 1 9.8 6.1C15.5 5 17 4.48 19 2c1 2 2 4.18 2 8 0 5.5-4.78 10-10 10z"/>
    <path d="M2 21c0-3 1.85-5.36 5.08-6C9.5 14.52 12 13 13 12"/>
  </svg>
);
const IconHub = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/>
    <rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/>
  </svg>
);
const IconChannels = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <rect x="2" y="3" width="20" height="4" rx="1"/>
    <rect x="2" y="10" width="20" height="4" rx="1"/>
    <rect x="2" y="17" width="20" height="4" rx="1"/>
    <circle cx="6" cy="5" r="1" fill="currentColor"/>
    <circle cx="6" cy="12" r="1" fill="currentColor"/>
    <circle cx="6" cy="19" r="1" fill="currentColor"/>
  </svg>
);
const IconUsers = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
    <circle cx="9" cy="7" r="4"/>
    <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
    <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
  </svg>
);
const IconRatios = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <line x1="12" y1="1" x2="12" y2="23"/>
    <path d="M17 5H9.5a3.5 3.5 0 0 0 0 7h5a3.5 3.5 0 0 1 0 7H6"/>
  </svg>
);
const IconOrders = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/>
    <polyline points="14 2 14 8 20 8"/>
    <line x1="16" y1="13" x2="8" y2="13"/>
    <line x1="16" y1="17" x2="8" y2="17"/>
    <polyline points="10 9 9 9 8 9"/>
  </svg>
);
const IconSettings = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <circle cx="12" cy="12" r="3"/>
    <path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/>
  </svg>
);
const IconSearch = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
  </svg>
);
const IconPlus = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
  </svg>
);
const IconCheck = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
    <polyline points="20 6 9 17 4 12"/>
  </svg>
);
const IconX = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.5">
    <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
  </svg>
);
const IconEdit = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/>
    <path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"/>
  </svg>
);
const IconTrash = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <polyline points="3 6 5 6 21 6"/>
    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a1 1 0 0 1 1-1h4a1 1 0 0 1 1 1v2"/>
  </svg>
);
const IconPlay = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <polygon points="5 3 19 12 5 21 5 3"/>
  </svg>
);
const IconRefresh = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <polyline points="1 4 1 10 7 10"/>
    <path d="M3.51 15a9 9 0 1 0 .49-4.5"/>
  </svg>
);
const IconInfo = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <circle cx="12" cy="12" r="10"/>
    <line x1="12" y1="8" x2="12" y2="12"/>
    <line x1="12" y1="16" x2="12.01" y2="16"/>
  </svg>
);
const IconArrow = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <line x1="5" y1="12" x2="19" y2="12"/>
    <polyline points="12 5 19 12 12 19"/>
  </svg>
);
const IconWarn = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
    <line x1="12" y1="9" x2="12" y2="13"/>
    <line x1="12" y1="17" x2="12.01" y2="17"/>
  </svg>
);
const IconSpinner = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="gtk-spin" style={{ width: 14, height: 14 }}>
    <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
  </svg>
);

// ── TAB: Hub ───────────────────────────────────────────────────────────────
function HubTab({ setTab, openAddChannel }: { setTab: (t: Tab) => void; openAddChannel: () => void }) {
  const [chans, setChans] = useState<Channel[]>([]);
  const [userTotal, setUserTotal] = useState<number | null>(null);
  const [todayReqs, setTodayReqs] = useState<number | null>(null);
  const [recentOrders, setRecentOrders] = useState<AdminOrderRow[]>([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    Promise.all([
      axios.get<{ success: boolean; data?: { channels: Channel[]; total: number } }>("/gtk/v1/admin/channels").catch(() => null),
      axios.get<{ success: boolean; data?: { users: User[]; total: number } }>("/gtk/v1/admin/users?page=1&page_size=1").catch(() => null),
      axios.get<{ success: boolean; data?: { total_requests: number } }>("/gtk/v1/usage/summary").catch(() => null),
      listAdminOrders({ limit: 5 }).catch(() => null),
    ]).then(([cr, ur, usg, ords]) => {
      if (!alive) return;
      if (cr?.data?.success && cr.data.data) setChans(cr.data.data.channels ?? []);
      if (ur?.data?.success && ur.data.data) setUserTotal(ur.data.data.total ?? null);
      if (usg?.data?.success && usg.data.data) setTodayReqs(usg.data.data.total_requests ?? null);
      if (ords) setRecentOrders(ords.orders ?? []);
      setLoading(false);
    });
    return () => { alive = false; };
  }, []);

  const online = chans.filter(c => c.status === 1).length;
  const degraded = chans.filter(c => c.status === 2).length;
  const offline = chans.filter(c => c.status === 0).length;

  return (
    <div>
      <div className="stats">
        <div className="stat">
          <div className="ic"><IconChannels /></div>
          <span className="lbl lbl">渠道总数</span>
          <div className="v">{loading ? "…" : chans.length}</div>
          <div className="delta">{loading ? "" : `${online} 在线 · ${degraded} 降级 · ${offline} 离线`}</div>
        </div>
        <div className="stat">
          <div className="ic"><IconUsers /></div>
          <span className="lbl lbl">注册用户</span>
          <div className="v">{loading ? "…" : (userTotal ?? "—")}</div>
          <div className="delta">全部注册用户</div>
        </div>
        <div className="stat">
          <div className="ic amber"><IconRatios /></div>
          <span className="lbl lbl">今日调用</span>
          <div className="v">{loading ? "…" : (todayReqs !== null ? todayReqs.toLocaleString() : "—")}</div>
          <div className="delta down">API 请求次数</div>
        </div>
        <div className="stat">
          <div className="ic dark"><IconOrders /></div>
          <span className="lbl lbl">最新订单</span>
          <div className="v">{loading ? "…" : recentOrders.length}</div>
          <div className="delta">近 5 条记录</div>
        </div>
      </div>

      <div className="hub-grid">
        <div className="card">
          <div className="card-head">
            <h4>渠道健康</h4>
            <button className="btn btn-ghost btn-sm" onClick={() => setTab("channels")}>查看全部</button>
          </div>
          <div className="health-list">
            {loading && <div style={{ color: "var(--fg-muted)", fontSize: 13 }}>加载中…</div>}
            {!loading && chans.length === 0 && <div style={{ color: "var(--fg-muted)", fontSize: 13 }}>暂无渠道</div>}
            {chans.slice(0, 5).map(ch => (
              <div key={ch.id} className="health-row">
                <div className={`led ${chStatusClass(ch.status)}`} />
                <div className="nm">
                  <b>{ch.name}</b>
                  <span className="tp">{CHANNEL_TYPE_MAP[ch.type] ?? `T${ch.type}`}</span>
                </div>
                <span className={`status ${chStatusClass(ch.status)}`}>
                  <span className="dot" />
                  {chStatusLabel(ch.status)}
                </span>
                <div className="lat">{ch.response_time != null ? `${ch.response_time}ms` : "—"}</div>
              </div>
            ))}
          </div>
        </div>

        <div className="card">
          <div className="card-head">
            <h4>最近订单</h4>
            <button className="btn btn-ghost btn-sm" onClick={() => setTab("orders")}>查看全部</button>
          </div>
          {loading && <div style={{ color: "var(--fg-muted)", fontSize: 13 }}>加载中…</div>}
          {!loading && recentOrders.length === 0 && <div style={{ color: "var(--fg-muted)", fontSize: 13 }}>暂无订单</div>}
          {recentOrders.map(o => (
            <div key={o.order_no} className="order-row">
              <div className="id-col">
                <b>{o.service_name || o.service_slug}</b>
                <span className="meta">{o.username || "—"} · {o.order_no.slice(0, 14)}…</span>
              </div>
              <div className="amt">{o.price_display_cny}</div>
              <span className={`status ${orderStatusClass(o.status)}`}>
                <span className="dot" />
                {orderStatusLabel(o.status)}
              </span>
            </div>
          ))}
        </div>
      </div>

      <div className="quick-grid">
        <button className="quick" onClick={openAddChannel}>
          <div className="ic"><IconPlus /></div>
          <div>
            <h5>添加渠道</h5>
            <p>接入新的 API 渠道</p>
          </div>
          <div className="arr"><IconArrow /></div>
        </button>
        <button className="quick" onClick={() => setTab("users")}>
          <div className="ic"><IconUsers /></div>
          <div>
            <h5>查看用量</h5>
            <p>用户配额与使用详情</p>
          </div>
          <div className="arr"><IconArrow /></div>
        </button>
        <button className="quick" onClick={() => setTab("settings")}>
          <div className="ic"><IconSettings /></div>
          <div>
            <h5>系统设置</h5>
            <p>全局配置与限速</p>
          </div>
          <div className="arr"><IconArrow /></div>
        </button>
      </div>
    </div>
  );
}

// ── TAB: Channels ──────────────────────────────────────────────────────────
function emptyChannel(): Partial<Channel> {
  return { name: "", type: 1, priority: 100, weight: 1, status: 1, models: "", key: "", base_url: "", other: "" };
}

type ChanStatusFilter = "all" | "1" | "2" | "0";

function ChannelsTab({ initialOpenModal }: { initialOpenModal: boolean }) {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(initialOpenModal);
  const [editChannel, setEditChannel] = useState<Partial<Channel>>(emptyChannel());
  const [isEdit, setIsEdit] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Channel | null>(null);
  const [statusFilter, setStatusFilter] = useState<ChanStatusFilter>("all");
  const [typeFilter, setTypeFilter] = useState<string>("all");
  const [keyword, setKeyword] = useState("");
  const [testingId, setTestingId] = useState<number | null>(null);
  const didMount = useRef(false);

  const load = useCallback(() => {
    setLoading(true);
    axios.get<{ success: boolean; data?: { channels: Channel[] } }>("/gtk/v1/admin/channels")
      .then(r => { if (r.data.success && r.data.data) setChannels(r.data.data.channels ?? []); })
      .catch(() => toast.error("加载渠道失败"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  useEffect(() => {
    if (didMount.current) setShowModal(initialOpenModal);
    else didMount.current = true;
  }, [initialOpenModal]);

  const openAdd = () => { setEditChannel(emptyChannel()); setIsEdit(false); setShowModal(true); };
  const openEdit = (ch: Channel) => { setEditChannel({ ...ch }); setIsEdit(true); setShowModal(true); };

  const saveChannel = () => {
    if (!editChannel.name?.trim()) { toast.error("渠道名称不能为空"); return; }
    const req = isEdit
      ? axios.put(`/gtk/v1/admin/channels/${editChannel.id}`, editChannel)
      : axios.post("/gtk/v1/admin/channels", editChannel);
    req.then(() => { toast.success(isEdit ? "更新成功" : "创建成功"); setShowModal(false); load(); })
      .catch(() => toast.error("操作失败"));
  };

  const testChannel = (id: number) => {
    setTestingId(id);
    axios.post(`/gtk/v1/admin/channels/${id}/test`)
      .then(r => toast.success((r.data as { message?: string })?.message ?? "测试成功"))
      .catch(() => toast.error("测试失败"))
      .finally(() => setTestingId(null));
  };

  const doDelete = () => {
    if (!deleteTarget) return;
    axios.delete(`/gtk/v1/admin/channels/${deleteTarget.id}`)
      .then(() => { toast.success("已删除"); setDeleteTarget(null); load(); })
      .catch(() => toast.error("删除失败"));
  };

  const filtered = channels.filter(ch => {
    if (statusFilter !== "all" && ch.status !== parseInt(statusFilter)) return false;
    if (typeFilter !== "all" && ch.type !== parseInt(typeFilter)) return false;
    if (keyword && !ch.name.toLowerCase().includes(keyword.toLowerCase())) return false;
    return true;
  });

  const statusFilters: { id: ChanStatusFilter; label: string; count: number }[] = [
    { id: "all", label: "全部", count: channels.length },
    { id: "1", label: "启用", count: channels.filter(c => c.status === 1).length },
    { id: "2", label: "降级", count: channels.filter(c => c.status === 2).length },
    { id: "0", label: "禁用", count: channels.filter(c => c.status === 0).length },
  ];

  return (
    <div>
      <div className="tbl">
        <div className="tbl-head">
          <h4>渠道列表</h4>
          <div className="filterbar">
            {statusFilters.map(f => (
              <button
                key={f.id}
                className={`chip${statusFilter === f.id ? " active" : ""}`}
                onClick={() => setStatusFilter(f.id)}
              >
                {f.label}
                <span className="ct">{f.count}</span>
              </button>
            ))}
            <select
              className="select-field"
              style={{ width: 120, height: 30, fontSize: 12 }}
              value={typeFilter}
              onChange={e => setTypeFilter(e.target.value)}
            >
              <option value="all">全部类型</option>
              {TYPE_OPTIONS.map(t => <option key={t} value={t}>{CHANNEL_TYPE_MAP[t] ?? `T${t}`}</option>)}
            </select>
            <div className="tbl-search">
              <IconSearch />
              <input placeholder="搜索渠道名…" value={keyword} onChange={e => setKeyword(e.target.value)} />
            </div>
            <button className="btn btn-primary btn-sm" onClick={openAdd}>
              <IconPlus />添加渠道
            </button>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>ID</th><th>名称</th><th>类型</th><th>状态</th>
              <th>模型</th><th>余额</th><th>用量</th><th>响应</th><th style={{ textAlign: "right" }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr><td colSpan={9} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>加载中…</td></tr>
            )}
            {!loading && filtered.length === 0 && (
              <tr><td colSpan={9} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>暂无渠道</td></tr>
            )}
            {filtered.map(ch => (
              <tr key={ch.id}>
                <td className="id">{ch.id}</td>
                <td style={{ fontWeight: 500 }}>{ch.name}</td>
                <td>
                  <span style={{
                    fontFamily: "var(--font-mono)", fontSize: 11,
                    background: "var(--card-2)", border: "1px solid var(--line)",
                    padding: "2px 7px", borderRadius: 4, color: "var(--fg-2)",
                  }}>
                    {CHANNEL_TYPE_MAP[ch.type] ?? `T${ch.type}`}
                  </span>
                </td>
                <td>
                  <span className={`status ${chStatusClass(ch.status)}`}>
                    <span className="dot" />
                    {chStatusLabel(ch.status)}
                  </span>
                </td>
                <td className="mono" style={{ maxWidth: 140, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: 11 }}>
                  {ch.models || "—"}
                </td>
                <td className="num">{ch.balance != null ? ch.balance.toFixed(2) : "—"}</td>
                <td className="num">{ch.used_quota != null ? q2c(ch.used_quota).toLocaleString() : "—"}</td>
                <td className="mono">{ch.response_time != null ? `${ch.response_time}ms` : "—"}</td>
                <td className="act">
                  <div className="row-act">
                    <button className="btn-sq" title="测试渠道" onClick={() => testChannel(ch.id)} disabled={testingId === ch.id}>
                      {testingId === ch.id ? <IconSpinner /> : <IconPlay />}
                    </button>
                    <button className="btn-sq" title="编辑" onClick={() => openEdit(ch)}>
                      <IconEdit />
                    </button>
                    <button className="btn-sq danger" title="删除" onClick={() => setDeleteTarget(ch)}>
                      <IconTrash />
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        <div className="tbl-foot">
          <span>共 {filtered.length} 个渠道{filtered.length !== channels.length ? `（已过滤，总 ${channels.length}）` : ""}</span>
        </div>
      </div>

      {/* Add/Edit Modal */}
      {showModal && (
        <div className="modal-bg active" onClick={e => { if (e.target === e.currentTarget) setShowModal(false); }}>
          <div className="modal">
            <div className="modal-head">
              <div>
                <h3>{isEdit ? "编辑渠道" : "添加渠道"}</h3>
                <div className="sub">{isEdit ? `ID ${editChannel.id}` : "新建 API 渠道"}</div>
              </div>
              <button className="x" onClick={() => setShowModal(false)}><IconX /></button>
            </div>
            <div className="modal-body">
              <div className="form-grid">
                <div className="full field">
                  <label>渠道名称 <span className="req">*</span></label>
                  <input className="input" value={editChannel.name ?? ""} onChange={e => setEditChannel(p => ({ ...p, name: e.target.value }))} />
                </div>
                <div className="full field">
                  <label>类型</label>
                  <select className="select-field" value={editChannel.type ?? 1} onChange={e => setEditChannel(p => ({ ...p, type: +e.target.value }))}>
                    {TYPE_OPTIONS.map(t => <option key={t} value={t}>{CHANNEL_TYPE_MAP[t] ?? `Type ${t}`}</option>)}
                  </select>
                </div>
                <div className="field">
                  <label>优先级</label>
                  <input className="input" type="number" value={editChannel.priority ?? 100} onChange={e => setEditChannel(p => ({ ...p, priority: +e.target.value }))} />
                </div>
                <div className="field">
                  <label>权重</label>
                  <input className="input" type="number" value={editChannel.weight ?? 1} onChange={e => setEditChannel(p => ({ ...p, weight: +e.target.value }))} />
                </div>
                <div className="full field">
                  <label>API Key</label>
                  <input className="input mono" type="password" value={editChannel.key ?? ""} onChange={e => setEditChannel(p => ({ ...p, key: e.target.value }))} />
                </div>
                <div className="full field">
                  <label>模型 (逗号分隔)</label>
                  <textarea className="textarea-field mono" value={editChannel.models ?? ""} onChange={e => setEditChannel(p => ({ ...p, models: e.target.value }))} />
                </div>
                <div className="full field">
                  <label>状态</label>
                  <select className="select-field" value={editChannel.status ?? 1} onChange={e => setEditChannel(p => ({ ...p, status: +e.target.value }))}>
                    <option value={1}>启用</option>
                    <option value={0}>禁用</option>
                  </select>
                </div>
                {editChannel.type === 45 && (
                  <>
                    <div className="full field">
                      <label>中转地址</label>
                      <input className="input mono" type="url" value={editChannel.base_url ?? ""} onChange={e => setEditChannel(p => ({ ...p, base_url: e.target.value }))} />
                    </div>
                    <div className="full field">
                      <label>Sub2API Token</label>
                      <input className="input mono" type="password" value={editChannel.other ?? ""} onChange={e => setEditChannel(p => ({ ...p, other: e.target.value }))} />
                    </div>
                  </>
                )}
              </div>
            </div>
            <div className="modal-foot">
              <button className="btn btn-ghost" onClick={() => setShowModal(false)}>取消</button>
              <button className="btn btn-primary" onClick={saveChannel}>保存</button>
            </div>
          </div>
        </div>
      )}

      {/* Delete confirm modal */}
      {deleteTarget && (
        <div className="modal-bg active" onClick={e => { if (e.target === e.currentTarget) setDeleteTarget(null); }}>
          <div className="modal narrow">
            <div className="modal-head">
              <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <div className="confirm-ic"><IconWarn /></div>
                <div>
                  <h3>确认删除</h3>
                  <div className="sub">此操作不可撤销</div>
                </div>
              </div>
              <button className="x" onClick={() => setDeleteTarget(null)}><IconX /></button>
            </div>
            <div className="modal-body">
              <p style={{ color: "var(--fg-2)", fontSize: 13.5, margin: 0 }}>
                确认删除渠道 <strong>{deleteTarget.name}</strong>？
                所有使用该渠道的请求将失败。
              </p>
            </div>
            <div className="modal-foot">
              <button className="btn btn-ghost" onClick={() => setDeleteTarget(null)}>取消</button>
              <button className="btn btn-danger-solid" onClick={doDelete}>确认删除</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── TAB: Users ─────────────────────────────────────────────────────────────
function UsersTab() {
  const [users, setUsers] = useState<User[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const pageSize = 20;
  const [keyword, setKeyword] = useState("");
  const [roleFilter, setRoleFilter] = useState<"all" | "admin" | "user">("all");
  const [statusFilter, setStatusFilter] = useState<"all" | "1" | "0">("all");
  const [loading, setLoading] = useState(true);
  const [editQuota, setEditQuota] = useState<Record<number, string>>({});

  // NewAPI routing group, joined by coai_user_id. Fetched separately —
  // the user-routing list only contains users with a NewAPI binding.
  const [routingMap, setRoutingMap] = useState<Map<number, UserRoutingRow>>(new Map());
  const [editRouting, setEditRouting] = useState<UserRoutingRow | null>(null);
  const [editGroup, setEditGroup] = useState("");
  const [savingRouting, setSavingRouting] = useState(false);

  const loadRouting = useCallback(() => {
    listUserRouting({ limit: 500, offset: 0 })
      .then(resp => setRoutingMap(new Map(resp.users.map(u => [u.coai_user_id, u]))))
      .catch(() => setRoutingMap(new Map())); // NewAPI down → degrade to no-binding view
  }, []);

  const load = useCallback((p: number, kw: string) => {
    setLoading(true);
    axios.get<{ success: boolean; data?: { users: User[]; total: number } }>(
      `/gtk/v1/admin/users?page=${p}&page_size=${pageSize}&keyword=${encodeURIComponent(kw)}`
    ).then(r => {
      if (r.data.success && r.data.data) {
        setUsers(r.data.data.users ?? []);
        setTotal(r.data.data.total ?? 0);
      }
    }).catch(() => toast.error("加载用户失败"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(page, keyword); }, [load, page, keyword]);
  useEffect(() => { loadRouting(); }, [loadRouting]);

  const openRouting = (r: UserRoutingRow) => { setEditRouting(r); setEditGroup(r.newapi_group); };
  const closeRouting = () => { if (savingRouting) return; setEditRouting(null); setEditGroup(""); };

  const submitRouting = () => {
    if (!editRouting) return;
    const next = editGroup.trim();
    if (!next) { toast.error("路由组不能为空"); return; }
    setSavingRouting(true);
    updateUserRouting(editRouting.coai_user_id, next)
      .then(() => {
        toast.success(`${editRouting.username || `#${editRouting.coai_user_id}`} 路由组已切换为 ${next}`);
        setEditRouting(null);
        setEditGroup("");
        loadRouting();
      })
      .catch(e => toast.error(e?.message || "切换失败"))
      .finally(() => setSavingRouting(false));
  };

  const toggleStatus = (u: User) => {
    const newStatus = u.status === 1 ? 0 : 1;
    axios.put(`/gtk/v1/admin/users/${u.id}/status`, { status: newStatus })
      .then(() => { toast.success("状态已更新"); load(page, keyword); })
      .catch(() => toast.error("操作失败"));
  };

  const commitQuota = (u: User) => {
    const val = editQuota[u.id];
    if (val == null) return;
    const newCredits = parseInt(val, 10);
    if (isNaN(newCredits)) {
      setEditQuota(p => { const n = { ...p }; delete n[u.id]; return n; });
      return;
    }
    const oldCredits = q2c(u.quota);
    const delta = c2q(newCredits - oldCredits);
    axios.put(`/gtk/v1/admin/users/${u.id}/quota`, { quota_delta: delta })
      .then(() => { toast.success("配额已更新"); load(page, keyword); })
      .catch(() => toast.error("更新失败"))
      .finally(() => setEditQuota(p => { const n = { ...p }; delete n[u.id]; return n; }));
  };

  const cancelQuota = (uid: number) => {
    setEditQuota(p => { const n = { ...p }; delete n[uid]; return n; });
  };

  const filtered = users.filter(u => {
    if (roleFilter === "admin" && u.role < 100) return false;
    if (roleFilter === "user" && u.role >= 100) return false;
    if (statusFilter !== "all" && u.status !== parseInt(statusFilter)) return false;
    return true;
  });

  const totalPages = Math.ceil(total / pageSize);

  return (
    <div>
      <div className="tbl">
        <div className="tbl-head">
          <h4>用户列表</h4>
          <div className="filterbar">
            <select className="select-field" style={{ width: 100, height: 30, fontSize: 12 }} value={roleFilter} onChange={e => setRoleFilter(e.target.value as "all" | "admin" | "user")}>
              <option value="all">全部角色</option>
              <option value="admin">管理员</option>
              <option value="user">普通用户</option>
            </select>
            <select className="select-field" style={{ width: 100, height: 30, fontSize: 12 }} value={statusFilter} onChange={e => setStatusFilter(e.target.value as "all" | "1" | "0")}>
              <option value="all">全部状态</option>
              <option value="1">正常</option>
              <option value="0">禁用</option>
            </select>
            <div className="tbl-search">
              <IconSearch />
              <input placeholder="搜索用户名 / 邮箱…" value={keyword} onChange={e => { setKeyword(e.target.value); setPage(1); }} />
            </div>
          </div>
        </div>

        <div style={{ padding: "8px 18px", background: "var(--accent-soft-2)", borderBottom: "1px solid var(--line)", fontSize: 12, color: "var(--fg-2)", display: "flex", alignItems: "center", gap: 8 }}>
          <IconInfo />
          <span>配额换算：<strong>1 credit = 500 quota</strong>。点击配额数字可内联编辑。</span>
        </div>

        <table>
          <thead>
            <tr>
              <th>ID</th><th>用户名</th><th>邮箱</th><th>角色</th><th>状态</th><th>路由组</th>
              <th>剩余配额</th><th>已用配额</th><th>注册时间</th><th style={{ textAlign: "right" }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={10} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>加载中…</td></tr>}
            {!loading && filtered.length === 0 && <tr><td colSpan={10} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>暂无用户</td></tr>}
            {filtered.map(u => (
              <tr key={u.id}>
                <td className="id">{u.id}</td>
                <td style={{ fontWeight: 500 }}>{u.username}</td>
                <td className="mono" style={{ fontSize: 11.5, color: "var(--fg-muted)" }}>{u.email ?? "—"}</td>
                <td>
                  {u.role >= 100
                    ? <span className="tier light">管理员</span>
                    : <span className="tier">普通</span>
                  }
                </td>
                <td>
                  <span className={`status ${u.status === 1 ? "" : "off"}`}>
                    <span className="dot" />
                    {u.status === 1 ? "正常" : "禁用"}
                  </span>
                </td>
                <td>
                  {routingMap.has(u.id) ? (
                    <span
                      className="editable-num"
                      onClick={() => openRouting(routingMap.get(u.id)!)}
                      title="点击切换 NewAPI 路由组"
                    >
                      <span className="tier">{routingMap.get(u.id)!.newapi_group}</span>
                      <IconEdit />
                    </span>
                  ) : (
                    <span style={{ color: "var(--fg-faint)", fontSize: 12 }} title="该用户无 NewAPI 绑定">—</span>
                  )}
                </td>
                <td>
                  {editQuota[u.id] != null ? (
                    <div className="inline-edit">
                      <input
                        type="number"
                        autoFocus
                        value={editQuota[u.id]}
                        onChange={e => setEditQuota(p => ({ ...p, [u.id]: e.target.value }))}
                        onKeyDown={e => {
                          if (e.key === "Enter") commitQuota(u);
                          if (e.key === "Escape") cancelQuota(u.id);
                        }}
                      />
                      <button className="ok" onClick={() => commitQuota(u)} title="确认"><IconCheck /></button>
                      <button className="cancel" onClick={() => cancelQuota(u.id)} title="取消"><IconX /></button>
                    </div>
                  ) : (
                    <span
                      className="editable-num"
                      onClick={() => setEditQuota(p => ({ ...p, [u.id]: String(q2c(u.quota)) }))}
                      title="点击编辑配额"
                    >
                      {q2c(u.quota).toLocaleString()}
                      <IconEdit />
                    </span>
                  )}
                </td>
                <td className="num">{q2c(u.used_quota).toLocaleString()}</td>
                <td className="mono" style={{ fontSize: 11.5, color: "var(--fg-muted)" }}>
                  {u.created_time ? new Date(u.created_time * 1000).toLocaleDateString("zh-CN") : "—"}
                </td>
                <td className="act">
                  <div className="row-act">
                    <button
                      className={`btn btn-xs ${u.status === 1 ? "btn-ghost" : "btn-primary"}`}
                      onClick={() => toggleStatus(u)}
                    >
                      {u.status === 1 ? "禁用" : "启用"}
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {totalPages > 1 && (
          <div className="tbl-foot">
            <span>共 {total} 用户</span>
            <div className="pager">
              <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}>‹</button>
              {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => {
                const pg = i + 1;
                return (
                  <button key={pg} className={page === pg ? "active" : ""} onClick={() => setPage(pg)}>{pg}</button>
                );
              })}
              {totalPages > 7 && <button disabled>…</button>}
              <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>›</button>
            </div>
          </div>
        )}
      </div>

      {/* Routing group edit modal */}
      {editRouting && (
        <div className="modal-bg active" onClick={e => { if (e.target === e.currentTarget) closeRouting(); }}>
          <div className="modal narrow">
            <div className="modal-head">
              <div>
                <h3>切换路由组</h3>
                <div className="sub">{editRouting.username || `#${editRouting.coai_user_id}`} · NewAPI #{editRouting.newapi_user_id}</div>
              </div>
              <button className="x" onClick={closeRouting}><IconX /></button>
            </div>
            <div className="modal-body">
              <div className="form-grid">
                <div className="full field">
                  <label>当前路由组</label>
                  <div><span className="tier">{editRouting.newapi_group}</span></div>
                </div>
                <div className="full field">
                  <label>选择预设组</label>
                  <select className="select-field" value={SUGGESTED_GROUPS.includes(editGroup) ? editGroup : ""} onChange={e => setEditGroup(e.target.value)}>
                    <option value="">— 自定义 —</option>
                    {SUGGESTED_GROUPS.map(g => <option key={g} value={g}>{g}</option>)}
                  </select>
                </div>
                <div className="full field">
                  <label>路由组名称 <span className="req">*</span></label>
                  <input className="input mono" value={editGroup} onChange={e => setEditGroup(e.target.value)} placeholder="可输入自定义组名" />
                </div>
              </div>
            </div>
            <div className="modal-foot">
              <button className="btn btn-ghost" onClick={closeRouting} disabled={savingRouting}>取消</button>
              <button className="btn btn-primary" onClick={submitRouting} disabled={savingRouting}>
                {savingRouting ? "保存中…" : "保存"}
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── TAB: Model Ratios ──────────────────────────────────────────────────────
function RatiosTab() {
  const [rows, setRows] = useState<RatioRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);

  const loadRatios = useCallback(() => {
    setLoading(true);
    axios.get<{ success: boolean; data?: Record<string, number> }>("/gtk/v1/admin/model-ratios")
      .then(r => {
        if (r.data.success && r.data.data) {
          setRows(Object.entries(r.data.data).map(([model, ratio]) => ({ model, ratio })));
        }
      }).catch(() => toast.error("加载失败"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { loadRatios(); }, [loadRatios]);

  const update = (idx: number, field: keyof RatioRow, val: string | number) => {
    setRows(prev => prev.map((r, i) => i === idx ? { ...r, [field]: val } : r));
    setDirty(true);
  };

  const addRow = () => { setRows(prev => [...prev, { model: "", ratio: 1 }]); setDirty(true); };

  const deleteRow = (idx: number) => { setRows(prev => prev.filter((_, i) => i !== idx)); setDirty(true); };

  const save = () => {
    setSaving(true);
    const ratios: Record<string, number> = {};
    rows.forEach(r => { if (r.model.trim()) ratios[r.model.trim()] = r.ratio; });
    axios.put("/gtk/v1/admin/model-ratios", { ratios })
      .then(() => { toast.success("已保存"); setDirty(false); })
      .catch(() => toast.error("保存失败"))
      .finally(() => setSaving(false));
  };

  return (
    <div>
      <div className="ratio-banner">
        <div className="ic"><IconInfo /></div>
        <div>
          公式：<b>1 credit = ¥0.02</b>，Ratio = N → <code>N credits / 1M tokens</code> = <b>¥{"{N×0.02}"}/1M tokens</b>。
          点击模型名或 ratio 数字可直接编辑，编辑后点击"全部保存"生效。
        </div>
      </div>

      <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginBottom: 12 }}>
        <button className="btn btn-ghost btn-sm" onClick={loadRatios}>
          <IconRefresh />恢复默认
        </button>
        <button className="btn btn-ghost btn-sm" onClick={addRow}>
          <IconPlus />新增行
        </button>
        <button className="btn btn-primary btn-sm" disabled={!dirty || saving} onClick={save}>
          {saving ? <IconSpinner /> : null}
          {saving ? "保存中…" : "全部保存"}
        </button>
      </div>

      <div className="tbl">
        <table>
          <thead>
            <tr>
              <th>模型名称</th><th style={{ textAlign: "right" }}>Ratio</th>
              <th>等价 ¥/1M tokens</th><th>档位</th><th></th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={5} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>加载中…</td></tr>}
            {rows.map((r, i) => (
              <tr key={i}>
                <td>
                  <input
                    className="input"
                    style={{ height: 32, fontSize: 13 }}
                    value={r.model}
                    onChange={e => update(i, "model", e.target.value)}
                  />
                </td>
                <td style={{ textAlign: "right" }}>
                  <input
                    type="number"
                    step="0.001"
                    className="ratio-input"
                    value={r.ratio}
                    onChange={e => update(i, "ratio", parseFloat(e.target.value) || 0)}
                  />
                </td>
                <td>
                  <span className="equiv">¥{(r.ratio * 0.02).toFixed(4)}</span>
                </td>
                <td>
                  <span className={`tier ${tierClass(r.ratio)}`}>{tierLabel(r.ratio)}</span>
                </td>
                <td style={{ textAlign: "right" }}>
                  <button className="btn-sq danger" onClick={() => deleteRow(i)} title="删除">
                    <IconTrash />
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="tbl-foot">
          <span>{rows.length} 个模型比例</span>
        </div>
      </div>
    </div>
  );
}

// ── TAB: Orders ────────────────────────────────────────────────────────────
const ORDER_PAGE_SIZE = 20;

function OrdersTab() {
  const [orders, setOrders] = useState<AdminOrderRow[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [statusFilter, setStatusFilter] = useState<OrderStatus | "">("");
  const [keyword, setKeyword] = useState("");
  const [loading, setLoading] = useState(true);
  const [markingId, setMarkingId] = useState<string | null>(null);
  const [refundTarget, setRefundTarget] = useState<AdminOrderRow | null>(null);
  const [refundReason, setRefundReason] = useState("");

  const load = useCallback((p: number, status: OrderStatus | "", q: string) => {
    setLoading(true);
    const params: AdminOrderListParams = {
      offset: (p - 1) * ORDER_PAGE_SIZE,
      limit: ORDER_PAGE_SIZE,
    };
    if (status) params.status = status;
    if (q) params.q = q;
    listAdminOrders(params)
      .then(res => {
        setOrders(res.orders ?? []);
        setTotal(res.total ?? 0);
      })
      .catch(() => toast.error("加载订单失败"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(page, statusFilter, keyword); }, [load, page, statusFilter, keyword]);

  const doMarkPaid = (orderNo: string) => {
    setMarkingId(orderNo);
    markOrderPaid(orderNo)
      .then(() => { toast.success("已标记为已付款"); load(page, statusFilter, keyword); })
      .catch(e => toast.error(e?.message ?? "操作失败"))
      .finally(() => setMarkingId(null));
  };

  const doRefund = () => {
    if (!refundTarget || !refundReason.trim()) return;
    refundOrder(refundTarget.order_no, refundReason)
      .then(() => { toast.success("已标记为退款"); setRefundTarget(null); setRefundReason(""); load(page, statusFilter, keyword); })
      .catch(e => toast.error(e?.message ?? "退款失败"));
  };

  const totalPages = Math.ceil(total / ORDER_PAGE_SIZE);

  const statusOptions: { value: OrderStatus | ""; label: string }[] = [
    { value: "", label: "全部状态" },
    { value: "pending_payment", label: "待付款" },
    { value: "paid", label: "已付款" },
    { value: "running", label: "进行中" },
    { value: "completed", label: "已完成" },
    { value: "refunded", label: "已退款" },
    { value: "failed", label: "失败" },
    { value: "canceled_mid_flight", label: "已取消" },
  ];

  return (
    <div>
      <div className="tbl">
        <div className="tbl-head">
          <h4>订单列表</h4>
          <div className="filterbar">
            <select
              className="select-field"
              style={{ width: 130, height: 30, fontSize: 12 }}
              value={statusFilter}
              onChange={e => { setStatusFilter(e.target.value as OrderStatus | ""); setPage(1); }}
            >
              {statusOptions.map(o => <option key={o.value} value={o.value}>{o.label}</option>)}
            </select>
            <div className="tbl-search">
              <IconSearch />
              <input
                placeholder="搜索订单号 / 用户…"
                value={keyword}
                onChange={e => { setKeyword(e.target.value); setPage(1); }}
              />
            </div>
            <button className="btn-sq" title="刷新" onClick={() => load(page, statusFilter, keyword)}>
              <IconRefresh />
            </button>
          </div>
        </div>

        <table>
          <thead>
            <tr>
              <th>订单号</th><th>用户</th><th>产品</th>
              <th style={{ textAlign: "right" }}>金额</th>
              <th>状态</th><th>支付</th><th>时间</th>
              <th style={{ textAlign: "right" }}>操作</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr><td colSpan={8} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>加载中…</td></tr>
            )}
            {!loading && orders.length === 0 && (
              <tr><td colSpan={8} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>暂无订单</td></tr>
            )}
            {orders.map(o => (
              <tr key={o.order_no}>
                <td className="id" title={o.order_no}>{o.order_no.slice(0, 16)}…</td>
                <td style={{ fontWeight: 500 }}>{o.username || <span style={{ color: "var(--fg-muted)" }}>—</span>}</td>
                <td>{o.service_name || o.service_slug}</td>
                <td className="num">{o.price_display_cny}</td>
                <td>
                  <span className={`status ${orderStatusClass(o.status)}`}>
                    <span className="dot" />
                    {orderStatusLabel(o.status)}
                  </span>
                </td>
                <td>
                  <span style={{
                    fontFamily: "var(--font-mono)", fontSize: 11,
                    background: "var(--card-2)", border: "1px solid var(--line)",
                    padding: "2px 7px", borderRadius: 4, color: "var(--fg-2)",
                  }}>
                    {o.payment_provider}
                  </span>
                </td>
                <td className="mono" style={{ fontSize: 11.5, color: "var(--fg-muted)" }}>
                  {new Date(o.created_at).toLocaleDateString("zh-CN")}
                </td>
                <td className="act">
                  <div className="row-act">
                    {o.status === "pending_payment" && (
                      <button
                        className="btn btn-xs btn-primary"
                        disabled={markingId === o.order_no}
                        onClick={() => doMarkPaid(o.order_no)}
                        title="标记已付款"
                      >
                        {markingId === o.order_no ? <IconSpinner /> : "✓ 付款"}
                      </button>
                    )}
                    {(o.status === "paid" || o.status === "completed") && (
                      <button
                        className="btn btn-xs btn-danger"
                        onClick={() => { setRefundTarget(o); setRefundReason(""); }}
                        title="退款"
                      >
                        退款
                      </button>
                    )}
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>

        {totalPages > 1 && (
          <div className="tbl-foot">
            <span>共 {total} 订单</span>
            <div className="pager">
              <button disabled={page <= 1} onClick={() => setPage(p => p - 1)}>‹</button>
              {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => {
                const pg = i + 1;
                return (
                  <button key={pg} className={page === pg ? "active" : ""} onClick={() => setPage(pg)}>{pg}</button>
                );
              })}
              {totalPages > 7 && <button disabled>…</button>}
              <button disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>›</button>
            </div>
          </div>
        )}
      </div>

      {/* Refund confirm modal */}
      {refundTarget && (
        <div className="modal-bg active" onClick={e => { if (e.target === e.currentTarget) setRefundTarget(null); }}>
          <div className="modal narrow">
            <div className="modal-head">
              <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
                <div className="confirm-ic"><IconWarn /></div>
                <div>
                  <h3>确认退款</h3>
                  <div className="sub">仅更新本系统记录，实际退款在支付平台操作</div>
                </div>
              </div>
              <button className="x" onClick={() => setRefundTarget(null)}><IconX /></button>
            </div>
            <div className="modal-body">
              <p style={{ color: "var(--fg-2)", fontSize: 13, marginBottom: 14 }}>
                订单 <code style={{ fontFamily: "var(--font-mono)", fontSize: 12, background: "var(--card-2)", padding: "2px 6px", borderRadius: 4 }}>{refundTarget.order_no}</code>，
                金额 <strong>{refundTarget.price_display_cny}</strong>。
              </p>
              <div className="field">
                <label>退款原因 <span className="req">*</span></label>
                <input
                  className="input"
                  placeholder="请填写退款原因…"
                  value={refundReason}
                  onChange={e => setRefundReason(e.target.value)}
                />
              </div>
            </div>
            <div className="modal-foot">
              <button className="btn btn-ghost" onClick={() => setRefundTarget(null)}>取消</button>
              <button
                className="btn btn-danger-solid"
                disabled={!refundReason.trim()}
                onClick={doRefund}
              >
                确认退款
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

// ── TAB: Settings ──────────────────────────────────────────────────────────
const AUTH_KEYS = ["PasswordLoginEnabled", "RegisterEnabled", "EmailVerification"];
const RATE_KEYS = ["GlobalApiRateLimitNum", "GlobalApiRateLimitDuration"];

function SettingsTab() {
  const [opts, setOpts] = useState<Record<string, string>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    axios.get<{ success: boolean; data?: Record<string, unknown> }>("/gtk/v1/admin/system-options")
      .then(r => {
        if (r.data.success && r.data.data) {
          const normalized = Object.fromEntries(
            Object.entries(r.data.data).map(([k, v]) => [k, typeof v === "string" ? v : JSON.stringify(v)])
          );
          setOpts(normalized);
        }
      })
      .catch(() => toast.error("加载配置失败"))
      .finally(() => setLoading(false));
  }, []);

  const put = (key: string, value: string) => {
    axios.put("/gtk/v1/admin/system-options", { key, value })
      .then(() => toast.success(`${key} 已更新`))
      .catch(() => toast.error("更新失败"));
  };

  const saveGroup = (keys: string[]) => {
    Promise.all(keys.filter(k => opts[k] != null).map(k => axios.put("/gtk/v1/admin/system-options", { key: k, value: opts[k] })))
      .then(() => toast.success("已保存"))
      .catch(() => toast.error("保存失败"));
  };

  const otherKeys = Object.keys(opts).filter(k => ![...AUTH_KEYS, ...RATE_KEYS].includes(k));

  if (loading) return <div style={{ color: "var(--fg-muted)", padding: 20, fontFamily: "var(--font-mono)", fontSize: 13 }}>加载中…</div>;

  const toggleOpt = (k: string) => {
    setOpts(p => ({ ...p, [k]: p[k] === "true" ? "false" : "true" }));
  };

  return (
    <div>
      <div className="settings-grid">
        <div className="settings-group">
          <h4>登录 & 注册</h4>
          <div className="desc">控制用户注册和认证方式</div>
          <div className="row">
            <div className="info"><b>允许密码登录</b><span>用户可用邮箱+密码登录</span></div>
            <button className={`toggle${opts["PasswordLoginEnabled"] === "true" ? " on" : ""}`} onClick={() => toggleOpt("PasswordLoginEnabled")} />
          </div>
          <div className="row">
            <div className="info"><b>开放注册</b><span>允许新用户自行注册账号</span></div>
            <button className={`toggle${opts["RegisterEnabled"] === "true" ? " on" : ""}`} onClick={() => toggleOpt("RegisterEnabled")} />
          </div>
          <div className="row">
            <div className="info"><b>邮箱验证</b><span>注册时要求验证邮箱</span></div>
            <button className={`toggle${opts["EmailVerification"] === "true" ? " on" : ""}`} onClick={() => toggleOpt("EmailVerification")} />
          </div>
          <div className="save-row">
            <span className="meta">修改后点击保存生效</span>
            <button className="btn btn-primary btn-sm" onClick={() => saveGroup(AUTH_KEYS)}>保存</button>
          </div>
        </div>

        <div className="settings-group">
          <h4>全局限速</h4>
          <div className="desc">API 请求速率控制</div>
          <div className="field" style={{ marginBottom: 12 }}>
            <label>速率限制次数</label>
            <input
              type="number"
              className="input"
              value={opts["GlobalApiRateLimitNum"] ?? ""}
              onChange={e => setOpts(p => ({ ...p, GlobalApiRateLimitNum: e.target.value }))}
            />
          </div>
          <div className="field" style={{ marginBottom: 12 }}>
            <label>时间窗口 (秒)</label>
            <input
              type="number"
              className="input"
              value={opts["GlobalApiRateLimitDuration"] ?? ""}
              onChange={e => setOpts(p => ({ ...p, GlobalApiRateLimitDuration: e.target.value }))}
            />
          </div>
          <div className="save-row">
            <span className="meta">次 / 秒区间</span>
            <button className="btn btn-primary btn-sm" onClick={() => saveGroup(RATE_KEYS)}>保存</button>
          </div>
        </div>
      </div>

      <div className="kv-section">
        <div className="tbl-head">
          <h4>其他配置</h4>
          <span style={{ color: "var(--fg-muted)", fontSize: 12, marginLeft: "auto" }}>{otherKeys.length} 项</span>
        </div>
        {otherKeys.length === 0 ? (
          <div style={{ padding: "20px 18px", color: "var(--fg-muted)", fontSize: 13 }}>暂无其他配置项</div>
        ) : (
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <thead>
              <tr>
                <th style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.14em", textTransform: "uppercase", color: "var(--fg-muted)", padding: "10px 18px", background: "var(--card-2)", textAlign: "left", borderBottom: "1px solid var(--line)" }}>键</th>
                <th style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.14em", textTransform: "uppercase", color: "var(--fg-muted)", padding: "10px 18px", background: "var(--card-2)", textAlign: "left", borderBottom: "1px solid var(--line)" }}>值</th>
              </tr>
            </thead>
            <tbody>
              {otherKeys.map(k => (
                <tr key={k} style={{ borderBottom: "1px solid var(--line)" }}>
                  <td style={{ padding: "10px 18px", fontFamily: "var(--font-mono)", fontSize: 12, color: "var(--fg-muted)", whiteSpace: "nowrap", width: 200 }}>{k}</td>
                  <td style={{ padding: "8px 18px" }}>
                    <input
                      className="input"
                      style={{ height: 32, fontSize: 12, fontFamily: "var(--font-mono)" }}
                      value={opts[k]}
                      onChange={e => setOpts(p => ({ ...p, [k]: e.target.value }))}
                      onBlur={() => put(k, opts[k])}
                    />
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  );
}

// ── Nav config ─────────────────────────────────────────────────────────────
const NAV: { id: Tab; label: string; Icon: React.FC }[] = [
  { id: "hub", label: "仪表盘", Icon: IconHub },
  { id: "channels", label: "渠道管理", Icon: IconChannels },
  { id: "users", label: "用户管理", Icon: IconUsers },
  { id: "ratios", label: "模型计费", Icon: IconRatios },
  { id: "orders", label: "订单", Icon: IconOrders },
  { id: "settings", label: "系统设置", Icon: IconSettings },
];

const TAB_TITLES: Record<Tab, string> = {
  hub: "仪表盘",
  channels: "渠道管理",
  users: "用户管理",
  ratios: "模型计费",
  orders: "订单",
  settings: "系统设置",
};

// ── Root component ─────────────────────────────────────────────────────────
function GtkAdmin() {
  const isAdmin = useSelector(selectAdmin);
  const [tab, setTab] = useState<Tab>("hub");
  const [triggerAddChannel, setTriggerAddChannel] = useState(false);

  const handleSetTab = useCallback((t: Tab) => setTab(t), []);

  const openAddChannel = useCallback(() => {
    setTab("channels");
    setTriggerAddChannel(p => !p);
  }, []);

  if (!isAdmin) {
    return (
      <div className="gtk-admin" style={{ display: "flex", alignItems: "center", justifyContent: "center", minHeight: "100vh", background: "var(--bg)" }}>
        <div className="card" style={{ textAlign: "center", padding: 48, maxWidth: 360 }}>
          <div className="font-display" style={{ fontSize: 24, marginBottom: 8 }}>权限不足</div>
          <div style={{ color: "var(--fg-muted)", fontSize: 14 }}>需要管理员权限才能访问此页面</div>
        </div>
      </div>
    );
  }

  return (
    <div className="gtk-admin gtk-wrap">
      {/* Sidebar */}
      <aside className="gtk-sb">
        <div className="gtk-sb-brand">
          <div className="mk"><IconLeaf /></div>
          greentokey
          <span className="pill">GTK</span>
        </div>

        <div className="gtk-sb-grouplabel">导航</div>

        {NAV.map(({ id, label, Icon }) => (
          <button
            key={id}
            className={`gtk-sb-item${tab === id ? " active" : ""}`}
            onClick={() => handleSetTab(id)}
          >
            <Icon />
            <span>{label}</span>
          </button>
        ))}

        <div className="gtk-sb-foot">
          <div className="gtk-sb-user">
            <div className="av">B</div>
            <div className="info">
              <b>brendan</b>
              <span>founder · root</span>
            </div>
          </div>
        </div>
      </aside>

      {/* Main area */}
      <div className="gtk-main">
        <header className="gtk-topbar">
          <h1>
            <span className="crumb">/admin/gtk</span>
            <em>{TAB_TITLES[tab]}</em>
          </h1>
          <div className="gtk-topbar-right">
            <span className="status">
              <span className="dot" />
              系统正常
            </span>
            <div className="gtk-topbar-user">
              <div className="av">B</div>
              <b>brendan</b>
            </div>
          </div>
        </header>

        <main className="gtk-page-area">
          {tab === "hub" && <HubTab setTab={handleSetTab} openAddChannel={openAddChannel} />}
          {tab === "channels" && <ChannelsTab initialOpenModal={triggerAddChannel} />}
          {tab === "users" && <UsersTab />}
          {tab === "ratios" && <RatiosTab />}
          {tab === "orders" && <OrdersTab />}
          {tab === "settings" && <SettingsTab />}
        </main>
      </div>
    </div>
  );
}

export default GtkAdmin;
