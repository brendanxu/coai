// Admin-side orders API client (PKG-N1).
//
// Three endpoints, all admin-only:
//
//   GET  /gtk/v1/admin/orders           List all service orders (this PKG)
//   POST /gtk/v1/admin/mark-paid        Flip pending_payment → paid (existing)
//   POST /gtk/v1/admin/refund           Flip → refunded (existing)
//
// Plus one helper that piggy-backs on the customer-facing endpoint:
//
//   POST /gtk/v1/service/order          Create a concierge / manual order
//
// The concierge-create path uses the existing /service/order endpoint
// with payment_provider="manual" rather than introducing a new admin
// surface, because:
//   - The endpoint already supports the manual flow (see
//     service/router.go::CreateOrderAPI's switch case "manual"),
//   - Creating from the admin UI behaves identically to a customer
//     selecting "concierge" — the only difference is who clicks the
//     button. No reason to fork the contract.
//   - LIMITATION: this means the order is created against the *admin's*
//     coai_user_id, not the customer's. v0 is fine because tana / founder
//     are the only admins; the concierge order is just a placeholder for
//     "we are about to take payment offline". v0.21 will add a real
//     "create on behalf of <user>" admin endpoint when we have multiple
//     concierge operators.
//
// Mirrors api/orders.ts + api/userRouting.ts conventions: thin axios
// wrappers, no client-side cache (caller refreshes manually after
// mutations), envelope unwrap with explicit error throw.

import axios from "axios";
import type { OrderStatus } from "./orders.ts";

// ──────────────────────────────────────────────────────────────────────
// Types — mirror service.AdminOrderRow.
// ──────────────────────────────────────────────────────────────────────

export type AdminOrderRow = {
  order_no: string;
  coai_user_id: number;
  username: string; // "" if auth row missing (orphan FK)
  service_slug: string;
  service_name: string;
  status: OrderStatus;
  payment_provider: string; // "lemonsqueezy" | "hupijiao" | "manual"
  price_cny_cents_paid: number;
  price_display_cny: string;
  credits_granted: number;
  has_run: boolean;
  refund_reason?: string;
  created_at: string;
  updated_at: string;
  paid_at?: string;
  completed_at?: string;
};

export type AdminOrderListResponse = {
  orders: AdminOrderRow[];
  total: number;
  limit: number;
  offset: number;
};

export type AdminOrderListParams = {
  status?: OrderStatus | "";
  user_id?: number;
  q?: string;
  limit?: number;
  offset?: number;
};

export type RefundResponse = {
  order_no: string;
  status: "refunded";
  refund_reason: string;
  already_refunded: boolean;
};

export type MarkPaidResponse = {
  order_no: string;
  product_type: string;
  status: "paid";
};

export type CreateConciergeOrderResponse = {
  order_no: string;
  price_cny_cents: number;
  price_display: string;
  included_credits: number;
  service_name: string;
  service_slug: string;
  concierge: boolean;
  concierge_message: string;
};

type Envelope<T> = {
  success: boolean;
  data?: T;
  message?: string;
};

// ──────────────────────────────────────────────────────────────────────
// Calls
// ──────────────────────────────────────────────────────────────────────

/**
 * List service orders across all users (admin-only).
 *
 * Throws on any non-success response so the page can render the error
 * inline rather than silently rendering an empty table (which would
 * mask transient DB issues).
 */
export async function listAdminOrders(
  params: AdminOrderListParams = {},
): Promise<AdminOrderListResponse> {
  const q = new URLSearchParams();
  if (params.status) q.set("status", params.status);
  if (params.user_id != null && params.user_id > 0) {
    q.set("user_id", String(params.user_id));
  }
  if (params.q) q.set("q", params.q);
  if (params.limit != null) q.set("limit", String(params.limit));
  if (params.offset != null) q.set("offset", String(params.offset));
  const suffix = q.toString() ? `?${q}` : "";

  const resp = await axios.get<Envelope<AdminOrderListResponse>>(
    `/gtk/v1/admin/orders${suffix}`,
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "list admin orders failed");
  }
  return resp.data.data;
}

/**
 * Flip a pending_payment service order → paid (admin-only). Triggers
 * commerce.MarkPaid → GrantEntitlement → entitlement provisioning.
 *
 * Idempotent server-side: re-marking an already-paid order no-ops.
 */
export async function markOrderPaid(orderNo: string): Promise<MarkPaidResponse> {
  const resp = await axios.post<Envelope<MarkPaidResponse>>(
    `/gtk/v1/admin/mark-paid`,
    {
      order_no: orderNo,
      product_type: "service", // v0 only supports service orders
    },
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "mark-paid failed");
  }
  return resp.data.data;
}

/**
 * Flip a service order → refunded (admin-only). Note: this updates
 * OUR record only. The actual money refund happens in the LemonSqueezy
 * / hupijiao dashboard, separately by the operator.
 *
 * refund_amount_cents is optional — leave 0/undefined for full refund.
 *
 * Idempotent server-side: refunding twice with the same reason no-ops;
 * with a different reason, the latest reason wins.
 */
export async function refundOrder(
  orderNo: string,
  reason: string,
  amountCents?: number,
): Promise<RefundResponse> {
  const body: Record<string, unknown> = {
    order_no: orderNo,
    refund_reason: reason,
  };
  if (amountCents != null && amountCents > 0) {
    body.refund_amount_cents = amountCents;
  }
  const resp = await axios.post<Envelope<RefundResponse>>(
    `/gtk/v1/admin/refund`,
    body,
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "refund failed");
  }
  return resp.data.data;
}

/**
 * Create a concierge / manual order. See file header for the v0
 * caveat (order is created against the *admin's* coai_user_id).
 *
 * Returns the new order_no — admin can then immediately call
 * markOrderPaid() against it once payment lands.
 */
export async function createConciergeOrder(
  serviceSlug: string,
): Promise<CreateConciergeOrderResponse> {
  const resp = await axios.post<Envelope<CreateConciergeOrderResponse>>(
    `/gtk/v1/service/order`,
    {
      service_slug: serviceSlug,
      payment_provider: "manual",
    },
  );
  if (!resp.data.success || !resp.data.data) {
    throw new Error(resp.data.message || "create concierge order failed");
  }
  return resp.data.data;
}
