// v0.6 carbon — per-message badge.
// Appears beneath each completed AI message. Tier-colored chip + leaf icon
// + formatted CO2 + model name. Hover → tooltip with coefficient details
// and the ±20% disclaimer.
//
// IMPORTANT: do NOT render during streaming. The wiring in ChatWrapper (b20)
// passes a `streaming` flag — show a skeleton when streaming, real chip when
// done. This avoids a flash mid-completion.

import { useMemo } from "react";
import { useSelector } from "react-redux";
import {
  selectCarbonFactors,
  selectFactorList,
} from "@/store/carbon.ts";
import { estimateCO2 } from "@/api/carbon.ts";
import { tierFor, tierClasses, formatCO2 } from "./tier.ts";
import { LeafIcon } from "./icons.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip.tsx";
import { cn } from "@/components/ui/lib/utils.ts";

type Props = {
  tokens: number;
  model: string;
  streaming?: boolean;
  className?: string;
};

export function CarbonBadge({ tokens, model, streaming, className }: Props) {
  const factors = useSelector(selectFactorList);
  const factorsTable = useSelector(selectCarbonFactors);

  const estimate = useMemo(
    () => estimateCO2(tokens || 0, model, factors, factorsTable?.default_region),
    [tokens, model, factors, factorsTable?.default_region],
  );

  // Loading / streaming → skeleton same dimensions as the chip
  if (streaming || tokens <= 0) {
    return (
      <span
        className={cn(
          "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs",
          "bg-muted/40 text-transparent select-none animate-pulse",
          className,
        )}
        aria-hidden="true"
      >
        <LeafIcon size={12} />
        ~0.0g · loading
      </span>
    );
  }

  // Coefficient gap — render with honest "~?g" + tooltip
  if (!estimate.coefficientFound) {
    return (
      <TooltipProvider delayDuration={200}>
        <Tooltip>
          <TooltipTrigger asChild>
            <span
              className={cn(
                "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium cursor-help",
                "bg-muted/60 text-muted-foreground",
                className,
              )}
            >
              <LeafIcon size={12} />~?g · {model}
            </span>
          </TooltipTrigger>
          <TooltipContent side="top" className="max-w-xs">
            <p className="text-xs leading-relaxed">
              Coefficient calibrating for <code className="font-mono">{model}</code>. We
              don&apos;t guess — once the per-1k-token estimate is sourced, this badge
              will fill in.
            </p>
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
    );
  }

  const tier = tierFor(estimate.co2g);
  const classes = tierClasses(tier);
  const errorMargin = factorsTable?.error_margin_pct ?? 20;
  const region = factorsTable?.default_region ?? "default";

  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <span
            className={cn(
              "inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium cursor-help transition-colors",
              classes.bg,
              classes.text,
              className,
            )}
            data-tier={classes.label}
          >
            <LeafIcon size={12} />
            <span className="tabular-nums">{formatCO2(estimate.co2g)}</span>
            <span className="opacity-70">· {model}</span>
          </span>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-xs">
          <div className="text-xs leading-relaxed space-y-1">
            <p>
              Estimate: <span className="font-mono">{formatCO2(estimate.co2g)}</span>{" "}
              CO<sub>2</sub>e for {tokens.toLocaleString()} tokens
            </p>
            <p className="text-muted-foreground">
              Model: <span className="font-mono">{model}</span>
              {" · "}Region: <span className="font-mono">{region}</span>
              {" · "}±{errorMargin}%
            </p>
            <p className="text-muted-foreground">
              Coefficient v{estimate.version}
            </p>
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
