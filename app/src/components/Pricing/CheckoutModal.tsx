/**
 * CheckoutModal — opens the LemonSqueezy Checkout overlay (lemon.js SDK)
 * for a given URL, with a redirect fallback if the SDK fails to load.
 *
 * Failure modes covered (per plan-eng-review F1):
 *   1. lemon.js script blocked by CSP / network → wait 2s, then redirect.
 *   2. lemon.js loaded but window.createLemonSqueezy() not invokable → redirect.
 *   3. User has JS disabled — N/A (whole app is React, JS-required).
 *
 * IMPORTANT: This component does not RENDER a button. It exposes
 * `openCheckout(url)` via a hook. Callers (UpgradeCTA) trigger it imperatively
 * from their click handler. This separation keeps the script-loading concern
 * out of the visual component.
 */

import { useCallback, useEffect, useRef } from "react";

const LS_SDK_URL = "https://app.lemonsqueezy.com/js/lemon.js";
const LS_LOAD_TIMEOUT_MS = 2000;

declare global {
  interface Window {
    createLemonSqueezy?: () => void;
    LemonSqueezy?: {
      Setup?: (opts: { eventHandler?: (e: { event: string }) => void }) => void;
      Url?: { Open: (url: string) => void };
    };
  }
}

/**
 * Inject lemon.js into <head> if not already present. Resolves when the
 * script's onload fires; rejects on error or after timeout.
 */
function loadLemonSqueezyScript(): Promise<void> {
  return new Promise((resolve, reject) => {
    if (typeof window === "undefined") {
      reject(new Error("no window (SSR?)"));
      return;
    }
    if (window.LemonSqueezy?.Url?.Open) {
      resolve();
      return;
    }
    const existing = document.querySelector(
      `script[src="${LS_SDK_URL}"]`,
    ) as HTMLScriptElement | null;
    if (existing) {
      existing.addEventListener("load", () => resolve(), { once: true });
      existing.addEventListener("error", () => reject(new Error("script error")), {
        once: true,
      });
      return;
    }
    const tag = document.createElement("script");
    tag.src = LS_SDK_URL;
    tag.async = true;
    tag.defer = true;
    tag.onload = () => {
      // lemon.js auto-initializes window.LemonSqueezy on first DOM ready,
      // but call createLemonSqueezy() explicitly in case the page already loaded.
      try {
        window.createLemonSqueezy?.();
      } catch {
        /* swallow — fallback path will catch it */
      }
      resolve();
    };
    tag.onerror = () => reject(new Error("failed to load lemon.js"));
    document.head.appendChild(tag);
  });
}

/**
 * useCheckout exposes a single imperative function. Returns:
 *   openCheckout(url): triggers overlay, falls back to full-page redirect
 *
 * Usage:
 *   const { openCheckout } = useCheckout();
 *   <Button onClick={() => openCheckout(checkoutUrl)}>Upgrade</Button>
 */
export function useCheckout() {
  const loadingRef = useRef(false);

  // Eager-load the script the moment the component using this hook mounts.
  // Cuts the perceived latency between click and overlay appearing.
  useEffect(() => {
    loadLemonSqueezyScript().catch(() => {
      // Swallow — open() falls back to redirect anyway.
    });
  }, []);

  const openCheckout = useCallback(async (url: string) => {
    if (!url) return;

    // Guard against double-click while we wait for the script.
    if (loadingRef.current) return;
    loadingRef.current = true;

    const fallback = () => {
      window.location.href = url;
    };

    try {
      // Race lemon.js readiness against our timeout.
      await Promise.race([
        loadLemonSqueezyScript(),
        new Promise((_, reject) =>
          setTimeout(() => reject(new Error("lemon.js timeout")), LS_LOAD_TIMEOUT_MS),
        ),
      ]);

      const open = window.LemonSqueezy?.Url?.Open;
      if (typeof open !== "function") {
        // F1: SDK loaded but the API surface isn't what we expect — redirect.
        fallback();
        return;
      }
      open(url);
    } catch (e) {
      console.debug("[payment] lemon.js unavailable, redirecting", e);
      fallback();
    } finally {
      loadingRef.current = false;
    }
  }, []);

  return { openCheckout };
}
