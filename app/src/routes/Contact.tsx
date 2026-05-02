import { useState } from "react";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import { Loader2, MessageCircle, Phone, MapPin } from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import { Label } from "@/components/ui/label.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import { submitLead, type LeadInput } from "@/api/lead.ts";

/**
 * Standalone /contact route. Same lead-capture as the modal Dialog,
 * but as a full page so it can be linked from Footer / shared via
 * 微信 / 朋友圈 / 二维码.
 *
 * Why mirror the modal: the modal converts faster (in-flow, no nav
 * away), but the standalone page is needed for:
 *   - Footer "联系" link
 *   - 微信 share previews
 *   - SEO ("greentokey 联系" search lands here)
 *   - Customers who want to think before submitting (modal feels rushed)
 */
export default function Contact() {
  const { t } = useTranslation();
  const [wechat, setWechat] = useState("");
  const [phone, setPhone] = useState("");
  const [homestayName, setHomestayName] = useState("");
  const [homestayLoc, setHomestayLoc] = useState("");
  const [notes, setNotes] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [inlineError, setInlineError] = useState<string | null>(null);
  const [submitted, setSubmitted] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (submitting) return;
    setInlineError(null);

    if (!wechat.trim() && !phone.trim()) {
      setInlineError(
        t(
          "contact.errors.no-channel",
          "请至少填写微信号或手机号 (我们用来联系你)",
        ),
      );
      return;
    }

    const payload: LeadInput = {
      wechat: wechat.trim(),
      phone: phone.trim(),
      homestay_name: homestayName.trim(),
      homestay_loc: homestayLoc.trim(),
      notes: notes.trim(),
      source: "contact-page",
    };

    setSubmitting(true);
    try {
      const result = await submitLead(payload);
      if (result.ok) {
        toast(t("contact.success.title", "已收到！"), {
          description: result.message,
        });
        setSubmitted(true);
        return;
      }
      const fallback =
        result.kind === "rate-limited"
          ? t("contact.errors.rate", "请稍后再试 (1 分钟内最多 5 次)")
          : result.kind === "network"
            ? t("contact.errors.network", "网络异常，请检查连接后重试")
            : result.kind === "server"
              ? t("contact.errors.server", "服务器忙，请稍后再试")
              : t("contact.errors.invalid", "信息有误，请检查后重新提交");
      setInlineError(result.message || fallback);
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex-1 overflow-y-auto bg-background">
      <Header />

      <main className="max-w-3xl mx-auto px-6 py-12 md:py-20">
        {/* Hero */}
        <div className="text-center mb-10 md:mb-14">
          <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
            {t("contact.eyebrow", "预约 demo")}
          </p>
          <h1 className="font-display text-3xl md:text-5xl font-medium leading-tight mb-4">
            {t("contact.heading.line1", "聊聊你的民宿，")}
            <br />
            <span className="text-[hsl(var(--gt-moss))]">
              {t("contact.heading.line2", "我们看怎么帮上你。")}
            </span>
          </h1>
          <p className="text-muted-foreground max-w-xl mx-auto">
            {t(
              "contact.lede",
              "留下联系方式 — founder 24 小时内通过微信或电话和你聊。我们会先问你民宿的现状、看你已有的小红书账号，再判断这套服务对你是否合适。不合适会直接说不合适。",
            )}
          </p>
        </div>

        {submitted ? (
          <div className="rounded-2xl border border-border/60 bg-muted/30 p-8 md:p-10 text-center space-y-3">
            <div className="text-3xl">✓</div>
            <h2 className="font-display text-xl font-medium">
              {t("contact.thanks.title", "提交成功")}
            </h2>
            <p className="text-muted-foreground text-sm">
              {t(
                "contact.thanks.body",
                "我们会在 24 小时内通过你留的联系方式与你沟通。如果想直接联系：加 founder 微信即可（聊聊你的民宿现状）。",
              )}
            </p>
            <div className="pt-2">
              <Button variant="outline" onClick={() => setSubmitted(false)}>
                {t("contact.thanks.again", "再提交一个")}
              </Button>
            </div>
          </div>
        ) : (
          <form onSubmit={handleSubmit} className="space-y-5 max-w-xl mx-auto">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="page-wechat" className="flex items-center gap-1.5">
                  <MessageCircle className="w-3.5 h-3.5" />
                  {t("contact.fields.wechat", "微信号")}
                </Label>
                <Input
                  id="page-wechat"
                  placeholder="your-wechat-id"
                  value={wechat}
                  onChange={(e) => setWechat(e.target.value)}
                  disabled={submitting}
                  autoComplete="off"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="page-phone" className="flex items-center gap-1.5">
                  <Phone className="w-3.5 h-3.5" />
                  {t("contact.fields.phone", "手机号")}
                </Label>
                <Input
                  id="page-phone"
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
            </div>
            <p className="text-xs text-muted-foreground -mt-2">
              {t(
                "contact.hint.channel",
                "至少填一个 — 微信优先（更快）",
              )}
            </p>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="page-homestay">
                  {t("contact.fields.homestay", "民宿名")}
                </Label>
                <Input
                  id="page-homestay"
                  placeholder={t("contact.fields.homestay-ph", "比如「洱海花房」")}
                  value={homestayName}
                  onChange={(e) => setHomestayName(e.target.value)}
                  disabled={submitting}
                  maxLength={64}
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="page-loc" className="flex items-center gap-1.5">
                  <MapPin className="w-3.5 h-3.5" />
                  {t("contact.fields.loc", "位置")}
                </Label>
                <Input
                  id="page-loc"
                  placeholder={t("contact.fields.loc-ph", "比如「大理双廊」")}
                  value={homestayLoc}
                  onChange={(e) => setHomestayLoc(e.target.value)}
                  disabled={submitting}
                  maxLength={64}
                />
              </div>
            </div>

            <div className="space-y-1.5">
              <Label htmlFor="page-notes">
                {t("contact.fields.notes", "想了解什么 / 现在最大的痛点")}
              </Label>
              <textarea
                id="page-notes"
                className="flex min-h-[100px] w-full rounded-md border border-input bg-background px-3 py-2 text-sm ring-offset-background placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50"
                placeholder={t(
                  "contact.fields.notes-ph",
                  "比如：现在每周自己写 3 篇小红书太累，效果一般",
                )}
                value={notes}
                onChange={(e) => setNotes(e.target.value)}
                disabled={submitting}
                rows={4}
                maxLength={500}
              />
            </div>

            {inlineError && (
              <p className="text-sm text-destructive bg-destructive/10 px-3 py-2 rounded-md">
                {inlineError}
              </p>
            )}

            <div className="pt-2">
              <Button
                type="submit"
                size="lg"
                className="w-full rounded-full"
                disabled={submitting}
              >
                {submitting && <Loader2 className="mr-2 w-4 h-4 animate-spin" />}
                {t("contact.submit", "提交，等我联系你")}
              </Button>
              <p className="text-xs text-center text-muted-foreground mt-3">
                {t(
                  "contact.disclaimer",
                  "我们只用你留的信息回复你这次咨询，不会群发短信、不会卖给第三方。",
                )}
              </p>
            </div>
          </form>
        )}
      </main>

      <Footer />
    </div>
  );
}
