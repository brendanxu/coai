// GtkAdmin — /admin/gtk
// Unified admin panel. Standalone full-page (NOT nested in CoAI AdminPage).
// Five-tab sidebar: Hub / Channels / Users / Ratios / Settings

import { useState, useEffect, useCallback, useRef } from "react";
import axios from "axios";
import { toast } from "sonner";
import { useSelector } from "react-redux";
import { selectAdmin } from "@/store/auth.ts";

// ── Design tokens ──────────────────────────────────────────────────────────
const T = {
  bg: "#F4EFE5",
  accent: "#2D5A3D",
  ink: "#2B2118",
  fg2: "#594E45",
  muted: "#9B8E85",
  card: "#FDFAF4",
  card2: "#F7F2E8",
  line: "rgba(43,33,24,0.09)",
  display: "Fraunces, serif",
  body: "Manrope, sans-serif",
  mono: "JetBrains Mono, monospace",
};

// ── Types ──────────────────────────────────────────────────────────────────
type Tab = "hub" | "channels" | "users" | "ratios" | "settings";

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

const CHANNEL_TYPE_MAP: Record<number, string> = {
  1: "OpenAI", 14: "Anthropic", 17: "Aliyun",
  24: "Moonshot", 36: "SunoAPI", 43: "DeepSeek",
  45: "Sub2API", 99: "Custom",
};

const TYPE_OPTIONS = [1, 14, 17, 24, 36, 43, 45, 99];

// ── Helpers ────────────────────────────────────────────────────────────────
const q2c = (q: number) => Math.round(q / 500);
const c2q = (c: number) => c * 500;

function Led({ status }: { status: number }) {
  const color = status === 1 ? "#22c55e" : status === 2 ? "#f59e0b" : "#ef4444";
  return (
    <span style={{
      display: "inline-block", width: 8, height: 8, borderRadius: "50%",
      background: color, flexShrink: 0,
    }} />
  );
}

function Chip({ label, color }: { label: string; color?: string }) {
  return (
    <span style={{
      fontSize: 11, fontFamily: T.mono, padding: "2px 6px", borderRadius: 4,
      background: color ?? T.card2, color: T.fg2, border: `1px solid ${T.line}`,
      whiteSpace: "nowrap",
    }}>{label}</span>
  );
}

function statusLabel(s: number) {
  return s === 1 ? "健康" : s === 2 ? "降级" : "离线";
}

function Card({ children, style }: { children: React.ReactNode; style?: React.CSSProperties }) {
  return (
    <div style={{
      background: T.card, borderRadius: 12, padding: "20px 24px",
      border: `1px solid ${T.line}`, ...style,
    }}>{children}</div>
  );
}

function Th({ children }: { children?: React.ReactNode }) {
  return (
    <th style={{
      textAlign: "left", fontSize: 11, fontFamily: T.mono, color: T.muted,
      padding: "8px 12px", borderBottom: `1px solid ${T.line}`,
      fontWeight: 600, whiteSpace: "nowrap",
    }}>{children}</th>
  );
}

function Td({ children, style }: { children: React.ReactNode; style?: React.CSSProperties }) {
  return (
    <td style={{ padding: "10px 12px", fontSize: 13, ...style }}>{children}</td>
  );
}

function Btn({
  children, onClick, variant = "default", style, disabled,
}: {
  children: React.ReactNode;
  onClick?: () => void;
  variant?: "default" | "primary" | "danger" | "ghost";
  style?: React.CSSProperties;
  disabled?: boolean;
}) {
  const base: React.CSSProperties = {
    border: "none", borderRadius: 8, padding: "8px 14px", fontSize: 13,
    fontFamily: T.body, cursor: disabled ? "not-allowed" : "pointer",
    opacity: disabled ? 0.5 : 1, transition: "opacity 0.15s",
  };
  const variants: Record<string, React.CSSProperties> = {
    default: { background: T.card2, color: T.ink, border: `1px solid ${T.line}` },
    primary: { background: T.accent, color: "#fff" },
    danger: { background: "#ef4444", color: "#fff" },
    ghost: { background: "transparent", color: T.fg2, padding: "4px 8px" },
  };
  return (
    <button onClick={onClick} disabled={disabled} style={{ ...base, ...variants[variant], ...style }}>
      {children}
    </button>
  );
}

