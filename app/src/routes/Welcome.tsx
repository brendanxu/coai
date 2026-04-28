import { useState } from "react";
import { useTranslation } from "react-i18next";

import HeroSection from "@/components/Marketing/HeroSection.tsx";
import ServiceCard from "@/components/Marketing/ServiceCard.tsx";
import ValueProps from "@/components/Marketing/ValueProps.tsx";
import WaitlistDialog from "@/components/Marketing/WaitlistDialog.tsx";
import { ServiceIcons } from "@/components/Marketing/icons.tsx";

import router from "@/router.tsx";

/**
 * greentokey marketing landing — shown to logged-out visitors.
 *
 * v0.6.1 IA shift: chat is no longer the front door. The landing positions
 * greentokey as a service marketplace (multi-model chat live now, AI 报税 +
 * 自动剪辑 coming soon) gated by a single subscription. Dual-currency CTA
 * routes via /register?next=/pricing — actual checkout (LemonSqueezy /
 * 虎皮椒) is owned by the post-signup UpgradeCTA on /pricing, so we don't
 * duplicate that flow here. Visitors who can't pay yet leave their email on
 * a Coming-Soon service via the waitlist endpoint.
 */
function Welcome() {
  const { t } = useTranslation();
  const [waitlistOpen, setWaitlistOpen] = useState(false);
  const [waitlistService, setWaitlistService] = useState<{
    slug: "tax-filing" | "video-editing" | "any";
    title: string;
  }>({ slug: "any", title: "" });

  const goRegisterForCheckout = (currency: "cny" | "usd") => {
    router.navigate(`/register?intent=${currency}&next=/pricing`);
  };

  const goTryChat = () => router.navigate("/login");

  const openWaitlist = (
    slug: "tax-filing" | "video-editing",
    title: string,
  ) => {
    setWaitlistService({ slug, title });
    setWaitlistOpen(true);
  };

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
        <HeroSection
          onUpgradeCNY={() => goRegisterForCheckout("cny")}
          onUpgradeUSD={() => goRegisterForCheckout("usd")}
        />

        {/* Service grid — three tiles */}
        <div className="space-y-4 mb-20">
          <h2 className="text-2xl font-display font-medium text-center">
            {t("landing.services.heading", "Services")}
          </h2>
          <p className="text-center text-sm text-muted-foreground mb-6">
            {t(
              "landing.services.sub",
              "Today: chat. Soon: tax filing & auto-editing. All on one subscription.",
            )}
          </p>
          <div className="grid grid-cols-1 md:grid-cols-3 gap-6">
            <ServiceCard
              icon={ServiceIcons.chat}
              title={t("landing.services.chat.title", "多模型对话")}
              desc={t(
                "landing.services.chat.desc",
                "GPT-4o, Claude, Gemini, DeepSeek and more — pick the best model per task. Carbon shown after every reply.",
              )}
              status="live"
              statusLabel={t("landing.services.chat.status", "订阅含")}
              ctaLabel={t("landing.services.chat.cta", "试用")}
              onCta={goTryChat}
            />
            <ServiceCard
              icon={ServiceIcons["tax-filing"]}
              title={t("landing.services.tax.title", "AI 报税助手")}
              desc={t(
                "landing.services.tax.desc",
                "Personal income tax done in minutes. Reads your statements, surfaces deductions, prepares filings.",
              )}
              status="coming-soon"
              statusLabel={t("landing.services.tax.status", "Coming soon")}
              ctaLabel={t("landing.services.tax.cta", "留邮箱抢先体验")}
              onCta={() =>
                openWaitlist(
                  "tax-filing",
                  t("landing.services.tax.title", "AI 报税助手"),
                )
              }
            />
            <ServiceCard
              icon={ServiceIcons["video-editing"]}
              title={t("landing.services.video.title", "自动剪辑")}
              desc={t(
                "landing.services.video.desc",
                "Drop a long take. Get a tight cut with timing, transitions, and captions. Eco mode keeps it lean.",
              )}
              status="coming-soon"
              statusLabel={t("landing.services.video.status", "Coming soon")}
              ctaLabel={t("landing.services.video.cta", "留邮箱抢先体验")}
              onCta={() =>
                openWaitlist(
                  "video-editing",
                  t("landing.services.video.title", "自动剪辑"),
                )
              }
            />
          </div>
        </div>

        {/* Why greentokey */}
        <h2 className="text-2xl font-display font-medium text-center mb-6">
          {t("landing.why.heading", "Why greentokey")}
        </h2>
        <ValueProps />

        {/* Brand statement footer */}
        <div className="text-center text-sm text-muted-foreground space-y-2 pt-12 border-t border-border">
          <p className="font-display text-base text-secondary">
            {t(
              "landing.footer.tagline",
              "One subscription. Transparent compute. Visible carbon.",
            )}
          </p>
          <p>
            {t(
              "landing.footer.attribution",
              "Built for indie hackers / solo founders / vibe coders who care. Open source CoAI fork · Apache 2.0.",
            )}
          </p>
        </div>
      </div>

      <WaitlistDialog
        open={waitlistOpen}
        onOpenChange={setWaitlistOpen}
        service={waitlistService.slug}
        serviceTitle={waitlistService.title}
      />
    </div>
  );
}

export default Welcome;
