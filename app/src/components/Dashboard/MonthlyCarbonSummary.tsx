// v0.6.1 — bottom carbon card on the logged-in home dashboard.
// Larger horizontal layout vs the sidebar MonthlyWidget tile. Same data
// source (useCarbonSummary → redux), so the number always matches the
// /dashboard hero.

import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowRight, ArrowDown, ArrowUp } from "lucide-react";
import { LeafIcon } from "@/components/Carbon/icons.tsx";
import { formatCO2 } from "@/components/Carbon/tier.ts";
import { useCarbonSummary } from "@/components/Carbon/useCarbonSummary.ts";
import { Button } from "@/components/ui/button.tsx";
import { cn } from "@/components/ui/lib/utils.ts";

export function MonthlyCarbonSummary() {
  const { t } = useTranslation();
  const { summary, loading } = useCarbonSummary();

  if (loading && !summary) {
    return (
      <div
        className="rounded-lg border border-border bg-card p-5 animate-pulse"
        aria-hidden="true"
      >
        <div className="h-5 w-32 bg-muted/40 rounded mb-2" />
        <div className="h-3 w-48 bg-muted/30 rounded" />
      </div>
    );
  }

  if (!summary || summary.total_g === 0) {
    return (
      <Link
        to="/dashboard"
        className="block rounded-lg border border-border bg-card p-5 hover:bg-card-hover transition-colors group"
      >
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <LeafIcon size={18} className="text-[hsl(var(--primary))]" />
            <div>
              <div className="font-medium">
                {t("dashboard.carbon-empty-title")}
              </div>
              <div className="text-xs text-muted-foreground mt-0.5">
                {t("dashboard.carbon-empty-body")}
              </div>
            </div>
          </div>
          <ArrowRight
            size={16}
            className="text-muted-foreground group-hover:translate-x-0.5 transition-transform"
          />
        </div>
      </Link>
    );
  }

  const isImprovement = summary.delta_pct < 0;
  const isFirstMonth = summary.first_month;
  const showDelta = !isFirstMonth && summary.last_month_g > 0;

  return (
    <div className="rounded-lg border border-border bg-card p-5">
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div className="flex items-center gap-3">
          <LeafIcon size={20} className="text-[hsl(var(--primary))]" />
          <div>
            <div className="text-xs text-muted-foreground uppercase tracking-wider">
              {t("dashboard.carbon-monthly-label")}
            </div>
            <div className="flex items-baseline gap-2 mt-0.5">
              <span className="font-display text-2xl tabular-nums">
                {formatCO2(summary.total_g)}
              </span>
              <span className="text-xs text-muted-foreground">CO₂</span>
              {showDelta && (
                <span
                  className={cn(
                    "flex items-center gap-0.5 text-sm tabular-nums ml-1",
                    isImprovement
                      ? "text-[hsl(var(--success))]"
                      : "text-[hsl(var(--gold))]",
                  )}
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
          </div>
        </div>
        <Link to="/dashboard">
          <Button variant="ghost" size="sm" className="gap-1">
            {t("dashboard.carbon-cta")}
            <ArrowRight size={14} />
          </Button>
        </Link>
      </div>
    </div>
  );
}
