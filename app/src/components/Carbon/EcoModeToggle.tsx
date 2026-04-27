// v0.6 carbon — Eco Mode toggle button.
// Icon-only toggle that lives left of the send button in ChatWrapper.
// Anti-pattern guard: framed as "smart routing", NOT virtue. Tooltip emphasizes
// quality + savings, not "save the planet" copy.
//
// Click           = toggle Eco Mode (persistent, server-stored)
// Long-press 600ms = "Eco off this turn" (60s override; X-Eco-Override: off)

import { useEffect, useRef, useState } from "react";
import { useDispatch, useSelector } from "react-redux";
import {
  selectEcoMode,
  selectEcoOverrideActive,
  setEcoMode as setEcoModeAction,
  setEcoOverride,
  clearEcoOverride,
} from "@/store/carbon.ts";
import { setEcoMode as setEcoModeAPI, getEcoMode } from "@/api/carbon.ts";
import axios from "axios";
import { LeafIcon } from "./icons.tsx";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip.tsx";
import { toast } from "sonner";
import { cn } from "@/components/ui/lib/utils.ts";
import type { AppDispatch } from "@/store/index.ts";

const LONG_PRESS_MS = 600;
const OVERRIDE_TTL_MS = 60_000;

export function EcoModeToggle({ className }: { className?: string }) {
  const dispatch = useDispatch<AppDispatch>();
  const ecoMode = useSelector(selectEcoMode);
  const overrideActive = useSelector(selectEcoOverrideActive);
  const longPressTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const longPressedRef = useRef(false);
  const [busy, setBusy] = useState(false);

  // Bootstrap once: read server-side prefs into Redux
  useEffect(() => {
    let cancelled = false;
    getEcoMode()
      .then((p) => {
        if (!cancelled) dispatch(setEcoModeAction(p.eco_mode));
      })
      .catch(() => {/* silently — toggle still works locally */});
    return () => {
      cancelled = true;
    };
  }, [dispatch]);

  // Auto-clear override when its TTL window passes (cosmetic — selector
  // already gates on Date.now() but we want re-render so the tooltip text
  // updates from "off this turn" → normal)
  useEffect(() => {
    if (!overrideActive) return;
    const id = setTimeout(() => dispatch(clearEcoOverride()), OVERRIDE_TTL_MS);
    return () => clearTimeout(id);
  }, [overrideActive, dispatch]);

  // Sync axios default header so every chat completion request carries the
  // X-Eco-Override flag while the override window is open. Backend's
  // ChatRelayAPI reads this header (see manager/chat_completions.go b7).
  useEffect(() => {
    if (overrideActive) {
      axios.defaults.headers.common["X-Eco-Override"] = "off";
    } else {
      delete axios.defaults.headers.common["X-Eco-Override"];
    }
  }, [overrideActive]);

  const handleToggle = async () => {
    if (busy) return;
    if (longPressedRef.current) {
      longPressedRef.current = false;
      return; // long-press fired — don't also toggle
    }
    setBusy(true);
    const next = !ecoMode;
    dispatch(setEcoModeAction(next));
    try {
      await setEcoModeAPI(next);
    } catch {
      // Revert on error + tell user
      dispatch(setEcoModeAction(!next));
      toast.error("Couldn't save Eco Mode preference");
    } finally {
      setBusy(false);
    }
  };

  const startLongPress = () => {
    if (!ecoMode) return; // long-press only meaningful when ON
    longPressedRef.current = false;
    longPressTimer.current = setTimeout(() => {
      longPressedRef.current = true;
      dispatch(setEcoOverride({ ms: OVERRIDE_TTL_MS }));
      toast("Eco off for the next chat (60s)", { duration: 3000 });
    }, LONG_PRESS_MS);
  };

  const cancelLongPress = () => {
    if (longPressTimer.current) {
      clearTimeout(longPressTimer.current);
      longPressTimer.current = null;
    }
  };

  const stateLabel = overrideActive
    ? "Eco off for this turn"
    : ecoMode
      ? "Eco Mode on"
      : "Eco Mode off";

  return (
    <TooltipProvider delayDuration={200}>
      <Tooltip>
        <TooltipTrigger asChild>
          <button
            type="button"
            onClick={handleToggle}
            onPointerDown={startLongPress}
            onPointerUp={cancelLongPress}
            onPointerLeave={cancelLongPress}
            disabled={busy}
            aria-pressed={ecoMode}
            aria-label={stateLabel}
            className={cn(
              "inline-flex h-8 w-8 items-center justify-center rounded-md border transition-colors",
              "focus:outline-none focus:ring-2 focus:ring-[hsl(var(--ring))]",
              ecoMode && !overrideActive
                ? "bg-[hsl(var(--accent)/0.25)] border-[hsl(var(--primary))] text-[hsl(var(--primary))]"
                : "border-border text-muted-foreground hover:bg-accent hover:text-accent-foreground",
              overrideActive && "opacity-60",
              busy && "cursor-wait",
              className,
            )}
          >
            <LeafIcon size={16} />
          </button>
        </TooltipTrigger>
        <TooltipContent side="top" className="max-w-xs">
          <div className="text-xs leading-relaxed space-y-1">
            <p className="font-medium">{stateLabel}</p>
            <p className="text-muted-foreground">
              Routes to smaller models when quality difference is acceptable.
              Saves ~85% CO<sub>2</sub> on average and bills cheaper too.
            </p>
            {ecoMode && !overrideActive && (
              <p className="text-muted-foreground italic">
                Long-press = off for one chat
              </p>
            )}
          </div>
        </TooltipContent>
      </Tooltip>
    </TooltipProvider>
  );
}
