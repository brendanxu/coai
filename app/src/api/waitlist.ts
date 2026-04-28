import axios, { AxiosError } from "axios";

/**
 * Waitlist signup result. Discriminated so the dialog can render distinct
 * copy for "new signup" vs "already on the list" — both are user-side
 * successes, no need to surface the duplicate path as an error.
 */
export type WaitlistJoinResult =
  | { ok: true; alreadyOnList: boolean; message?: string }
  | { ok: false; kind: WaitlistErrorKind; message?: string };

export type WaitlistErrorKind =
  | "invalid" // 400 — bad email / unknown service / malformed body
  | "rate-limited" // 429 — caller should ease off
  | "network" // request never reached server
  | "server" // 5xx
  | "unknown";

type ServerResponse = {
  status: boolean;
  message?: string;
  error?: string;
};

/**
 * POST /api/waitlist with the visitor's email and the service slug they
 * tapped. `source` lets us segment "marketing-landing" vs (future) "blog-
 * post-cta" without changing the schema.
 *
 * Server-side idempotency: backend returns status:true with message="已在等待
 * 列表" when the (email, service) pair already exists. We surface that as
 * `alreadyOnList=true` so the dialog can show a softer toast.
 */
export async function joinWaitlist(
  email: string,
  service: string,
  source: string = "marketing-landing",
): Promise<WaitlistJoinResult> {
  try {
    const response = await axios.post<ServerResponse>("/waitlist", {
      email,
      service,
      source,
    });
    if (response.data.status) {
      const alreadyOnList =
        typeof response.data.message === "string" &&
        response.data.message.includes("已在");
      return { ok: true, alreadyOnList, message: response.data.message };
    }
    return {
      ok: false,
      kind: "unknown",
      message: response.data.error,
    };
  } catch (e) {
    return classifyError(e);
  }
}

function classifyError(e: unknown): WaitlistJoinResult {
  if (!(e instanceof AxiosError)) {
    return { ok: false, kind: "unknown" };
  }
  if (!e.response) {
    return { ok: false, kind: "network", message: e.message };
  }
  const status = e.response.status;
  const errMsg =
    typeof e.response.data === "object" && e.response.data
      ? (e.response.data as { error?: string }).error
      : undefined;
  if (status === 400) return { ok: false, kind: "invalid", message: errMsg };
  if (status === 429) return { ok: false, kind: "rate-limited", message: errMsg };
  if (status >= 500) return { ok: false, kind: "server", message: errMsg };
  return { ok: false, kind: "unknown", message: errMsg };
}
