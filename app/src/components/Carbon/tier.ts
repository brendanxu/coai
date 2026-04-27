// v0.6 carbon — tier classification + Tailwind class lookup.
// Boundaries MUST match backend carbon.TierFor in coai/carbon/carbon.go —
// any change here requires a matching edit there (and the unit test
// TestTierFor in carbon_test.go).

export type Tier = "low" | "mid" | "high";

export function tierFor(co2g: number): Tier {
  if (co2g < 0.5) return "low";
  if (co2g < 2.0) return "mid";
  return "high";
}

// tierClasses returns Tailwind class strings keyed off the locked palette
// in app/src/assets/globals.less. The HSL tokens are already shipped:
//   --accent (fern @ light), --secondary (sage), --gold (sun)
//   --accent-foreground (forest)  — used for text on tinted bg
//
// IMPORTANT: NO RED. Per design-consultation, tier-high uses sun (gold/amber),
// not destructive red. Guilt → churn.

export function tierClasses(tier: Tier): { bg: string; text: string; label: string } {
  switch (tier) {
    case "low":
      return {
        bg: "bg-[hsl(var(--accent)/0.25)]",            // fern @ 25%
        text: "text-[hsl(var(--accent-foreground))]",  // forest
        label: "low",
      };
    case "mid":
      return {
        bg: "bg-[hsl(var(--secondary)/0.30)]",         // sage @ 30%
        text: "text-[hsl(var(--accent-foreground))]",  // forest
        label: "mid",
      };
    case "high":
      return {
        bg: "bg-[hsl(var(--gold)/0.25)]",              // sun @ 25%
        text: "text-[hsl(var(--gold-foreground))]",    // gold-foreground
        label: "high",
      };
  }
}

// Format a CO2 value for display. Below 0.05g → "<0.1g" to avoid faux precision
// on tiny numbers. Above 1000g → kg display.
export function formatCO2(co2g: number): string {
  if (co2g <= 0) return "0g";
  if (co2g < 0.1) return "<0.1g";
  if (co2g < 1) return `${co2g.toFixed(2)}g`;
  if (co2g < 100) return `${co2g.toFixed(1)}g`;
  if (co2g < 1000) return `${Math.round(co2g)}g`;
  return `${(co2g / 1000).toFixed(2)}kg`;
}
