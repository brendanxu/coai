// API client for the PKG-5 customer self-serve order endpoints.
//
// Backend mounts:
//   GET  /gtk/v1/orders                List my service orders.
//   GET  /gtk/v1/orders/:order_no      One order's detail (mine only).
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
 * Returns [] on any error (transport, 5xx, malformed envelope) — the
 * page renders an empty state rather than a stack trace. We deliberately
 * don't surface error details in the UI; ops can read them from the
 * browser console / server logs.
 */
export async function listMyOrders(
  statusFilter?: OrderStatus | "",
): Promise<CustomerOrderSummary[]> {
  try {
    const params: Record<string, string> = {};
    if (statusFilter) params.status = statusFilter;
    const resp = await axios.get<
      Envelope<{ orders: CustomerOrderSummary[]; total: number }>
    >("/gtk/v1/orders", { params });
    if (resp.data?.success && Array.isArray(resp.data.data?.orders)) {
      return resp.data.data!.orders;
    }
    return [];
  } catch (e) {
    console.debug("[orders] list failed", e);
    return [];
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
