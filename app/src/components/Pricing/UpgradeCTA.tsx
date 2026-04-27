/**
 * UpgradeCTA — the upsell button for greentokey's BYOK Starter plan.
 *
 * Behavior:
 *   level === 0 (no subscription) → renders "Upgrade to Starter — $15/mo"
 *   level >  0 (paid)             → renders "Plan: Starter — until <date>"
 *
 * On click (when level === 0):
 *   1. GET /api/payment/checkout → backend returns LS URL with user_id
 *   2. useCheckout() opens lemon.js overlay; F1 fallback: full redirect
 *
 * MOUNT GUIDE (post-v0.6.a4 follow-up):
 *   This component is self-contained but NOT yet wired into a route. To
 *   make it reachable for QA, add a /pricing route in app/src/router.tsx:
 *
 *     {
 *       id: "pricing",
 *       path: "pricing",
 *       element: <Suspense><Pricing /></Suspense>,
 *     }
 *
 *   ... where Pricing is a lightweight route file that renders <UpgradeCTA />.
 *   Router.tsx is shared with window B (carbon) so we'll merge the route
 *   add at PR-merge time, not in this commit.
 */

import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useSelector } from "react-redux";
import { toast } from "sonner";
import { Sparkles } from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import { getLemonSqueezyCheckoutURL } from "@/api/payment.ts";
import { useCheckout } from "@/components/Pricing/CheckoutModal.tsx";
import {
  expiredAtSelector,
  levelSelector,
} from "@/store/subscription.ts";

// Plan price displayed inline. Server-side variant_id determines what the
// user actually pays at LS checkout, so this is purely a label.
const STARTER_PRICE_LABEL = "$15/mo";

type UpgradeCTAProps = {
  /** Optional className passthrough for layout containers. */
  className?: string;
};

export default function UpgradeCTA({ className }: UpgradeCTAProps) {
  const { t } = useTranslation();
  const { openCheckout } = useCheckout();
  const level = useSelector(levelSelector);
  const expiredAt = useSelector(expiredAtSelector);
  const [busy, setBusy] = useState(false);
  const [checkoutUrl, setCheckoutUrl] = useState<string | null>(null);

  // Pre-fetch the URL once the user lands on this view so the click is
  // instant — saves ~200ms vs. on-click fetch.
  useEffect(() => {
    if (level !== 0) return;
    let cancelled = false;
    (async () => {
      const url = await getLemonSqueezyCheckoutURL();
      if (!cancelled) setCheckoutUrl(url);
    })();
    return () => {
      cancelled = true;
    };
  }, [level]);

  if (level > 0) {
    return (
      <div className={className}>
        <p className="text-sm text-muted-foreground">
          {t("pricing.current-plan", "Current plan")}:&nbsp;
          <span className="font-medium">Starter</span>
          {expiredAt && (
            <>
              &nbsp;·&nbsp;
              {t("pricing.until", "until")}&nbsp;
              <span className="font-mono">{formatDate(expiredAt)}</span>
            </>
          )}
        </p>
      </div>
    );
  }

  const handleClick = async () => {
    if (busy) return;
    setBusy(true);
    try {
      // Use prefetched URL when available; refetch otherwise.
      const url = checkoutUrl ?? (await getLemonSqueezyCheckoutURL());
      if (!url) {
        toast.error(
          t(
            "pricing.checkout-failed",
            "Couldn't open checkout. Please try again in a moment.",
          ),
        );
        return;
      }
      await openCheckout(url);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={className}>
      <Button onClick={handleClick} disabled={busy} size="lg">
        <Sparkles className="mr-2 h-4 w-4" />
        {t("pricing.upgrade", "Upgrade to Starter")}
        <span className="ml-2 text-muted-foreground">{STARTER_PRICE_LABEL}</span>
      </Button>
    </div>
  );
}

/**
 * Format the redux store's `expired_at` (string, e.g. "2026-05-27 12:00:00")
 * as YYYY-MM-DD. Falls back to empty string on unparseable input — silent
 * is fine here, the date is decorative.
 */
function formatDate(value: string): string {
  if (!value) return "";
  const d = new Date(value);
  if (Number.isNaN(d.getTime())) return "";
  return d.toISOString().slice(0, 10);
}
