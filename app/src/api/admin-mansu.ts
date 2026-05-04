import axios, { AxiosError } from "axios";

/**
 * /admin/mansu API client — founder concierge workspace.
 *
 * Endpoints (server may not all exist at v0.12 ship; demo fallback in
 * the route handles 404 gracefully):
 *   GET  /api/gtk/v1/admin/leads                — list with optional status filter
 *   PATCH /api/gtk/v1/admin/leads/:id/status    — move a lead between stages
 *   GET  /api/gtk/v1/admin/service-orders      — list orders linked to leads
 */

export type LeadStatus = "new" | "contacted" | "signed" | "running" | "done" | "lost";

export type Lead = {
  id: number;
  wechat: string;
  phone: string;
  homestay_name: string;
  homestay_loc: string;
  notes: string;
  source: string;
  status: LeadStatus;
  created_at: string;
  updated_at: string;
  // Pull-throughs for ops convenience
  active_order_no?: string | null;
};

export type ListLeadsResult =
  | { ok: true; leads: Lead[] }
  | { ok: false; kind: "not-found" | "unauthorized" | "network" | "server" | "unknown"; message: string };

export async function listLeads(filter?: { status?: LeadStatus }): Promise<ListLeadsResult> {
  try {
    const resp = await axios.get<{ success: boolean; data?: Lead[]; message?: string }>(
      "/api/gtk/v1/admin/leads",
      { params: filter, timeout: 8000 },
    );
    if (resp.data.success && resp.data.data) {
      return { ok: true, leads: resp.data.data };
    }
    return { ok: false, kind: "server", message: resp.data.message || "未知错误" };
  } catch (err) {
    return mapErr(err);
  }
}

export async function patchLeadStatus(
  id: number,
  status: LeadStatus,
): Promise<{ ok: true } | { ok: false; message: string }> {
  try {
    const resp = await axios.patch<{ success: boolean; message?: string }>(
      `/api/gtk/v1/admin/leads/${id}/status`,
      { status },
    );
    if (resp.data.success) return { ok: true };
    return { ok: false, message: resp.data.message || "更新失败" };
  } catch (err) {
    if (axios.isAxiosError(err)) {
      return { ok: false, message: (err as AxiosError<{ message?: string }>).response?.data?.message || err.message };
    }
    return { ok: false, message: String(err) };
  }
}

// v0.16 — concierge order creation off the kanban.

export type ServiceCatalogEntry = {
  slug: string;
  name: string;
  description?: string;
  category: string;
  price_cny_cents: number;
  price_display_cny: string;
  included_credits: number;
  billing_type: string;
  display_order: number;
};

export async function fetchActiveServices(): Promise<ServiceCatalogEntry[]> {
  // Same public endpoint Services.tsx uses. Public catalog is fine for
  // admin too — we just need the slug + name + price.
  try {
    const resp = await axios.get<{
      success: boolean;
      data?: { services?: ServiceCatalogEntry[] };
    }>("/gtk/v1/services");
    if (resp.data.success && resp.data.data?.services) {
      return resp.data.data.services;
    }
    return [];
  } catch {
    return [];
  }
}

export type ConciergeOrderResult = {
  ok: true;
  order_no: string;
  access_token: string;
  runner_url: string; // path-only; caller prefixes origin
  service_name: string;
  price_display: string;
} | {
  ok: false;
  message: string;
};

export async function createConciergeOrder(
  leadId: number,
  serviceSlug: string,
  notes: string,
): Promise<ConciergeOrderResult> {
  try {
    const resp = await axios.post<{
      success: boolean;
      data?: {
        order_no: string;
        access_token: string;
        runner_url: string;
        service_name: string;
        price_display: string;
      };
      message?: string;
    }>(
      `/api/gtk/v1/admin/leads/${leadId}/create-order`,
      { service_slug: serviceSlug, notes },
    );
    if (resp.data.success && resp.data.data) {
      return { ok: true, ...resp.data.data };
    }
    return { ok: false, message: resp.data.message || "创建失败" };
  } catch (err) {
    if (axios.isAxiosError(err)) {
      return {
        ok: false,
        message:
          (err as AxiosError<{ message?: string }>).response?.data?.message ||
          err.message,
      };
    }
    return { ok: false, message: String(err) };
  }
}

function mapErr(err: unknown): ListLeadsResult {
  if (axios.isAxiosError(err)) {
    const ax = err as AxiosError<{ message?: string }>;
    if (!ax.response) return { ok: false, kind: "network", message: "网络无法到达服务器" };
    const status = ax.response.status;
    const msg = ax.response.data?.message || ax.message;
    if (status === 404) return { ok: false, kind: "not-found", message: "endpoint 未上线" };
    if (status === 401 || status === 403) return { ok: false, kind: "unauthorized", message: "需要管理员权限" };
    if (status >= 500) return { ok: false, kind: "server", message: msg };
    return { ok: false, kind: "unknown", message: msg };
  }
  return { ok: false, kind: "unknown", message: String(err) };
}

// Demo data shown when the backend endpoint isn't deployed yet, so the
// founder can sanity-check the UI immediately after merge. Returns the
// kind of lead distribution that matches the 大理 wedge: 5-10 prospects
// at various stages.
export function makeDemoLeads(): Lead[] {
  const now = new Date().toISOString();
  const days = (d: number) =>
    new Date(Date.now() - d * 24 * 60 * 60 * 1000).toISOString();
  return [
    {
      id: 101,
      wechat: "li_dali_2024",
      phone: "",
      homestay_name: "洱海星空小院",
      homestay_loc: "大理 · 双廊",
      notes: "看到朋友圈推荐,周末入住率不到 50%",
      source: "home",
      status: "new",
      created_at: days(0),
      updated_at: days(0),
    },
    {
      id: 102,
      wechat: "",
      phone: "138****6912",
      homestay_name: "苍山月台",
      homestay_loc: "大理 · 喜洲",
      notes: "希望提升淡季入住,自己拍照不会写文案",
      source: "pricing",
      status: "new",
      created_at: days(1),
      updated_at: days(1),
    },
    {
      id: 103,
      wechat: "wang_xizhou",
      phone: "139****2811",
      homestay_name: "海街 1872",
      homestay_loc: "大理 · 海西",
      notes: "约定本周三晚上微信聊",
      source: "contact-page",
      status: "contacted",
      created_at: days(2),
      updated_at: days(0),
    },
    {
      id: 104,
      wechat: "ergeng_inn",
      phone: "",
      homestay_name: "二更洱海",
      homestay_loc: "大理 · 龙龛",
      notes: "已签约 ¥1980,首批 4 条内容,先排周末",
      source: "home",
      status: "signed",
      created_at: days(5),
      updated_at: days(1),
      active_order_no: "GTK-DEMO-001",
    },
    {
      id: 105,
      wechat: "yu_caicun",
      phone: "",
      homestay_name: "彩村洱光",
      homestay_loc: "大理 · 才村",
      notes: "已生成 2 条,客户在审核",
      source: "home",
      status: "running",
      created_at: days(9),
      updated_at: now,
      active_order_no: "GTK-DEMO-002",
    },
    {
      id: 106,
      wechat: "",
      phone: "187****0033",
      homestay_name: "云岚客栈",
      homestay_loc: "大理 · 古城",
      notes: "首月转化 +3 单,续费已沟通",
      source: "pricing",
      status: "done",
      created_at: days(40),
      updated_at: days(2),
    },
  ];
}