// ── Modal wrapper ──────────────────────────────────────────────────────────
function Modal({ title, onClose, children }: {
  title: string; onClose: () => void; children: React.ReactNode;
}) {
  return (
    <div style={{
      position: "fixed", inset: 0, background: "rgba(0,0,0,0.4)",
      display: "flex", alignItems: "center", justifyContent: "center", zIndex: 1000,
    }}>
      <div style={{
        background: T.card, borderRadius: 16, padding: 28,
        width: 520, maxWidth: "90vw", maxHeight: "85vh", overflowY: "auto",
        boxShadow: "0 8px 40px rgba(0,0,0,0.2)",
      }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 20 }}>
          <h3 style={{ margin: 0, fontFamily: T.display, fontSize: 18 }}>{title}</h3>
          <button onClick={onClose} style={{ background: "none", border: "none", cursor: "pointer", fontSize: 20, color: T.muted }}>×</button>
        </div>
        {children}
      </div>
    </div>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <label style={{ display: "block", fontSize: 12, color: T.muted, marginBottom: 4, fontFamily: T.body }}>{label}</label>
      {children}
    </div>
  );
}

const inputStyle: React.CSSProperties = {
  width: "100%", boxSizing: "border-box", padding: "8px 10px",
  borderRadius: 8, border: `1px solid ${T.line}`, background: T.bg,
  fontFamily: T.body, fontSize: 13, color: T.ink, outline: "none",
};

// ── TAB: Hub ───────────────────────────────────────────────────────────────
function HubTab({ setTab, openAddChannel }: { setTab: (t: Tab) => void; openAddChannel: () => void }) {
  const [chans, setChans] = useState<Channel[]>([]);
  const [userTotal, setUserTotal] = useState<number | null>(null);
  const [todayReqs, setTodayReqs] = useState<number | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let alive = true;
    Promise.all([
      axios.get<{ success: boolean; data?: { channels: Channel[]; total: number } }>("/gtk/v1/admin/channels").catch(() => null),
      axios.get<{ success: boolean; data?: { users: User[]; total: number } }>("/gtk/v1/admin/users?page=1&page_size=1").catch(() => null),
      axios.get<{ success: boolean; data?: { total_requests: number } }>("/gtk/v1/usage/summary").catch(() => null),
    ]).then(([cr, ur, usg]) => {
      if (!alive) return;
      if (cr?.data?.success && cr.data.data) setChans(cr.data.data.channels ?? []);
      if (ur?.data?.success && ur.data.data) setUserTotal(ur.data.data.total ?? null);
      if (usg?.data?.success && usg.data.data) setTodayReqs(usg.data.data.total_requests ?? null);
      setLoading(false);
    });
    return () => { alive = false; };
  }, []);

  const online = chans.filter(c => c.status === 1).length;
  const offline = chans.filter(c => c.status === 0).length;

  const statCards = [
    { label: "渠道总数", value: loading ? "…" : chans.length, sub: loading ? "" : `${online} 在线 · ${offline} 离线` },
    { label: "注册用户", value: loading ? "…" : (userTotal ?? "—"), sub: "" },
    { label: "今日调用", value: loading ? "…" : (todayReqs !== null ? todayReqs.toLocaleString() : "—"), sub: "" },
    { label: "今日营收", value: "—", sub: "暂未接入" },
  ];

  return (
    <div>
      <div style={{ display: "grid", gridTemplateColumns: "repeat(4,1fr)", gap: 16, marginBottom: 28 }}>
        {statCards.map(c => (
          <Card key={c.label}>
            <div style={{ fontSize: 12, color: T.muted, marginBottom: 6 }}>{c.label}</div>
            <div style={{ fontFamily: T.display, fontSize: 28, fontWeight: 700, color: T.accent }}>{c.value}</div>
            {c.sub && <div style={{ fontSize: 11, color: T.muted, marginTop: 4 }}>{c.sub}</div>}
          </Card>
        ))}
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16, marginBottom: 28 }}>
        <Card>
          <div style={{ fontFamily: T.display, fontSize: 15, marginBottom: 14 }}>渠道健康</div>
          {loading ? <div style={{ color: T.muted, fontSize: 13 }}>加载中…</div> : chans.slice(0, 5).map(ch => (
            <div key={ch.id} style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 0", borderBottom: `1px solid ${T.line}` }}>
              <Led status={ch.status} />
              <span style={{ flex: 1, fontSize: 13, fontWeight: 500 }}>{ch.name}</span>
              <Chip label={CHANNEL_TYPE_MAP[ch.type] ?? `T${ch.type}`} />
              <span style={{ fontSize: 12, color: T.muted, minWidth: 36, textAlign: "right" }}>{statusLabel(ch.status)}</span>
              {ch.response_time != null && (
                <span style={{ fontSize: 11, color: T.muted, fontFamily: T.mono }}>{ch.response_time}ms</span>
              )}
            </div>
          ))}
          {!loading && chans.length === 0 && <div style={{ color: T.muted, fontSize: 13 }}>暂无渠道</div>}
        </Card>

        <Card>
          <div style={{ fontFamily: T.display, fontSize: 15, marginBottom: 14 }}>最近订单</div>
          <div style={{ color: T.muted, fontSize: 13 }}>暂无数据</div>
        </Card>
      </div>

      <div style={{ display: "grid", gridTemplateColumns: "repeat(3,1fr)", gap: 16 }}>
        {[
          { label: "添加渠道", sub: "接入新的 API 渠道", action: () => openAddChannel() },
          { label: "查看用量", sub: "用户配额与使用详情", action: () => setTab("users") },
          { label: "系统设置", sub: "全局配置与限速", action: () => setTab("settings") },
        ].map(a => (
          <button key={a.label} onClick={a.action} style={{
            background: T.card2, border: `1px solid ${T.line}`, borderRadius: 12,
            padding: "18px 20px", textAlign: "left", cursor: "pointer", transition: "box-shadow 0.15s",
          }}
            onMouseEnter={e => (e.currentTarget.style.boxShadow = "0 4px 16px rgba(0,0,0,0.1)")}
            onMouseLeave={e => (e.currentTarget.style.boxShadow = "none")}
          >
            <div style={{ fontFamily: T.display, fontSize: 15, color: T.ink, marginBottom: 4 }}>{a.label} →</div>
            <div style={{ fontSize: 12, color: T.muted }}>{a.sub}</div>
          </button>
        ))}
      </div>
    </div>
  );
}

