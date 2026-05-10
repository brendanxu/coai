// AdminOrders — /admin/orders
//
// PKG-N1: closes the audit's highest-friction ops gap. Founder used to
// reach for raw curl + SQL to mark concierge / 民宿 orders as paid; this
// page wraps the existing /admin/mark-paid + /admin/refund endpoints in
// a table the founder can scan.
//
// Layout mirrors UserRouting.tsx (PKG-4) almost cell-for-cell:
//   - Card + admin-card class for the chrome
//   - Filter row with Select + Input + Refresh button
//   - Table with shadcn primitives
//   - Modal for the destructive action (refund) with confirm + reason
//   - Toast on success/error
//
// What this page DOESN'T do:
//   - Create concierge orders. The existing /pricing page already
//     supports payment_provider='manual', and the admin can drive that
//     flow from the customer side. v0.21 adds a first-class
//     "create-on-behalf" admin endpoint when concierge ops scales past
//     one operator. The adminOrders.ts client exposes
//     createConciergeOrder() so the UI can layer it on later without a
//     contract change.
//   - Direct customer messaging / refund-money-out-of-LS. Both are
//     out-of-band — admin clicks "Mark refunded" here, then opens the
//     LemonSqueezy / hupijiao dashboard to actually move the money.
//     The dialog reminds the operator of this with a "this updates our
//     record only" hint.

import { useEffect, useState, useCallback, useMemo } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select.tsx";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.tsx";
import { Badge } from "@/components/ui/badge.tsx";
import { Textarea } from "@/components/ui/textarea.tsx";
import {
  RotateCw,
  Loader2,
  CheckCircle2,
  Undo2,
  Search,
} from "lucide-react";
import {
  listAdminOrders,
  markOrderPaid,
  refundOrder,
  type AdminOrderRow,
  type AdminOrderListParams,
} from "@/api/adminOrders.ts";
import type { OrderStatus } from "@/api/orders.ts";

const PAGE_SIZE = 50;

// Badge variant union mirrors badge.tsx's `cva` definition. Inlined
// here rather than imported because BadgeVariants in badge.tsx is
// `keyof ReturnType<typeof badgeVariants>` (the className string keys),
// not the variant value union we actually want.
type BadgeVariant =
  | "default"
  | "secondary"
  | "destructive"
  | "outline"
  | "full_outline"
  | "gold";

// All schema-valid statuses (kept in lockstep with service/migration.go
// CHECK constraint and validOrderStatuses in customer_orders.go).
const ALL_STATUSES: OrderStatus[] = [
  "pending_payment",
  "paid",
  "running",
  "completed",
  "refunded",
  "refunded_post_delivery",
  "failed",
  "canceled_mid_flight",
];

// Map status → badge color. Visual cue for the table operator: green
// for terminal-good, red for terminal-bad, amber for in-flight, neutral
// for pending.
function statusBadgeVariant(s: OrderStatus): BadgeVariant {
  switch (s) {
    case "paid":
    case "completed":
      return "default";
    case "refunded":
    case "refunded_post_delivery":
    case "failed":
    case "canceled_mid_flight":
      return "destructive";
    case "running":
      return "gold";
    case "pending_payment":
    default:
      return "secondary";
  }
}

