/**
 * UpgradeCTA — the upsell + plan-status surface for greentokey's Starter plan.
 *
 * Visible states (in priority order — first matching wins):
 *   1. NOT SUBSCRIBED  (level === 0)               → "Upgrade to Starter — $15/mo"
 *   2. PAST DUE        (ls_status === past_due)    → "Update payment method"
 *   3. ON TRIAL        (ls_status === on_trial)    → "Trial — auto-bills <date>"
 *   4. CANCELLED       (ls_status === cancelled,
 *                       still within period)        → "Cancelled — access until <date>"
 *   5. ACTIVE          (default for level > 0)     → "Plan: Starter — until <date>"
 *
 * On click (when CTA is shown):
 *   1. GET /api/payment/checkout → backend returns LS URL with user_id
 *   2. useCheckout() opens lemon.js overlay; F1 fallback: full redirect
 *   3. Toasts surface specific failure modes (auth / config / network).
 */

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSelector } from "react-redux";
import { toast } from "sonner";
import { Sparkles, Loader2, AlertCircle, Clock } from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import {
  getLemonSqueezyCheckoutURL,
  getLemonSqueezySubscription,
  type CheckoutFetchResult,
  type SubscriptionState,
} from "@/api/payment.ts";
import { useCheckout } from "@/components/Pricing/CheckoutModal.tsx";
import { levelSelector } from "@/store/subscription.ts";

const STARTER_PRICE_LABEL = "$15/mo";

type UpgradeCTAProps = {
  /** Optional className passthrough for layout containers. */
  className?: string;
};

