/**
 * /pricing — public pricing page. v0.8 民宿 wedge.
 *
 * 单档 ¥1980/月含全闭环（生成 + 自动发布 + 互动 + ROI 归因）。
 *
 * Layout:
 *   1. Hero — headline + sub
 *   2. Pricing card — features list + 联系预约 demo CTA (NOT direct subscribe)
 *   3. Three value props (替代 MCN / 民宿垂直 / 老板自己掌控)
 *   4. FAQ (4 questions about 民宿 SaaS)
 *   5. Trust strip — concierge mode + 退款承诺
 *
 * 为啥不是 LemonSqueezy 直接 checkout: Concierge-first delivery (v4 brief).
 * 首批 5 客户走"加微信预约 demo → founder 当面陪跑 → 老板付 ¥1980/月" 流程，
 * 不是匿名注册扣款。直到 Quality Gate (Week 10-12) 验证产品能 retain 客户，
 * 才上自助 checkout。这是 PG 说的 "do things that don't scale"。
 */

import { useState } from "react";
import { useTranslation } from "react-i18next";
import {
  ArrowRight,
  BadgeCheck,
  Check,
  HandCoins,
  RefreshCcw,
  Smartphone,
  Trees,
} from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";

function Pricing() {
  const { t } = useTranslation();
  const [contactOpen, setContactOpen] = useState(false);

  const openContactDemo = () => setContactOpen(true);

  return (
    <div className="flex-1 overflow-y-auto">
      <Header />
      <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
        {/* ---- Hero ---- */}
        <div className="text-center space-y-6 mb-12">
          <p className="text-sm tracking-widest uppercase text-muted-foreground">
            {t("pricing-page.tagline", "民宿 · 小红书运营 · 全托管")}
          </p>
          <h1 className="text-4xl md:text-6xl leading-tight font-display">
            {t("pricing-page.headline-1", "¥1980 一价全包，")}
            <br />
            <span style={{ color: "hsl(var(--primary))" }}>
              {t("pricing-page.headline-2", "替你的本地 MCN。")}
            </span>
          </h1>
          <p className="text-lg text-secondary max-w-2xl mx-auto leading-relaxed">
            {t(
              "pricing-page.subheadline",
              "AI 帮你写 + 帮你发 + 帮你回 + 帮你看效果。每月 ¥1980 不分级、不打折、首批客户高接触陪跑。30 天若入住率没看到提升，无理由退 50%。",
            )}
          </p>
        </div>

        {/* ---- Pricing card ---- */}
        <div className="max-w-md mx-auto rounded-xl border border-border bg-card p-8 space-y-6 mb-20 shadow-sm">
          <div className="space-y-1">
            <p className="text-sm tracking-wide uppercase text-muted-foreground">
              {t("pricing-page.starter", "民宿全托管")}
            </p>
            <div className="flex items-baseline gap-2">
              <span className="text-5xl font-display">¥1980</span>
              <span className="text-muted-foreground">
                /{t("pricing-page.month", "月")}
              </span>
            </div>
            <p className="text-sm text-muted-foreground">
              {t(
                "pricing-page.starter-tagline",
                "单档全功能，没有进阶/旗舰/企业等让你纠结的版本。",
              )}
            </p>
          </div>

          <ul className="space-y-3 text-sm">
            <FeatureRow
              text={t(
                "pricing-page.feature-content",
                "每月 20+ 篇可发布小红书内容（文案 + 优化封面 + hashtag）",
              )}
            />
            <FeatureRow
              text={t(
                "pricing-page.feature-style",
                "AI 学你已有爆款的风格 — 上传 5+ 篇当 few-shot 例子",
              )}
            />
            <FeatureRow
              text={t(
                "pricing-page.feature-publish",
                "自动发布：内容自动到你手机草稿箱，一键确认即发",
              )}
            />
            <FeatureRow
              text={t(
                "pricing-page.feature-engage",
                "评论 / 私信 AI 草稿，你审一键发，订房咨询直接打到你",
              )}
            />
            <FeatureRow
              text={t(
                "pricing-page.feature-roi",
                "每日 ROI dashboard：本周 X 间订单从小红书来",
              )}
            />
            <FeatureRow
              text={t(
                "pricing-page.feature-concierge",
                "首批 5 客户高接触陪跑（founder 当面教 + 调 prompt）",
              )}
            />
            <FeatureRow
              text={t(
                "pricing-page.feature-refund",
                "30 天若入住率没看到提升，无理由退 50%",
              )}
            />
          </ul>

          <div className="pt-2">
            <Button
              size="lg"
              onClick={openContactDemo}
              className="w-full px-8 py-6 text-base"
            >
              {t("pricing-page.cta-demo", "加微信预约 demo")}
              <ArrowRight className="ml-2 w-4 h-4" />
            </Button>
          </div>

          <p className="text-xs text-muted-foreground text-center">
            {t(
              "pricing-page.disclaimer",
              "首批不开放自助下单 · founder 一对一沟通后开通 · 微信支付 / 支付宝 / 银行转账皆可",
            )}
          </p>
        </div>

        {/* ---- 3 value props ---- */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-20">
          <ValueProp
            icon={<BadgeCheck className="w-5 h-5" />}
            title={t("pricing-page.value-replace-title", "替代 MCN")}
            body={t(
              "pricing-page.value-replace-body",
              "你给本地工作室付 ¥2-5K/月、3 个月退订是常态。我们用 AI 把那套活儿做实，更稳更便宜，还有 30 天 50% 退款保障。",
            )}
          />
          <ValueProp
            icon={<Trees className="w-5 h-5" />}
            title={t("pricing-page.value-vertical-title", "民宿垂直")}
            body={t(
              "pricing-page.value-vertical-body",
              "AI 学过的不是通用语料，是大理民宿爆款数据：洱海、苍山、旅拍、蜜月、亲子、季节性玩法。先做大理一个区域，做透了再扩。",
            )}
          />
          <ValueProp
            icon={<Smartphone className="w-5 h-5" />}
            title={t("pricing-page.value-control-title", "你掌控账号")}
            body={t(
              "pricing-page.value-control-body",
              "我们不接管你的小红书账号。内容生成后推到你手机草稿箱，你一键确认即发。封号风险低，账号永远是你的。",
            )}
          />
        </div>

        {/* ---- FAQ ---- */}
        <div className="max-w-2xl mx-auto mb-16 space-y-6">
          <h2 className="text-2xl font-display text-center">
            {t("pricing-page.faq-title", "常见问题")}
          </h2>
          <FaqItem
            question={t(
              "pricing-page.faq-vs-mcn-q",
              "你们和我现在用的 MCN 工作室有什么区别？",
            )}
            answer={t(
              "pricing-page.faq-vs-mcn-a",
              "MCN 是人工写，受限于一个写手的小红书理解。我们用 AI + 民宿垂直语料 + 老板自己的爆款 few-shot，输出更稳。价格 ¥1980/月通常比本地工作室便宜，还有 30 天退款保障。最大的差别是『你掌控账号』 — 我们不要你的密码。",
            )}
          />
          <FaqItem
            question={t(
              "pricing-page.faq-account-q",
              "我自己已经有小红书账号怎么办？要给你们密码吗？",
            )}
            answer={t(
              "pricing-page.faq-account-a",
              "不需要。我们生成的内容会自动出现在你账号草稿箱，你打开手机一键确认即可发布。整个过程账号 100% 在你手里，零封号风险。",
            )}
          />
          <FaqItem
            question={t(
              "pricing-page.faq-cancel-q",
              "可以随时取消吗？",
            )}
            answer={t(
              "pricing-page.faq-cancel-a",
              "随时。提前 1 天微信告诉我们就行。当月已付费用持续到月底，下月不再扣费。30 天内若入住率没改善，无理由退 50%。",
            )}
          />
          <FaqItem
            question={t(
              "pricing-page.faq-data-q",
              "你们会留我的什么数据？",
            )}
            answer={t(
              "pricing-page.faq-data-a",
              "你上传的房源照片 + 历史爆款用于 AI 学习风格，不外传。AI 生成的内容版权归你。你的小红书账号、订房系统数据、客人信息我们 0 接触。",
            )}
          />
        </div>

        {/* ---- Trust strip ---- */}
        <div className="text-center pt-12 border-t border-border">
          <div className="flex flex-wrap justify-center gap-6 text-xs text-muted-foreground">
            <TrustItem
              icon={<HandCoins className="w-3.5 h-3.5" />}
              text={t("pricing-page.trust-concierge", "首批 5 客户高接触陪跑")}
            />
            <TrustItem
              icon={<RefreshCcw className="w-3.5 h-3.5" />}
              text={t("pricing-page.trust-refund", "30 天 50% 退款承诺")}
            />
            <TrustItem
              icon={<Smartphone className="w-3.5 h-3.5" />}
              text={t("pricing-page.trust-account", "账号永远在你手里")}
            />
          </div>
        </div>
      </div>

      <Footer />

      <ContactDialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        source="pricing"
      />
    </div>
  );
}

// ----------------------------------------------------------------------------
// Sub-components
// ----------------------------------------------------------------------------

function FeatureRow({ text }: { text: string }) {
  return (
    <li className="flex items-start gap-3">
      <Check
        className="w-4 h-4 mt-0.5 flex-shrink-0"
        style={{ color: "hsl(var(--primary))" }}
      />
      <span className="text-secondary leading-relaxed">{text}</span>
    </li>
  );
}

type ValuePropProps = {
  icon: React.ReactNode;
  title: string;
  body: string;
};

function ValueProp({ icon, title, body }: ValuePropProps) {
  return (
    <div className="rounded-lg border border-border bg-card p-6 space-y-3">
      <div className="flex items-center gap-3">
        <div
          className="flex items-center justify-center w-9 h-9 rounded-md"
          style={{
            background: "hsl(var(--accent))",
            color: "hsl(var(--accent-foreground))",
          }}
        >
          {icon}
        </div>
        <div className="font-display text-lg font-medium">{title}</div>
      </div>
      <p className="text-sm leading-relaxed text-secondary">{body}</p>
    </div>
  );
}

function FaqItem({ question, answer }: { question: string; answer: string }) {
  return (
    <div className="space-y-2">
      <h3 className="font-medium text-base">{question}</h3>
      <p className="text-sm text-secondary leading-relaxed">{answer}</p>
    </div>
  );
}

function TrustItem({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {icon}
      {text}
    </span>
  );
}

export default Pricing;