// ── TAB: Channels ──────────────────────────────────────────────────────────
function emptyChannel(): Partial<Channel> {
  return { name: "", type: 1, priority: 100, weight: 1, status: 1, models: "", key: "", base_url: "", other: "" };
}

function ChannelsTab({ initialOpenModal }: { initialOpenModal: boolean }) {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(true);
  const [showModal, setShowModal] = useState(initialOpenModal);
  const [editChannel, setEditChannel] = useState<Partial<Channel>>(emptyChannel());
  const [isEdit, setIsEdit] = useState(false);
  const [deleteTarget, setDeleteTarget] = useState<Channel | null>(null);
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
    axios.post(`/gtk/v1/admin/channels/${id}/test`)
      .then(r => toast.success((r.data as { message?: string })?.message ?? "测试成功"))
      .catch(() => toast.error("测试失败"));
  };

  const doDelete = () => {
    if (!deleteTarget) return;
    axios.delete(`/gtk/v1/admin/channels/${deleteTarget.id}`)
      .then(() => { toast.success("已删除"); setDeleteTarget(null); load(); })
      .catch(() => toast.error("删除失败"));
  };

  return (
    <div>
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 20 }}>
        <span style={{ fontSize: 14, color: T.muted }}>{channels.length} 个渠道</span>
        <Btn variant="primary" onClick={openAdd}>+ 添加渠道</Btn>
      </div>

      <Card style={{ padding: 0, overflowX: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <Th>ID</Th><Th>名称</Th><Th>类型</Th><Th>状态</Th>
              <Th>模型</Th><Th>余额</Th><Th>用量</Th><Th>响应</Th><Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr><td colSpan={9} style={{ padding: 20, textAlign: "center", color: T.muted }}>加载中…</td></tr>
            )}
            {!loading && channels.length === 0 && (
              <tr><td colSpan={9} style={{ padding: 20, textAlign: "center", color: T.muted }}>暂无渠道</td></tr>
            )}
            {channels.map(ch => (
              <tr key={ch.id} style={{ borderBottom: `1px solid ${T.line}` }}>
                <Td><span style={{ fontFamily: T.mono, fontSize: 12 }}>{ch.id}</span></Td>
                <Td>{ch.name}</Td>
                <Td><Chip label={CHANNEL_TYPE_MAP[ch.type] ?? `T${ch.type}`} /></Td>
                <Td>
                  <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <Led status={ch.status} />
                    <span style={{ fontSize: 12 }}>{statusLabel(ch.status)}</span>
                  </div>
                </Td>
                <Td style={{ maxWidth: 140, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap", fontSize: 12 }}>
                  {ch.models || "—"}
                </Td>
                <Td>{ch.balance != null ? ch.balance.toFixed(2) : "—"}</Td>
                <Td>{ch.used_quota != null ? q2c(ch.used_quota) : "—"}</Td>
                <Td>{ch.response_time != null ? `${ch.response_time}ms` : "—"}</Td>
                <Td>
                  <div style={{ display: "flex", gap: 6 }}>
                    <Btn variant="ghost" onClick={() => testChannel(ch.id)} style={{ fontSize: 12 }}>▶</Btn>
                    <Btn variant="ghost" onClick={() => openEdit(ch)} style={{ fontSize: 12 }}>✏</Btn>
                    <Btn variant="ghost" onClick={() => setDeleteTarget(ch)} style={{ fontSize: 12, color: "#ef4444" }}>🗑</Btn>
                  </div>
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {showModal && (
        <Modal title={isEdit ? "编辑渠道" : "添加渠道"} onClose={() => setShowModal(false)}>
          <Field label="渠道名称">
            <input style={inputStyle} value={editChannel.name ?? ""} onChange={e => setEditChannel(p => ({ ...p, name: e.target.value }))} />
          </Field>
          <Field label="类型">
            <select style={inputStyle} value={editChannel.type ?? 1} onChange={e => setEditChannel(p => ({ ...p, type: +e.target.value }))}>
              {TYPE_OPTIONS.map(t => <option key={t} value={t}>{CHANNEL_TYPE_MAP[t] ?? `Type ${t}`}</option>)}
            </select>
          </Field>
          <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12 }}>
            <Field label="优先级">
              <input style={inputStyle} type="number" value={editChannel.priority ?? 100} onChange={e => setEditChannel(p => ({ ...p, priority: +e.target.value }))} />
            </Field>
            <Field label="权重">
              <input style={inputStyle} type="number" value={editChannel.weight ?? 1} onChange={e => setEditChannel(p => ({ ...p, weight: +e.target.value }))} />
            </Field>
          </div>
          <Field label="API Key">
            <input style={inputStyle} type="password" value={editChannel.key ?? ""} onChange={e => setEditChannel(p => ({ ...p, key: e.target.value }))} />
          </Field>
          <Field label="模型 (逗号分隔)">
            <textarea style={{ ...inputStyle, minHeight: 64, resize: "vertical" }} value={editChannel.models ?? ""} onChange={e => setEditChannel(p => ({ ...p, models: e.target.value }))} />
          </Field>
          <Field label="状态">
            <select style={inputStyle} value={editChannel.status ?? 1} onChange={e => setEditChannel(p => ({ ...p, status: +e.target.value }))}>
              <option value={1}>启用</option>
              <option value={0}>禁用</option>
            </select>
          </Field>
          {editChannel.type === 45 && (
            <>
              <Field label="中转地址">
                <input style={inputStyle} type="url" value={editChannel.base_url ?? ""} onChange={e => setEditChannel(p => ({ ...p, base_url: e.target.value }))} />
              </Field>
              <Field label="Sub2API Token">
                <input style={inputStyle} type="password" value={editChannel.other ?? ""} onChange={e => setEditChannel(p => ({ ...p, other: e.target.value }))} />
              </Field>
            </>
          )}
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", marginTop: 8 }}>
            <Btn onClick={() => setShowModal(false)}>取消</Btn>
            <Btn variant="primary" onClick={saveChannel}>保存</Btn>
          </div>
        </Modal>
      )}

      {deleteTarget && (
        <Modal title="确认删除" onClose={() => setDeleteTarget(null)}>
          <p style={{ color: T.ink, fontSize: 14 }}>
            确认删除渠道 <strong>{deleteTarget.name}</strong>？此操作不可撤销，所有使用该渠道的请求将失败。
          </p>
          <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
            <Btn onClick={() => setDeleteTarget(null)}>取消</Btn>
            <Btn variant="danger" onClick={doDelete}>确认删除</Btn>
          </div>
        </Modal>
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
  const [loading, setLoading] = useState(true);
  const [editQuota, setEditQuota] = useState<Record<number, string>>({});

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

  const toggleStatus = (u: User) => {
    const newStatus = u.status === 1 ? 0 : 1;
    axios.put(`/gtk/v1/admin/users/${u.id}/status`, { status: newStatus })
      .then(() => { toast.success("状态已更新"); load(page, keyword); })
      .catch(() => toast.error("操作失败"));
  };

  const saveQuota = (u: User) => {
    const val = editQuota[u.id];
    if (val == null) return;
    const newCredits = parseInt(val, 10);
    if (isNaN(newCredits)) return;
    const oldCredits = q2c(u.quota);
    const delta = c2q(newCredits - oldCredits);
    axios.put(`/gtk/v1/admin/users/${u.id}/quota`, { quota_delta: delta })
      .then(() => { toast.success("配额已更新"); load(page, keyword); })
      .catch(() => toast.error("更新失败"))
      .finally(() => setEditQuota(p => { const n = { ...p }; delete n[u.id]; return n; }));
  };

  const totalPages = Math.ceil(total / pageSize);

  return (
    <div>
      <div style={{ display: "flex", gap: 12, marginBottom: 16, alignItems: "center" }}>
        <input style={{ ...inputStyle, width: 220 }} placeholder="搜索用户名 / 邮箱…" value={keyword}
          onChange={e => { setKeyword(e.target.value); setPage(1); }} />
        <span style={{ fontSize: 12, color: T.muted }}>共 {total} 用户</span>
        <div style={{ marginLeft: "auto", background: T.card2, padding: "6px 12px", borderRadius: 8, fontSize: 12, color: T.muted, border: `1px solid ${T.line}` }}>
          1 credit ≈ 500 quota
        </div>
      </div>

      <Card style={{ padding: 0, overflowX: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr>
              <Th>ID</Th><Th>用户名</Th><Th>邮箱</Th><Th>角色</Th><Th>状态</Th>
              <Th>剩余配额</Th><Th>已用配额</Th><Th>注册时间</Th><Th>操作</Th>
            </tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={9} style={{ padding: 20, textAlign: "center", color: T.muted }}>加载中…</td></tr>}
            {!loading && users.length === 0 && <tr><td colSpan={9} style={{ padding: 20, textAlign: "center", color: T.muted }}>暂无用户</td></tr>}
            {users.map(u => (
              <tr key={u.id} style={{ borderBottom: `1px solid ${T.line}` }}>
                <Td><span style={{ fontFamily: T.mono, fontSize: 12 }}>{u.id}</span></Td>
                <Td style={{ fontWeight: 500 }}>{u.username}</Td>
                <Td style={{ fontSize: 12, color: T.muted }}>{u.email ?? "—"}</Td>
                <Td><Chip label={u.role >= 100 ? "管理员" : "普通"} color={u.role >= 100 ? "#dcfce7" : undefined} /></Td>
                <Td>
                  <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
                    <Led status={u.status} />
                    <span style={{ fontSize: 12 }}>{u.status === 1 ? "正常" : "禁用"}</span>
                  </div>
                </Td>
                <Td>
                  {editQuota[u.id] != null ? (
                    <input type="number" autoFocus
                      style={{ ...inputStyle, width: 80, padding: "4px 6px", fontSize: 12 }}
                      value={editQuota[u.id]}
                      onChange={e => setEditQuota(p => ({ ...p, [u.id]: e.target.value }))}
                      onBlur={() => saveQuota(u)}
                      onKeyDown={e => { if (e.key === "Enter") saveQuota(u); }}
                    />
                  ) : (
                    <span style={{ cursor: "pointer", textDecoration: "underline dotted", fontFamily: T.mono, fontSize: 12 }}
                      onClick={() => setEditQuota(p => ({ ...p, [u.id]: String(q2c(u.quota)) }))}>
                      {q2c(u.quota).toLocaleString()}
                    </span>
                  )}
                </Td>
                <Td><span style={{ fontFamily: T.mono, fontSize: 12 }}>{q2c(u.used_quota).toLocaleString()}</span></Td>
                <Td style={{ fontSize: 12, color: T.muted }}>
                  {u.created_time ? new Date(u.created_time * 1000).toLocaleDateString() : "—"}
                </Td>
                <Td>
                  <Btn variant="ghost" onClick={() => toggleStatus(u)} style={{ fontSize: 12 }}>
                    {u.status === 1 ? "禁用" : "启用"}
                  </Btn>
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>

      {totalPages > 1 && (
        <div style={{ display: "flex", gap: 8, marginTop: 16, justifyContent: "center" }}>
          <Btn disabled={page <= 1} onClick={() => setPage(p => p - 1)}>上一页</Btn>
          <span style={{ alignSelf: "center", fontSize: 13, color: T.muted }}>{page} / {totalPages}</span>
          <Btn disabled={page >= totalPages} onClick={() => setPage(p => p + 1)}>下一页</Btn>
        </div>
      )}
    </div>
  );
}

// ── TAB: Model Ratios ──────────────────────────────────────────────────────
interface RatioRow { model: string; ratio: number }

function tierLabel(r: number) {
  return r >= 10 ? "Premium" : r >= 1 ? "Standard" : "Light";
}
function tierColor(r: number) {
  return r >= 10 ? "#fef3c7" : r >= 1 ? "#dbeafe" : "#f0fdf4";
}

function RatiosTab() {
  const [rows, setRows] = useState<RatioRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [dirty, setDirty] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    axios.get<{ success: boolean; data?: Record<string, number> }>("/gtk/v1/admin/model-ratios")
      .then(r => {
        if (r.data.success && r.data.data) {
          setRows(Object.entries(r.data.data).map(([model, ratio]) => ({ model, ratio })));
        }
      }).catch(() => toast.error("加载失败"))
      .finally(() => setLoading(false));
  }, []);

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
      <div style={{ background: T.card2, border: `1px solid ${T.line}`, borderRadius: 8, padding: "10px 14px", fontSize: 12, color: T.fg2, marginBottom: 16 }}>
        公式：1 credit = ¥0.02 · Ratio = N → N credits/1M tokens = ¥{"{N×0.02}"}/1M tokens
      </div>

      <div style={{ display: "flex", justifyContent: "flex-end", gap: 8, marginBottom: 12 }}>
        <Btn onClick={addRow}>+ 新增行</Btn>
        <Btn variant="primary" disabled={!dirty || saving} onClick={save}>{saving ? "保存中…" : "全部保存"}</Btn>
      </div>

      <Card style={{ padding: 0, overflowX: "auto" }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr><Th>模型名称</Th><Th>Ratio</Th><Th>等价 ¥/1M tokens</Th><Th>档位</Th><Th></Th></tr>
          </thead>
          <tbody>
            {loading && <tr><td colSpan={5} style={{ padding: 20, textAlign: "center", color: T.muted }}>加载中…</td></tr>}
            {rows.map((r, i) => (
              <tr key={i} style={{ borderBottom: `1px solid ${T.line}` }}>
                <Td>
                  <input style={{ ...inputStyle, width: "100%" }} value={r.model}
                    onChange={e => update(i, "model", e.target.value)} />
                </Td>
                <Td>
                  <input type="number" step="0.001" style={{ ...inputStyle, width: 90, fontFamily: T.mono }}
                    value={r.ratio} onChange={e => update(i, "ratio", parseFloat(e.target.value) || 0)} />
                </Td>
                <Td><span style={{ fontFamily: T.mono, fontSize: 12 }}>¥{(r.ratio * 0.02).toFixed(4)}</span></Td>
                <Td><Chip label={tierLabel(r.ratio)} color={tierColor(r.ratio)} /></Td>
                <Td>
                  <Btn variant="ghost" onClick={() => deleteRow(i)} style={{ fontSize: 12, color: "#ef4444" }}>🗑</Btn>
                </Td>
              </tr>
            ))}
          </tbody>
        </table>
      </Card>
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
    axios.get<{ success: boolean; data?: Record<string, string> }>("/gtk/v1/admin/system-options")
      .then(r => { if (r.data.success && r.data.data) setOpts(r.data.data); })
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

  const Toggle = ({ k, label }: { k: string; label: string }) => (
    <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", padding: "10px 0", borderBottom: `1px solid ${T.line}` }}>
      <span style={{ fontSize: 13 }}>{label}</span>
      <button onClick={() => setOpts(p => ({ ...p, [k]: p[k] === "true" ? "false" : "true" }))}
        style={{
          width: 40, height: 22, borderRadius: 11, border: "none", cursor: "pointer",
          background: opts[k] === "true" ? T.accent : T.muted, transition: "background 0.2s",
          position: "relative",
        }}>
        <span style={{
          position: "absolute", top: 3, left: opts[k] === "true" ? 20 : 3,
          width: 16, height: 16, borderRadius: "50%", background: "#fff", transition: "left 0.2s",
        }} />
      </button>
    </div>
  );

  const otherKeys = Object.keys(opts).filter(k => ![...AUTH_KEYS, ...RATE_KEYS].includes(k));

  if (loading) return <div style={{ color: T.muted, padding: 20 }}>加载中…</div>;

  return (
    <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 20 }}>
      <div>
        <Card style={{ marginBottom: 20 }}>
          <div style={{ fontFamily: T.display, fontSize: 15, marginBottom: 12 }}>登录 & 注册</div>
          <Toggle k="PasswordLoginEnabled" label="允许密码登录" />
          <Toggle k="RegisterEnabled" label="开放注册" />
          <Toggle k="EmailVerification" label="邮箱验证" />
          <div style={{ marginTop: 12, textAlign: "right" }}>
            <Btn variant="primary" onClick={() => saveGroup(AUTH_KEYS)}>保存</Btn>
          </div>
        </Card>

        <Card>
          <div style={{ fontFamily: T.display, fontSize: 15, marginBottom: 12 }}>全局限速</div>
          <Field label="速率限制次数">
            <input type="number" style={inputStyle} value={opts["GlobalApiRateLimitNum"] ?? ""}
              onChange={e => setOpts(p => ({ ...p, GlobalApiRateLimitNum: e.target.value }))} />
          </Field>
          <Field label="时间窗口 (秒)">
            <input type="number" style={inputStyle} value={opts["GlobalApiRateLimitDuration"] ?? ""}
              onChange={e => setOpts(p => ({ ...p, GlobalApiRateLimitDuration: e.target.value }))} />
          </Field>
          <div style={{ textAlign: "right" }}>
            <Btn variant="primary" onClick={() => saveGroup(RATE_KEYS)}>保存</Btn>
          </div>
        </Card>
      </div>

      <Card>
        <div style={{ fontFamily: T.display, fontSize: 15, marginBottom: 12 }}>其他配置</div>
        {otherKeys.length === 0 && <div style={{ color: T.muted, fontSize: 13 }}>暂无其他配置项</div>}
        {otherKeys.map(k => (
          <div key={k} style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 8 }}>
            <span style={{ fontSize: 12, fontFamily: T.mono, color: T.muted, minWidth: 160, flexShrink: 0 }}>{k}</span>
            <input style={{ ...inputStyle, flex: 1, fontSize: 12 }} value={opts[k]}
              onChange={e => setOpts(p => ({ ...p, [k]: e.target.value }))}
              onBlur={() => put(k, opts[k])} />
          </div>
        ))}
      </Card>
    </div>
  );
}

// ── Sidebar icons ──────────────────────────────────────────────────────────
const IconHub = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/>
    <rect x="14" y="14" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/>
  </svg>
);
const IconChannels = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <rect x="2" y="3" width="20" height="4" rx="1"/>
    <rect x="2" y="10" width="20" height="4" rx="1"/>
    <rect x="2" y="17" width="20" height="4" rx="1"/>
    <circle cx="6" cy="5" r="1" fill="currentColor"/>
    <circle cx="6" cy="12" r="1" fill="currentColor"/>
    <circle cx="6" cy="19" r="1" fill="currentColor"/>
  </svg>
);
const IconUsers = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/>
    <circle cx="9" cy="7" r="4"/>
    <path d="M23 21v-2a4 4 0 0 0-3-3.87"/>
    <path d="M16 3.13a4 4 0 0 1 0 7.75"/>
  </svg>
);
const IconRatios = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <circle cx="12" cy="12" r="10"/>
    <path d="M12 6v6l4 2"/>
    <line x1="8" y1="14" x2="16" y2="14"/>
  </svg>
);
const IconSettings = () => (
  <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <circle cx="12" cy="12" r="3"/>
    <path d="M12 1v4M12 19v4M4.22 4.22l2.83 2.83M16.95 16.95l2.83 2.83M1 12h4M19 12h4M4.22 19.78l2.83-2.83M16.95 7.05l2.83-2.83"/>
  </svg>
);

