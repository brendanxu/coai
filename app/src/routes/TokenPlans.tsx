import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Check, Key, Wallet, Network } from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";

/**
 * /token-plans — Token 套餐 marketing + checkout entry.
 *
 * v0.11 baseline plan: ¥99/月 = 5000 credits, OpenAI-compatible API,
 * 一个 sk-xxx Key 调全池。
 *
 * Per business-model-v3.md: Token 套餐 is one of two product axes
 * (the other being 服务市场). This page is the dedicated landing
 * for the Token-buying axis — devs and power users who want
 * BYOK-style multi-model access without the 一站式 service.
 *
 * Checkout flow today: lead-capture (founder follows up with
 * payment + LemonSqueezy provisioning).
 *
 * v0.12 will swap the lead-capture CTA for direct LS hosted-checkout
 * once the LS variants are configured (currently deferred per
 * memory/ls-variants-deferred.md).
 */
function TokenPlans() {
  const { t } = useTranslation();
  const [contactOpen, setContactOpen] = useState(false);

  return (
    <>
      <Header />
      <main className="flex-1 overflow-y-auto">
        <div
          className="mx-auto px-6 py-16 md:py-24"
          style={{ maxWidth: "var(--max-content)" }}
        >
          {/* Hero */}
          <header className="text-center mb-14 md:mb-20">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
              {t("token.eyebrow", "Token 套餐 · 模型池接入")}
            </p>
            <h1 className="font-display text-4xl md:text-6xl tracking-tight mb-5">
              {t("token.heading.line1", "一个 Key,")}
              <br />
              <span style={{ color: "hsl(var(--primary))" }}>
                {t("token.heading.line2", "一池模型。")}
              </span>
            </h1>
            <p className="text-base md:text-lg text-secondary-foreground/80 max-w-2xl mx-auto leading-relaxed">
              {t(
                "token.sub",
                "OpenAI 兼容 endpoint，Bearer 认证。切换 GPT/Claude/DeepSeek/Qwen 只改 model 字段，不再换账号、换 Key。基于 NewAPI 路由，sub2API 即将接入官方 API。",
              )}
            </p>
          </header>

          {/* Plan card — single tier (¥99/月) */}
          <div className="max-w-xl mx-auto mb-16">
            <div
              className="rounded-3xl p-8 md:p-10"
              style={{
                background: "hsl(var(--card))",
                border: "1px solid hsl(var(--border-soft))",
                boxShadow: "var(--shadow)",
              }}
            >
              <div className="flex items-baseline justify-between mb-5">
                <span className="text-sm font-medium text-muted-foreground">
                  {t("token.plan.name", "标准版")}
                </span>
                <span
                  className="text-[10px] uppercase tracking-wider px-2 py-0.5 rounded-full font-medium"
                  style={{
                    background: "hsl(var(--accent-soft))",
                    color: "hsl(var(--primary-deep))",
                  }}
                >
                  {t("token.plan.badge", "唯一档位")}
                </span>
              </div>

              <div className="flex items-baseline gap-2 mb-1.5">
                <span className="font-display text-5xl md:text-6xl font-semibold">
                  ¥99
                </span>
                <span className="text-muted-foreground">/ {t("token.plan.month", "月")}</span>
              </div>
              <p className="text-sm text-secondary-foreground/85 mb-7">
                {t(
                  "token.plan.tagline",
                  "5,000 credits · 约可调 5,000 次标准模型 · 全池模型可切",
                )}
              </p>

              <ul className="space-y-3 mb-8 text-sm">
                <FeatureRow
                  text={t(
                    "token.plan.feat.1",
                    "一个 sk-xxx API Key，OpenAI 兼容",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.2",
                    "5,000 credits/月，¥0.02 / 标准 call (约 1k+1k tokens)",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.3",
                    "切模型只改 model 字段，路由 + 计费自动",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.4",
                    "Light/Standard/Premium 3 档：DeepSeek/Qwen 0.5×、Claude-haiku 1×、GPT-4o/Sonnet 3×",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.5",
                    "Dashboard 实时余额 · 用量明细 · 模型分布",
                  )}
                />
              </ul>

              <Button
                onClick={() => setContactOpen(true)}
                size="lg"
                className="w-full rounded-full"
              >
                {t("token.plan.cta", "联系开通")}
              </Button>
              <p className="text-xs text-center text-muted-foreground mt-3">
                {t(
                  "token.plan.disclaimer",
                  "首批客户 founder 一对一沟通 · 微信支付/支付宝/银行转账皆可 · LemonSqueezy 海外卡支付即将开通",
                )}
              </p>
            </div>
          </div>

          {/* Why */}
          <div className="grid grid-cols-1 md:grid-cols-3 gap-8 md:gap-10 mb-20 md:mb-24">
            <Why
              icon={<Key className="w-6 h-6" strokeWidth={1.6} />}
              title={t("token.why.one.title", "一 Key 通模型")}
              body={t(
                "token.why.one.body",
                "不再为每家模型供应商单独申请 Key、单独充钱、单独看用量。一套对接一次到位。",
              )}
            />
            <Why
              icon={<Wallet className="w-6 h-6" strokeWidth={1.6} />}
              title={t("token.why.two.title", "Credit 抽象")}
              body={t(
                "token.why.two.body",
                "我们替你抹平 OpenAI/Anthropic 官方 API 跟 sub2API 的成本差。轻/标准/高级 3 档定价透明，不暴露 75x 的 spread。",
              )}
            />
            <Why
              icon={<Network className="w-6 h-6" strokeWidth={1.6} />}
              title={t("token.why.three.title", "Pool 自动调度")}
              body={t(
                "token.why.three.body",
                "底层 NewAPI 路由：对单次调用，自动选可用且最便宜的渠道。某家挂了你看不见。",
              )}
            />
          </div>

          {/* Pool peek + link to /pool */}
          <div className="text-center">
            <Link
              to="/pool"
              className="inline-flex items-center gap-2 text-sm font-medium hover:gap-3 transition-all"
              style={{ color: "hsl(var(--primary))" }}
            >
              {t("token.pool-link", "查看完整模型池")}
              <span>→</span>
            </Link>
          </div>
        </div>

        <Footer />
      </main>

      <ContactDialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        source="token-plans"
        contextLabel={t("token.plan.name", "Token 套餐 ¥99/月")}
      />
    </>
  );
}

function FeatureRow({ text }: { text: string }) {
  return (
    <li className="flex items-start gap-3">
      <Check
        className="w-4 h-4 mt-0.5 flex-shrink-0"
        style={{ color: "hsl(var(--primary))" }}
      />
      <span>{text}</span>
    </li>
  );
}

function Why({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="space-y-3">
      <span style={{ color: "hsl(var(--primary))" }}>{icon}</span>
      <h4 className="font-display text-lg font-medium">{title}</h4>
      <p className="text-sm leading-relaxed text-secondary-foreground/85">
        {body}
      </p>
    </div>
  );
}

export default TokenPlans;
