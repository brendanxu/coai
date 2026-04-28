import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Loader2 } from "lucide-react";

import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import { joinWaitlist } from "@/api/waitlist.ts";

export type WaitlistDialogProps = {
  /** Controlled open state. */
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Service slug captured by the row that opened this dialog. */
  service: "tax-filing" | "video-editing" | "any";
  /** Display name of the service ("AI 报税") shown in the dialog title. */
  serviceTitle: string;
};

/**
 * Email capture modal for Coming-Soon services. Owns its own input state
 * and submission lifecycle; the parent only flips `open`.
 *
 * Submit semantics:
 *   - HTTP 200 + new row     → success toast, close
 *   - HTTP 200 + already on  → "已在等待列表" toast, close (it IS a success)
 *   - HTTP 400               → inline error (don't close, let user fix)
 *   - HTTP 429 / 5xx         → inline error, allow retry
 */
export default function WaitlistDialog({
  open,
  onOpenChange,
  service,
  serviceTitle,
}: WaitlistDialogProps) {
  const { t } = useTranslation();
  const [email, setEmail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [inlineError, setInlineError] = useState<string | null>(null);

  const reset = () => {
    setEmail("");
    setSubmitting(false);
    setInlineError(null);
  };

  const handleClose = (next: boolean) => {
    if (!next) reset();
    onOpenChange(next);
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setInlineError(null);
    const trimmed = email.trim();
    if (!trimmed) {
      setInlineError(t("landing.waitlist.invalid", "Please enter a valid email."));
      return;
    }
    setSubmitting(true);
    try {
      const result = await joinWaitlist(trimmed, service);
      if (result.ok) {
        toast(
          result.alreadyOnList
            ? t("landing.waitlist.dup", "已在等待列表")
            : t("landing.waitlist.ok", "已加入等待列表"),
          {
            description: t(
              "landing.waitlist.ok-desc",
              "We'll email you the moment {{name}} ships.",
              { name: serviceTitle },
            ),
          },
        );
        handleClose(false);
        return;
      }
      // Map error kinds to copy.
      const msg =
        result.kind === "invalid"
          ? t("landing.waitlist.invalid", "Please enter a valid email.")
          : result.kind === "rate-limited"
            ? t(
                "landing.waitlist.rate-limited",
                "Too many requests — please try again in a minute.",
              )
            : result.kind === "network"
              ? t(
                  "landing.waitlist.network",
                  "Network error — please check your connection.",
                )
              : t(
                  "landing.waitlist.server",
                  "Couldn't sign you up right now. Please try again.",
                );
      setInlineError(msg);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={handleClose}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={handleSubmit}>
          <DialogHeader>
            <DialogTitle>
              {t("landing.waitlist.title", "Join the waitlist")} · {serviceTitle}
            </DialogTitle>
            <DialogDescription>
              {t(
                "landing.waitlist.desc",
                "Drop your email and we'll notify you the moment this service launches.",
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="py-4 space-y-2">
            <Input
              type="email"
              autoFocus
              placeholder={t("landing.waitlist.placeholder", "you@example.com")}
              value={email}
              onChange={(e) => setEmail(e.target.value)}
              disabled={submitting}
              aria-invalid={inlineError ? true : undefined}
            />
            {inlineError && (
              <p className="text-sm text-destructive">{inlineError}</p>
            )}
          </div>

          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => handleClose(false)}
              disabled={submitting}
            >
              {t("landing.waitlist.cancel", "Cancel")}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="mr-2 w-4 h-4 animate-spin" />}
              {t("landing.waitlist.submit", "Notify me")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
