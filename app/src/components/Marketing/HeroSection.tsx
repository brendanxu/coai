import { Button } from "@/components/ui/button.tsx";
import { ArrowRight } from "lucide-react";
import { useTranslation } from "react-i18next";

export type HeroSectionProps = {
  onUpgradeCNY: () => void;
  onUpgradeUSD: () => void;
};

/**
 * Marketing landing hero. Eyebrow + headline + sub + dual-currency upgrade
 * CTAs. Visitors hit this before any chat UI; the goal is "click one of the
 * two checkout buttons" or "scroll to the service grid".
 */
export default function HeroSection({
  onUpgradeCNY,
  onUpgradeUSD,
}: HeroSectionProps) {
  const { t } = useTranslation();

  return (
    <div className="text-center space-y-6 mb-16">
      <p className="text-sm tracking-widest uppercase text-muted-foreground">
        {t("landing.eyebrow", "Sustainable · BYOK · Indie-built")}
      </p>
      <h1 className="text-5xl md:text-7xl leading-tight font-display">
        {t("landing.headline.before", "Sustainable AI services,")}
        <br />
        {t("landing.headline.middle", "made for")}{" "}
        <span style={{ color: "hsl(var(--primary))" }}>
          {t("landing.headline.accent", "indie builders")}
        </span>
      </h1>
      <p className="text-lg md:text-xl text-secondary max-w-2xl mx-auto leading-relaxed">
        {t(
          "landing.sub",
          "One subscription · multiple services · carbon visible · zero prompt retention",
        )}
      </p>

      <div className="flex flex-col sm:flex-row gap-3 justify-center pt-4">
        <Button
          size="lg"
          onClick={onUpgradeCNY}
          className="px-8 py-6 text-base"
        >
          {t("landing.cta.cny", "立即开通 ¥99/月")}
          <ArrowRight className="ml-2 w-4 h-4" />
        </Button>
        <Button
          variant="outline"
          size="lg"
          onClick={onUpgradeUSD}
          className="px-8 py-6 text-base"
        >
          {t("landing.cta.usd", "Upgrade · $15/mo")}
          <ArrowRight className="ml-2 w-4 h-4" />
        </Button>
      </div>

      <p className="text-xs text-muted-foreground pt-1">
        {t(
          "landing.cta.fineprint",
          "中国用户 支付宝 / Global · card via LemonSqueezy",
        )}
      </p>
    </div>
  );
}
