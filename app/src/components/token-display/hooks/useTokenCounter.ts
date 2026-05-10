import { useCallback, useEffect, useRef, useState } from "react";
import { tokens } from "../design-tokens";

/**
 * Drives one numeric counter through a 4-state machine
 * (idle → streaming → settling → done).
 *
 * Copied from sandbox `experiments/token-display/src/components/token-display/hooks/useTokenCounter.ts`.
 * See sandbox README for full behaviour spec.
 */

type Phase = "idle" | "streaming" | "settling" | "done";

export interface UseTokenCounterArgs {
  realValue: number | null;
  isStreaming: boolean;
  streamRatePerSec: number;
  isAborted?: boolean;
}

interface SettleAnchor {
  startTs: number;
  fromValue: number;
  toValue: number;
  durationMs: number;
}

export function useTokenCounter({
  realValue,
  isStreaming,
  streamRatePerSec,
  isAborted = false,
}: UseTokenCounterArgs): { displayValue: number; reset: () => void } {
  const [displayValue, setDisplayValue] = useState(0);

  // P2-4: defensively round any incoming `realValue`. Mirrors sandbox.
  const roundedReal = realValue != null ? Math.round(realValue) : null;

  const valueRef = useRef(0);
  const phaseRef = useRef<Phase>("idle");
  const streamStartTsRef = useRef<number | null>(null);
  const lastTickTsRef = useRef<number>(0);
  const settleRef = useRef<SettleAnchor | null>(null);
  const rafRef = useRef<number | null>(null);

  // P1-2: explicit reset for callers reusing this hook instance across streams.
  const reset = useCallback(() => {
    if (rafRef.current != null) {
      cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
    valueRef.current = 0;
    phaseRef.current = "idle";
    streamStartTsRef.current = null;
    lastTickTsRef.current = 0;
    settleRef.current = null;
    setDisplayValue(0);
  }, []);

  useEffect(() => {
    if (
      !isStreaming &&
      roundedReal != null &&
      (phaseRef.current === "idle" || phaseRef.current === "done") &&
      valueRef.current !== roundedReal
    ) {
      valueRef.current = roundedReal;
      phaseRef.current = "done";
      setDisplayValue(roundedReal);
    }
  }, [isStreaming, roundedReal]);

  useEffect(() => {
    if (isAborted) {
      phaseRef.current = "done";
      if (rafRef.current != null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
      return;
    }

    if (isStreaming && phaseRef.current === "idle") {
      phaseRef.current = "streaming";
      streamStartTsRef.current = performance.now();
      lastTickTsRef.current = streamStartTsRef.current;
    }

    if (
      roundedReal != null &&
      phaseRef.current === "streaming" &&
      streamStartTsRef.current != null
    ) {
      const now = performance.now();
      const current = valueRef.current;
      if (Math.round(current) === roundedReal) {
        valueRef.current = roundedReal;
        phaseRef.current = "done";
        setDisplayValue(roundedReal);
      } else {
        phaseRef.current = "settling";
        settleRef.current = {
          startTs: now,
          fromValue: current,
          toValue: roundedReal,
          durationMs:
            current < roundedReal
              ? tokens.animation.catchupMs
              : tokens.animation.walkbackMs,
        };
      }
    }

    if (phaseRef.current === "idle" || phaseRef.current === "done") {
      return;
    }

    const tickMs = tokens.animation.tickRateMs;

    const loop = (now: number) => {
      const phase = phaseRef.current;

      if (phase === "streaming") {
        if (now - lastTickTsRef.current >= tickMs) {
          const elapsedSec = (now - (streamStartTsRef.current ?? now)) / 1000;
          const projected = elapsedSec * streamRatePerSec;
          valueRef.current = projected;
          lastTickTsRef.current = now;
          const rounded = Math.round(projected);
          setDisplayValue((prev) => (prev === rounded ? prev : rounded));
        }
      } else if (phase === "settling") {
        const s = settleRef.current;
        if (s == null) {
          phaseRef.current = "done";
          rafRef.current = null;
          return;
        }
        const t = Math.min(1, (now - s.startTs) / s.durationMs);
        const eased = 1 - Math.pow(1 - t, 3);
        const v = s.fromValue + (s.toValue - s.fromValue) * eased;
        valueRef.current = v;
        const rounded = Math.round(v);
        setDisplayValue((prev) => (prev === rounded ? prev : rounded));
        if (t >= 1) {
          valueRef.current = s.toValue;
          phaseRef.current = "done";
          setDisplayValue(s.toValue);
          rafRef.current = null;
          return;
        }
      } else {
        rafRef.current = null;
        return;
      }

      rafRef.current = requestAnimationFrame(loop);
    };

    rafRef.current = requestAnimationFrame(loop);

    return () => {
      if (rafRef.current != null) {
        cancelAnimationFrame(rafRef.current);
        rafRef.current = null;
      }
    };
  }, [isStreaming, roundedReal, streamRatePerSec, isAborted]);

  return { displayValue, reset };
}
