/**
 * Shared types for the token-display module.
 *
 * Copied from sandbox at `experiments/token-display/src/components/token-display/types.ts`.
 * Keep in sync with the sandbox.
 */

export interface UsagePayload {
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_creation_input_tokens?: number;
  cache_read_input_tokens?: number;
  cost: number;
  saved_amount?: number;
}

export interface StreamState {
  text: string;
  usage: UsagePayload | null;
  isStreaming: boolean;
  isAborted: boolean;
  error: Error | null;
}

export interface FinishedMessage {
  id: string;
  text: string;
  usage: UsagePayload;
}

export interface SessionTotals {
  sessionTokens: number;
  sessionCost: number;
  todayCost: number;
  monthCost?: number;
  dailyBudget?: number;
}
