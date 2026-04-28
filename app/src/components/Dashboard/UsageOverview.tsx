// v0.6.1 — top banner on the logged-in home dashboard.
// Shows "Welcome back" + this-month-usage + carbon-vs-last-month delta.
//
// Usage count is currently a placeholder (em-dash + tooltip) until a backend
// /v1/usage/summary endpoint exists. See TODOS.md.

import { useTranslation } from "react-i18next";
import { useCarbonSummary } from "@/components/Carbon/useCarbonSummary.ts";
import { LeafIcon } from "@/components/Carbon/icons.tsx";
import { formatCO2 } from "@/components/Carbon/tier.ts";
import { ArrowDown, ArrowUp } from "lucide-react";
import { cn } from "@/components/ui/lib/utils.ts";

export function UsageOverview() {
  const { t } = useTranslation();
  const { summary } = useCarbonSummary();

  const isImprovement = summary && summary.delta_pct < 0;
  const isFirstMonth = summary?.first_month;
  const showDelta =
    summary && !isFirstMonth && summary.last_month_g > 0;

  return (
    <div className="flex flex-col sm:flex-row sm:items-end sm:justify-between gap-3">
      <div>
        <h1 className="font-display text-3xl tracking-tight">
          {t("dashboard.welcome-back")}
        </h1>
        <p className="text-sm text-muted-foreground mt-1">
          {t("dashboard.this-month-overview")}
        </p>
      </div>
      {summary && (
        <div className="flex items-center gap-2 text-sm">
          <LeafIcon
            size={14}
            className="text-[hsl(var(--primary))]"
          />
          <span className="tabular-nums font-medium">
            {formatCO2(summary.total_g)}
          </span>
          <span className="text-muted-foreground">
            {t("dashboard.co2-this-month")}
          </span>
          {showDelta && (
            <span
              className={cn(
                "flex items-center gap-0.5 ml-1 tabular-nums",
                isImprovement
                  ? "text-[hsl(var(--success))]"
                  : "text-[hsl(var(--gold))]",
              )}
              title={t("dashboard.vs-last-month")}
            >
              {isImprovement ? (
                <ArrowDown size={12} aria-hidden="true" />
              ) : (
                <ArrowUp size={12} aria-hidden="true" />
              )}
              {Math.abs(summary.delta_pct).toFixed(0)}%
            </span>
          )}
        </div>
      )}
    </div>
  );
}
