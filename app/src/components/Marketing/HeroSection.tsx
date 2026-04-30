import { Button } from "@/components/ui/button.tsx";
import { ArrowRight } from "lucide-react";
import { useTranslation } from "react-i18next";

export type HeroSectionProps = {
  /** Primary CTA — opens 微信 / contact dialog. */
  onContactDemo: () => void;
  /** Secondary CTA — scrolls to /pricing or services section. */
  onViewPricing: () => void;
};

/**
 * Marketing landing hero — v0.8 民宿 wedge.
 *
 * 客户群: 大理环洱海民宿主，1K+ 优质池（房源 5-15 间）。
 * 痛点: 同等硬件条件下，因小红书运营弱 → 入住率受损。
 * 替代品: 本地 MCN 工作室 ¥2-5K/月，3 个月退订。
 * 价值 prop: 用 AI 把 MCN 的承诺做实，¥1980/月不分级。
 */
export default function HeroSection({
  onContactDemo,
  onViewPricing,
}: HeroSectionProps) {
  const { t } = useTranslation();

  return (
    <div className="text-center space-y-6 mb-16">
      <p className="text-sm tracking-widest uppercase text-muted-foreground">
        {t("landing.eyebrow", "民宿 · 小红书运营 · 全托管")}
      </p>
      <h1 className="text-5xl md:text-7xl leading-tight font-display">
        {t("landing.headline.before", "你做民宿，")}
        <br />
        {t("landing.headline.middle", "我做")}{" "}
        <span style={{ color: "hsl(var(--primary))" }}>
          {t("landing.headline.accent", "你的小红书。")}
        </span>
      </h1>
      <p className="text-lg md:text-xl text-secondary max-w-2xl mx-auto leading-relaxed">
        {t(
          "landing.sub",
          "AI 帮你写文案、出图、自动发布、应对评论私信。每月 ¥1980 全托管，比本地 MCN 更稳更便宜。先做大理环洱海。",
        )}
      </p>

      <div className="flex flex-col sm:flex-row gap-3 justify-center pt-4">
        <Button
          size="lg"
          onClick={onContactDemo}
          className="px-8 py-6 text-base"
        >
          {t("landing.cta.demo", "联系预约 demo")}
          <ArrowRight className="ml-2 w-4 h-4" />
        </Button>
        <Button
          variant="outline"
          size="lg"
          onClick={onViewPricing}
          className="px-8 py-6 text-base"
        >
          {t("landing.cta.pricing", "了解套餐 ¥1980/月")}
          <ArrowRight className="ml-2 w-4 h-4" />
        </Button>
      </div>

      <p className="text-xs text-muted-foreground pt-1">
        {t(
          "landing.cta.fineprint",
          "首批客户高接触陪跑 · 30 天 50% 退款承诺 · 不打折不分级",
        )}
      </p>
    </div>
  );
}
