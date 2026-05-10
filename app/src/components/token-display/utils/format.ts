/**
 * Number formatting helpers.
 * Copied from sandbox `experiments/token-display/src/components/token-display/utils/format.ts`.
 *
 * Aligned with Claude Design 2026-05-08:
 *   - Tokens shorthand starts at 1k, with extra precision in the 1-10k band.
 *   - Cost decimals adapt: 4dp under ¥0.01, 3dp under ¥1, 2dp from ¥1 up.
 */

import { tokens } from "../design-tokens";

const f = tokens.format;

export function formatTokens(n: number): string {
  if (!Number.isFinite(n) || n < 0) return "0";
  const rounded = Math.round(n);
  if (rounded < f.tokenShorthandThreshold) {
    return String(rounded);
  }
  const decimals = rounded < f.tokenSingleDecimalThreshold ? 2 : 1;
  return (rounded / 1000).toFixed(decimals).replace(/\.?0+$/, "") + "k";
}

export function formatCost(n: number, currency = "¥"): string {
  if (!Number.isFinite(n)) return `${currency}0.00`;
  const decimals =
    n < 0.01
      ? f.costDecimalSmall
      : n < 1
      ? f.costDecimalMid
      : f.costDecimalLarge;
  return `${currency}${n.toFixed(decimals)}`;
}

export function formatPercent(rate: number): string {
  if (!Number.isFinite(rate)) return "0%";
  const clamped = Math.max(0, Math.min(1, rate));
  return `${Math.round(clamped * 100)}%`;
}
