import { Message, UserRole } from "@/api/types.tsx";
import { MessageMetadata } from "./token-display/MessageMetadata";
import "./token-display/styles.css";

/**
 * Bridge between CoAI's Message shape and the sandbox MessageMetadata.
 *
 * **Preview-mode wrapper.** CoAI's current `Message` type only carries
 * `quota` (= total cost). The sandbox component shows a richer strip
 * (`model · in · out · cost · cache pill · saved pill`). Until the
 * gateway emits structured `usage` data, we MOCK the missing fields
 * so the visual review is meaningful:
 *
 *   - cost              ← `message.quota` (real)
 *   - outputTokens      ← `Math.round(message.content.length / 4)` (rough char→token)
 *   - inputTokens       ← `outputTokens × 5` (plausible chat context size)
 *   - cacheHitRate      ← 0.42 (realistic-looking value, see playground)
 *   - savedAmount       ← `cost × 0.18` (typical savings from a 42% cache hit)
 *   - modelName         ← falls back to a hardcoded Sonnet id; CoAI's Message
 *                          doesn't expose model info, but we can infer or
 *                          plug in once that lands
 *   - isStreaming       ← `message.end === false`
 *   - isLoading         ← always false (we render under the existing chat
 *                          message tree, which already gates with its own loader)
 *   - isAborted         ← always false (CoAI handles abort separately)
 *
 * Renders `null` for user messages (only assistant turns get the strip)
 * or when there's no quota and no content (nothing meaningful to show).
 *
 * Wraps everything in a `.gtk-token-meter` div so the local CSS vars +
 * BEM classes resolve from a scoped subtree, not from CoAI's :root.
 *
 * Toggle: opt-out via `VITE_TOKEN_METER_PREVIEW=false` to hide.
 */

const PREVIEW_ENABLED =
  (import.meta.env.VITE_TOKEN_METER_PREVIEW ?? "true") !== "false";

const FALLBACK_MODEL = "claude-3-5-sonnet-20241022";

interface MessageTokenMeterProps {
  message: Message;
  /** Optional explicit model id; otherwise we use the fallback. */
  modelName?: string;
}

export default function MessageTokenMeter({
  message,
  modelName,
}: MessageTokenMeterProps) {
  if (!PREVIEW_ENABLED) return null;
  if (message.role === UserRole) return null;

  const cost = message.quota ?? 0;
  // Skip if we genuinely have no signal at all. Lets the strip stay
  // hidden on system messages, errors, etc.
  if (cost === 0 && message.content.length === 0) return null;

  // Rough char-to-token ratio for English+code is ~4 chars/token; for
  // dense Chinese it's closer to 2. Use 3.5 as a compromise.
  const charsPerToken = 3.5;
  const outputTokens = Math.max(
    1,
    Math.round(message.content.length / charsPerToken),
  );
  // Chat input typically ~5x output (system prompt + context + question).
  // Pure mock until the backend wires it through.
  const inputTokens = outputTokens * 5;

  const cacheHitRate = cost > 0 ? 0.42 : undefined;
  const savedAmount = cost > 0 ? cost * 0.18 : undefined;

  const isStreaming = message.end === false;

  return (
    <div className="gtk-token-meter">
      <MessageMetadata
        modelName={modelName ?? FALLBACK_MODEL}
        inputTokens={isStreaming ? null : inputTokens}
        outputTokens={isStreaming ? null : outputTokens}
        cost={isStreaming ? null : cost}
        cacheHitRate={cacheHitRate}
        savedAmount={savedAmount}
        isStreaming={isStreaming}
      />
    </div>
  );
}
