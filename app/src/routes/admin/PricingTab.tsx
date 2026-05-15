// PricingTab.tsx — PKG-ADMIN-L2-1 "套餐定价" tab
// Three sections: Plan CRUD / Billing-config knob / Provider-pricing timeline.
// Structurally mirrors RatiosTab in GtkAdmin.tsx (inline editable table +
// bulk save pattern).

import { useState, useEffect, useCallback } from "react";
import axios from "axios";
import { toast } from "sonner";

// ── Types ──────────────────────────────────────────────────────────────────

interface PlanRow {
  id: number;
  code: string;
  name: string;
  type: string;
  product_type: string;
  billing_mode: string;
  price_cents: number;
  duration_days: number;
  quota_grant?: number;
  is_active: boolean;
  created_at: string;
}

interface ProviderPricingRow {
  id: number;
  provider: string;
  model_id: string;
  token_type: string;
  upstream_per_m: number;
  effective_from: string;
  notes?: string;
  // PKG-PRICING-DYNAMIC display fields (nullable — NULL = not published)
  display_in_cny_per_m?: number | null;
  display_out_cny_per_m?: number | null;
  display_credits_per_m?: number | null;
  display_name?: string | null;
  vendor_label?: string | null;
  context_size?: string | null;
  cache_flag?: string | null;
}

// ── Small SVG icons (inline, same style as GtkAdmin.tsx) ──────────────────

const IconSpinner = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2"
    className="gtk-spin" style={{ width: 14, height: 14 }}>
    <path d="M12 2v4M12 18v4M4.93 4.93l2.83 2.83M16.24 16.24l2.83 2.83M2 12h4M18 12h4M4.93 19.07l2.83-2.83M16.24 7.76l2.83-2.83"/>
  </svg>
);

