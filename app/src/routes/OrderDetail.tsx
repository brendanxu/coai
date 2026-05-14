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

import { useCallback, useEffect, useRef, useState } from "react";
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
        className="inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors mb-8"
      >
        <ArrowLeft size={14} />
        {t("order_detail.back")}
      </Link>

      {/* Header card: status pill + order_no + amount + created_at */}
      <header
        className="rounded-2xl p-6 md:p-8 mb-8 flex flex-col md:flex-row md:items-start md:justify-between gap-4"
        style={{
          background: "hsl(var(--card))",
          border: "1px solid hsl(var(--border-soft, var(--border)))",
          boxShadow: "var(--shadow-xs, none)",
        }}
      >
        <div className="space-y-2">
          <div className="flex items-center gap-2 flex-wrap">
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
            <span className="text-xs text-muted-foreground font-mono">
              {fmtDate(order.created_at)}
            </span>
          </div>
          <h1 className="font-display text-2xl md:text-3xl tracking-tight">
            {t("order_detail.header.title", "订单")}{" "}
            <em
              className="not-italic"
              style={{ color: "hsl(var(--primary))", fontStyle: "italic" }}
            >
              {order.order_no}
            </em>
          </h1>
          <p className="text-sm text-muted-foreground">{order.service_name}</p>
        </div>
        <div className="text-right shrink-0">
          <p className="text-xs text-muted-foreground mb-1">
            {t("order_detail.metadata.amount")}
          </p>
          <div className="font-display text-3xl md:text-4xl font-semibold tabular-nums tracking-tight">
            {order.price_display_cny}
          </div>
          {order.credits_granted > 0 && (
            <p className="text-xs text-muted-foreground mt-1">
              +{order.credits_granted} credits
            </p>
          )}
        </div>
      </header>

      {/* Tabs: 时间线 / 输出详情 / 退款申请 */}
      <OrderTabs
        order={order}
        canRefund={canRefund}
        onRefundOpen={() => setRefundOpen(true)}
        t={t}
      />

      {/* Action row */}
      <div className="flex flex-wrap gap-3 items-center justify-between border-t border-border pt-6 mt-6">
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

// OrderTabs — 时间线 / 输出详情 / 退款申请 per mockup §3.5
type OrderTabId = "timeline" | "output" | "refund";

function OrderTabs({
  order,
  canRefund,
  onRefundOpen,
  t,
}: {
  order: CustomerOrderDetail;
  canRefund: boolean;
  onRefundOpen: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  const [activeTab, setActiveTab] = useState<OrderTabId>("timeline");

  const tabs: { id: OrderTabId; label: string; show: boolean }[] = [
    { id: "timeline", label: t("order_detail.tab.timeline", "时间线"), show: true },
    { id: "output", label: t("order_detail.tab.output", "输出详情"), show: true },
    { id: "refund", label: t("order_detail.tab.refund", "退款申请"), show: canRefund },
  ];

  return (
    <div>
      {/* Tab bar */}
      <div
        className="flex gap-0 mb-6 border-b border-border"
      >
        {tabs.filter((tab) => tab.show).map((tab) => (
          <button
            key={tab.id}
            type="button"
            onClick={() => setActiveTab(tab.id)}
            className="px-4 py-2.5 text-sm font-medium border-b-2 transition-colors -mb-px"
            style={{
              borderColor: activeTab === tab.id ? "hsl(var(--primary))" : "transparent",
              color: activeTab === tab.id
                ? "hsl(var(--foreground))"
                : "hsl(var(--muted-foreground))",
            }}
          >
            {tab.label}
          </button>
        ))}
      </div>

      {/* Tab: 时间线 */}
      {activeTab === "timeline" && (
        <div className="space-y-0">
          {/* Created */}
          <TimelineRow
            label={t("order_detail.timeline.created", "订单创建")}
            time={order.created_at}
            active
          />
          {/* Paid */}
          {order.paid_at && (
            <TimelineRow
              label={t("order_detail.timeline.paid", "支付成功 · webhook order_created")}
              sublabel={order.credits_granted > 0
                ? `+${order.credits_granted} credits ${t("order_detail.timeline.credited", "已到账")}`
                : undefined}
              time={order.paid_at}
              active
            />
          )}
          {/* Completed */}
          {order.completed_at && (
            <TimelineRow
              label={t("order_detail.timeline.completed", "已完成")}
              sublabel={t("order_detail.timeline.completed_sub", "entitlement granted · cost_ledger written")}
              time={order.completed_at}
              active
            />
          )}
          {/* Payment provider */}
          <div className="mt-4 pt-4 border-t border-border">
            <dl className="grid grid-cols-2 md:grid-cols-3 gap-x-4 gap-y-3 text-sm">
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
            </dl>
          </div>
        </div>
      )}

      {/* Tab: 输出详情 */}
      {activeTab === "output" && (
        <div className="text-sm text-muted-foreground py-4 space-y-3">
          {order.credits_granted > 0 ? (
            <p>
              {t("order_detail.output.credits_granted", "已授予")} <strong>{order.credits_granted}</strong> credits。
              {t("order_detail.output.credits_note", "余额在 Dashboard 实时可查。")}
            </p>
          ) : (
            <p>{t("order_detail.output.no_output", "此订单暂无结构化输出详情。")}</p>
          )}
        </div>
      )}

      {/* Tab: 退款申请 (only if canRefund) */}
      {activeTab === "refund" && canRefund && (
        <div className="py-4 space-y-4">
          {order.refund_reason ? (
            <div className="rounded-lg border border-border p-4 bg-muted/30">
              <p className="text-sm font-medium mb-2">
                {t("order_detail.refund_log.title")}
              </p>
              <pre className="text-xs whitespace-pre-wrap font-mono leading-relaxed text-muted-foreground">
                {order.refund_reason}
              </pre>
            </div>
          ) : (
            <div className="space-y-3">
              <p className="text-sm text-muted-foreground">
                {t("order_detail.refund_log.empty", "尚未提交退款申请。")}
              </p>
              <button
                type="button"
                onClick={onRefundOpen}
                className="text-sm font-medium underline underline-offset-4 hover:opacity-70 transition-opacity"
                style={{ color: "hsl(var(--primary))" }}
              >
                {t("order_detail.action.refund")} →
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function TimelineRow({
  label,
  sublabel,
  time,
  active,
}: {
  label: string;
  sublabel?: string;
  time?: string;
  active?: boolean;
}) {
  return (
    <div
      className="grid gap-3 py-3 border-b border-border last:border-0"
      style={{ gridTemplateColumns: "20px 1fr auto" }}
    >
      <span
        className="w-3.5 h-3.5 rounded-full mt-0.5 flex-shrink-0"
        style={{
          background: active ? "hsl(var(--primary))" : "hsl(var(--border))",
        }}
      />
      <div>
        <p className="text-sm font-medium">{label}</p>
        {sublabel && (
          <p className="text-xs text-muted-foreground mt-0.5">{sublabel}</p>
        )}
      </div>
      {time && (
        <span className="font-mono text-xs text-muted-foreground self-start pt-0.5">
          {fmtDate(time)}
        </span>
      )}
    </div>
  );
}
