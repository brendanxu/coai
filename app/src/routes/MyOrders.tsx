// /orders — customer self-serve order list (PKG-5).
//
// Behavior:
//   - Auth-gated via <AuthRequired> in router.tsx.
//   - Lists the caller's most recent service orders, newest first.
//   - Status filter chips: All / Pending / Paid / Running / Completed /
//     Refunded. Clicking refetches.
//   - Each card surfaces order_no, service_name, status badge, paid
//     price, granted credits, and timestamps.
//   - Empty state explains "no orders yet" + nudges to contact support
//     for a concierge order (since there's no self-serve catalog UI in
//     this branch yet).
//
// Why no router link to a detail page in this PKG:
//   The detail-page UI doesn't exist yet. The detail endpoint (loadMyOrder)
//   is exported from @/api/orders.ts so a future PKG can build the page
//   without re-doing the API client. Keeping the route surface minimal
//   here = no broken-link 404s.

import { useEffect, useMemo, useState } from "react";
import { useTranslation } from "react-i18next";
import {
  listMyOrders,
  type CustomerOrderSummary,
  type OrderStatus,
} from "@/api/orders.ts";
import { Badge } from "@/components/ui/badge.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Loader2, PackageOpen, RefreshCw } from "lucide-react";

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

  async function refetch(f: FilterValue = filter) {
    setLoading(true);
    try {
      const rows = await listMyOrders(f || undefined);
      setOrders(rows);
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
    <div className="max-w-3xl mx-auto px-6 py-12">
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

      <div className="flex flex-wrap gap-2 mb-8">{filterChips}</div>

      {loading && orders.length === 0 ? (
        <div className="flex justify-center py-16">
          <Loader2 size={20} className="animate-spin text-muted-foreground" />
        </div>
      ) : total === 0 ? (
        <EmptyState filter={filter} />
      ) : (
        <ul className="space-y-3">
          {orders.map((o) => (
            <OrderCard key={o.order_no} order={o} />
          ))}
        </ul>
      )}
    </div>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Subcomponents
// ──────────────────────────────────────────────────────────────────────

function OrderCard({ order }: { order: CustomerOrderSummary }) {
  const { t } = useTranslation();
  const variant = statusBadgeVariant(order.status);
  const statusLabel = t(statusI18nKey(order.status));

  return (
    <li className="border border-border rounded-lg p-5 hover:border-foreground/20 transition-colors">
      <div className="flex items-start justify-between gap-4">
        <div className="min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <span className="text-base font-medium truncate">
              {order.service_name}
            </span>
            <Badge variant={variant}>{statusLabel}</Badge>
          </div>
          <div className="text-xs text-muted-foreground mt-1 font-mono">
            {order.order_no}
          </div>
        </div>
        <div className="text-right shrink-0">
          <div className="text-lg font-medium tabular-nums">
            {order.price_display_cny}
          </div>
          {order.credits_granted > 0 && (
            <div className="text-xs text-muted-foreground mt-0.5">
              {t("my_orders.credits", { count: order.credits_granted })}
            </div>
          )}
        </div>
      </div>

      <div className="mt-4 flex flex-wrap gap-x-6 gap-y-1 text-xs text-muted-foreground">
        <span>
          {t("my_orders.created_at")}: {fmtDate(order.created_at)}
        </span>
        {order.paid_at && (
          <span>
            {t("my_orders.paid_at")}: {fmtDate(order.paid_at)}
          </span>
        )}
        {order.completed_at && (
          <span>
            {t("my_orders.completed_at")}: {fmtDate(order.completed_at)}
          </span>
        )}
      </div>
    </li>
  );
}

function EmptyState({ filter }: { filter: FilterValue }) {
  const { t } = useTranslation();
  return (
    <div className="text-center py-20 border border-dashed border-border rounded-lg">
      <PackageOpen
        size={32}
        className="mx-auto text-muted-foreground/60 mb-4"
      />
      <h2 className="text-lg font-medium mb-2">
        {filter
          ? t("my_orders.empty.filtered_title")
          : t("my_orders.empty.title")}
      </h2>
      <p className="text-sm text-muted-foreground max-w-sm mx-auto">
        {filter ? t("my_orders.empty.filtered_body") : t("my_orders.empty.body")}
      </p>
    </div>
  );
}
