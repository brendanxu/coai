// /orders/:order_no — customer-facing single order detail page (PKG-N2).
//
// Closes the customer self-serve loop after PKG-5 added /orders list.
// Audit report finding: customers couldn't see what was delivered, the
// run progress, the refund history; couldn't request a refund or
// reorder without contacting support. This page surfaces all of that.
//
// Sections (top → bottom):
//   1. Breadcrumb (← /orders)
//   2. Header: service name + status badge + polling indicator
//   3. Status-specific banner (running / pending_payment / refunded / etc.)
//   4. Order metadata: order_no, paid amount, payment provider, timestamps
//   5. Refund log (if refund_reason populated)
//   6. Action row: Reorder + Request Refund buttons + Refresh
//
// Polling for "running": setInterval at 5s, cleared on status change
// or unmount. Simple — no SSE/websockets needed for v0.
//
// Note on "delivered content" markdown:
//   Spec asked for agent_output markdown rendering when
//   status=completed. CustomerOrderDetail does NOT carry agent_output
//   in the current backend response shape — only agent_run_id +
//   refund_reason. Per the spec's documented fallback ("Markdown
//   rendering library not available in current frontend → use plain
//   text fallback + document"), this page surfaces the *fact* of
//   completion + timestamp + a refund_reason note when present, but
//   leaves the agent_output rendering to a follow-up PKG that extends
//   the backend response. Customers can still see "this order
//   completed at <timestamp>" and act on it (reorder / refund).

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate, useParams } from "react-router-dom";
import { toast } from "sonner";
import { ArrowLeft, Loader2, RefreshCw } from "lucide-react";

import {
  loadMyOrder,
  requestRefund,
  requestReorder,
  type CustomerOrderDetail,
  type OrderStatus,
} from "@/api/orders.ts";
import { Badge } from "@/components/ui/badge.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Textarea } from "@/components/ui/textarea.tsx";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.tsx";

// Status sets — mirror backend statesAcceptingCustomerRefund map.
const REFUND_REQUESTABLE: OrderStatus[] = ["paid", "running", "completed"];
const REORDER_AVAILABLE: OrderStatus[] = [
  "completed",
  "refunded_post_delivery",
];
const POLLING_STATUSES: OrderStatus[] = ["running"];

function statusBadgeVariant(
  s: OrderStatus,
): "default" | "secondary" | "destructive" | "outline" {
  switch (s) {
    case "paid":
    case "completed":
      return "default";
    case "pending_payment":
    case "running":
      return "secondary";
    case "refunded":
    case "refunded_post_delivery":
    case "canceled_mid_flight":
      return "outline";
    case "failed":
      return "destructive";
    default:
      return "secondary";
  }
}

// Best-effort timestamp formatting. Backend ships values like
// "2026-05-10 12:34:56" (UTC, no timezone). Mirror MyOrders.tsx.
function fmtDate(s?: string): string {
  if (!s) return "";
  const iso = s.includes("T") ? s : s.replace(" ", "T") + "Z";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleString();
}

// ──────────────────────────────────────────────────────────────────────
// Page
// ──────────────────────────────────────────────────────────────────────

