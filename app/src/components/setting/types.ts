/**
 * Shared types for the token management UI.
 * These mirror the Go structs in newapi/admin_tokens.go.
 *
 * PKG-A-3 Wave 2
 */

/** MaskedToken — returned by GET /api/gtk/v1/tokens (list) */
export interface MaskedToken {
  id: number;
  user_id: number;
  name: string;
  /** Masked form: "sk-tnx-..." or similar prefix + "***" + last4 */
  key: string;
  /** 1 = active, 2 = revoked/disabled, 3 = expired */
  status: number;
  remain_quota: number;
  unlimited_quota: boolean;
  expired_time: number; // unix seconds; -1 = never
}

/** CreatedToken — returned by POST /api/gtk/v1/tokens (one-time plaintext) */
export interface CreatedToken {
  id: number;
  name: string;
  /** Full plaintext sk-tnx-xxx. Must not be stored; shown once then discarded. */
  key: string;
  status: number;
  remain_quota: number;
  expired_time: number;
  created_at: string;
  one_time_key: boolean;
}

/** UsageByModel — one row in TokenUsage.by_model */
export interface UsageByModel {
  model_id: string;
  total_calls: number;
  input_tokens: number;
  output_tokens: number;
  credits_used: number;
}

/** TokenUsage — returned by GET /api/gtk/v1/tokens/:id/usage */
export interface TokenUsage {
  token_id: number;
  total_calls: number;
  total_tokens_used: number;
  input_tokens: number;
  output_tokens: number;
  by_model: UsageByModel[];
}