export default function UpgradeCTA({ className }: UpgradeCTAProps) {
  const { t } = useTranslation();
  const { openCheckout } = useCheckout();
  const reduxLevel = useSelector(levelSelector);
  const [busy, setBusy] = useState(false);
  const [checkoutUrl, setCheckoutUrl] = useState<string | null>(null);
  const [sub, setSub] = useState<SubscriptionState | null>(null);

  // Fetch backend subscription state on mount + whenever the redux level
  // changes (e.g. after webhook → /subscription store refresh).
  useEffect(() => {
    let cancelled = false;
    (async () => {
      const data = await getLemonSqueezySubscription();
      if (!cancelled) setSub(data);
    })();
    return () => {
      cancelled = true;
    };
  }, [reduxLevel]);

  // Pre-fetch checkout URL only when the user is in a state where they'd click.
  useEffect(() => {
    if (!sub) return;
    const needsCheckout =
      sub.level === 0 || sub.ls_status === "past_due" || sub.ls_status === "cancelled";
    if (!needsCheckout) return;

    let cancelled = false;
    (async () => {
      const result = await getLemonSqueezyCheckoutURL();
      if (cancelled) return;
      if (result.ok) setCheckoutUrl(result.url);
      // Pre-fetch errors are silent — handleClick re-fetches with a visible toast.
    })();
    return () => {
      cancelled = true;
    };
  }, [sub]);

  // Loading skeleton during initial fetch keeps the layout from jumping.
  if (sub === null) {
    return (
      <div className={className}>
        <div className="inline-flex items-center text-sm text-muted-foreground">
          <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          {t("pricing-page.loading", "Loading plan…")}
        </div>
      </div>
    );
  }

  const handleClick = async () => {
    if (busy) return;
    setBusy(true);
    try {
      // Reuse pre-fetched URL when present; otherwise fetch with full error reporting.
      let url = checkoutUrl;
      if (!url) {
        const result = await getLemonSqueezyCheckoutURL();
        if (!result.ok) {
          surfaceCheckoutError(result, t);
          return;
        }
        url = result.url;
      }
      await openCheckout(url);
    } finally {
      setBusy(false);
    }
  };

  // ----------------------------------------------------------------------------
  // State 1: never subscribed (level === 0).
  // ----------------------------------------------------------------------------
  if (sub.level === 0) {
    return (
      <div className={className}>
        <Button onClick={handleClick} disabled={busy} size="lg">
          {busy ? (
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
          ) : (
            <Sparkles className="mr-2 h-4 w-4" />
          )}
          {t("pricing-page.upgrade", "Upgrade to Starter")}
          <span className="ml-2 text-muted-foreground">{STARTER_PRICE_LABEL}</span>
        </Button>
      </div>
    );
  }

  // ----------------------------------------------------------------------------
  // State 2: past_due — payment failed; user must update card.
  // ----------------------------------------------------------------------------
  if (sub.ls_status === "past_due") {
    return (
      <div className={className}>
        <div className="space-y-2">
          <p className="inline-flex items-center text-sm text-destructive">
            <AlertCircle className="mr-2 h-4 w-4" />
            {t(
              "pricing-page.past-due",
              "Your last payment didn't go through.",
            )}
          </p>
          <Button onClick={handleClick} disabled={busy} variant="destructive" size="sm">
            {busy && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            {t("pricing-page.update-payment", "Update payment method")}
          </Button>
        </div>
      </div>
    );
  }

  // ----------------------------------------------------------------------------
  // State 3: on_trial — show trial expiry + price after.
  // ----------------------------------------------------------------------------
  if (sub.ls_status === "on_trial") {
    return (
      <div className={className}>
        <p className="inline-flex items-center text-sm text-muted-foreground">
          <Clock className="mr-2 h-4 w-4" />
          {t("pricing-page.on-trial", "Trial")}
          {sub.renews_at && (
            <>
              &nbsp;·&nbsp;{t("pricing-page.auto-bills", "auto-bills")}&nbsp;
              <span className="font-mono">{formatDate(sub.renews_at)}</span>
              &nbsp;@ {STARTER_PRICE_LABEL}
            </>
          )}
        </p>
      </div>
    );
  }

  // ----------------------------------------------------------------------------
  // State 4: cancelled but still within paid period.
  // ----------------------------------------------------------------------------
  if (sub.ls_status === "cancelled" && sub.expired_at) {
    return (
      <div className={className}>
        <div className="space-y-2">
          <p className="text-sm text-muted-foreground">
            <span className="font-medium">{t("pricing-page.cancelled", "Cancelled")}</span>
            &nbsp;·&nbsp;
            {t("pricing-page.access-until", "access until")}&nbsp;
            <span className="font-mono">{formatDate(sub.expired_at)}</span>
          </p>
          <Button onClick={handleClick} disabled={busy} size="sm" variant="outline">
            {busy && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
            <Sparkles className="mr-2 h-4 w-4" />
            {t("pricing-page.resume", "Resume subscription")}
          </Button>
        </div>
      </div>
    );
  }

  // ----------------------------------------------------------------------------
  // State 5: active (default for paid users).
  // ----------------------------------------------------------------------------
  return (
    <div className={className}>
      <p className="text-sm text-muted-foreground">
        {t("pricing-page.current-plan", "Current plan")}:&nbsp;
        <span className="font-medium">Starter</span>
        {sub.expired_at && (
          <>
            &nbsp;·&nbsp;
            {t("pricing-page.until", "until")}&nbsp;
            <span className="font-mono">{formatDate(sub.expired_at)}</span>
          </>
        )}
      </p>
    </div>
  );
}

/**
 * Map the discriminated checkout error to a user-friendly toast. Different
 * failure modes get different copy + persistence so users know whether to
 * retry, log in again, or contact support.
 */
function surfaceCheckoutError(
  result: Extract<CheckoutFetchResult, { ok: false }>,
  t: (key: string, fallback: string) => string,
) {
  switch (result.kind) {
    case "unauthorized":
      toast.error(
        t(
          "pricing-page.error-unauthorized",
          "Your session expired. Please sign in again to upgrade.",
        ),
      );
      return;
    case "misconfigured":
      // ops issue — surface long-form so it gets reported, not just retried.
      toast.error(
        t(
          "pricing-page.error-misconfigured",
          "Checkout isn't configured yet. Please contact support.",
        ),
        { duration: 8000 },
      );
      return;
    case "network":
      toast.error(
        t(
          "pricing-page.error-network",
          "Couldn't reach our server. Check your connection and try again.",
        ),
      );
      return;
    case "unknown":
    default:
      toast.error(
        t(
          "pricing-page.error-unknown",
          "Couldn't open checkout. Please try again in a moment.",
        ),
      );
      return;
  }
}

/**
 * Format an ISO-ish date string ("2026-05-27 12:00:00" or "2026-05-27T12:00:00Z")
 * as YYYY-MM-DD using the user's local time zone. Decorative — silent fallback
 * to empty string is fine.
 */
function formatDate(value: string): string {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  // Use Intl when available for locale-aware month names in future expansion.
  return d.toISOString().slice(0, 10);
}
