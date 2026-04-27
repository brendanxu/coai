// v0.6 carbon — /dashboard route. Single-column editorial layout.
// Reference: Stripe Climate, Patagonia digital, Cal.com — NOT corporate dashboard.

import { useEffect } from "react";
import { Link } from "react-router-dom";
import { useDispatch, useSelector } from "react-redux";
import {
  selectCarbonSummary,
  selectCarbonSummaryLoading,
  setSummary,
  setSummaryLoading,
} from "@/store/carbon.ts";
import { getCarbonSummary } from "@/api/carbon.ts";
import { CarbonBar } from "@/components/Carbon/CarbonBar.tsx";
import { EquivalentNarrative } from "@/components/Carbon/EquivalentNarrative.tsx";
import { ShareCard } from "@/components/Carbon/ShareCard.tsx";
import { LeafIcon } from "@/components/Carbon/icons.tsx";
import { formatCO2 } from "@/components/Carbon/tier.ts";
import { Button } from "@/components/ui/button.tsx";
import { ArrowDown, ArrowUp, ArrowRight } from "lucide-react";
import type { AppDispatch } from "@/store/index.ts";
import { cn } from "@/components/ui/lib/utils.ts";

function Hero({ totalG, deltaPct, firstMonth, lastMonthG }: {
  totalG: number;
  deltaPct: number;
  firstMonth: boolean;
  lastMonthG: number;
}) {
  const isImprovement = deltaPct < 0;

  return (
    <div className="flex flex-col md:flex-row md:items-end md:gap-8">
      <div>
        <div className="text-6xl md:text-7xl font-display leading-none tabular-nums tracking-tight">
          {formatCO2(totalG)}
        </div>
        <div className="text-sm text-muted-foreground mt-3">CO₂ this month</div>
      </div>
      {!firstMonth && lastMonthG > 0 && (
        <div className="mt-4 md:mt-0 md:mb-2 flex items-center gap-1.5">
          {isImprovement ? (
            <ArrowDown size={16} className="text-[hsl(var(--success))]" />
          ) : (
            <ArrowUp size={16} className="text-[hsl(var(--gold))]" />
          )}
          <span
            className={cn(
              "tabular-nums font-medium",
              isImprovement
                ? "text-[hsl(var(--success))]"
                : "text-[hsl(var(--gold))]",
            )}
          >
            {Math.abs(deltaPct).toFixed(0)}%
          </span>
          <span className="text-muted-foreground text-sm">vs last month</span>
        </div>
      )}
      {firstMonth && (
        <div className="mt-4 md:mt-0 md:mb-2 text-sm text-muted-foreground italic">
          Just getting started
        </div>
      )}
    </div>
  );
}

function EmptyState() {
  return (
    <div className="text-center py-24">
      <LeafIcon size={48} className="mx-auto text-[hsl(var(--secondary))] mb-6" />
      <h2 className="text-2xl font-display mb-3">No carbon to report yet</h2>
      <p className="text-muted-foreground max-w-md mx-auto mb-8">
        Send your first message and we&apos;ll start estimating the impact of every
        chat — and showing you the trend.
      </p>
      <Link to="/">
        <Button>Start chatting →</Button>
      </Link>
    </div>
  );
}

export default function Dashboard() {
  const dispatch = useDispatch<AppDispatch>();
  const summary = useSelector(selectCarbonSummary);
  const loading = useSelector(selectCarbonSummaryLoading);

  useEffect(() => {
    let cancelled = false;
    dispatch(setSummaryLoading(true));
    getCarbonSummary()
      .then((s) => {
        if (!cancelled) dispatch(setSummary(s));
      })
      .catch(() => {
        if (!cancelled) dispatch(setSummaryLoading(false));
      });
    return () => {
      cancelled = true;
    };
  }, [dispatch]);

  if (loading && !summary) {
    return (
      <div className="mx-auto max-w-2xl px-6 py-12 space-y-12">
        <div className="h-24 w-48 bg-muted/40 rounded animate-pulse" />
        <div className="h-6 w-64 bg-muted/30 rounded animate-pulse" />
        <div className="h-32 bg-muted/20 rounded animate-pulse" />
      </div>
    );
  }

  if (!summary || summary.total_g === 0) {
    return (
      <div className="mx-auto max-w-2xl px-6 py-16">
        <h1 className="font-display text-3xl mb-2">{summary?.month ?? "Carbon"}</h1>
        <EmptyState />
      </div>
    );
  }

  const maxPct = Math.max(...summary.by_model.map((m) => m.pct), 1);

  return (
    <div className="mx-auto max-w-2xl px-6 py-12 space-y-12">
      {/* Page header */}
      <div className="text-sm text-muted-foreground">
        <Link to="/" className="hover:underline">greentokey</Link>
        <span className="mx-1">›</span>
        <span>carbon report</span>
      </div>
      <h1 className="font-display text-3xl tracking-tight">{summary.month}</h1>

      {/* Hero number */}
      <Hero
        totalG={summary.total_g}
        deltaPct={summary.delta_pct}
        firstMonth={summary.first_month}
        lastMonthG={summary.last_month_g}
      />

      {/* By-model breakdown — only if multiple models */}
      {summary.by_model.length > 1 && (
        <section>
          <h2 className="font-display text-xl mb-3 text-muted-foreground">By model</h2>
          <div>
            {summary.by_model.map((m) => (
              <CarbonBar
                key={m.model}
                model={m.model}
                g={m.g}
                pct={m.pct}
                maxPct={maxPct}
              />
            ))}
          </div>
        </section>
      )}

      {/* Equivalent narrative */}
      <EquivalentNarrative co2g={summary.total_g} />

      {/* Actions */}
      <div className="flex flex-wrap items-center gap-3">
        <ShareCard summary={summary} />
        <Link to="/methodology">
          <Button variant="ghost" className="gap-1">
            Methodology
            <ArrowRight size={14} />
          </Button>
        </Link>
      </div>

      {/* Footer disclaimer */}
      <div className="border-t border-border pt-6 text-xs text-muted-foreground tabular-nums leading-relaxed">
        Estimate ±{summary.error_margin_pct}%. Coefficient v{summary.coefficient_version}.{" "}
        <Link to="/methodology" className="underline hover:no-underline">
          See methodology
        </Link>
        .
      </div>
    </div>
  );
}
