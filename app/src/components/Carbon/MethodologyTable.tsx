// v0.6 carbon — designed coefficient table for /methodology.
// Reads from the cached factors table (loaded once via getCarbonFactors).
// Keeps the methodology page in sync with what the middleware actually uses
// (single source of truth: data/carbon_factors.json embedded in the binary).

import type { FactorEntry } from "@/api/carbon.ts";
import { cn } from "@/components/ui/lib/utils.ts";

type Props = {
  factors: FactorEntry[];
  className?: string;
};

export function MethodologyTable({ factors, className }: Props) {
  if (!factors.length) {
    return (
      <p className="text-muted-foreground italic">
        Coefficient table loading…
      </p>
    );
  }

  return (
    <div className={cn("overflow-x-auto rounded-md border border-border", className)}>
      <table className="w-full text-sm tabular-nums">
        <thead>
          <tr className="bg-muted/50 text-left">
            <th className="px-4 py-2 font-medium">Model</th>
            <th className="px-4 py-2 font-medium">Region</th>
            <th className="px-4 py-2 font-medium text-right">gCO₂e / 1k tokens</th>
            <th className="px-4 py-2 font-medium">Version</th>
            <th className="px-4 py-2 font-medium">Notes</th>
          </tr>
        </thead>
        <tbody>
          {factors.map((f, i) => (
            <tr
              key={`${f.model}-${f.region}-${f.version}`}
              className={cn(
                "border-t border-border",
                i % 2 === 1 && "bg-muted/20",
              )}
            >
              <td className="px-4 py-2 font-mono text-xs">{f.model}</td>
              <td className="px-4 py-2 text-muted-foreground">{f.region}</td>
              <td className="px-4 py-2 text-right font-mono">
                {f.gco2e_per_1k_tokens.toFixed(2)}
              </td>
              <td className="px-4 py-2 text-muted-foreground text-xs">{f.version}</td>
              <td className="px-4 py-2 text-muted-foreground text-xs">
                {f.notes ?? "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
