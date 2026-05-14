// /orders — customer self-serve order list (PKG-5 + PKG-N2 cards-clickable).
//
// Behavior:
//   - Auth-gated via <AuthRequired> in router.tsx.
//   - Lists the caller's most recent service orders, newest first.
//   - Status filter chips: All / Pending / Paid / Running / Completed /
//     Refunded. Clicking refetches.
//   - Each card surfaces order_no, service_name, status badge, paid
//     price, granted credits, and timestamps.
//   - PKG-N2: cards are clickable — wrapped in a react-router <Link>
//     to /orders/:order_no for refund / reorder / status detail.
//   - Empty state explains "no orders yet" + nudges to contact support
//     for a concierge order (since there's no self-serve catalog UI in
//     this branch yet).

import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Link } from "react-router-dom";
import {
  listMyOrders,
  type CustomerOrderSummary,
  type OrderStatus,
} from "@/api/orders.ts";
import { Button } from "@/components/ui/button.tsx";
import {
  AlertTriangle,
  Loader2,
  MessageCircle,
  PackageOpen,
  RefreshCw,
} from "lucide-react";
import WaitlistDialog from "@/components/Marketing/WaitlistDialog.tsx";

// Status filter chips. "" = no filter (all statuses).
type FilterValue = "" | OrderStatus;

const FILTERS: { value: FilterValue; i18n: string }[] = [
  { value: "", i18n: "my_orders.filter.all" },
  { value: "pending_payment", i18n: "my_orders.filter.pending_payment" },
  { value: "paid", i18n: "my_orders.filter.paid" },
  { value: "running", i18n: "my_orders.filter.running" },
  { value: "completed", i18n: "my_orders.filter.completed" },
  { value: "refunded", i18n: "my_orders.filter.refunded" },
];

// Map status → badge variant + i18n label key. The "refunded_post_delivery"
// and "canceled_mid_flight" enum values land here too because we render
// every order, not just the filterable subset.

function statusI18nKey(s: OrderStatus): string {
  return `my_orders.status.${s}`;
}

// Best-effort timestamp formatting. Backend ships values like
// "2026-05-10 12:34:56" (UTC, no timezone). We parse, fall back to the
// raw string if Date can't make sense of it.
function fmtDate(s: string): string {
  if (!s) return "";
  // Convert "YYYY-MM-DD HH:MM:SS" → ISO-ish so Date handles it across browsers.
  const iso = s.includes("T") ? s : s.replace(" ", "T") + "Z";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return s;
  return d.toLocaleString();
}

// ──────────────────────────────────────────────────────────────────────
// Page
// ──────────────────────────────────────────────────────────────────────