export default function OrderDetail() {
  const { t } = useTranslation();
  const { order_no } = useParams<{ order_no: string }>();
  const navigate = useNavigate();

  const [order, setOrder] = useState<CustomerOrderDetail | null>(null);
  const [loading, setLoading] = useState(true);
  const [notFound, setNotFound] = useState(false);

  // Refund modal state.
  const [refundOpen, setRefundOpen] = useState(false);
  const [refundReason, setRefundReason] = useState("");
  const [refundSubmitting, setRefundSubmitting] = useState(false);

  // Reorder loading.
  const [reordering, setReordering] = useState(false);

  // Polling — keep ref so refetch closures don't capture stale state.
  const pollTimerRef = useRef<number | null>(null);

  const refetch = useCallback(async () => {
    if (!order_no) return;
    const data = await loadMyOrder(order_no);
    if (!data) {
      setNotFound(true);
      setOrder(null);
    } else {
      setNotFound(false);
      setOrder(data);
    }
    setLoading(false);
  }, [order_no]);

  useEffect(() => {
    setLoading(true);
    refetch();
  }, [refetch]);

  // Manage 5s polling when status === running. Clear on status change
  // or unmount. Simplest correct implementation; no SSE needed for v0.
  useEffect(() => {
    if (!order) return;
    const shouldPoll = POLLING_STATUSES.includes(order.status);
    if (!shouldPoll) {
      if (pollTimerRef.current !== null) {
        window.clearInterval(pollTimerRef.current);
        pollTimerRef.current = null;
      }
      return;
    }
    if (pollTimerRef.current !== null) return; // already polling
    pollTimerRef.current = window.setInterval(() => {
      refetch();
    }, 5000);
    return () => {
      if (pollTimerRef.current !== null) {
        window.clearInterval(pollTimerRef.current);
        pollTimerRef.current = null;
      }
    };
  }, [order, refetch]);

  // ────────────────────────────────────────────────────────────────────
  // Loading + 404 short-circuits
  // ────────────────────────────────────────────────────────────────────

  if (loading) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-20 flex justify-center">
        <Loader2
          size={20}
          className="animate-spin text-muted-foreground"
          aria-label={t("order_detail.loading")}
        />
      </div>
    );
  }

  if (notFound || !order) {
    return (
      <div className="max-w-3xl mx-auto px-6 py-20 text-center">
        <h1 className="text-2xl font-display mb-3">
          {t("order_detail.not_found.title")}
        </h1>
        <p className="text-sm text-muted-foreground mb-8 max-w-md mx-auto">
          {t("order_detail.not_found.body")}
        </p>
        <Button onClick={() => navigate("/orders")} variant="outline">
          <ArrowLeft size={14} className="mr-1.5" />
          {t("order_detail.back")}
        </Button>
      </div>
    );
  }

  // ────────────────────────────────────────────────────────────────────
  // Action handlers
  // ────────────────────────────────────────────────────────────────────

  async function submitRefundRequest() {
    if (!order_no) return;
    const trimmed = refundReason.trim();
    if (trimmed.length < 4) {
      toast.warning(t("order_detail.modal.refund_too_short"));
      return;
    }
    setRefundSubmitting(true);
    try {
      const r = await requestRefund(order_no, trimmed);
      if (r.ok) {
        toast.success(t("order_detail.toast.refund_submitted"));
        setRefundOpen(false);
        setRefundReason("");
        // Refetch so any state-display tweaks (e.g. note appended)
        // pick up.
        refetch();
      } else {
        toast.error(t("order_detail.toast.refund_failed"), {
          description: r.message,
        });
      }
    } finally {
      setRefundSubmitting(false);
    }
  }

  async function submitReorder() {
    if (!order_no) return;
    setReordering(true);
    try {
      const r = await requestReorder(order_no);
      if (r.ok && r.redirect_url) {
        // Stub returns /pricing; treat as in-app navigation when it
        // starts with "/", external otherwise.
        if (r.redirect_url.startsWith("/")) {
          navigate(r.redirect_url);
        } else {
          window.location.href = r.redirect_url;
        }
      } else {
        toast.error(t("order_detail.toast.reorder_failed"), {
          description: r.message,
        });
      }
    } finally {
      setReordering(false);
    }
  }

  // ────────────────────────────────────────────────────────────────────
  // Render
  // ────────────────────────────────────────────────────────────────────

  const variant = statusBadgeVariant(order.status);
  const statusLabel = t(`order_detail.status.${order.status}.label`);
  const canRefund = REFUND_REQUESTABLE.includes(order.status);
  const canReorder = REORDER_AVAILABLE.includes(order.status);

  return (
    <div className="max-w-3xl mx-auto px-6 py-12">
      {/* Breadcrumb */}
      <Link
        to="/orders"
        className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors mb-6"
      >
        <ArrowLeft size={14} />
        {t("order_detail.back")}
      </Link>

      {/* Header */}
      <header className="mb-8">
        <div className="flex items-center gap-3 flex-wrap mb-2">
          <h1 className="text-2xl md:text-3xl font-display tracking-tight">
            {order.service_name}
          </h1>
          <Badge variant={variant}>{statusLabel}</Badge>
          {POLLING_STATUSES.includes(order.status) && (
            <span
              className="text-xs text-muted-foreground inline-flex items-center gap-1"
              aria-live="polite"
            >
              <Loader2 size={12} className="animate-spin" />
              {t("order_detail.polling")}
            </span>
          )}
        </div>
        <div className="text-xs text-muted-foreground font-mono">
          {order.order_no}
        </div>
      </header>

      {/* Status-specific banner */}
      <StatusBanner order={order} />

      {/* Metadata grid */}
      <section className="border border-border rounded-lg p-5 mb-8">
        <h2 className="text-sm font-medium text-muted-foreground mb-3">
          {t("order_detail.metadata.title")}
        </h2>
        <dl className="grid grid-cols-2 md:grid-cols-3 gap-x-4 gap-y-3 text-sm">
          <MetaItem
            label={t("order_detail.metadata.amount")}
            value={
              <span className="font-medium tabular-nums">
                {order.price_display_cny}
              </span>
            }
          />
          {order.credits_granted > 0 && (
            <MetaItem
              label={t("order_detail.metadata.credits")}
              value={
                <span className="tabular-nums">{order.credits_granted}</span>
              }
            />
          )}
          <MetaItem
            label={t("order_detail.metadata.payment_provider")}
            value={t(
              `order_detail.payment_provider.${order.payment_provider}`,
              { defaultValue: order.payment_provider },
            )}
          />
          <MetaItem
            label={t("order_detail.metadata.created_at")}
            value={fmtDate(order.created_at)}
          />
          {order.paid_at && (
            <MetaItem
              label={t("order_detail.metadata.paid_at")}
              value={fmtDate(order.paid_at)}
            />
          )}
          {order.completed_at && (
            <MetaItem
              label={t("order_detail.metadata.completed_at")}
              value={fmtDate(order.completed_at)}
            />
          )}
        </dl>
      </section>

      {/* Refund note (if any) */}
      {order.refund_reason && (
        <section className="border border-border rounded-lg p-5 mb-8 bg-muted/30">
          <h2 className="text-sm font-medium text-muted-foreground mb-2">
            {t("order_detail.refund_log.title")}
          </h2>
          <pre className="text-xs whitespace-pre-wrap font-mono leading-relaxed">
            {order.refund_reason}
          </pre>
        </section>
      )}

      {/* Action row */}
      <div className="flex flex-wrap gap-3 items-center justify-between border-t border-border pt-6">
        <Button
          variant="ghost"
          size="sm"
          onClick={() => refetch()}
          aria-label={t("order_detail.refresh")}
        >
          <RefreshCw size={14} className="mr-1.5" />
          {t("order_detail.refresh")}
        </Button>

        <div className="flex flex-wrap gap-2">
          {canReorder && (
            <Button
              variant="outline"
              size="sm"
              onClick={submitReorder}
              disabled={reordering}
            >
              {reordering && (
                <Loader2 size={12} className="animate-spin mr-1.5" />
              )}
              {t("order_detail.action.reorder")}
            </Button>
          )}
          {canRefund && (
            <Button
              variant="outline"
              size="sm"
              onClick={() => setRefundOpen(true)}
            >
              {t("order_detail.action.refund")}
            </Button>
          )}
        </div>
      </div>

      {/* Refund request modal */}
      <Dialog open={refundOpen} onOpenChange={setRefundOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>{t("order_detail.modal.refund_title")}</DialogTitle>
            <DialogDescription>
              {t("order_detail.modal.refund_description")}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2 py-2">
            <Textarea
              value={refundReason}
              onChange={(e) => setRefundReason(e.target.value)}
              placeholder={t("order_detail.modal.refund_placeholder")}
              rows={4}
              maxLength={500}
              aria-label={t("order_detail.modal.refund_reason_label")}
            />
            <p className="text-xs text-muted-foreground text-right">
              {refundReason.length}/500
            </p>
          </div>
          <DialogFooter>
            <Button
              variant="ghost"
              onClick={() => setRefundOpen(false)}
              disabled={refundSubmitting}
            >
              {t("order_detail.modal.cancel")}
            </Button>
            <Button onClick={submitRefundRequest} disabled={refundSubmitting}>
              {refundSubmitting && (
                <Loader2 size={12} className="animate-spin mr-1.5" />
              )}
              {t("order_detail.modal.refund_submit")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Subcomponents
// ──────────────────────────────────────────────────────────────────────

function MetaItem({
  label,
  value,
}: {
  label: string;
  value: React.ReactNode;
}) {
  return (
    <div>
      <dt className="text-xs text-muted-foreground mb-0.5">{label}</dt>
      <dd>{value}</dd>
    </div>
  );
}

// StatusBanner renders a contextual message above the metadata grid for
// the in-flight / waiting / terminal states. We keep it visually simple
// (one short paragraph) to avoid stealing focus from the data below.
function StatusBanner({ order }: { order: CustomerOrderDetail }) {
  const { t } = useTranslation();
  const variants = useMemo(() => {
    switch (order.status) {
      case "pending_payment":
        return {
          tone: "amber" as const,
          message: t("order_detail.status.pending_payment.message"),
        };
      case "running":
        return {
          tone: "blue" as const,
          message: t("order_detail.status.running.message"),
        };
      case "completed":
        return {
          tone: "green" as const,
          message: t("order_detail.status.completed.message"),
        };
      case "refunded":
      case "refunded_post_delivery":
        return {
          tone: "neutral" as const,
          message: t("order_detail.status.refunded.message"),
        };
      case "canceled_mid_flight":
        return {
          tone: "neutral" as const,
          message: t("order_detail.status.canceled.message"),
        };
      case "failed":
        return {
          tone: "red" as const,
          message: t("order_detail.status.failed.message"),
        };
      default:
        return null;
    }
  }, [order.status, t]);

  if (!variants) return null;

  const toneClass = {
    amber:
      "border-amber-300/40 bg-amber-50/40 text-amber-900 dark:bg-amber-900/10 dark:text-amber-200",
    blue: "border-blue-300/40 bg-blue-50/40 text-blue-900 dark:bg-blue-900/10 dark:text-blue-200",
    green:
      "border-emerald-300/40 bg-emerald-50/40 text-emerald-900 dark:bg-emerald-900/10 dark:text-emerald-200",
    red: "border-red-300/40 bg-red-50/40 text-red-900 dark:bg-red-900/10 dark:text-red-200",
    neutral: "border-border bg-muted/30 text-muted-foreground",
  }[variants.tone];

  return (
    <div className={`border rounded-lg p-4 mb-6 text-sm ${toneClass}`}>
      {variants.message}
    </div>
  );
}
