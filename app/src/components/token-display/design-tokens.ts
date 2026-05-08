/**
 * Behavioural tokens for the token-display component.
 *
 * This file is COPIED from the sandbox at
 * `experiments/token-display/src/design-tokens.ts` (2026-05-08).
 * Keep in sync with the sandbox version.
 *
 * Visual constants (colors, spacing, fonts, motion durations) live in
 * `./styles.css` under the `.gtk-token-meter` scope, mirrored as
 * `--dur-*` / `--tick-rate-ms` CSS custom properties. Update both
 * sides together when tweaking timings.
 */

export const tokens = {
  animation: {
    tickRateMs: 60,
    catchupMs: 500,
    walkbackMs: 200,
    flashMs: 120,
    flashHoldMs: 240,
  },

  format: {
    tokenShorthandThreshold: 1_000,
    tokenSingleDecimalThreshold: 10_000,
    costDecimalSmall: 4,
    costDecimalMid: 3,
    costDecimalLarge: 2,
  },

  streaming: {
    defaultInputRatePerSec: 600,
    todayWarnRatio: 0.9,
  },
} as const;

export type Tokens = typeof tokens;
