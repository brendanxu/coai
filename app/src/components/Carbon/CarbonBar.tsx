// v0.6 carbon — single horizontal bar for the Dashboard by-model breakdown.
// Plain DOM (no chart lib) per D11 — keeps Lighthouse Performance ≥90.
import { formatCO2 } from "./tier.ts";
import { cn } from "@/components/ui/lib/utils.ts";

type Props = {
  model: string;
  g: number;
  pct: number;
  // total to scale visual width (so the longest bar fills 100%)
  maxPct?: number;
  className?: string;
};

export function CarbonBar({ model, g, pct, maxPct, className }: Props) {
  const fillPct = maxPct && maxPct > 0 ? (pct / maxPct) * 100 : pct;
  return (
    <div
      className={cn("flex items-center gap-3 py-2 text-sm", className)}
      role="row"
      aria-label={`${model}: ${formatCO2(g)} (${pct.toFixed(1)}%)`}
    >
      {/* Model label — fixed width, left-aligned */}
      <div className="w-32 truncate font-mono text-xs text-muted-foreground" title={model}>
        {model}
      </div>
      {/* Bar — sage fill, rounded right edge, smooth transition on data change */}
      <div className="relative flex-1 h-6 rounded-r-full bg-muted/40 overflow-hidden">
        <div
          className="h-full bg-[hsl(var(--primary)/0.85)] rounded-r-full transition-[width] duration-700 ease-out"
          style={{ width: `${Math.max(fillPct, 1)}%` }}
        />
      </div>
      {/* Number — right-aligned, tabular-nums so digits don't jitter */}
      <div className="w-20 text-right tabular-nums font-mono text-xs">
        {formatCO2(g)}
      </div>
      <div className="w-14 text-right tabular-nums text-xs text-muted-foreground">
        ~{pct.toFixed(0)}%
      </div>
    </div>
  );
}