const NAV: { id: Tab; label: string; Icon: React.FC }[] = [
  { id: "hub", label: "仪表盘", Icon: IconHub },
  { id: "channels", label: "渠道管理", Icon: IconChannels },
  { id: "users", label: "用户管理", Icon: IconUsers },
  { id: "ratios", label: "模型计费", Icon: IconRatios },
  { id: "settings", label: "系统设置", Icon: IconSettings },
];

const TAB_TITLES: Record<Tab, string> = {
  hub: "仪表盘",
  channels: "渠道管理",
  users: "用户管理",
  ratios: "模型计费",
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
      <div style={{ display: "flex", alignItems: "center", justifyContent: "center", minHeight: "100vh", background: T.bg, fontFamily: T.body }}>
        <Card style={{ textAlign: "center", padding: 48 }}>
          <div style={{ fontFamily: T.display, fontSize: 24, color: T.ink, marginBottom: 8 }}>权限不足</div>
          <div style={{ color: T.muted, fontSize: 14 }}>需要管理员权限才能访问此页面</div>
        </Card>
      </div>
    );
  }

  return (
    <div style={{ display: "flex", height: "100vh", fontFamily: T.body, background: T.bg, color: T.ink, overflow: "hidden" }}>
      {/* Sidebar */}
      <aside style={{ width: 200, flexShrink: 0, background: T.card, borderRight: `1px solid ${T.line}`, display: "flex", flexDirection: "column" }}>
        <div style={{ padding: "24px 20px 20px", borderBottom: `1px solid ${T.line}` }}>
          <div style={{ fontFamily: T.display, fontSize: 16, fontWeight: 700, color: T.ink }}>GTK 管理</div>
          <div style={{ fontSize: 11, color: T.muted, marginTop: 2 }}>greentokey admin</div>
        </div>
        <nav style={{ flex: 1, padding: "12px 8px" }}>
          {NAV.map(({ id, label, Icon }) => {
            const active = tab === id;
            return (
              <button key={id} onClick={() => handleSetTab(id)} style={{
                display: "flex", alignItems: "center", gap: 10, width: "100%",
                padding: "10px 12px", marginBottom: 2, border: "none", borderRadius: 8,
                background: active ? T.accent : "transparent",
                color: active ? "#fff" : T.ink,
                fontSize: 13, fontFamily: T.body, cursor: "pointer", textAlign: "left",
                transition: "background 0.15s",
              }}
                onMouseEnter={e => { if (!active) e.currentTarget.style.background = "rgba(0,0,0,0.05)"; }}
                onMouseLeave={e => { if (!active) e.currentTarget.style.background = "transparent"; }}
              >
                <Icon />
                <span>{label}</span>
              </button>
            );
          })}
        </nav>
      </aside>

      {/* Main area */}
      <div style={{ flex: 1, display: "flex", flexDirection: "column", overflow: "hidden" }}>
        {/* Topbar */}
        <header style={{
          height: 56, flexShrink: 0, display: "flex", alignItems: "center",
          padding: "0 28px", borderBottom: `1px solid ${T.line}`, background: T.card,
          justifyContent: "space-between",
        }}>
          <h1 style={{ fontFamily: T.display, fontSize: 18, fontWeight: 700, margin: 0 }}>{TAB_TITLES[tab]}</h1>
          <div style={{ fontSize: 12, color: T.muted, fontFamily: T.mono }}>greentokey · admin</div>
        </header>

        {/* Page content */}
        <main style={{ flex: 1, overflowY: "auto", padding: 28 }}>
          {tab === "hub" && <HubTab setTab={handleSetTab} openAddChannel={openAddChannel} />}
          {tab === "channels" && <ChannelsTab initialOpenModal={triggerAddChannel} />}
          {tab === "users" && <UsersTab />}
          {tab === "ratios" && <RatiosTab />}
          {tab === "settings" && <SettingsTab />}
        </main>
      </div>
    </div>
  );
}

export default GtkAdmin;
