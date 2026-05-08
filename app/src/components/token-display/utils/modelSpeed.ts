/**
 * Per-model defaults — output stream rate (tokens/sec) + per-token pricing.
 *
 * Copied from sandbox `experiments/token-display/src/components/token-display/utils/modelSpeed.ts`.
 * Pricing here is **illustrative**, derived from public per-1k figures.
 * Replace with telemetry once the gateway emits canonical numbers
 * (see TECHDEBT.md P0-1 / P0-2 in the sandbox).
 */

export interface ModelDefaults {
  outputStreamRatePerSec: number;
  inputStreamRatePerSec: number;
  pricePerInputToken: number;
  pricePerOutputToken: number;
}

const FAST_INPUT_RATE = 600;

export const MODEL_DEFAULTS: Readonly<Record<string, ModelDefaults>> = {
  'claude-3-5-sonnet-20241022': {
    outputStreamRatePerSec: 35,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.022 / 1000,
    pricePerOutputToken: 0.108 / 1000,
  },
  'claude-3-5-haiku-20241022': {
    outputStreamRatePerSec: 95,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.005 / 1000,
    pricePerOutputToken: 0.025 / 1000,
  },
  'claude-3-opus-20240229': {
    outputStreamRatePerSec: 22,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.108 / 1000,
    pricePerOutputToken: 0.54 / 1000,
  },
  'gpt-4o': {
    outputStreamRatePerSec: 65,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.018 / 1000,
    pricePerOutputToken: 0.072 / 1000,
  },
  'gpt-4o-mini': {
    outputStreamRatePerSec: 105,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.001 / 1000,
    pricePerOutputToken: 0.004 / 1000,
  },
  'deepseek-chat': {
    outputStreamRatePerSec: 45,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.001 / 1000,
    pricePerOutputToken: 0.002 / 1000,
  },
  default: {
    outputStreamRatePerSec: 40,
    inputStreamRatePerSec: FAST_INPUT_RATE,
    pricePerInputToken: 0.02 / 1000,
    pricePerOutputToken: 0.1 / 1000,
  },
};

export function getModelDefaults(model: string): ModelDefaults {
  return MODEL_DEFAULTS[model] ?? MODEL_DEFAULTS.default;
}

export function getModelSpeed(model: string): number {
  return getModelDefaults(model).outputStreamRatePerSec;
}
