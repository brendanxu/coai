// v0.6 carbon — sidebar monthly widget.
// Compact 2-line stat block at the top of the chat sidebar. Click → /dashboard.
//
// Data freshness: useCarbonSummary fetches once per session and refetches when
// ecoMode toggles. v0.6.1 extracted the fetch into a shared hook so the new
// dashboard MonthlyCarbonSummary can read the same redux summary without
// duplicating the fetch logic.

import { useNavigate } from "react-router-dom";
import { LeafIcon } from "./icons.tsx";
import { formatCO2 } from "./tier.ts";
import { useCarbonSummary } from "./useCarbonSummary.ts";
import { cn } from "@/components/ui/lib/utils.ts";
import { ArrowDown, ArrowUp } from "lucide-react";

type Props = { className?: string };

export function MonthlyWidget({ className }: Props) {
  const { summary, loading } = useCarbonSummary();
  const navigate = useNavigate();

  // Loading state — skeleton same dimensions
  if (loading && !summary) {
    return (
      <div
        className={cn(
          "rounded-md border border-border bg-card px-3 py-2 mb-2 select-none",
          "animate-pulse",
          className,
        )}
        aria-hidden="true"
      >
        <div className="h-5 w-20 bg-muted/60 rounded mb-1" />
        <div className="h-3 w-28 bg-muted/40 rounded" />
      </div>
    );
  }

  // Error / no data — show a soft empty state
  if (!summary) {
    return (
      <button
        type="button"
        onClick={() => navigate("/dashboard")}
        className={cn(
          "w-full text-left rounded-md border border-border bg-card hover:bg-card-hover px-3 py-2 mb-2 transition-colors cursor-pointer",
          className,
        )}
        aria-label="Carbon usage — open dashboard"
      >
        <div className="flex items-center gap-1.5 text-base font-medium leading-none">
          <LeafIcon size={14} className="text-muted-foreground" />
          <span className="text-muted-foreground">Carbon</span>
        </div>
        <div className="text-xs text-muted-foreground mt-1">Tap to view</div>
      </button>
    );
  }

  const isImprovement = summary.delta_pct < 0;
  const isFirstMonth = summary.first_month;

  return (
    <button
      type="button"
      onClick={() => navigate("/dashboard")}
      className={cn(
        "w-full text-left rounded-md border border-border bg-card hover:bg-card-hover",
        "px-3 py-2 mb-2 transition-colors cursor-pointer group",
        className,
      )}
      aria-label={`Carbon this month: ${formatCO2(summary.total_g)}, open dashboard`}
    >
      {/* Line 1: leaf + number — Newsreader/Fraunces feel via existing display font */}
      <div className="flex items-center gap-1.5 leading-none">
        <LeafIcon size={14} className="text-[hsl(var(--primary))]" />
        <span className="text-lg font-semibold tabular-nums">
          {formatCO2(summary.total_g)}
        </span>
        <span className="text-xs text-muted-foreground ml-0.5">CO₂</span>
      </div>
      {/* Line 2: delta + comparison context */}
      <div className="flex items-center gap-1 mt-1 text-xs">
        {isFirstMonth ? (
          <span className="text-muted-foreground">Just getting started</span>
        ) : summary.last_month_g === 0 ? (
          <span className="text-muted-foreground">— vs last month</span>
        ) : (
          <>
            {isImprovement ? (
              <ArrowDown
                size={12}
                className="text-[hsl(var(--success))]"
                aria-hidden="true"
              />
            ) : (
              <ArrowUp
                size={12}
                className="text-[hsl(var(--gold))]"
                aria-hidden="true"
              />
            )}
            <span
              className={cn(
                "tabular-nums font-medium",
                isImprovement
                  ? "text-[hsl(var(--success))]"
                  : "text-[hsl(var(--gold))]",
              )}
            >
              ~{Math.abs(summary.delta_pct).toFixed(0)}%
            </span>
            <span className="text-muted-foreground">vs last month</span>
          </>
        )}
      </div>
    </button>
  );
}
