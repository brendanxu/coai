import { useEffect, useRef, useState } from "react";
import { useTokenCounter } from "./hooks/useTokenCounter";
import { formatTokens, formatCost, formatPercent } from "./utils/format";
import { getModelDefaults } from "./utils/modelSpeed";
import { tokens } from "./design-tokens";

/**
 * Per-message metadata strip. Visual + behaviour: Claude Design 2026-05-08.
 *
 * Copied from sandbox `experiments/token-display/src/components/token-display/MessageMetadata.tsx`.
 * Renders inside a `.gtk-token-meter` wrapper (provided by MessageTokenMeter)
 * so the BEM classes resolve their CSS vars from a scoped subtree, not :root.
 */

export interface MessageMetadataProps {
  modelName: string;
  inputTokens: number | null;
  outputTokens: number | null;
  cost: number | null;
  cacheHitRate?: number;
  savedAmount?: number;
  isStreaming: boolean;
  isLoading?: boolean;
  isAborted?: boolean;
  costOverBudget?: boolean;
  currency?: string;
  pricePerInputToken?: number;
  pricePerOutputToken?: number;
}

export function MessageMetadata({
  modelName,
  inputTokens,
  outputTokens,
  cost,
  cacheHitRate,
  savedAmount,
  isStreaming,
  isLoading = false,
  isAborted = false,
  costOverBudget = false,
  currency = "¥",
  pricePerInputToken,
  pricePerOutputToken,
}: MessageMetadataProps) {
  const defaults = getModelDefaults(modelName);
  const priceIn = pricePerInputToken ?? defaults.pricePerInputToken;
  const priceOut = pricePerOutputToken ?? defaults.pricePerOutputToken;

  const inCounter = useTokenCounter({
    realValue: inputTokens,
    isStreaming,
    streamRatePerSec: defaults.inputStreamRatePerSec,
    isAborted,
  });
  const outCounter = useTokenCounter({
    realValue: outputTokens,
    isStreaming,
    streamRatePerSec: defaults.outputStreamRatePerSec,
    isAborted,
  });

  const wasStreamingRef = useRef(isStreaming);
  const [flash, setFlash] = useState(false);
  useEffect(() => {
    const wasStreaming = wasStreamingRef.current;
    if (wasStreaming && !isStreaming && !isAborted && cost != null) {
      setFlash(true);
      const t = setTimeout(() => setFlash(false), tokens.animation.flashHoldMs);
      return () => clearTimeout(t);
    }
    wasStreamingRef.current = isStreaming;
    return undefined;
  }, [isStreaming, isAborted, cost]);

  if (isLoading) {
    return (
      <div className="meta meta--skeleton" aria-hidden>
        <span className="meta__cell">
          <span className="skel" style={{ width: 90 }} />
        </span>
        <span className="meta__cell">
          <span className="skel" style={{ width: 44 }} />
        </span>
        <span className="meta__cell">
          <span className="skel" style={{ width: 44 }} />
        </span>
        <span className="meta__cell">
          <span className="skel" style={{ width: 60 }} />
        </span>
      </div>
    );
  }

  const liveCost =
    inCounter.displayValue * priceIn + outCounter.displayValue * priceOut;
  const displayCost = !isStreaming && cost != null ? cost : liveCost;

  const showCache = !isAborted && cacheHitRate != null && cacheHitRate > 0;
  const showSaved = !isAborted && savedAmount != null && savedAmount > 0;

  return (
    <div className="meta">
      <span className="meta__cell">
        {isStreaming && <span className="stream-dot" aria-hidden />}
        <span className="meta__model">{modelName}</span>
      </span>

      <span className="meta__cell">
        <span className="meta__v">{formatTokens(inCounter.displayValue)}</span>
        <span className="meta__k">in</span>
      </span>

      <span className="meta__cell">
        <span className="meta__v">{formatTokens(outCounter.displayValue)}</span>
        <span className="meta__k">out</span>
      </span>

      <span className={"meta__cell" + (flash ? " flash" : "")}>
        <span
          className={
            "meta__v " + (costOverBudget ? "meta__v--warn" : "meta__v--cost")
          }
        >
          {formatCost(displayCost, currency)}
        </span>
        {costOverBudget && <span className="tag tag--warn">over budget</span>}
      </span>

      {showCache && (
        <span className="meta__cell">
          <span className="tag tag--cache">
            cache {formatPercent(cacheHitRate)}
          </span>
          {showSaved && (
            <span className="tag tag--saved">
              saved {formatCost(savedAmount, currency)}
            </span>
          )}
        </span>
      )}

      {isAborted && (
        <span className="meta__cell">
          <span className="tag tag--abort">中断</span>
        </span>
      )}
    </div>
  );
}
