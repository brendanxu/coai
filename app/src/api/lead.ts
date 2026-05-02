import axios, { AxiosError } from "axios";

/**
 * Lead-capture API helper. POSTs to /api/gtk/v1/contact.
 *
 * Server validates:
 *   - At least one of (wechat, phone) is required
 *   - phone: 11-digit China mainland format
 *   - wechat: 6-20 alphanumeric+underscore (must start with letter)
 *   - notes: <=500 chars
 *
 * Idempotency: server upserts on (wechat OR phone). Re-submit updates
 * the existing row instead of creating a duplicate. This makes the
 * client's job easier — just submit, no need to track state.
 */
export type LeadSubmitResult =
  | { ok: true; leadId: number; isNew: boolean; message: string }
  | { ok: false; kind: LeadErrorKind; message: string };

export type LeadErrorKind =
  | "invalid"      // 400 — validation failed (bad phone / wechat / etc)
  | "rate-limited" // 429 — too many submissions
  | "network"      // request never reached server
  | "server"       // 5xx
  | "unknown";

export type LeadInput = {
  wechat?: string;
  phone?: string;
  homestay_name?: string;
  homestay_loc?: string;
  notes?: string;
  source?: string; // "home" | "pricing" | "footer" | "contact-page" | etc.
};

type ServerOk = {
  success: true;
  data: {
    lead_id: number;
    is_new: boolean;
    message: string;
  };
};

type ServerErr = {
  success: false;
  message: string;
};

export async function submitLead(input: LeadInput): Promise<LeadSubmitResult> {
  try {
    const response = await axios.post<ServerOk | ServerErr>(
      "/gtk/v1/contact",
      {
        wechat: input.wechat?.trim() || "",
        phone: input.phone?.trim() || "",
        homestay_name: input.homestay_name?.trim() || "",
        homestay_loc: input.homestay_loc?.trim() || "",
        notes: input.notes?.trim() || "",
        source: input.source || "home",
      },
    );
    if (response.data.success) {
      const data = (response.data as ServerOk).data;
      return {
        ok: true,
        leadId: data.lead_id,
        isNew: data.is_new,
        message: data.message,
      };
    }
    return {
      ok: false,
      kind: "unknown",
      message: (response.data as ServerErr).message,
    };
  } catch (e) {
    return classifyError(e);
  }
}

function classifyError(e: unknown): LeadSubmitResult {
  if (!(e instanceof AxiosError)) {
    return { ok: false, kind: "unknown", message: "未知错误" };
  }
  if (!e.response) {
    return { ok: false, kind: "network", message: e.message || "网络异常" };
  }
  const status = e.response.status;
  const errMsg =
    typeof e.response.data === "object" && e.response.data
      ? (e.response.data as { message?: string }).message || ""
      : "";
  if (status === 400) return { ok: false, kind: "invalid", message: errMsg || "信息有误" };
  if (status === 429) return { ok: false, kind: "rate-limited", message: errMsg || "请求过频" };
  if (status >= 500) return { ok: false, kind: "server", message: errMsg || "服务器忙" };
  return { ok: false, kind: "unknown", message: errMsg || "未知错误" };
}
