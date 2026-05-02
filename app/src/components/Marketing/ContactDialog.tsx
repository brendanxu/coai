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
import { Label } from "@/components/ui/label.tsx";
import { submitLead, type LeadInput } from "@/api/lead.ts";

export type ContactDialogProps = {
  /** Controlled open state. */
  open: boolean;
  onOpenChange: (open: boolean) => void;
  /** Where on the site this dialog was opened from — used for analytics. */
  source?: string;
  /** Optional service slug the dialog was opened from (e.g. "publish" /
   *  "engage"). Surfaced as part of `notes` so founder follow-up knows
   *  which feature pulled the lead. */
  contextLabel?: string;
};

/**
 * 民宿主 demo 预约 / contact form. Replaces the BYOK-era email-only
 * WaitlistDialog. Captures:
 *   - 微信号 (优先 — 民宿主常用，比邮箱触达率高)
 *   - 手机号 (备选 — 万一微信不方便)
 *   - 民宿名 (可选，帮 founder follow-up 时定位)
 *   - 民宿位置 (可选)
 *   - 备注 (可选 — 房型/特色/痛点等)
 *
 * 至少填 微信 或 手机 之一。提交后清空 + 关闭 + toast 反馈。
 *
 * 服务端幂等: 同一个 微信/手机 重复提交会更新已有 lead 而不是建重复行。
 * 所以用户多次点不会造成 founder follow-up 时的重复联系问题。
 */
export default function ContactDialog({
  open,
  onOpenChange,
  source = "home",
  contextLabel,
}: ContactDialogProps) {
  const { t } = useTranslation();
  const [wechat, setWechat] = useState("");
  const [phone, setPhone] = useState("");
  const [homestayName, setHomestayName] = useState("");
  const [homestayLoc, setHomestayLoc] = useState("");
  const [notes, setNotes] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [inlineError, setInlineError] = useState<string | null>(null);

  const reset = () => {
    setWechat("");
    setPhone("");
    setHomestayName("");
    setHomestayLoc("");
    setNotes("");
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

    if (!wechat.trim() && !phone.trim()) {
      setInlineError(
        t(
          "lead.errors.no-channel",
          "请至少填写微信号或手机号 (我们用来联系你)",
        ),
      );
      return;
    }

    const fullNotes = contextLabel
      ? `[${contextLabel}] ${notes}`.trim()
      : notes;

    const payload: LeadInput = {
      wechat: wechat.trim(),
      phone: phone.trim(),
      homestay_name: homestayName.trim(),
      homestay_loc: homestayLoc.trim(),
      notes: fullNotes,
      source,
    };

    setSubmitting(true);
    try {
      const result = await submitLead(payload);
      if (result.ok) {
        toast(
          t("lead.success.title", "已收到！"),
          {
            description: result.message,
          },
        );
        handleClose(false);
        return;
      }

      // Surface server-provided message when present (already in Chinese).
      const fallback =
        result.kind === "rate-limited"
          ? t("lead.errors.rate", "请稍后再试 (1 分钟内最多 5 次)")
          : result.kind === "network"
            ? t("lead.errors.network", "网络异常，请检查连接后重试")
            : result.kind === "server"
              ? t("lead.errors.server", "服务器忙，请稍后再试")
              : t("lead.errors.invalid", "信息有误，请检查后重新提交");
      setInlineError(result.message || fallback);
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
              {t("lead.title", "预约 demo")}
              {contextLabel ? ` · ${contextLabel}` : ""}
            </DialogTitle>
            <DialogDescription>
              {t(
                "lead.desc",
                "留下你的联系方式，我们 24 小时内通过微信或电话和你聊聊你的民宿和小红书运营情况。",
              )}
            </DialogDescription>
          </DialogHeader>

          <div className="py-4 space-y-4">
            <div className="space-y-1.5">
              <Label htmlFor="contact-wechat">
                {t("lead.fields.wechat", "微信号")}
                <span className="ml-1 text-xs text-muted-foreground">
                  ({t("lead.fields.preferred", "推荐")})
                </span>
              </Label>
              <Input
                id="contact-wechat"
                autoFocus
                placeholder={t(
                  "lead.fields.wechat-ph",
                  "your-wechat-id",
                )}
                value={wechat}
                onChange={(e) => setWechat(e.target.value)}
                disabled={submitting}
                autoComplete="off"
              />
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="contact-phone">
                {t("lead.fields.phone", "手机号")}
                <span className="ml-1 text-xs text-muted-foreground">
                  ({t("lead.fields.alt", "微信不方便填这个")})
                </span>
              </Label>
              <Input
                id="contact-phone"
                type="tel"
                inputMode="numeric"
                placeholder="13800138000"
                value={phone}
                onChange={(e) => setPhone(e.target.value)}
                disabled={submitting}
                autoComplete="tel"
                maxLength={11}
              />
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div className="space-y-1.5">
                <Label htmlFor="contact-homestay">
                  {t("lead.fields.homestay", "民宿名")}
                  <span className="ml-1 text-xs text-muted-foreground">
                    ({t("lead.fields.optional", "选填")})
                  </span>
                </Label>
                <Input
                  id="contact-homestay"
                  placeholder={t(
                    "lead.fields.homestay-ph",
                    "比如「洱海花房」",
                  )}
                  value={homestayName}
                  onChange={(e) => setHomestayName(e.target.value)}
                  disabled={submitting}
                  maxLength={64}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="contact-loc">
                  {t("lead.fields.loc", "位置")}
                  <span className="ml-1 text-xs text-muted-foreground">
                    ({t("lead.fields.optional", "选填")})
                  </span>
                </Label>
                <Input
                  id="contact-loc"
                  placeholder={t(
                    "lead.fields.loc-ph",
                    "比如「大理双廊」",
                  )}
                  value={homestayLoc}
                  onChange={(e) => setHomestayLoc(e.target.value)}
                  disabled={submitting}
                  maxLength={64}
                />
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="contact-notes">
                {t("lead.fields.notes", "想了解什么 / 现在最大的痛点")}
                <span className="ml-1 text-xs text-muted-foreground">
                  ({t("lead.fields.optional", "选填")})
                </span>
              </Label>
              <textarea
                id="contact-notes"
                className="flex min-h-[80px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                placeholder={t(
                  "lead.fields.notes-ph",
                  "比如：现在每周自己写 3 篇小红书太累，效果一般",
                )}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                disabled={submitting}
                rows={3}
                maxLength={500}
              />
            </div>

            {inlineError && (
              <p className="text-sm text-destructive">{inlineError}</p>
            )}
          </div>

          <DialogFooter className="gap-2 sm:gap-0">
            <Button
              type="button"
              variant="outline"
              onClick={() => handleClose(false)}
              disabled={submitting}
            >
              {t("lead.cancel", "取消")}
            </Button>
            <Button type="submit" disabled={submitting}>
              {submitting && <Loader2 className="mr-2 w-4 h-4 animate-spin" />}
              {t("lead.submit", "提交，等我联系你")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
