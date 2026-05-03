import axios, { AxiosError } from "axios";

/**
 * Service-order API helper for /services/run/:order_no.
 *
 * Talks to /api/gtk/v1/service-order/:order_no:
 *   GET  → fetch order (service info + status + result)
 *   POST → submit input + kick off agent run (returns 202 + new status)
 *
 * Server states:
 *   pending  — paid but not started
 *   running  — agent picked up, in progress
 *   complete — done; `result` populated
 *   failed   — agent error; `error` populated, retryable
 *
 * Backend endpoints are NOT YET WIRED at v0.12 ship. The page handles
 * 404 gracefully so /services/run/:order_no is reachable for screenshot
 * + demo purposes ahead of the runtime cutover. Founder may fill the
 * server side with /admin/mansu (block 5) concierge tooling first.
 */

export type ServiceOrderStatus = "pending" | "running" | "complete" | "failed";

export type ServiceOrder = {
  order_no: string;
  service_id: number;
  service_name: string;
  service_kind: "diy-agent" | "concierge"; // self-serve vs human-in-loop
  status: ServiceOrderStatus;
  inputs: Record<string, unknown> | null;
  result: ServiceResult | null;
  error: string | null;
  created_at: string;
  updated_at: string;
};

export type ServiceResult =
  | { kind: "text"; content: string }
  | { kind: "image"; url: string; alt?: string }
  | { kind: "multi"; items: ServiceResult[] }
  | { kind: "rednote-post"; title: string; body: string; tags: string[]; images: string[] };

export type FetchResult =
  | { ok: true; order: ServiceOrder }
  | { ok: false; kind: FetchErrorKind; message: string };

export type FetchErrorKind = "not-found" | "unauthorized" | "network" | "server" | "unknown";

export async function fetchServiceOrder(orderNo: string): Promise<FetchResult> {
  try {
    const resp = await axios.get<{ success: boolean; data?: ServiceOrder; message?: string }>(
      `/api/gtk/v1/service-order/${encodeURIComponent(orderNo)}`,
      { timeout: 8000 },
    );
    if (resp.data.success && resp.data.data) {
      return { ok: true, order: resp.data.data };
    }
    return { ok: false, kind: "server", message: resp.data.message || "未知服务器错误" };
  } catch (err) {
    return mapAxiosError(err);
  }
}

export type SubmitInput = {
  fields: Record<string, string>;
  files?: { name: string; data: File }[];
};

export type SubmitResult =
  | { ok: true; order: ServiceOrder }
  | { ok: false; kind: FetchErrorKind | "validation"; message: string };

export async function submitServiceRun(
  orderNo: string,
  input: SubmitInput,
): Promise<SubmitResult> {
  try {
    const form = new FormData();
    for (const [k, v] of Object.entries(input.fields)) {
      form.append(k, v);
    }
    if (input.files) {
      for (const f of input.files) {
        form.append(f.name, f.data);
      }
    }
    const resp = await axios.post<{ success: boolean; data?: ServiceOrder; message?: string }>(
      `/api/gtk/v1/service-order/${encodeURIComponent(orderNo)}/run`,
      form,
      { timeout: 30000 },
    );
    if (resp.data.success && resp.data.data) {
      return { ok: true, order: resp.data.data };
    }
    return { ok: false, kind: "server", message: resp.data.message || "提交失败" };
  } catch (err) {
    return mapAxiosError(err) as SubmitResult;
  }
}

function mapAxiosError(err: unknown): { ok: false; kind: FetchErrorKind; message: string } {
  if (axios.isAxiosError(err)) {
    const ax = err as AxiosError<{ message?: string }>;
    if (!ax.response) {
      return { ok: false, kind: "network", message: "网络无法到达服务器" };
    }
    const status = ax.response.status;
    const msg = ax.response.data?.message || ax.message;
    if (status === 404) return { ok: false, kind: "not-found", message: "订单不存在或链接已失效" };
    if (status === 401 || status === 403) return { ok: false, kind: "unauthorized", message: "请先登录或确认订单归属" };
    if (status >= 500) return { ok: false, kind: "server", message: msg };
    return { ok: false, kind: "unknown", message: msg };
  }
  return { ok: false, kind: "unknown", message: String(err) };
}