function AdminOrders() {
  const { t } = useTranslation();

  // ── List + filter state ────────────────────────────────────────────
  const [rows, setRows] = useState<AdminOrderRow[]>([]);
  const [total, setTotal] = useState(0);
  const [offset, setOffset] = useState(0);
  const [statusFilter, setStatusFilter] = useState<OrderStatus | "">("");
  const [searchInput, setSearchInput] = useState("");
  const [searchApplied, setSearchApplied] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // ── Mark-paid confirm state ────────────────────────────────────────
  // We keep this as a single "row pending mark-paid confirmation"
  // rather than a global "isOpen" + "selectedRow" pair so React can't
  // race to a stale row reference while the modal is animating out.
  const [markPaidTarget, setMarkPaidTarget] =
    useState<AdminOrderRow | null>(null);
  const [markPaidPending, setMarkPaidPending] = useState(false);

  // ── Refund modal state ─────────────────────────────────────────────
  const [refundTarget, setRefundTarget] = useState<AdminOrderRow | null>(null);
  const [refundReason, setRefundReason] = useState("");
  const [refundAmountInput, setRefundAmountInput] = useState("");
  const [refundPending, setRefundPending] = useState(false);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const params: AdminOrderListParams = {
        limit: PAGE_SIZE,
        offset,
      };
      if (statusFilter) params.status = statusFilter;
      if (searchApplied) params.q = searchApplied;
      const resp = await listAdminOrders(params);
      setRows(resp.orders);
      setTotal(resp.total);
    } catch (e: any) {
      setError(e?.message || String(e));
    } finally {
      setLoading(false);
    }
  }, [offset, statusFilter, searchApplied]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const currentPage = Math.floor(offset / PAGE_SIZE) + 1;

  const submitSearch = () => {
    setSearchApplied(searchInput.trim());
    setOffset(0);
  };

  const clearFilters = () => {
    setStatusFilter("");
    setSearchInput("");
    setSearchApplied("");
    setOffset(0);
  };

  // ── Mark-paid flow ─────────────────────────────────────────────────
  const openMarkPaid = (row: AdminOrderRow) => {
    setMarkPaidTarget(row);
  };
  const closeMarkPaid = () => {
    if (markPaidPending) return;
    setMarkPaidTarget(null);
  };
  const submitMarkPaid = async () => {
    if (!markPaidTarget) return;
    setMarkPaidPending(true);
    try {
      await markOrderPaid(markPaidTarget.order_no);
      toast.success(
        t("admin.orders.mark-paid-success", {
          order_no: markPaidTarget.order_no,
        }),
      );
      setMarkPaidTarget(null);
      await refresh();
    } catch (e: any) {
      toast.error(e?.message || String(e));
    } finally {
      setMarkPaidPending(false);
    }
  };

  // ── Refund flow ────────────────────────────────────────────────────
  const openRefund = (row: AdminOrderRow) => {
    setRefundTarget(row);
    setRefundReason(row.refund_reason || "");
    setRefundAmountInput("");
  };
  const closeRefund = () => {
    if (refundPending) return;
    setRefundTarget(null);
    setRefundReason("");
    setRefundAmountInput("");
  };
  const submitRefund = async () => {
    if (!refundTarget) return;
    const reason = refundReason.trim();
    if (!reason) {
      toast.error(t("admin.orders.refund-reason-required"));
      return;
    }
    // amount field is optional. If filled, parse to cents (¥X.XX → X*100).
    let amountCents: number | undefined;
    if (refundAmountInput.trim()) {
      const yuan = Number(refundAmountInput);
      if (!Number.isFinite(yuan) || yuan <= 0) {
        toast.error(t("admin.orders.refund-amount-invalid"));
        return;
      }
      amountCents = Math.round(yuan * 100);
    }
    setRefundPending(true);
    try {
      const resp = await refundOrder(
        refundTarget.order_no,
        reason,
        amountCents,
      );
      if (resp.already_refunded) {
        toast.success(
          t("admin.orders.refund-already-refunded", {
            order_no: refundTarget.order_no,
          }),
        );
      } else {
        toast.success(
          t("admin.orders.refund-success", {
            order_no: refundTarget.order_no,
          }),
        );
      }
      setRefundTarget(null);
      setRefundReason("");
      setRefundAmountInput("");
      await refresh();
    } catch (e: any) {
      toast.error(e?.message || String(e));
    } finally {
      setRefundPending(false);
    }
  };

  const hasFilters = useMemo(
    () => Boolean(statusFilter || searchApplied),
    [statusFilter, searchApplied],
  );

  return (
    <div className={`admin-orders`}>
      <Card className={`admin-card`}>
        <CardHeader className={`select-none`}>
          <CardTitle className={`flex items-center justify-between`}>
            <span>{t("admin.orders.title")}</span>
            <Button
              variant="ghost"
              size="sm"
              onClick={refresh}
              disabled={loading}
              aria-label={t("admin.orders.refresh")}
            >
              <RotateCw
                className={loading ? "h-4 w-4 animate-spin" : "h-4 w-4"}
              />
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col gap-3">
            {/* ── Filter row ───────────────────────────────────────── */}
            <div className="flex flex-row items-center gap-2 flex-wrap">
              <span className="text-sm text-muted-foreground">
                {t("admin.orders.filter-status")}
              </span>
              <Select
                value={statusFilter || "__all__"}
                onValueChange={(v) => {
                  setStatusFilter(v === "__all__" ? "" : (v as OrderStatus));
                  setOffset(0);
                }}
              >
                <SelectTrigger className="w-[200px]">
                  <SelectValue
                    placeholder={t("admin.orders.all-statuses")}
                  />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="__all__">
                    {t("admin.orders.all-statuses")}
                  </SelectItem>
                  {ALL_STATUSES.map((s) => (
                    <SelectItem key={s} value={s}>
                      {t(`admin.orders.status.${s}`)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Input
                placeholder={t("admin.orders.search-placeholder")}
                value={searchInput}
                onChange={(e) => setSearchInput(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") submitSearch();
                }}
                className="max-w-[260px]"
              />
              <Button
                variant="outline"
                size="sm"
                onClick={submitSearch}
                disabled={loading}
              >
                <Search className="h-4 w-4 mr-1" />
                {t("admin.orders.search")}
              </Button>
              {hasFilters && (
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={clearFilters}
                  disabled={loading}
                >
                  {t("admin.orders.clear-filters")}
                </Button>
              )}
              <span className="text-sm text-muted-foreground ml-auto">
                {t("admin.orders.total", { total })}
              </span>
            </div>

            {error && (
              <div className="text-sm text-destructive">
                {t("admin.orders.load-error", { error })}
                <Button
                  variant="link"
                  size="sm"
                  onClick={refresh}
                  className="ml-2"
                >
                  {t("admin.orders.retry")}
                </Button>
              </div>
            )}

            {/* ── Table ────────────────────────────────────────────── */}
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t("admin.orders.col-order-no")}</TableHead>
                  <TableHead>{t("admin.orders.col-username")}</TableHead>
                  <TableHead>{t("admin.orders.col-service")}</TableHead>
                  <TableHead>{t("admin.orders.col-status")}</TableHead>
                  <TableHead>{t("admin.orders.col-amount")}</TableHead>
                  <TableHead>{t("admin.orders.col-provider")}</TableHead>
                  <TableHead>{t("admin.orders.col-created")}</TableHead>
                  <TableHead className="text-right">
                    {t("admin.orders.col-actions")}
                  </TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 && !loading && (
                  <TableRow>
                    <TableCell
                      colSpan={8}
                      className="text-center text-muted-foreground"
                    >
                      {hasFilters
                        ? t("admin.orders.empty-filtered")
                        : t("admin.orders.empty")}
                    </TableCell>
                  </TableRow>
                )}
                {rows.map((row) => {
                  // Refund button visible for any non-pending status
                  // (pending = nothing to refund — they didn't pay yet).
                  // The backend further blocks refunds on terminal 'failed'
                  // with a 409; we surface that error via toast if it
                  // happens.
                  const canMarkPaid = row.status === "pending_payment";
                  const canRefund =
                    row.status !== "pending_payment" &&
                    row.status !== "refunded";
                  return (
                    <TableRow key={row.order_no}>
                      <TableCell className="font-mono text-xs">
                        {row.order_no}
                      </TableCell>
                      <TableCell>
                        {row.username ? (
                          <span>{row.username}</span>
                        ) : (
                          <span className="text-muted-foreground italic">
                            #{row.coai_user_id}
                          </span>
                        )}
                      </TableCell>
                      <TableCell>{row.service_name}</TableCell>
                      <TableCell>
                        <Badge variant={statusBadgeVariant(row.status)}>
                          {t(`admin.orders.status.${row.status}`)}
                        </Badge>
                      </TableCell>
                      <TableCell>{row.price_display_cny}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {row.payment_provider}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">
                        {row.created_at}
                      </TableCell>
                      <TableCell className="text-right">
                        {canMarkPaid && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => openMarkPaid(row)}
                          >
                            <CheckCircle2 className="h-4 w-4 mr-1" />
                            {t("admin.orders.actions.mark-paid")}
                          </Button>
                        )}
                        {canRefund && (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => openRefund(row)}
                          >
                            <Undo2 className="h-4 w-4 mr-1" />
                            {t("admin.orders.actions.refund")}
                          </Button>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>

            <div className="flex flex-row items-center justify-between text-sm text-muted-foreground">
              <span>
                {t("admin.orders.page-of", {
                  page: currentPage,
                  total: totalPages,
                })}
              </span>
              <div className="flex flex-row gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset === 0 || loading}
                  onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
                >
                  {t("admin.orders.prev")}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={offset + PAGE_SIZE >= total || loading}
                  onClick={() => setOffset(offset + PAGE_SIZE)}
                >
                  {t("admin.orders.next")}
                </Button>
              </div>
            </div>
          </div>
        </CardContent>
      </Card>

      {/* ── Mark Paid confirmation ──────────────────────────────────── */}
      <Dialog
        open={markPaidTarget != null}
        onOpenChange={(open) => {
          if (!open) closeMarkPaid();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t("admin.orders.mark-paid-title", {
                order_no: markPaidTarget?.order_no ?? "",
              })}
            </DialogTitle>
            <DialogDescription>
              {t("admin.orders.mark-paid-desc")}
            </DialogDescription>
          </DialogHeader>
          {markPaidTarget && (
            <div className="flex flex-col gap-2 text-sm">
              <div>
                <span className="text-muted-foreground">
                  {t("admin.orders.col-username")}:
                </span>{" "}
                {markPaidTarget.username || `#${markPaidTarget.coai_user_id}`}
              </div>
              <div>
                <span className="text-muted-foreground">
                  {t("admin.orders.col-service")}:
                </span>{" "}
                {markPaidTarget.service_name}
              </div>
              <div>
                <span className="text-muted-foreground">
                  {t("admin.orders.col-amount")}:
                </span>{" "}
                {markPaidTarget.price_display_cny}
              </div>
            </div>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={closeMarkPaid}
              disabled={markPaidPending}
            >
              {t("admin.orders.cancel")}
            </Button>
            <Button onClick={submitMarkPaid} disabled={markPaidPending}>
              {markPaidPending && (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              )}
              {t("admin.orders.confirm-mark-paid")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ── Refund modal ────────────────────────────────────────────── */}
      <Dialog
        open={refundTarget != null}
        onOpenChange={(open) => {
          if (!open) closeRefund();
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>
              {t("admin.orders.refund-title", {
                order_no: refundTarget?.order_no ?? "",
              })}
            </DialogTitle>
            <DialogDescription>
              {t("admin.orders.refund-desc")}
            </DialogDescription>
          </DialogHeader>
          {refundTarget && (
            <div className="flex flex-col gap-3">
              <div className="text-sm flex flex-col gap-1">
                <div>
                  <span className="text-muted-foreground">
                    {t("admin.orders.col-username")}:
                  </span>{" "}
                  {refundTarget.username || `#${refundTarget.coai_user_id}`}
                </div>
                <div>
                  <span className="text-muted-foreground">
                    {t("admin.orders.col-amount")}:
                  </span>{" "}
                  {refundTarget.price_display_cny}
                </div>
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("admin.orders.refund-reason-label")}
                </label>
                <Textarea
                  value={refundReason}
                  onChange={(e) => setRefundReason(e.target.value)}
                  placeholder={t("admin.orders.refund-reason-placeholder")}
                  rows={3}
                />
              </div>
              <div className="flex flex-col gap-1">
                <label className="text-sm font-medium">
                  {t("admin.orders.refund-amount-label")}
                </label>
                <Input
                  type="number"
                  inputMode="decimal"
                  step="0.01"
                  min="0"
                  value={refundAmountInput}
                  onChange={(e) => setRefundAmountInput(e.target.value)}
                  placeholder={t("admin.orders.refund-amount-placeholder")}
                />
                <span className="text-xs text-muted-foreground">
                  {t("admin.orders.refund-amount-hint")}
                </span>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button
              variant="outline"
              onClick={closeRefund}
              disabled={refundPending}
            >
              {t("admin.orders.cancel")}
            </Button>
            <Button
              variant="destructive"
              onClick={submitRefund}
              disabled={refundPending}
            >
              {refundPending && (
                <Loader2 className="h-4 w-4 mr-2 animate-spin" />
              )}
              {t("admin.orders.confirm-refund")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}

export default AdminOrders;
