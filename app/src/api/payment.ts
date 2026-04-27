import axios, { AxiosError } from "axios";

/**
 * Categorized failure modes for the checkout fetch. Caller (UpgradeCTA)
 * picks a copy variant per category so users see actionable messages
 * instead of a generic "something went wrong".
 */
export type CheckoutErrorKind =
  | "unauthorized" // user session expired — needs re-login
  | "misconfigured" // LS env vars missing on server — ops issue
  | "network" // request never reached server / timed out
  | "unknown"; // we got a response but it didn't include a URL

export type CheckoutFetchResult =
  | { ok: true; url: string }
  | { ok: false; kind: CheckoutErrorKind; message?: string };

type CheckoutResponseBody = {
  status: boolean;
  url?: string;
  error?: string;
};

/**
 * Fetch a personalized LemonSqueezy Checkout URL for the current user.
 * Returns a discriminated union so callers can route to specific copy.
 *
 * Errors do not throw — UpgradeCTA's UX is "show toast, allow retry",
 * not "crash the page".
 */
export async function getLemonSqueezyCheckoutURL(): Promise<CheckoutFetchResult> {
  try {
    const response = await axios.get<CheckoutResponseBody>("/payment/checkout");
    if (response.data.status && response.data.url) {
      return { ok: true, url: response.data.url };
    }
    return { ok: false, kind: "unknown", message: response.data.error };
  } catch (e) {
    return classifyAxiosError(e);
  }
}

function classifyAxiosError(e: unknown): CheckoutFetchResult {
  if (!(e instanceof AxiosError)) {
    return { ok: false, kind: "unknown" };
  }
  // No HTTP response body = transport-level failure (offline, DNS, CORS).
  if (!e.response) {
    return { ok: false, kind: "network", message: e.message };
  }
  const status = e.response.status;
  if (status === 401 || status === 403) {
    return { ok: false, kind: "unauthorized" };
  }
  if (status === 500) {
    // Server hit our own buildCheckoutURL "env not configured" path.
    const errMsg =
      typeof e.response.data === "object" && e.response.data
        ? (e.response.data as { error?: string }).error
        : undefined;
    return { ok: false, kind: "misconfigured", message: errMsg };
  }
  return { ok: false, kind: "unknown", message: String(status) };
}

/**
 * Backend-side subscription view. Combines CoAI's authoritative `subscription`
 * row (level + expired_at) with our LS-mapping row (status, cancelled_at,
 * renews_at, test_mode). Empty strings mean "no row yet" — perfectly normal
 * for a free user who never paid.
 */
export type SubscriptionState = {
  level: number;
  expired_at: string;
  ls_status: string;
  cancelled_at: string;
  renews_at: string;
  test_mode: boolean;
};

type SubscriptionResponseBody = {
  status: boolean;
  data?: SubscriptionState;
  error?: string;
};

const EMPTY_SUBSCRIPTION: SubscriptionState = {
  level: 0,
  expired_at: "",
  ls_status: "",
  cancelled_at: "",
  renews_at: "",
  test_mode: false,
};

/**
 * Fetch the authenticated user's subscription state. On error returns an
 * empty (level=0) state so the UI degrades to "user is not subscribed"
 * instead of breaking — worst-case: we show the upgrade CTA to a paid user
 * who can dismiss it after a refresh, vs. blank screen.
 */
export async function getLemonSqueezySubscription(): Promise<SubscriptionState> {
  try {
    const response = await axios.get<SubscriptionResponseBody>(
      "/payment/subscription",
    );
    if (response.data.status && response.data.data) {
      return response.data.data;
    }
  } catch (e) {
    console.debug("[payment] subscription fetch failed", e);
  }
  return EMPTY_SUBSCRIPTION;
}