export default function MyOrders() {
  const { t } = useTranslation();
  const [filter, setFilter] = useState<FilterValue>("");
  const [orders, setOrders] = useState<CustomerOrderSummary[]>([]);
  const [loading, setLoading] = useState(true);
  // PKG-N5 — track load failure so we can render a retry banner instead
  // of the empty state. Without this, transport / 5xx errors looked
  // identical to "no orders yet".
  const [loadError, setLoadError] = useState(false);

  async function refetch(f: FilterValue = filter) {
    setLoading(true);
    try {
      const result = await listMyOrders(f || undefined);
      setOrders(result.orders);
      setLoadError(result.error);
      if (result.error) {
        toast.error(
          t("my_orders.load_failed", "加载订单失败,请重试。"),
        );
      }
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    refetch(filter);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);

  const total = orders.length;
  const filterChips = useMemo(
    () =>
      FILTERS.map((f) => (
        <button
          key={f.value || "all"}
          type="button"
          onClick={() => setFilter(f.value)}
          className={
            "px-3 py-1.5 rounded-full text-sm font-medium transition-colors border " +
            (filter === f.value
              ? "bg-foreground text-background border-foreground"
              : "bg-transparent text-muted-foreground border-border hover:border-foreground/40")
          }
        >
          {t(f.i18n)}
        </button>
      )),
    [filter, t],
  );

  return (
    <div className="max-w-5xl mx-auto px-6 py-12">
      <header className="flex items-end justify-between mb-8 gap-4">
        <div>
          <h1 className="text-3xl md:text-4xl font-display tracking-tight">
            {t("my_orders.title")}
          </h1>
          <p className="text-sm text-muted-foreground mt-2">
            {t("my_orders.subtitle")}
          </p>
        </div>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => refetch()}
          disabled={loading}
          className="shrink-0"
          aria-label={t("my_orders.refresh")}
        >
          <RefreshCw size={16} className={loading ? "animate-spin" : ""} />
        </Button>
      </header>

      <div className="flex flex-wrap gap-2 mb-6">{filterChips}</div>

      {loading && orders.length === 0 ? (
        <div className="flex justify-center py-16">
          <Loader2 size={20} className="animate-spin text-muted-foreground" />
        </div>
      ) : loadError && total === 0 ? (
        <ErrorState onRetry={() => refetch()} loading={loading} />
      ) : total === 0 ? (
        <EmptyState filter={filter} />
      ) : (
        <OrderTable orders={orders} t={t} />
      )}
    </div>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Subcomponents
// ──────────────────────────────────────────────────────────────────────

// Status pill color tokens from mockup §3.4:
//   paid / completed  → green
//   pending / running → amber
//   refunded / canceled → gray
//   failed            → red
function StatusPill({ status, label }: { status: OrderStatus; label: string }) {
  const style: React.CSSProperties = (() => {
    switch (status) {
      case "paid":
      case "completed":
        return {
          background: "hsl(142 76% 94%)",
          color: "hsl(142 72% 29%)",
        };
      case "pending_payment":
      case "running":
        return {
          background: "hsl(38 92% 92%)",
          color: "hsl(32 95% 35%)",
        };
      case "refunded":
      case "refunded_post_delivery":
      case "canceled_mid_flight":
        return {
          background: "hsl(var(--muted))",
          color: "hsl(var(--muted-foreground))",
        };
      case "failed":
        return {
          background: "hsl(0 86% 94%)",
          color: "hsl(0 72% 40%)",
        };
      default:
        return {
          background: "hsl(var(--muted))",
          color: "hsl(var(--muted-foreground))",
        };
    }
  })();

  return (
    <span
      className="inline-flex items-center gap-1 px-2 py-0.5 rounded-full text-[11px] font-medium"
      style={style}
    >
      <span
        className="w-1.5 h-1.5 rounded-full flex-shrink-0"
        style={{ background: style.color }}
      />
      {label}
    </span>
  );
}

// OrderTable — desktop table + mobile stacked cards per mockup §3.4
function OrderTable({
  orders,
  t,
}: {
  orders: CustomerOrderSummary[];
  t: ReturnType<typeof useTranslation>["t"];
}) {
  return (
    <>
      {/* Desktop table */}
      <div className="hidden md:block overflow-x-auto rounded-xl border border-border">
        <table className="w-full text-sm border-collapse">
          <thead>
            <tr
              className="text-left text-xs font-medium text-muted-foreground"
              style={{ borderBottom: "1px solid hsl(var(--border))" }}
            >
              <th className="px-4 py-3">{t("my_orders.col.order_no", "订单号")}</th>
              <th className="px-4 py-3">{t("my_orders.col.plan", "套餐")}</th>
              <th className="px-4 py-3 text-right">{t("my_orders.col.amount", "金额")}</th>
              <th className="px-4 py-3">{t("my_orders.col.status", "状态")}</th>
              <th className="px-4 py-3">{t("my_orders.col.time", "时间")}</th>
              <th className="px-4 py-3 text-right">{t("my_orders.col.action", "操作")}</th>
            </tr>
          </thead>
          <tbody>
            {orders.map((o, i) => {
              const statusLabel = t(statusI18nKey(o.status));
              const isLast = i === orders.length - 1;
              return (
                <tr
                  key={o.order_no}
                  className="hover:bg-muted/30 transition-colors"
                  style={
                    isLast
                      ? undefined
                      : { borderBottom: "1px solid hsl(var(--border) / 0.5)" }
                  }
                >
                  <td className="px-4 py-3.5 font-mono text-xs text-muted-foreground">
                    {o.order_no}
                  </td>
                  <td className="px-4 py-3.5">
                    <span className="font-medium">{o.service_name}</span>
                    {o.credits_granted > 0 && (
                      <div className="text-xs text-muted-foreground mt-0.5">
                        {t("my_orders.credits", { count: o.credits_granted })}
                      </div>
                    )}
                  </td>
                  <td className="px-4 py-3.5 text-right tabular-nums font-medium">
                    {o.price_display_cny}
                  </td>
                  <td className="px-4 py-3.5">
                    <StatusPill status={o.status} label={statusLabel} />
                  </td>
                  <td className="px-4 py-3.5 text-xs text-muted-foreground">
                    {fmtDate(o.created_at)}
                  </td>
                  <td className="px-4 py-3.5 text-right">
                    <Link
                      to={`/orders/${encodeURIComponent(o.order_no)}`}
                      className="text-xs font-medium hover:opacity-70 transition-opacity"
                      style={{ color: "hsl(var(--primary))" }}
                    >
                      {t("my_orders.col.detail", "查看详情")} →
                    </Link>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>

      {/* Mobile stacked cards */}
      <ul className="md:hidden space-y-3">
        {orders.map((o) => {
          const statusLabel = t(statusI18nKey(o.status));
          return (
            <li key={o.order_no}>
              <Link
                to={`/orders/${encodeURIComponent(o.order_no)}`}
                className="block border border-border rounded-xl p-4 hover:border-foreground/30 hover:bg-muted/30 transition-colors"
                aria-label={`${o.service_name} ${o.order_no}`}
              >
                <div className="flex items-start justify-between gap-3 mb-3">
                  <div className="min-w-0">
                    <span className="font-medium">{o.service_name}</span>
                    <div className="text-xs text-muted-foreground font-mono mt-0.5 truncate">
                      {o.order_no}
                    </div>
                  </div>
                  <StatusPill status={o.status} label={statusLabel} />
                </div>
                <div className="flex items-center justify-between text-sm">
                  <span className="tabular-nums font-medium">
                    {o.price_display_cny}
                  </span>
                  <span className="text-xs text-muted-foreground">
                    {fmtDate(o.created_at)}
                  </span>
                </div>
              </Link>
            </li>
          );
        })}
      </ul>
    </>
  );
}

function ErrorState({
  onRetry,
  loading,
}: {
  onRetry: () => void;
  loading: boolean;
}) {
  const { t } = useTranslation();
  return (
    <div className="text-center py-20 border border-dashed border-destructive/40 rounded-lg">
      <AlertTriangle
        size={32}
        className="mx-auto text-destructive/70 mb-4"
      />
      <h2 className="text-lg font-medium mb-2">
        {t("my_orders.load_failed", "加载订单失败,请重试。")}
      </h2>
      <div className="mt-6">
        <Button
          type="button"
          size="sm"
          variant="outline"
          onClick={onRetry}
          disabled={loading}
          className="gap-1.5"
        >
          <RefreshCw size={14} className={loading ? "animate-spin" : ""} />
          {t("my_orders.retry", "重试")}
        </Button>
      </div>
    </div>
  );
}

function EmptyState({ filter }: { filter: FilterValue }) {
  const { t } = useTranslation();
  // PKG-N4 — only the "all empty" branch (no filter applied) gets the
  // concierge contact CTA. If the user is staring at "Nothing in this
  // status" they don't want a sales contact, they want to clear the chip.
  const isFilteredEmpty = !!filter;
  const [waitlistOpen, setWaitlistOpen] = useState(false);

  return (
    <>
      <div className="text-center py-20 border border-dashed border-border rounded-lg">
        <PackageOpen
          size={32}
          className="mx-auto text-muted-foreground/60 mb-4"
        />
        <h2 className="text-lg font-medium mb-2">
          {isFilteredEmpty
            ? t("my_orders.empty.filtered_title")
            : t("my_orders.empty.title")}
        </h2>
        <p className="text-sm text-muted-foreground max-w-sm mx-auto">
          {isFilteredEmpty
            ? t("my_orders.empty.filtered_body")
            : t("my_orders.empty.body")}
        </p>
        {!isFilteredEmpty && (
          <div className="mt-6">
            <Button
              type="button"
              size="sm"
              onClick={() => setWaitlistOpen(true)}
              className="gap-1.5"
            >
              <MessageCircle size={14} />
              {t("my_orders.empty.contact_cta", "加微信预约")}
            </Button>
          </div>
        )}
      </div>

      {/* PKG-N4 — concierge-first wedge has no self-serve checkout, so the
          "no orders yet" path opens the same email-capture used on the
          marketing pages. Mounted unconditionally inside the fragment;
          `open` controls visibility. */}
      <WaitlistDialog
        open={waitlistOpen}
        onOpenChange={setWaitlistOpen}
        service="any"
        serviceTitle={t(
          "my_orders.empty.contact_dialog_title",
          "联系预约 demo",
        )}
      />
    </>
  );
}
