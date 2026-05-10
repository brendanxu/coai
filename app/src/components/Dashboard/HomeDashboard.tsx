// PKG-N3 (v0.20) — logged-in home (/) dashboard, reoriented for 民宿 SaaS.
//
// History:
//   v0.6.1 — first home dashboard (chat-first → service-first).
//   v0.6.1.1 — removed redundant Chat Playground card.
//   v0.20 PKG-N3 (this file) — full reorient. The pre-pivot dashboard
//     surfaced "AI 报税 / 自动剪辑 — coming soon" cards, which are stale
//     pre-pivot-v4 placeholder products. For a 民宿 customer who paid
//     ¥1980 they had nothing about their actual purchase. Audit called
//     this a TRUST GAP.
//
// New layout (top → bottom):
//   1. Welcome banner — greeting + 民宿 positioning. No carbon stat (the
//      /dashboard route still hosts the dedicated carbon report).
//   2. Active orders summary — calls listMyOrders() and shows up to 3
//      most-recent orders inline, with a prominent "查看全部订单 →" CTA
//      to /orders. Loading / empty / error states are handled.
//   3. Product phase cards — the real 民宿 SaaS roadmap (Phase 1.A 内容
//      生成 = live; 1.B 自动发布 = soon; 1.C 互动管理 = planned; 1.D
//      ROI 归因 = planned). No more 报税 / 剪辑 placeholders.
//   4. Support card — WeChat contact (image asset is a TODO; until it
//      lands we show a placeholder card with founder email).
//
// File named HomeDashboard.tsx to avoid collision with routes/Dashboard.tsx
// (the v0.6 carbon report at /dashboard, which is unchanged).

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { useSelector } from "react-redux";
import {
  ArrowRight,
  PenTool,
  Send,
  MessageCircle,
  TrendingUp,
  Loader2,
  PackageOpen,
  AlertCircle,
  MessageSquare,
  Mail,
} from "lucide-react";
import { Button } from "@/components/ui/button.tsx";
import { Badge } from "@/components/ui/badge.tsx";
import { selectUsername } from "@/store/auth.ts";
import {
  listMyOrders,
  type CustomerOrderSummary,
  type OrderStatus,
} from "@/api/orders.ts";

// ──────────────────────────────────────────────────────────────────────
// Constants — service phase definitions. Static, no backend.
// Source of truth: docs/strategy/2026-04-30-pivot-v4-民宿-saas.md
// ──────────────────────────────────────────────────────────────────────

type PhaseStatus = "live" | "soon" | "planned";

type PhaseDef = {
  id: string;
  icon: React.ComponentType<{ className?: string }>;
  titleKey: string;
  descKey: string;
  status: PhaseStatus;
};

const PHASES: PhaseDef[] = [
  {
    id: "1a",
    icon: PenTool,
    titleKey: "home_dashboard.phase.content.title",
    descKey: "home_dashboard.phase.content.desc",
    status: "live",
  },
  {
    id: "1b",
    icon: Send,
    titleKey: "home_dashboard.phase.publish.title",
    descKey: "home_dashboard.phase.publish.desc",
    status: "soon",
  },
  {
    id: "1c",
    icon: MessageCircle,
    titleKey: "home_dashboard.phase.engage.title",
    descKey: "home_dashboard.phase.engage.desc",
    status: "planned",
  },
  {
    id: "1d",
    icon: TrendingUp,
    titleKey: "home_dashboard.phase.attribution.title",
    descKey: "home_dashboard.phase.attribution.desc",
    status: "planned",
  },
];

// ──────────────────────────────────────────────────────────────────────
// Order status → badge variant. Mirrors MyOrders.tsx so the summary
// cards on this page use the exact same visual language as the /orders
// page.
// ──────────────────────────────────────────────────────────────────────

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

// ──────────────────────────────────────────────────────────────────────
// Page
// ──────────────────────────────────────────────────────────────────────