const IconPlus = () => (
  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
    <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
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

// ── TOKEN_TYPES whitelist ──────────────────────────────────────────────────

const TOKEN_TYPES = ["input", "output", "cache_write_5m", "cache_write_1h", "cache_read"] as const;
type TokenType = typeof TOKEN_TYPES[number];

// ── Section 1: Plans ──────────────────────────────────────────────────────

interface DirtyPlan extends PlanRow {
  _dirty?: boolean;
  _new?: boolean;
}

function PlansSection() {
  const [plans, setPlans] = useState<DirtyPlan[]>([]);
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [showAddForm, setShowAddForm] = useState(false);
  const [newPlan, setNewPlan] = useState({
    code: "", name: "", price_cents: 0, duration_days: 30,
    type: "subscription", product_type: "token", billing_mode: "subscription",
  });

  const load = useCallback(() => {
    setLoading(true);
    axios.get<{ success: boolean; data?: { plans: PlanRow[]; total: number } }>(
      "/gtk/v1/admin/plans?limit=200"
    ).then(r => {
      if (r.data.success && r.data.data) {
        setPlans(r.data.data.plans);
      }
    }).catch(() => toast.error("加载套餐失败"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  const updateField = (id: number, field: keyof PlanRow, val: string | number | boolean) => {
    setPlans(prev => prev.map(p =>
      p.id === id ? { ...p, [field]: val, _dirty: true } : p
    ));
  };

  const saveAll = () => {
    const dirty = plans.filter(p => p._dirty);
    if (dirty.length === 0) return;
    setSaving(true);
    Promise.all(dirty.map(p =>
      axios.put(`/gtk/v1/admin/plans/${p.id}`, {
        name: p.name,
        price_cents: p.price_cents,
        duration_days: p.duration_days,
        type: p.type,
        product_type: p.product_type,
        billing_mode: p.billing_mode,
        is_active: p.is_active,
      })
    )).then(() => {
      toast.success("已保存");
      setPlans(prev => prev.map(p => ({ ...p, _dirty: false })));
    }).catch(e => {
      toast.error(e?.response?.data?.message ?? "保存失败");
    }).finally(() => setSaving(false));
  };

  const retire = (plan: PlanRow) => {
    axios.delete(`/gtk/v1/admin/plans/${plan.id}`)
      .then(() => { toast.success(`套餐 "${plan.name}" 已停用`); load(); })
      .catch(e => {
        const msg: string = e?.response?.data?.message ?? "";
        if (msg.includes("active subscription")) {
          toast.error("该套餐有活跃订阅，无法停用");
        } else {
          toast.error("停用失败：" + msg);
        }
      });
  };

  const addPlan = () => {
    if (!newPlan.code.trim() || !newPlan.name.trim()) {
      toast.error("code 和名称不能为空");
      return;
    }
    if (newPlan.price_cents <= 0) { toast.error("价格必须 > 0"); return; }
    axios.post<{ success: boolean; data?: { plan: PlanRow } }>("/gtk/v1/admin/plans", newPlan)
      .then(r => {
        if (r.data.success && r.data.data) {
          toast.success("套餐已创建");
          setShowAddForm(false);
          setNewPlan({ code: "", name: "", price_cents: 0, duration_days: 30, type: "subscription", product_type: "token", billing_mode: "subscription" });
          load();
        }
      }).catch(e => {
        const msg: string = e?.response?.data?.message ?? "创建失败";
        toast.error(msg.includes("already exists") ? `code "${newPlan.code}" 已存在` : msg);
      });
  };

  const dirty = plans.some(p => p._dirty);

  return (
    <div className="pricing-section">
      <div className="tbl-head">
        <h4>套餐管理</h4>
        <div style={{ display: "flex", gap: 8 }}>
          <button className="btn btn-ghost btn-sm" onClick={load}>
            <IconRefresh /> 刷新
          </button>
          <button className="btn btn-ghost btn-sm" onClick={() => setShowAddForm(v => !v)}>
            <IconPlus /> 新增套餐
          </button>
          <button className="btn btn-primary btn-sm" disabled={!dirty || saving} onClick={saveAll}>
            {saving ? <IconSpinner /> : null}
            {saving ? "保存中…" : "保存修改"}
          </button>
        </div>
      </div>

      {showAddForm && (
        <div className="pricing-add-form">
          <div className="pricing-add-row">
            <input className="input" placeholder="code (唯一标识)" value={newPlan.code}
              onChange={e => setNewPlan(p => ({ ...p, code: e.target.value }))} />
            <input className="input" placeholder="套餐名称" value={newPlan.name}
              onChange={e => setNewPlan(p => ({ ...p, name: e.target.value }))} />
            <input className="input" type="number" placeholder="价格 (分)" value={newPlan.price_cents || ""}
              onChange={e => setNewPlan(p => ({ ...p, price_cents: parseInt(e.target.value) || 0 }))} />
            <input className="input" type="number" placeholder="有效天数" value={newPlan.duration_days}
              onChange={e => setNewPlan(p => ({ ...p, duration_days: parseInt(e.target.value) || 30 }))} />
            <select className="select-field" value={newPlan.product_type}
              onChange={e => setNewPlan(p => ({ ...p, product_type: e.target.value }))}>
              <option value="token">token</option>
              <option value="service">service</option>
            </select>
            <button className="btn btn-primary btn-sm" onClick={addPlan}>确认新增</button>
            <button className="btn btn-ghost btn-sm" onClick={() => setShowAddForm(false)}>取消</button>
          </div>
          <div style={{ fontSize: 11, color: "var(--fg-muted)", marginTop: 4 }}>
            价格单位：分（CNY）。例：¥19.80 = 1980 分
          </div>
        </div>
      )}

      <div className="tbl">
        <table>
          <thead>
            <tr>
              <th>ID</th>
              <th>Code</th>
              <th>名称</th>
              <th>类型</th>
              <th style={{ textAlign: "right" }}>价格 (¥)</th>
              <th style={{ textAlign: "right" }}>天数</th>
              <th>状态</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr><td colSpan={8} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>
                加载中…
              </td></tr>
            )}
            {!loading && plans.length === 0 && (
              <tr><td colSpan={8} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>
                暂无套餐
              </td></tr>
            )}
            {plans.map(p => (
              <tr key={p.id} style={p._dirty ? { background: "var(--warn-bg, #fffbe6)" } : undefined}>
                <td style={{ color: "var(--fg-muted)", fontSize: 12 }}>{p.id}</td>
                <td>
                  <span style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>{p.code}</span>
                </td>
                <td>
                  <input className="input" style={{ height: 30, fontSize: 13 }}
                    value={p.name}
                    onChange={e => updateField(p.id, "name", e.target.value)} />
                </td>
                <td>
                  <span style={{ fontSize: 12, color: "var(--fg-muted)" }}>
                    {p.product_type} / {p.billing_mode}
                  </span>
                </td>
                <td style={{ textAlign: "right" }}>
                  <input type="number" className="ratio-input"
                    value={(p.price_cents / 100).toFixed(2)}
                    onChange={e => updateField(p.id, "price_cents", Math.round(parseFloat(e.target.value) * 100) || 0)} />
                </td>
                <td style={{ textAlign: "right" }}>
                  <input type="number" className="ratio-input"
                    value={p.duration_days}
                    onChange={e => updateField(p.id, "duration_days", parseInt(e.target.value) || 1)} />
                </td>
                <td>
                  <span className={`status${p.is_active ? "" : " off"}`}>
                    <span className="dot" />
                    {p.is_active ? "启用" : "停用"}
                  </span>
                </td>
                <td style={{ textAlign: "right" }}>
                  {p.is_active && (
                    <button className="btn-sq danger" title="停用" onClick={() => retire(p)}>
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                        <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                      </svg>
                    </button>
                  )}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        <div className="tbl-foot">
          <span>{plans.length} 个套餐</span>
          {dirty && <span style={{ color: "var(--warn, #d97706)", marginLeft: 8 }}>有未保存的修改</span>}
        </div>
      </div>
    </div>
  );
}

// ── Section 2: Billing config ──────────────────────────────────────────────

function BillingConfigSection() {
  const [markup, setMarkup] = useState("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    axios.get<{ success: boolean; data?: { config: Record<string, string> } }>(
      "/gtk/v1/admin/billing-config"
    ).then(r => {
      if (r.data.success && r.data.data?.config) {
        setMarkup(r.data.data.config["markup_multiplier"] ?? "");
      }
    }).catch(() => toast.error("加载计费配置失败"))
      .finally(() => setLoading(false));
  }, []);

  const save = () => {
    const v = parseFloat(markup);
    if (isNaN(v) || v <= 0 || v >= 10) {
      toast.error("加价系数须在 (0, 10) 范围内");
      return;
    }
    setSaving(true);
    axios.put("/gtk/v1/admin/billing-config", { key: "markup_multiplier", value: markup })
      .then(() => toast.success("已保存"))
      .catch(e => toast.error(e?.response?.data?.message ?? "保存失败"))
      .finally(() => setSaving(false));
  };

  return (
    <div className="pricing-section">
      <div className="tbl-head">
        <h4>计费配置</h4>
      </div>
      <div className="ratio-banner">
        <div className="ic"><IconInfo /></div>
        <div>
          <b>markup_multiplier</b> 控制向用户收取的价格倍率。例：1.300 = 上游成本 × 1.3。
          写入后对下一次 <code>WriteUsageLog</code> 即时生效，无缓存。
        </div>
      </div>
      <div style={{ display: "flex", alignItems: "center", gap: 12, padding: "12px 18px" }}>
        <label style={{ fontSize: 13, fontWeight: 600, minWidth: 160 }}>加价系数 (markup_multiplier)</label>
        {loading ? (
          <span style={{ color: "var(--fg-muted)", fontSize: 13 }}>加载中…</span>
        ) : (
          <>
            <input
              type="number"
              step="0.01"
              min="0.01"
              max="9.99"
              className="input"
              style={{ width: 120, height: 34, fontSize: 14 }}
              value={markup}
              onChange={e => setMarkup(e.target.value)}
            />
            <button className="btn btn-primary btn-sm" disabled={saving} onClick={save}>
              {saving ? <IconSpinner /> : null}
              {saving ? "保存中…" : "保存"}
            </button>
          </>
        )}
      </div>
      <div style={{ padding: "0 18px 12px", fontSize: 12, color: "var(--fg-muted)", fontStyle: "italic" }}>
        改动实时生效 — 慎重大流量时段修改
      </div>
    </div>
  );
}

// ── Section 3: Provider pricing ────────────────────────────────────────────

type PricingFilter = "all" | "current" | "history";

function ProviderPricingSection() {
  const [rows, setRows] = useState<ProviderPricingRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [filter, setFilter] = useState<PricingFilter>("current");
  const [showForm, setShowForm] = useState(false);
  const [form, setForm] = useState({
    provider: "", model_id: "", token_type: "input" as TokenType,
    upstream_per_m: "", notes: "",
  });
  const [showDisplay, setShowDisplay] = useState(false);
  const [displayForm, setDisplayForm] = useState({
    display_name: "", vendor_label: "", context_size: "",
    display_in_cny_per_m: "", display_out_cny_per_m: "",
    display_credits_per_m: "", cache_flag: "true",
  });
  const [submitting, setSubmitting] = useState(false);

  const load = useCallback(() => {
    setLoading(true);
    axios.get<{ success: boolean; data?: { rows: ProviderPricingRow[] } }>(
      "/gtk/v1/admin/provider-pricing"
    ).then(r => {
      if (r.data.success && r.data.data) {
        setRows(r.data.data.rows);
      }
    }).catch(() => toast.error("加载供应商定价失败"))
      .finally(() => setLoading(false));
  }, []);

  useEffect(() => { load(); }, [load]);

  // Determine which rows are "current" (latest effective_from per provider+model+token_type tuple).
  const currentKeys = new Set<string>();
  // rows are already ordered effective_from DESC from backend.
  rows.forEach(r => {
    const k = `${r.provider}|${r.model_id}|${r.token_type}`;
    if (!currentKeys.has(k)) currentKeys.add(k);
  });
  // Build set of IDs that are current.
  const currentIDSet = new Set<number>();
  const seen = new Set<string>();
  rows.forEach(r => {
    const k = `${r.provider}|${r.model_id}|${r.token_type}`;
    if (!seen.has(k)) { seen.add(k); currentIDSet.add(r.id); }
  });

  const visible = rows.filter(r => {
    if (filter === "current") return currentIDSet.has(r.id);
    if (filter === "history") return !currentIDSet.has(r.id);
    return true;
  });

  const submit = () => {
    const v = parseFloat(form.upstream_per_m);
    if (!form.provider.trim() || !form.model_id.trim()) { toast.error("provider 和 model_id 不能为空"); return; }
    if (isNaN(v) || v <= 0) { toast.error("upstream_per_m 须 > 0"); return; }

    // All-or-nothing: if display toggle is ON, all 7 display fields must be non-empty.
    let displayPayload: Record<string, string | number> | null = null;
    if (showDisplay) {
      const { display_name, vendor_label, context_size,
              display_in_cny_per_m, display_out_cny_per_m,
              display_credits_per_m, cache_flag } = displayForm;
      if (!display_name.trim() || !vendor_label.trim() || !context_size.trim() ||
          !display_in_cny_per_m.trim() || !display_out_cny_per_m.trim() ||
          !display_credits_per_m.trim() || !cache_flag.trim()) {
        toast.error("勾选「公开」时，所有 7 个展示字段均为必填");
        return;
      }
      const inCNY = parseFloat(display_in_cny_per_m);
      const outCNY = parseFloat(display_out_cny_per_m);
      const credits = parseInt(display_credits_per_m, 10);
      if (isNaN(inCNY) || isNaN(outCNY) || isNaN(credits)) {
        toast.error("¥/1M in、¥/1M out、credits 须为有效数字");
        return;
      }
      displayPayload = {
        display_name: display_name.trim(),
        vendor_label: vendor_label.trim(),
        context_size: context_size.trim(),
        display_in_cny_per_m: inCNY,
        display_out_cny_per_m: outCNY,
        display_credits_per_m: credits,
        cache_flag: cache_flag.trim(),
      };
    }

    setSubmitting(true);
    const body = { ...form, upstream_per_m: v, ...(displayPayload ?? {}) };
    axios.post<{ success: boolean; data?: { row: ProviderPricingRow } }>(
      "/gtk/v1/admin/provider-pricing", body
    ).then(() => {
      toast.success("价格已记录");
      setShowForm(false);
      setShowDisplay(false);
      setForm({ provider: "", model_id: "", token_type: "input", upstream_per_m: "", notes: "" });
      setDisplayForm({ display_name: "", vendor_label: "", context_size: "",
        display_in_cny_per_m: "", display_out_cny_per_m: "",
        display_credits_per_m: "", cache_flag: "true" });
      load();
    }).catch(e => toast.error(e?.response?.data?.message ?? "提交失败"))
      .finally(() => setSubmitting(false));
  };

  const filterBtns: { val: PricingFilter; label: string }[] = [
    { val: "all", label: "全部" },
    { val: "current", label: "仅当前" },
    { val: "history", label: "仅历史" },
  ];

  return (
    <div className="pricing-section">
      <div className="tbl-head">
        <h4>供应商定价</h4>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          {filterBtns.map(b => (
            <button
              key={b.val}
              className={`btn btn-sm ${filter === b.val ? "btn-primary" : "btn-ghost"}`}
              onClick={() => setFilter(b.val)}
            >{b.label}</button>
          ))}
          <button className="btn btn-ghost btn-sm" onClick={load}><IconRefresh /> 刷新</button>
          <button className="btn btn-ghost btn-sm" onClick={() => setShowForm(v => !v)}>
            <IconPlus /> 记录新价格
          </button>
        </div>
      </div>

      {showForm && (
        <div className="pricing-add-form">
          <div className="pricing-add-row" style={{ flexWrap: "wrap" }}>
            <input className="input" placeholder="provider (e.g. anthropic)" value={form.provider}
              onChange={e => setForm(p => ({ ...p, provider: e.target.value }))} />
            <input className="input" placeholder="model_id" value={form.model_id}
              onChange={e => setForm(p => ({ ...p, model_id: e.target.value }))} />
            <select className="select-field" value={form.token_type}
              onChange={e => setForm(p => ({ ...p, token_type: e.target.value as TokenType }))}>
              {TOKEN_TYPES.map(t => <option key={t} value={t}>{t}</option>)}
            </select>
            <input className="input" type="number" step="0.000001" placeholder="upstream_per_m (USD)"
              value={form.upstream_per_m}
              onChange={e => setForm(p => ({ ...p, upstream_per_m: e.target.value }))} />
            <input className="input" placeholder="备注 (可选)" value={form.notes}
              onChange={e => setForm(p => ({ ...p, notes: e.target.value }))} />
            <button className="btn btn-primary btn-sm" disabled={submitting} onClick={submit}>
              {submitting ? <IconSpinner /> : null}
              {submitting ? "提交中…" : "确认记录"}
            </button>
            <button className="btn btn-ghost btn-sm" onClick={() => { setShowForm(false); setShowDisplay(false); }}>取消</button>
          </div>

          {/* Display toggle — all-or-nothing */}
          <div style={{ marginTop: 10, display: "flex", alignItems: "center", gap: 8 }}>
            <label style={{ display: "flex", alignItems: "center", gap: 6, fontSize: 13, cursor: "pointer" }}>
              <input type="checkbox" checked={showDisplay}
                onChange={e => setShowDisplay(e.target.checked)} />
              在公开 Pricing 页显示此模型
            </label>
            {showDisplay && (
              <span style={{ fontSize: 11, color: "var(--fg-muted)" }}>
                （7 个字段全部必填，any NULL = 不发布）
              </span>
            )}
          </div>

          {showDisplay && (
            <div className="pricing-add-row" style={{ flexWrap: "wrap", marginTop: 8 }}>
              <input className="input" placeholder="display_name (e.g. GPT-4o)" value={displayForm.display_name}
                onChange={e => setDisplayForm(p => ({ ...p, display_name: e.target.value }))} />
              <input className="input" placeholder="vendor_label (e.g. openai)" value={displayForm.vendor_label}
                onChange={e => setDisplayForm(p => ({ ...p, vendor_label: e.target.value }))} />
              <input className="input" placeholder="context_size (e.g. 128k)" value={displayForm.context_size}
                onChange={e => setDisplayForm(p => ({ ...p, context_size: e.target.value }))} />
              <input className="input" type="number" step="0.01" placeholder="¥/1M in" value={displayForm.display_in_cny_per_m}
                onChange={e => setDisplayForm(p => ({ ...p, display_in_cny_per_m: e.target.value }))} />
              <input className="input" type="number" step="0.01" placeholder="¥/1M out" value={displayForm.display_out_cny_per_m}
                onChange={e => setDisplayForm(p => ({ ...p, display_out_cny_per_m: e.target.value }))} />
              <input className="input" type="number" step="1" placeholder="credits/1M out" value={displayForm.display_credits_per_m}
                onChange={e => setDisplayForm(p => ({ ...p, display_credits_per_m: e.target.value }))} />
              <select className="select-field" value={displayForm.cache_flag}
                onChange={e => setDisplayForm(p => ({ ...p, cache_flag: e.target.value }))}>
                <option value="true">支持 (true)</option>
                <option value="cache_control">cache_control</option>
                <option value="false">不支持 (false)</option>
              </select>
            </div>
          )}

          <div style={{ fontSize: 11, color: "var(--fg-muted)", marginTop: 4 }}>
            append-only — 不修改已有行，每次涨价/降价插入新行。effective_from 自动设为当前时间。
          </div>
        </div>
      )}

      <div className="tbl">
        <table>
          <thead>
            <tr>
              <th>Provider</th>
              <th>Model ID</th>
              <th>Token Type</th>
              <th style={{ textAlign: "right" }}>USD/1M tokens</th>
              <th>生效时间</th>
              <th>备注</th>
              <th>公开</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr><td colSpan={7} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>
                加载中…
              </td></tr>
            )}
            {!loading && visible.length === 0 && (
              <tr><td colSpan={7} style={{ padding: 20, textAlign: "center", color: "var(--fg-muted)" }}>
                暂无记录
              </td></tr>
            )}
            {visible.map(r => {
              const isCurrent = currentIDSet.has(r.id);
              const isPublished = r.display_in_cny_per_m != null;
              return (
                <tr key={r.id} style={!isCurrent ? { opacity: 0.5 } : undefined}>
                  <td style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>{r.provider}</td>
                  <td style={{ fontFamily: "var(--font-mono)", fontSize: 12 }}>{r.model_id}</td>
                  <td>
                    <span style={{ fontSize: 11, padding: "2px 6px", borderRadius: 4,
                      background: "var(--card-2)", fontFamily: "var(--font-mono)" }}>
                      {r.token_type}
                    </span>
                  </td>
                  <td style={{ textAlign: "right", fontFamily: "var(--font-mono)", fontSize: 13 }}>
                    {r.upstream_per_m.toFixed(6)}
                    {!isCurrent && (
                      <span style={{ fontSize: 10, color: "var(--fg-muted)", marginLeft: 4 }}>(历史)</span>
                    )}
                  </td>
                  <td style={{ fontSize: 11, color: "var(--fg-muted)" }}>
                    {new Date(r.effective_from).toLocaleString("zh-CN")}
                  </td>
                  <td style={{ fontSize: 11, color: "var(--fg-muted)" }}>{r.notes ?? "—"}</td>
                  <td>
                    {isPublished ? (
                      <span style={{ fontSize: 10, padding: "2px 6px", borderRadius: 4,
                        background: "hsl(142 60% 88%)", color: "hsl(142 60% 25%)", fontWeight: 600 }}>
                        公开
                      </span>
                    ) : (
                      <span style={{ fontSize: 10, color: "var(--fg-muted)" }}>未公开</span>
                    )}
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
        <div className="tbl-foot">
          <span>{visible.length} 条记录{filter !== "all" ? `（共 ${rows.length} 条）` : ""}</span>
        </div>
      </div>
    </div>
  );
}

// ── Root PricingTab ────────────────────────────────────────────────────────

export function PricingTab() {
  return (
    <div className="pricing-tab">
      <PlansSection />
      <div style={{ height: 24 }} />
      <BillingConfigSection />
      <div style={{ height: 24 }} />
      <ProviderPricingSection />
    </div>
  );
}
