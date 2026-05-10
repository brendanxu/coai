// API client for the PKG-5 customer self-serve order endpoints +
// PKG-N2 customer-side write actions.
//
// Backend mounts:
//   GET  /gtk/v1/orders                              List my service orders.
//   GET  /gtk/v1/orders/:order_no                    One order's detail (mine only).
//   POST /gtk/v1/orders/:order_no/refund-request     PKG-N2: ask for refund.
//   POST /gtk/v1/orders/:order_no/reorder            PKG-N2: reorder stub.
//
// Path convention follows the carbon API client (`/carbon/summary`,
// `/carbon/factors` etc.) — axios.defaults.baseURL is the rest API
// root, and the gin engine groups routes at "" in dev / "/api" in
// production. Either way the leading "/" is right.

import axios from "axios";

// ──────────────────────────────────────────────────────────────────────
// Types — mirror service.CustomerOrderSummary / CustomerOrderDetail.
// ──────────────────────────────────────────────────────────────────────

export type OrderStatus =
  | "pending_payment"
  | "paid"
  | "running"
  | "completed"
  | "refunded"
  | "refunded_post_delivery"
  | "failed"
  | "canceled_mid_flight";

export type CustomerOrderSummary = {
  order_no: string;
  service_slug: string;
  service_name: string;
  status: OrderStatus;
  payment_provider: string;
  price_cny_cents_paid: number;
  price_display_cny: string;
  credits_granted: number;
  has_run: boolean;
  created_at: string;
  updated_at: string;
  paid_at?: string;
  completed_at?: string;
};

export type CustomerOrderDetail = CustomerOrderSummary & {
  agent_run_id?: string;
  refund_reason?: string;
};

// Shape of {success, data: ...} returned by all gtk/v1 handlers.
type Envelope<T> = {
  success: boolean;
  data?: T;
  message?: string;
};

// ──────────────────────────────────────────────────────────────────────
// Calls
// ──────────────────────────────────────────────────────────────────────

/**
 * List the current user's service orders. Server-side caps at 100 rows;
 * an empty list is a valid response (= "no orders yet").
 *
 * statusFilter: optional. Pass undefined / "" for "all statuses".
 *
 * PKG-N5: returns a discriminated result with an `error` flag rather
 * than silently swallowing failures into `[]`. Empty + error=false ===
 * "no orders yet"; empty + error=true === "we couldn't reach the
 * server". Caller is responsible for surfacing the error (toast /
 * banner) so a 5xx / network blip stops looking identical to a brand-
 * new account.
 *
 * envelope success === false (server returned a structured failure) is
 * also flagged as error=true; otherwise the empty data branch would
 * skip past it silently.
 */
export async function listMyOrders(
  statusFilter?: OrderStatus | "",
): Promise<{ orders: CustomerOrderSummary[]; error: boolean }> {
  try {
    const params: Record<string, string> = {};
    if (statusFilter) params.status = statusFilter;
    const resp = await axios.get<
      Envelope<{ orders: CustomerOrderSummary[]; total: number }>
    >("/gtk/v1/orders", { params });
    if (resp.data?.success && Array.isArray(resp.data.data?.orders)) {
      return { orders: resp.data.data!.orders, error: false };
    }
    // 200 OK but envelope reports failure — count as an error so the
    // caller can show "couldn't load" instead of "no orders".
    console.error("[orders] list returned non-success envelope", resp.data);
    return { orders: [], error: true };
  } catch (e) {
    console.error("[orders] list failed", e);
    return { orders: [], error: true };
  }
}

/**
 * Load one order detail. Returns null when:
 *   - 404 (order not found OR not owned by caller — same response)
 *   - any other error
 *
 * Caller decides what to render for null (typically a "not found / no
 * permission" message).
 */
export async function loadMyOrder(
  orderNo: string,
): Promise<CustomerOrderDetail | null> {
  try {
    const resp = await axios.get<Envelope<CustomerOrderDetail>>(
      `/gtk/v1/orders/${encodeURIComponent(orderNo)}`,
    );
    if (resp.data?.success && resp.data.data) {
      return resp.data.data;
    }
    return null;
  } catch (e) {
    console.debug("[orders] detail failed", e);
    return null;
  }
}

// ──────────────────────────────────────────────────────────────────────
// PKG-N2 customer write actions.
// ──────────────────────────────────────────────────────────────────────

export type RefundRequestResult = {
  ok: boolean;
  /**
   * Server-side reason if non-2xx, e.g. "order status \"refunded\" does
   * not accept refund requests". Frontend surfaces this in the toast
   * for actionable cases (state mismatch); for transport errors stays
   * empty.
   */
  message?: string;
};

/**
 * Submit a customer-side refund request. Backend appends the reason
 * to gtk_service_order.refund_reason — does NOT flip status. Founder
 * still confirms via admin/refund. Returns 202 Accepted on success.
 *
 * Does not throw. Caller switches on .ok to decide toast variant.
 */
export async function requestRefund(
  orderNo: string,
  reason: string,
): Promise<RefundRequestResult> {
  try {
    const resp = await axios.post<Envelope<unknown>>(
      `/gtk/v1/orders/${encodeURIComponent(orderNo)}/refund-request`,
      { reason },
      { validateStatus: () => true },
    );
    if (resp.status === 200 || resp.status === 202) {
      return { ok: true };
    }
    return {
      ok: false,
      message: resp.data?.message ?? `HTTP ${resp.status}`,
    };
  } catch (e) {
    console.debug("[orders] refund-request failed", e);
    return { ok: false, message: "网络错误,请稍后重试" };
  }
}

export type ReorderResult = {
  ok: boolean;
  /** "/pricing" today; future versions may return a checkout-ready order_no. */
  redirect_url?: string;
  /** The slug from the original order — frontend can highlight on /pricing. */
  original_service_slug?: string;
  is_stub?: boolean;
  message?: string;
};

/**
 * Trigger reorder. v0 stub: returns a redirect_url (currently /pricing)
 * the frontend should navigate the customer to. Real one-click reorder
 * is deferred — customer needs to re-pick payment provider anyway.
 */
export async function requestReorder(
  orderNo: string,
): Promise<ReorderResult> {
  try {
    const resp = await axios.post<
      Envelope<{
        redirect_url: string;
        original_service_slug: string;
        is_stub: boolean;
      }>
    >(
      `/gtk/v1/orders/${encodeURIComponent(orderNo)}/reorder`,
      undefined,
      { validateStatus: () => true },
    );
    if (resp.status === 200 && resp.data?.success && resp.data.data) {
      return {
        ok: true,
        redirect_url: resp.data.data.redirect_url,
        original_service_slug: resp.data.data.original_service_slug,
        is_stub: resp.data.data.is_stub,
      };
    }
    return {
      ok: false,
      message: resp.data?.message ?? `HTTP ${resp.status}`,
    };
  } catch (e) {
    console.debug("[orders] reorder failed", e);
    return { ok: false, message: "网络错误,请稍后重试" };
  }
}