export function HomeDashboard() {
  const username = useSelector(selectUsername);

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-3xl mx-auto px-6 py-10 space-y-10">
        <WelcomeBanner username={username} />
        <ActiveOrdersSection />
        <PhaseRoadmapSection />
        <SupportCard />
      </div>
    </div>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Section 1 — Welcome banner
// ──────────────────────────────────────────────────────────────────────

function WelcomeBanner({ username }: { username: string }) {
  const { t } = useTranslation();
  // Username can be empty for the brief moment before /info hydrates.
  const greetingName = username || t("home_dashboard.welcome.fallback_name");

  return (
    <section
      style={{
        padding: "28px 32px",
        background: "hsl(var(--accent-soft))",
        color: "hsl(var(--primary-deep))",
        borderRadius: "var(--radius-card-sm)",
      }}
    >
      <div
        className="font-mono text-[11px] uppercase tracking-[0.18em] mb-2"
        style={{ color: "hsl(var(--primary-deep) / 0.6)" }}
      >
        {t("home_dashboard.welcome.eyebrow")}
      </div>
      <h2
        className="font-display"
        style={{
          fontSize: "1.625rem",
          lineHeight: 1.2,
          letterSpacing: "-0.01em",
          fontWeight: 700,
          color: "hsl(var(--primary-deep))",
        }}
      >
        {t("home_dashboard.welcome.greeting", { name: greetingName })}
      </h2>
      <p
        className="text-sm mt-1.5"
        style={{ color: "hsl(var(--primary-deep) / 0.75)" }}
      >
        {t("home_dashboard.welcome.subtitle")}
      </p>
    </section>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Section 2 — Active orders summary
// ──────────────────────────────────────────────────────────────────────

type OrdersState =
  | { kind: "loading" }
  | { kind: "ready"; orders: CustomerOrderSummary[] }
  | { kind: "error" };

function ActiveOrdersSection() {
  const { t } = useTranslation();
  const [state, setState] = useState<OrdersState>({ kind: "loading" });

  async function load() {
    setState({ kind: "loading" });
    try {
      // listMyOrders() swallows transport errors and returns []. We can't
      // distinguish "real empty" from "error" at this layer, so any
      // resolved value is treated as ready and only thrown promises hit
      // the error branch. (Future improvement: have listMyOrders() throw
      // on real errors so we can show a retry instead of a misleading
      // empty state.)
      const orders = await listMyOrders(undefined);
      // Sort defensively by created_at desc so the top-3 slice is
      // deterministic regardless of server ordering.
      const sorted = [...orders].sort((a, b) =>
        b.created_at.localeCompare(a.created_at),
      );
      setState({ kind: "ready", orders: sorted });
    } catch (e) {
      console.debug("[HomeDashboard] orders load failed", e);
      setState({ kind: "error" });
    }
  }

  useEffect(() => {
    load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  return (
    <section className="space-y-3">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
          {t("home_dashboard.orders.heading")}
        </h2>
        {state.kind === "ready" && state.orders.length > 0 && (
          <Link
            to="/orders"
            className="text-sm text-muted-foreground hover:text-foreground transition-colors inline-flex items-center gap-1"
          >
            {t("home_dashboard.orders.view_all")}
            <ArrowRight size={14} />
          </Link>
        )}
      </div>

      {state.kind === "loading" && <OrdersLoading />}
      {state.kind === "error" && <OrdersError onRetry={load} />}
      {state.kind === "ready" && state.orders.length === 0 && <OrdersEmpty />}
      {state.kind === "ready" && state.orders.length > 0 && (
        <ul className="space-y-2.5">
          {state.orders.slice(0, 3).map((o) => (
            <OrderSummaryCard key={o.order_no} order={o} />
          ))}
        </ul>
      )}
    </section>
  );
}

function OrdersLoading() {
  return (
    <div
      className="rounded-lg border border-border bg-card p-8 flex justify-center"
      aria-busy="true"
    >
      <Loader2 size={20} className="animate-spin text-muted-foreground" />
    </div>
  );
}

function OrdersError({ onRetry }: { onRetry: () => void }) {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-border bg-card p-5 flex items-start gap-3">
      <AlertCircle
        size={18}
        className="text-destructive shrink-0 mt-0.5"
        aria-hidden="true"
      />
      <div className="flex-1">
        <div className="text-sm font-medium">
          {t("home_dashboard.orders.error_title")}
        </div>
        <div className="text-xs text-muted-foreground mt-0.5">
          {t("home_dashboard.orders.error_body")}
        </div>
      </div>
      <Button variant="outline" size="sm" onClick={onRetry}>
        {t("home_dashboard.orders.retry")}
      </Button>
    </div>
  );
}

function OrdersEmpty() {
  const { t } = useTranslation();
  return (
    <div className="rounded-lg border border-dashed border-border bg-card p-8 text-center">
      <PackageOpen
        size={28}
        className="mx-auto text-muted-foreground/60 mb-3"
        aria-hidden="true"
      />
      <div className="text-sm font-medium mb-1">
        {t("home_dashboard.orders.empty_title")}
      </div>
      <div className="text-xs text-muted-foreground mb-4 max-w-xs mx-auto">
        {t("home_dashboard.orders.empty_body")}
      </div>
      <Button variant="outline" size="sm" asChild>
        <a href="#wechat-contact">
          <MessageSquare size={14} className="mr-1.5" />
          {t("home_dashboard.orders.empty_cta")}
        </a>
      </Button>
    </div>
  );
}

function OrderSummaryCard({ order }: { order: CustomerOrderSummary }) {
  const { t } = useTranslation();
  const variant = statusBadgeVariant(order.status);
  const statusLabel = t(`my_orders.status.${order.status}`);
  // Until /orders/:order_no exists (PKG-N2), link to /orders so users
  // always have somewhere to land. Once PKG-N2 ships its detail route
  // this can be upgraded to `/orders/${order.order_no}` without
  // restructuring this card.
  return (
    <li>
      <Link
        to="/orders"
        className="block rounded-lg border border-border bg-card p-4 hover:border-foreground/20 hover:bg-card-hover transition-colors"
      >
        <div className="flex items-start justify-between gap-4">
          <div className="min-w-0 flex-1">
            <div className="flex items-center gap-2 flex-wrap">
              <span className="text-sm font-medium truncate">
                {order.service_name}
              </span>
              <Badge variant={variant}>{statusLabel}</Badge>
            </div>
            <div className="text-xs text-muted-foreground mt-1 font-mono truncate">
              {order.order_no}
            </div>
          </div>
          <div className="text-right shrink-0">
            <div className="text-sm font-medium tabular-nums">
              {order.price_display_cny}
            </div>
          </div>
        </div>
      </Link>
    </li>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Section 3 — Product phase roadmap (replaces the stale 报税/剪辑 cards)
// ──────────────────────────────────────────────────────────────────────

function PhaseRoadmapSection() {
  const { t } = useTranslation();
  return (
    <section className="space-y-3">
      <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
        {t("home_dashboard.roadmap.heading")}
      </h2>
      <div className="space-y-2.5">
        {PHASES.map((p) => (
          <PhaseCard key={p.id} def={p} />
        ))}
      </div>
    </section>
  );
}

function PhaseCard({ def }: { def: PhaseDef }) {
  const { t } = useTranslation();
  const isLive = def.status === "live";

  // Status chip styling (mirrors Marketing/ServiceCard.tsx visual rhythm
  // but tuned smaller for dashboard density).
  const chipBg = isLive
    ? "hsl(var(--accent-soft))"
    : "hsl(var(--secondary))";
  const chipFg = isLive
    ? "hsl(var(--primary-deep))"
    : "hsl(var(--muted-foreground))";
  const statusLabel = t(`home_dashboard.status.${def.status}`);

  // Live phase links to /orders (where customer manages their active
  // service). Future-soon phases are non-clickable cards — no waitlist
  // form yet because founder is doing concierge sales, not lead capture.
  const Wrapper = ({ children }: { children: React.ReactNode }) =>
    isLive ? (
      <Link
        to="/orders"
        className="block rounded-lg border border-border bg-card p-5 hover:border-foreground/20 hover:bg-card-hover transition-colors"
      >
        {children}
      </Link>
    ) : (
      <div className="rounded-lg border border-border bg-card p-5 opacity-70">
        {children}
      </div>
    );

  const Icon = def.icon;

  return (
    <Wrapper>
      <div className="flex items-start gap-4">
        <div
          className="flex items-center justify-center w-10 h-10 rounded-md shrink-0"
          style={{
            background: "hsl(var(--accent))",
            color: "hsl(var(--accent-foreground))",
          }}
          aria-hidden="true"
        >
          <Icon className="w-5 h-5" />
        </div>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap">
            <div className="font-display text-lg font-medium leading-tight">
              {t(def.titleKey)}
            </div>
            <span
              className="inline-flex items-center text-[11px] font-medium tracking-wide rounded-full px-2 py-0.5"
              style={{ background: chipBg, color: chipFg }}
            >
              {statusLabel}
            </span>
          </div>
          <div className="text-sm text-muted-foreground mt-1.5 leading-relaxed">
            {t(def.descKey)}
          </div>
        </div>
        {isLive && (
          <ArrowRight
            size={16}
            className="text-muted-foreground shrink-0 mt-2.5"
            aria-hidden="true"
          />
        )}
      </div>
    </Wrapper>
  );
}

// ──────────────────────────────────────────────────────────────────────
// Section 4 — Support card (WeChat handle + email)
// ──────────────────────────────────────────────────────────────────────
//
// TODO(PKG-N3 follow-up): Once the founder commits a real WeChat QR PNG
// to app/src/assets/wechat-qr.png we should swap the placeholder block
// for the actual <img>. For now we display the WeChat handle as text +
// founder email as the primary contact path, which matches the audit's
// guidance ("WeChat QR or contact text — pick whatever is in-codebase").
// No QR image is currently committed under app/src/assets or app/public.

function SupportCard() {
  const { t } = useTranslation();
  return (
    <section
      id="wechat-contact"
      className="rounded-lg border border-border bg-card p-5"
    >
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div className="flex items-start gap-3">
          <div
            className="flex items-center justify-center w-10 h-10 rounded-md shrink-0"
            style={{
              background: "hsl(var(--accent))",
              color: "hsl(var(--accent-foreground))",
            }}
            aria-hidden="true"
          >
            <MessageSquare className="w-5 h-5" />
          </div>
          <div className="min-w-0">
            <div className="font-display text-lg font-medium leading-tight">
              {t("home_dashboard.support.title")}
            </div>
            <div className="text-sm text-muted-foreground mt-1">
              {t("home_dashboard.support.body")}
            </div>
            <div className="text-xs text-muted-foreground mt-2 font-mono">
              {t("home_dashboard.support.wechat_label")}:{" "}
              {t("home_dashboard.support.wechat_handle")}
            </div>
          </div>
        </div>
        <Button variant="outline" size="sm" asChild className="shrink-0">
          <a href={`mailto:${t("home_dashboard.support.email")}`}>
            <Mail size={14} className="mr-1.5" />
            {t("home_dashboard.support.email_cta")}
          </a>
        </Button>
      </div>
    </section>
  );
}
