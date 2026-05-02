import { useState } from "react";
import { useTranslation } from "react-i18next";

import HeroSection from "@/components/Marketing/HeroSection.tsx";
import ServiceCard from "@/components/Marketing/ServiceCard.tsx";
import ValueProps from "@/components/Marketing/ValueProps.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import { ServiceIcons } from "@/components/Marketing/icons.tsx";

import router from "@/router.tsx";

/**
 * greentokey marketing landing — v0.8 民宿 wedge (大理环洱海).
 *
 * 5-day pivot from indie hacker BYOK hub → 民宿小红书全托管 SaaS.
 * Wedge details: see docs/strategy/2026-04-30-pivot-v4-民宿-saas.md.
 *
 * 4 个 service cards = 全闭环的 4 个环节:
 *   1. 内容生成（含风格学习 few-shot）— LIVE on Concierge first
 *   2. 自动发布（老板手机端 helper）— Coming soon (Phase 1.B)
 *   3. 互动管理（评论 + 私信 AI 草稿）— Coming soon (Phase 1.C)
 *   4. ROI 归因 — LIVE manual mode → Phase 2 自动归因
 *
 * Concierge-first delivery: 首批 5 客户 Week 1-2 已开始付费 + 完整体验，
 * 后台 founder + 亲人手工跑（用 Claude/GPT + 微信 + Notion），代码逐步
 * 接管。Hero CTA 不直接 checkout — 走 demo 预约（信任优先 over 转化优先）。
 */
function Welcome() {
  const { t } = useTranslation();
  const [contactOpen, setContactOpen] = useState(false);
  const [contactCtx, setContactCtx] = useState<string>("");

  // Open dialog for general "demo 预约" — no specific service context.
  const openContactDemo = () => {
    setContactCtx("");
    setContactOpen(true);
  };

  // Open dialog tagged with a specific service the customer was reading
  // about (e.g. "自动发布"). The label is surfaced into the lead's notes
  // so founder follow-up knows which feature pulled them in.
  const openContactFor = (label: string) => {
    setContactCtx(label);
    setContactOpen(true);
  };

  const goPricing = () => router.navigate("/pricing");

  return (
    <div className="flex-1 overflow-y-auto">
      <Header />
      <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
        <HeroSection
          onContactDemo={openContactDemo}
          onViewPricing={goPricing}
        />

        {/* Service grid — 4 个民宿闭环环节 */}
        <div id="services" className="space-y-4 mb-20 scroll-mt-20">
          <h2 className="text-2xl font-display font-medium text-center">
            {t("landing.services.heading", "全闭环 4 个环节")}
          </h2>
          <p className="text-center text-sm text-muted-foreground mb-6">
            {t(
              "landing.services.sub",
              "从写到发到回到归因，¥1980/月一价全包。前 6 周 concierge 模式陪跑，后台逐步自动化。",
            )}
          </p>
          <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-4 gap-6">
            <ServiceCard
              icon={ServiceIcons.content}
              title={t("landing.services.content.title", "内容生成")}
              desc={t(
                "landing.services.content.desc",
                "上传 5-10 张原片 + 1 句房型说明 → AI 写小红书爆款文案、出优化封面图、配 hashtag。每周 5-7 篇。学你已有爆款的语气和风格。",
              )}
              status="live"
              statusLabel={t("landing.services.content.status", "已上线 · 陪跑模式")}
              ctaLabel={t("landing.services.content.cta", "看示例")}
              onCta={openContactDemo}
            />
            <ServiceCard
              icon={ServiceIcons.publish}
              title={t("landing.services.publish.title", "自动发布")}
              desc={t(
                "landing.services.publish.desc",
                "内容生成后自动出现在你小红书草稿箱。手机收到通知 → 一键确认即发。账号 100% 你自己掌控，封号风险低，省 95% 工时。",
              )}
              status="coming-soon"
              statusLabel={t("landing.services.publish.status", "Phase 1.B · 第 4-6 周")}
              ctaLabel={t("landing.services.publish.cta", "留微信抢先体验")}
              onCta={() =>
                openContactFor(
                  t("landing.services.publish.title", "自动发布"),
                )
              }
            />
            <ServiceCard
              icon={ServiceIcons.engage}
              title={t("landing.services.engage.title", "互动管理")}
              desc={t(
                "landing.services.engage.desc",
                "评论 AI 自动起草 → 你审 → 一键发。私信 AI 初筛（订房咨询 vs 闲聊）→ 订房咨询直接打到你。前 100 条人工 sample，后续闭环自学习。",
              )}
              status="coming-soon"
              statusLabel={t("landing.services.engage.status", "Phase 1.C · 第 6-8 周")}
              ctaLabel={t("landing.services.engage.cta", "留微信抢先体验")}
              onCta={() =>
                openContactFor(
                  t("landing.services.engage.title", "互动管理"),
                )
              }
            />
            <ServiceCard
              icon={ServiceIcons.roi}
              title={t("landing.services.roi.title", "ROI 归因")}
              desc={t(
                "landing.services.roi.desc",
                "每天看：「这周新增 X 间订单从小红书来 / 内容投入产出比 Y」。Phase 1 老板每来订单 30 秒标来源；Phase 2 与携程/Airbnb 自动直连归因。",
              )}
              status="live"
              statusLabel={t("landing.services.roi.status", "已上线 · 手动归因")}
              ctaLabel={t("landing.services.roi.cta", "看示例")}
              onCta={openContactDemo}
            />
          </div>
        </div>

        {/* Why greentokey */}
        <h2 className="text-2xl font-display font-medium text-center mb-6">
          {t("landing.why.heading", "为啥找我们做")}
        </h2>
        <ValueProps />

        {/* Brand statement footer */}
        <div className="text-center text-sm text-muted-foreground space-y-2 pt-12 border-t border-border">
          <p className="font-display text-base text-secondary">
            {t(
              "landing.footer.tagline",
              "AI 替代 MCN，更稳更便宜，民宿主自己掌控。",
            )}
          </p>
          <p>
            {t(
              "landing.footer.attribution",
              "首阶段只做大理环洱海 · 优质池 1K+ 民宿主 · 单档 ¥1980/月不分级 · 30 天 50% 退款承诺",
            )}
          </p>
        </div>
      </div>

      <Footer />

      <ContactDialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        source="home"
        contextLabel={contactCtx}
      />
    </div>
  );
}

export default Welcome;
