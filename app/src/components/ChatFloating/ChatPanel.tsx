// v0.6.1 — slide-in chat panel mounted globally via ChatFloating.
//
// Critical design: ChatWrapper is lazy-loaded AND only mounts when the panel
// is open. This avoids the double-mount race with /chat — at most one
// ChatWrapper instance ever exists in the DOM (otherwise the popMemory("history")
// effect, getElementById("input").focus(), and ?q= deep-link processSend would
// all fire twice). See plan-eng-review notes on ChatWrapper not being multi-
// instance safe.
//
// AnimatePresence keeps the panel mounted during exit animation so the slide
// works on close. Conditional <ChatWrapper /> render means the chat boots
// only after first open and unmounts again after close completes.

import { Suspense, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { useLocation } from "react-router-dom";
import { AnimatePresence, motion } from "framer-motion";
import { X } from "lucide-react";
import { lazyFactor } from "@/utils/loader.tsx";
import { Button } from "@/components/ui/button.tsx";

const ChatWrapper = lazyFactor(
  () => import("@/components/home/ChatWrapper.tsx"),
);

type Props = {
  open: boolean;
  onClose: () => void;
};

export function ChatPanel({ open, onClose }: Props) {
  const { t } = useTranslation();
  const { pathname } = useLocation();

  // Close on Escape — chat widget convention.
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  return (
    <AnimatePresence>
      {open && (
        <>
          <motion.div
            key="chat-panel-backdrop"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.2 }}
            onClick={onClose}
            className="fixed inset-0 bg-black/30 z-40"
            aria-hidden="true"
          />
          <motion.aside
            key="chat-panel"
            role="dialog"
            aria-label={t("chat-floating.panel-label")}
            initial={{ x: "100%" }}
            animate={{ x: 0 }}
            exit={{ x: "100%" }}
            transition={{ duration: 0.28, ease: [0.22, 1, 0.36, 1] }}
            className="fixed top-0 right-0 z-50 h-full w-full max-w-md flex flex-col"
            style={{
              background: "hsl(var(--card))",
              borderTopLeftRadius: "22px",
              borderBottomLeftRadius: "22px",
              boxShadow: "-20px 0 60px rgba(43,33,24,0.10)",
            }}
            // Path tag helps debugging double-mount issues — see comment at top.
            data-route={pathname}
          >
            <div
              className="flex items-center justify-between px-5 py-4 shrink-0"
              style={{ borderBottom: "1px solid hsl(var(--border))" }}
            >
              <div
                className="font-display"
                style={{ fontSize: "15px", fontWeight: 700, letterSpacing: "-0.01em" }}
              >
                {t("chat-floating.panel-title")}
              </div>
              <Button
                variant="ghost"
                size="icon"
                onClick={onClose}
                aria-label={t("chat-floating.close")}
              >
                <X className="w-4 h-4" />
              </Button>
            </div>
            <div className="flex-1 min-h-0 flex flex-col">
              <Suspense fallback={null}>
                <ChatWrapper />
              </Suspense>
            </div>
          </motion.aside>
        </>
      )}
    </AnimatePresence>
  );
}
