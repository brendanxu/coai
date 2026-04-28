// v0.7-design — claudo-style banner with accent-soft tint background.
// Shows welcome + this-month-overview + carbon vs last month delta.

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
  const showDelta = summary && !isFirstMonth && summary.last_month_g > 0;

  return (
    <section
      className="grid grid-cols-1 md:grid-cols-[2fr_1fr_1fr] gap-6 md:gap-8 items-center"
      style={{
        padding: "28px 32px",
        background: "hsl(var(--accent-soft))",
        color: "hsl(var(--primary-deep))",
        borderRadius: "var(--radius-card-sm)",
      }}
    >
      {/* Lead — welcome + tagline */}
      <div>
        <div
          className="font-mono text-[11px] uppercase tracking-[0.18em] mb-2"
          style={{ color: "hsl(var(--primary-deep) / 0.6)" }}
        >
          This month
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
          {t("dashboard.welcome-back")}
        </h2>
        <p
          className="text-sm mt-1.5"
          style={{ color: "hsl(var(--primary-deep) / 0.7)" }}
        >
          {t("dashboard.this-month-overview")}
        </p>
      </div>

      {/* Carbon stat */}
      {summary && (
        <div className="flex items-baseline gap-2">
          <LeafIcon size={16} />
          <span
            className="font-display tabular-nums"
            style={{
              fontSize: "1.5rem",
              fontWeight: 700,
              letterSpacing: "-0.01em",
            }}
          >
            {formatCO2(summary.total_g)}
          </span>
          <span
            className="text-xs"
            style={{ color: "hsl(var(--primary-deep) / 0.65)" }}
          >
            {t("dashboard.co2-this-month")}
          </span>
        </div>
      )}

      {/* Delta */}
      {showDelta && (
        <div
          className={cn(
            "inline-flex items-center gap-1.5 tabular-nums text-sm font-medium",
          )}
          style={{
            color: isImprovement
              ? "hsl(var(--primary))"
              : "hsl(var(--gold))",
          }}
          title={t("dashboard.vs-last-month")}
        >
          {isImprovement ? (
            <ArrowDown size={14} aria-hidden="true" />
          ) : (
            <ArrowUp size={14} aria-hidden="true" />
          )}
          <span>
            {Math.abs(summary.delta_pct).toFixed(0)}%
          </span>
          <span
            className="text-xs"
            style={{ color: "hsl(var(--primary-deep) / 0.65)" }}
          >
            {t("dashboard.vs-last-month")}
          </span>
        </div>
      )}
    </section>
  );
}
