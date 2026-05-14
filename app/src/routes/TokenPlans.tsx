import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useNavigate } from "react-router-dom";
import axios from "axios";
import {
  ArrowRight,
  Check,
  Code2,
  Copy,
  Key,
  Network,
  Shield,
  Wallet,
  Zap,
} from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";

/**
 * /token-plans — Token 套餐 dedicated product page.
 *
 * v0.23 (2026-05-13) section reorder + content additions per cross-AI
 * review (tana 5-站对标 + Codex 4-agent 中文 reseller 研究, surfaced
 * docs/codex-reviews/18-token-sale-code-review.md and the agents
 * synthesis). Old order was hero → pricing → pool → code → tier →
 * why → faq → trust. New order surfaces buy-decision info first
 * (matches CN reseller cognitive priority: 价/迁移/对比/风险),
 * defers brand narrative below the fold.
 *
 * NEW order:
 *   1. Hero (with 5-fact scan strip)
 *   2. Pricing card (with small free-trial concierge link)
 *   3. Quick Start (3-tab code: OpenAI SDK / Claude Code / curl)
 *   4. vs DIY (6-row decision-clarification table)
 *   5. Live pool snapshot (was 3 — moved below decision section)
 *   6. Tier multiplier (was 5)
 *   7. Why us 3 cards (was 6)
 *   8. FAQ (was 7)
 *   9. Trust 反驳 (was 8 — expanded from 4 items to 6)
 *
 * CHECKOUT FLOW unchanged from v0.22 — dual rail self-serve:
 *   USD card → /api/payment/checkout?plan_code=token-99 (LemonSqueezy)
 *   微信/支付宝 → /api/payment/hupijiao/checkout?plan_code=token-99
 *
 * Both webhooks land in auth.RedeemPlanForOrder which atomically
 * creates gtk_user_plan + grants 5,000 credits/month quota. v0.22.1
 * BL-01 hotfix narrowed LS dispatch to order_created event only.
 *
 * STRICT NO-灰色文案 (per Codex agent 2 + L23 architecture review):
 * never write 无限制 / 账号池 / 反代 / 封号兜底 / 官方同质 / 全网最低.
 * Use 多线路 / 限速透明 / 失败重试 / 可用性监控 / OpenAI-compatible /
 * 性价比优 instead.
 */

const TOKEN_PLAN_CODE = "token-99";

type PoolModel = {
  model: string;
  provider_label: string;
  is_sub2api?: boolean;
  average_latency_ms?: number;
  credit_tier?: string;
  credit_tier_label?: string;
};

type PoolSnapshot = {
  total_models: number;
  enabled_channels: number;
  avg_latency_ms: number;
  models: PoolModel[];
};

type QuickStartTab = "openai" | "claudecode" | "curl";

function TokenPlans() {
  const { t } = useTranslation();
  const navigate = useNavigate();
  const [contactOpen, setContactOpen] = useState(false);
  const [pool, setPool] = useState<PoolSnapshot | null>(null);
  const [activeTab, setActiveTab] = useState<QuickStartTab>("openai");

  useEffect(() => {
    let mounted = true;
    axios
      .get("/gtk/v1/pool")
      .then((r) => {
        if (mounted && r.data?.success) setPool(r.data.data);
      })
      .catch(() => {
        // silent — section shows skeleton
      });
    return () => {
      mounted = false;
    };
  }, []);

  // Both CTA buttons lead to /checkout for payment-method selection +
  // agreement confirmation before hitting the payment provider.
  const onCheckout = () => {
    navigate(`/checkout?plan_code=${TOKEN_PLAN_CODE}`);
  };


  // 5 hero scan-facts: surfaced ABOVE the fold so CN audience gets the
  // 5 buy-decision answers before any narrative copy. Order matters —
  // price first, then compatibility, payment, migration cost, refund.
  const heroFacts = [
    { icon: <Zap className="w-3 h-3" />, text: t("token.facts.cost", "¥99/月 单档") },
    { icon: <Code2 className="w-3 h-3" />, text: t("token.facts.compat", "OpenAI 兼容") },
    { icon: <Wallet className="w-3 h-3" />, text: t("token.facts.pay", "支付宝 / 微信") },
    { icon: <ArrowRight className="w-3 h-3" />, text: t("token.facts.migrate", "改一行 base_url") },
    { icon: <Shield className="w-3 h-3" />, text: t("token.facts.refund", "7 天无理由退款") },
  ];

  return (
    <>
      <Header />
      <main className="flex-1 overflow-y-auto">
        <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
          {/* ─── 1. Hero (with 5-fact strip) ────────────────────────── */}
          <header className="text-center mb-14 md:mb-20">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-5">
              {t("token.eyebrow", "Token 套餐 · 模型池接入")}
            </p>
            <h1 className="font-display text-4xl md:text-6xl leading-[1.1] mb-6 tracking-tight">
              <span className="block">
                {t("token.heading.line1", "一个 Key,")}
              </span>
              <span className="block">
                {t("token.heading.line2", "一池模型。")}
              </span>
            </h1>
            <p className="text-base md:text-lg text-secondary-foreground/80 max-w-2xl mx-auto leading-relaxed mb-7">
              {t(
                "token.sub",
                "OpenAI 兼容 endpoint。GPT / Claude / DeepSeek / Qwen 一个 sk-xxx 全跑,只改 model 字段。底层智能路由,挑实时最便宜可用 channel。",
              )}
            </p>
            {/* 5-fact scan strip — buy-decision answers above the fold */}
            <div className="flex flex-wrap justify-center gap-2">
              {heroFacts.map((f, i) => (
                <HeroFactPill key={i} icon={f.icon} text={f.text} />
              ))}
            </div>
          </header>

          {/* ─── 2. Pricing card ─────────────────────────────────────── */}
          <div className="max-w-xl mx-auto mb-16 md:mb-20">
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
                <span className="text-muted-foreground">
                  / {t("token.plan.month", "月")}
                </span>
              </div>
              <p className="text-sm text-secondary-foreground/85 mb-7">
                {t(
                  "token.plan.tagline",
                  "5,000 credits · 约 5,000 次标准调用 · 池内全模型可切",
                )}
              </p>

              <ul className="space-y-3 mb-8 text-sm">
                <FeatureRow
                  text={t(
                    "token.plan.feat.1",
                    "一个 sk-xxx API Key,OpenAI 兼容 endpoint",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.2",
                    "5,000 credits/月,标准调用 ¥0.02/次 (≈1k+1k tokens)",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.3",
                    "切模型只改 model 字段——智能路由 + 计费自动",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.4-v2",
                    "三档计费 (1 credit ≈ ¥0.02/标准调用):Light 0.5× = DeepSeek/Qwen,Standard 1× = Claude-haiku/GPT-4o-mini,Premium 3× = Claude-sonnet/GPT-4o。无隐藏加价。",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.5",
                    "Dashboard 实时余额 · 用量明细 · 模型分布",
                  )}
                />
              </ul>

              <div className="space-y-2.5">
                <Button
                  onClick={onCheckout}
                  size="lg"
                  className="w-full rounded-full"
                >
                  {t("token.plan.cta.cny", "微信 / 支付宝 ¥99 立即开通")}
                  <ArrowRight className="w-4 h-4 ml-1.5" />
                </Button>
                <Button
                  onClick={onCheckout}
                  size="lg"
                  variant="outline"
                  className="w-full rounded-full"
                >
                  {t("token.plan.cta.usd", "Pay with Card · USD $14")}
                </Button>
              </div>

              <p className="text-xs text-center text-muted-foreground mt-3">
                {t(
                  "token.plan.disclaimer-v2",
                  "境内 微信/支付宝 即时到账 · 境外卡 LemonSqueezy MoR 自动开通 · 5,000 credits/月",
                )}
              </p>

              <p className="text-xs text-center text-muted-foreground mt-2">
                <button
                  type="button"
                  onClick={() => setContactOpen(true)}
                  className="underline-offset-4 hover:underline"
                >
                  {t("token.plan.cta.enterprise", "或联系销售 · 企业 / 团队 / 量大客户")}
                </button>
              </p>

              {/* CHANGE 6 OPTION A: free-trial concierge link (no Free
                  tier code built — just a contact path for evaluators) */}
              <p className="text-xs text-center text-muted-foreground mt-1.5">
                <button
                  type="button"
                  onClick={() => setContactOpen(true)}
                  className="underline-offset-4 hover:underline"
                >
                  {t("token.plan.cta.trial", "想先免费试用 50 credits 体验? 加微信")}
                </button>
              </p>


            </div>
          </div>

          {/* ─── 3. Quick Start (3-tab code) ─────────────────────────── */}
          <section className="mb-16 md:mb-20">
            <div className="text-center mb-8">
              <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-2">
                {t("token.quickstart.eyebrow", "30 秒接入 · 30-second integration")}
              </p>
              <h3 className="font-display text-2xl md:text-3xl mb-2">
                {t("token.quickstart.heading", "改一行 base_url 就跑。")}
              </h3>
              <p className="text-sm text-secondary-foreground/80 max-w-xl mx-auto">
                {t(
                  "token.quickstart.sub",
                  "OpenAI SDK / Claude Code / curl 全部支持,SDK 不变,仅替换 endpoint + key。",
                )}
              </p>
            </div>

            <QuickStartTabs activeTab={activeTab} onChange={setActiveTab} />

            <p className="text-xs text-muted-foreground text-center mt-4">
              {t(
                "token.quickstart.hint",
                "需要 Anthropic SDK / Vercel AI SDK / LangChain? 全兼容,文档查询 →",
              )}{" "}
              <Link to="/docs" className="underline hover:opacity-70">
                /docs
              </Link>
            </p>
          </section>

          {/* ─── 4. vs DIY decision-clarification table ─────────────── */}
          <section className="mb-16 md:mb-20">
            <div className="text-center mb-8">
              <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-2">
                {t("token.vsdiy.eyebrow", "决策澄清")}
              </p>
              <h3 className="font-display text-2xl md:text-3xl mb-2">
                {t("token.vsdiy.heading", "想清楚再买。")}
              </h3>
              <p className="text-sm text-secondary-foreground/80 max-w-xl mx-auto">
                {t(
                  "token.vsdiy.sub",
                  "公平比较 greentokey 跟自己直接接 OpenAI + Anthropic + DeepSeek 三家 API,看哪个更适合你。",
                )}
              </p>
            </div>

            <VsDiyTable t={t} />

            <p className="text-xs text-center text-muted-foreground mt-5">
              {t(
                "token.vsdiy.footer",
                "都不太合适?",
              )}{" "}
              <button
                type="button"
                onClick={() => setContactOpen(true)}
                className="underline underline-offset-4 hover:opacity-70"
              >
                {t("token.vsdiy.contact", "联系销售聊企业方案")}
              </button>
            </p>
          </section>

          {/* ─── 5. Live pool snapshot (was 3) ──────────────────────── */}
          <section
            className="rounded-3xl p-7 md:p-9 mb-16 md:mb-20"
            style={{
              background: "hsl(var(--ink))",
              color: "hsl(var(--ink-foreground))",
              boxShadow: "var(--shadow-ink)",
            }}
          >
            <div className="flex items-center justify-between text-xs uppercase tracking-[0.16em] mb-5 opacity-80">
              <span>{t("token.pool.label", "Live grid")}</span>
              <span className="inline-flex items-center gap-1.5">
                <span className="relative flex h-2 w-2">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-300 opacity-75" />
                  <span className="relative inline-flex rounded-full h-2 w-2 bg-green-300" />
                </span>
                {t("token.pool.realtime", "Realtime")}
              </span>
            </div>

            <h3 className="font-display text-2xl md:text-3xl mb-2">
              {t("token.pool.heading", "你买到的就是这个池子。")}
            </h3>
            <p className="font-mono text-xs opacity-70 mb-7">
              {pool
                ? t(
                    "token.pool.delta",
                    "{{count}} 个模型在线 · {{channels}} 个渠道 · 平均延迟 {{ms}}ms",
                    {
                      count: pool.total_models,
                      channels: pool.enabled_channels,
                      ms: pool.avg_latency_ms || "—",
                    },
                  )
                : t("token.pool.loading", "加载中…")}
            </p>

            <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-2.5 font-mono text-xs">
              {(pool?.models || []).slice(0, 8).map((m) => (
                <div
                  key={m.model}
                  className="flex justify-between items-center gap-3 py-1 border-b"
                  style={{ borderColor: "hsl(var(--ink-foreground) / 0.08)" }}
                >
                  <span
                    className="flex items-center gap-2 truncate"
                    style={{ color: "hsl(var(--ink-accent))" }}
                  >
                    <span className="opacity-80">●</span>
                    <span className="truncate">{m.model}</span>
                    {m.is_sub2api && (
                      <span className="text-[9px] opacity-60 uppercase tracking-wider">
                        sub2api
                      </span>
                    )}
                  </span>
                  <span className="opacity-60 flex-shrink-0">
                    {m.average_latency_ms ? `${m.average_latency_ms}ms` : "—"}
                  </span>
                </div>
              ))}
              {!pool &&
                ["gpt-4o", "claude-sonnet-4", "deepseek-v3", "qwen-max"].map(
                  (m) => (
                    <div
                      key={m}
                      className="flex justify-between items-center gap-3 py-1 opacity-50"
                    >
                      <span style={{ color: "hsl(var(--ink-accent))" }}>
                        ● {m}
                      </span>
                      <span>—</span>
                    </div>
                  ),
                )}
            </div>

            <div className="mt-7">
              <Link
                to="/pool"
                className="inline-flex items-center gap-1.5 text-sm font-medium hover:gap-2.5 active:gap-2.5 transition-all opacity-90 hover:opacity-100 active:opacity-100"
                style={{ color: "hsl(var(--ink-accent))" }}
              >
                {t("token.pool.cta", "查看完整模型池 + 渠道状态")}
                <ArrowRight className="w-3.5 h-3.5" />
              </Link>
            </div>
          </section>

          {/* ─── 6. Tier multiplier strip (was 5) ───────────────────── */}
          <section className="mb-16 md:mb-20">
            <div className="text-center mb-8">
              <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-2">
                {t("token.tier.eyebrow", "三档透明计费")}
              </p>
              <h3 className="font-display text-2xl md:text-3xl mb-2">
                {t("token.tier.heading", "5,000 credits 能调多少次?")}
              </h3>
              <p className="text-sm text-secondary-foreground/80 max-w-xl mx-auto">
                {t(
                  "token.tier.sub",
                  "我们用统一的 credit 抽象抹平 OpenAI 官方 API 跟 sub2API 的成本差,你看到的就是下面三档。",
                )}
              </p>
            </div>

            <div className="grid grid-cols-1 md:grid-cols-3 gap-4 md:gap-5">
              <TierCard
                badge={t("token.tier.light.label", "Light · 0.5×")}
                models={t(
                  "token.tier.light.models",
                  "DeepSeek-v3 · Qwen-max · Llama-3.x",
                )}
                rate={t("token.tier.light.rate", "¥0.01 / 次")}
                volume={t("token.tier.light.volume", "约 10,000 次 / 月")}
              />
              <TierCard
                badge={t("token.tier.std.label", "Standard · 1×")}
                models={t(
                  "token.tier.std.models",
                  "Claude-haiku · GPT-4o-mini · Gemini-flash",
                )}
                rate={t("token.tier.std.rate", "¥0.02 / 次")}
                volume={t("token.tier.std.volume", "约 5,000 次 / 月")}
                highlight
              />
              <TierCard
                badge={t("token.tier.premium.label", "Premium · 3×")}
                models={t(
                  "token.tier.premium.models",
                  "GPT-4o · Claude-Sonnet-4 · o1",
                )}
                rate={t("token.tier.premium.rate", "¥0.06 / 次")}
                volume={t("token.tier.premium.volume", "约 1,666 次 / 月")}
              />
            </div>
            <p className="text-xs text-muted-foreground text-center mt-5">
              {t(
                "token.tier.note",
                "每次调用按 model 字段自动归档。Dashboard 显示每个模型用了多少 credits。",
              )}
            </p>
          </section>

          {/* ─── 7. Why 3 cards (was 6) ─────────────────────────────── */}
          <section className="mb-16 md:mb-20">
            <div className="grid grid-cols-1 md:grid-cols-3 gap-6 md:gap-8">
              <Why
                icon={<Key className="w-5 h-5" strokeWidth={1.6} />}
                title={t("token.why.one.title", "一 Key 通模型")}
                body={t(
                  "token.why.one.body",
                  "不再为每家模型供应商单独申请 Key、单独充钱、单独看用量。一套对接,一次到位。",
                )}
              />
              <Why
                icon={<Wallet className="w-5 h-5" strokeWidth={1.6} />}
                title={t("token.why.two.title", "Credit 抽象")}
                body={t(
                  "token.why.two.body",
                  "Light / Standard / Premium 三档定价透明,不暴露 sub2API 与官方 API 之间最高 75× 的成本差。",
                )}
              />
              <Why
                icon={<Network className="w-5 h-5" strokeWidth={1.6} />}
                title={t("token.why.three.title", "Pool 智能路由")}
                body={t(
                  "token.why.three.body",
                  "底层 NewAPI 路由:对单次调用,自动选可用且性价比优的渠道。某家挂了 fallback 到下一家,你看不见。",
                )}
              />
            </div>
          </section>

          {/* ─── 8. FAQ (was 7) ─────────────────────────────────────── */}
          <section className="max-w-3xl mx-auto mb-16 md:mb-20">
            <h2 className="font-display text-2xl md:text-3xl mb-8 text-center">
              {t("token.faq.heading", "常见问题")}
            </h2>
            <div className="space-y-7">
              <FaqItem
                question={t(
                  "token.faq.bill.q",
                  "credits 怎么扣? 不同模型一样贵吗?",
                )}
                answer={t(
                  "token.faq.bill.a",
                  "一次标准调用 (≈1k 输入 + 1k 输出) = 1 credit (Standard 档)。Light 档 (DeepSeek/Qwen) 0.5 credit, Premium 档 (GPT-4o/Sonnet) 3 credits。Dashboard 实时显示余额 + 每模型用量。",
                )}
              />
              <FaqItem
                question={t(
                  "token.faq.sub2api.q",
                  "sub2API 是什么? 跟 OpenAI 官方 API 有什么差?",
                )}
                answer={t(
                  "token.faq.sub2api.a",
                  "sub2API 是把 ChatGPT Plus / Claude Pro 订阅账号转成 API 接口的桥接层,价格只有官方 API 的 1.3% – 5%。延迟略高、SLA 更弱,但适合大量轻量调用。我们正在并行接入官方 API,Dashboard 会让你按调用选择走哪条线。",
                )}
              />
              <FaqItem
                question={t(
                  "token.faq.privacy.q",
                  "我发给 API 的内容你们会留吗?",
                )}
                answer={t(
                  "token.faq.privacy.a",
                  "调用内容不写入持久化日志,只保留计费元数据 (时间、模型、token 数、credit 扣减)。日志保留 90 天用于审计与问题排查。详见 /privacy。",
                )}
              />
              <FaqItem
                question={t(
                  "token.faq.cancel.q",
                  "可以随时取消吗? 余额怎么办?",
                )}
                answer={t(
                  "token.faq.cancel.a",
                  "随时。取消后当月 credits 用完为止,不再续扣。多档套餐 / 加 credits 包即将上线,首批用户支持手动续费 / 升级。",
                )}
              />
            </div>
          </section>

          {/* ─── 9. Trust 反驳 (was 8 — expanded) ───────────────────── */}
          <div className="pt-12 border-t border-border">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground text-center mb-6">
              {t("token.trust.eyebrow", "你可能担心的事")}
            </p>
            <div className="grid grid-cols-2 md:grid-cols-3 gap-x-6 gap-y-4 max-w-3xl mx-auto">
              <TrustAssertion
                text={t("token.trust.balance", "余额永久有效,不清零")}
              />
              <TrustAssertion
                text={t("token.trust.failure", "失败请求不扣费")}
              />
              <TrustAssertion
                text={t("token.trust.privacy", "不存储 prompt / 不读历史")}
              />
              <TrustAssertion
                text={t("token.trust.refund", "7 天无理由全额退款")}
              />
              <TrustAssertion
                text={t("token.trust.routing", "多线路 + 实时可用性监控")}
              />
              <TrustAssertion
                text={t("token.trust.payment", "支付宝 / 微信 / 信用卡 任选")}
              />
            </div>
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
      <span className="text-secondary-foreground/90 leading-relaxed">
        {text}
      </span>
    </li>
  );
}

function HeroFactPill({
  icon,
  text,
}: {
  icon: React.ReactNode;
  text: string;
}) {
  return (
    <span
      className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs font-medium"
      style={{
        background: "hsl(var(--accent-soft))",
        color: "hsl(var(--primary-deep))",
      }}
    >
      {icon}
      {text}
    </span>
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
    <div>
      <div
        className="w-10 h-10 rounded-2xl flex items-center justify-center mb-4"
        style={{
          background: "hsl(var(--accent-soft))",
          color: "hsl(var(--primary-deep))",
        }}
      >
        {icon}
      </div>
      <h4 className="font-display text-lg mb-2">{title}</h4>
      <p className="text-sm text-secondary-foreground/80 leading-relaxed">
        {body}
      </p>
    </div>
  );
}

function TierCard({
  badge,
  models,
  rate,
  volume,
  highlight,
}: {
  badge: string;
  models: string;
  rate: string;
  volume: string;
  highlight?: boolean;
}) {
  return (
    <div
      className="rounded-2xl p-5"
      style={{
        background: highlight
          ? "hsl(var(--accent-soft))"
          : "hsl(var(--card))",
        border: highlight
          ? "1px solid hsl(var(--primary) / 0.4)"
          : "1px solid hsl(var(--border-soft))",
      }}
    >
      <p
        className="text-[11px] uppercase tracking-wider font-medium mb-2.5"
        style={{
          color: highlight
            ? "hsl(var(--primary-deep))"
            : "hsl(var(--muted-foreground))",
        }}
      >
        {badge}
      </p>
      <p className="text-xs text-secondary-foreground/80 mb-3 leading-relaxed">
        {models}
      </p>
      <p className="font-display text-xl mb-0.5">{rate}</p>
      <p className="text-xs text-muted-foreground">{volume}</p>
    </div>
  );
}

function FaqItem({
  question,
  answer,
}: {
  question: string;
  answer: string;
}) {
  return (
    <div>
      <h3 className="font-display text-base md:text-lg mb-2">{question}</h3>
      <p className="text-sm text-secondary-foreground/80 leading-relaxed">
        {answer}
      </p>
    </div>
  );
}

function TrustAssertion({ text }: { text: string }) {
  return (
    <div className="flex items-start gap-2">
      <Check
        className="w-4 h-4 mt-0.5 flex-shrink-0"
        style={{ color: "hsl(var(--primary))" }}
      />
      <span className="text-xs text-secondary-foreground/85 leading-relaxed">
        {text}
      </span>
    </div>
  );
}

// QuickStartTabs renders 3 tabs of integration code (OpenAI SDK / Claude
// Code / curl). State-managed inline (no shadcn/Tabs dep needed for 3
// content blocks). Copy button uses navigator.clipboard with toast.
function QuickStartTabs({
  activeTab,
  onChange,
}: {
  activeTab: QuickStartTab;
  onChange: (tab: QuickStartTab) => void;
}) {
  const { t } = useTranslation();

  const snippets: Record<QuickStartTab, { label: string; code: string }> = {
    openai: {
      label: t("token.quickstart.tab.openai", "OpenAI SDK · node.js"),
      code: `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "https://api.greentokey.com/v1",
  apiKey:  "sk-tnx-xxxxxx",
});

// 切到 GPT-4o 只改 model 字段 — 计费自动按 Premium 档
client.chat.completions.create({
  model: "gpt-4o",          // or "deepseek-v3" / "claude-sonnet-4"
  messages: [...],
});`,
    },
    claudecode: {
      label: t("token.quickstart.tab.claudecode", "Claude Code CLI"),
      code: `# 写入 ~/.zshrc 或 ~/.bashrc
export ANTHROPIC_BASE_URL="https://api.greentokey.com/anthropic"
export ANTHROPIC_API_KEY="sk-tnx-xxxxxx"

# 重启 shell 或 source
source ~/.zshrc

# Claude Code 直接用,不需要 Anthropic 官方 key
claude "解释这段代码"`,
    },
    curl: {
      label: t("token.quickstart.tab.curl", "curl · raw HTTP"),
      code: `curl https://api.greentokey.com/v1/chat/completions \\
  -H "Authorization: Bearer sk-tnx-xxxxxx" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "deepseek-v3",
    "messages": [{"role":"user","content":"hi"}]
  }'`,
    },
  };

  const onCopy = async () => {
    try {
      await navigator.clipboard.writeText(snippets[activeTab].code);
      toast.success(t("token.quickstart.copied", "已复制"));
    } catch {
      toast.error(t("token.quickstart.copy-failed", "复制失败,请手动选择"));
    }
  };

  const tabKeys: QuickStartTab[] = ["openai", "claudecode", "curl"];

  return (
    <div
      className="rounded-2xl overflow-hidden"
      style={{
        background: "hsl(var(--ink))",
        border: "1px solid hsl(var(--border-soft))",
      }}
    >
      {/* Tab bar */}
      <div
        className="flex items-center justify-between border-b"
        style={{ borderColor: "hsl(var(--ink-foreground) / 0.08)" }}
      >
        <div className="flex">
          {tabKeys.map((k) => {
            const isActive = activeTab === k;
            return (
              <button
                key={k}
                type="button"
                onClick={() => onChange(k)}
                className="px-4 py-3 text-xs font-mono border-b-2 transition-colors"
                style={{
                  color: isActive
                    ? "hsl(var(--ink-foreground))"
                    : "hsl(var(--ink-foreground) / 0.55)",
                  borderColor: isActive
                    ? "hsl(var(--ink-accent))"
                    : "transparent",
                }}
              >
                {snippets[k].label}
              </button>
            );
          })}
        </div>
        <button
          type="button"
          onClick={onCopy}
          className="mr-3 inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium transition-opacity hover:opacity-80"
          style={{
            background: "hsl(var(--ink-foreground) / 0.08)",
            color: "hsl(var(--ink-foreground))",
          }}
          aria-label="copy code to clipboard"
        >
          <Copy className="w-3 h-3" />
          {/* No label on mobile; full word desktop */}
          <span className="hidden md:inline">Copy</span>
        </button>
      </div>

      {/* Code block */}
      <pre
        className="p-5 md:p-6 text-xs md:text-sm font-mono leading-relaxed overflow-x-auto whitespace-pre"
        style={{ color: "hsl(var(--ink-foreground))" }}
      >
        <code>{snippets[activeTab].code}</code>
      </pre>
    </div>
  );
}

// VsDiyTable renders the 6-row decision-clarification table for buyers
// weighing greentokey vs DIY-接 3 家. Layout: desktop = 4-col table
// (你关心的事 / greentokey ¥99/月 / 自己接 3 家 / 为什么重要), mobile =
// stacked cards per row. Color rules: greentokey 列 moss green,
// DIY 列 neutral gray (NOT competitor red — decision clarification, not
// attack). Per Codex agent 3 + tana review.
function VsDiyTable({ t }: { t: ReturnType<typeof useTranslation>["t"] }) {
  const rows = [
    {
      what: t("token.vsdiy.row.cost.what", "月度成本"),
      us: t("token.vsdiy.row.cost.us", "固定 ¥99"),
      diy: t("token.vsdiy.row.cost.diy", "多家充值,预算 ~$50-80"),
      why: t("token.vsdiy.row.cost.why", "成本可预期"),
    },
    {
      what: t("token.vsdiy.row.signup.what", "开通"),
      us: t("token.vsdiy.row.signup.us", "一个账户一次注册"),
      diy: t("token.vsdiy.row.signup.diy", "多平台注册 + Key + 支付方式"),
      why: t("token.vsdiy.row.signup.why", "降低首次门槛"),
    },
    {
      what: t("token.vsdiy.row.models.what", "模型覆盖"),
      us: t("token.vsdiy.row.models.us", "聚合多模型一处"),
      diy: t("token.vsdiy.row.models.diy", "自己维护各家 provider"),
      why: t("token.vsdiy.row.models.why", "DIY 更自由但更费心"),
    },
    {
      what: t("token.vsdiy.row.bill.what", "账单"),
      us: t("token.vsdiy.row.bill.us", "一张账单一次对账"),
      diy: t("token.vsdiy.row.bill.diy", "多处用量 + 汇率 + 充值"),
      why: t("token.vsdiy.row.bill.why", "月底不用对账"),
    },
    {
      what: t("token.vsdiy.row.fault.what", "故障 / 限额"),
      us: t("token.vsdiy.row.fault.us", "平台侧路由切换"),
      diy: t("token.vsdiy.row.fault.diy", "自己 fallback + 排查"),
      why: t("token.vsdiy.row.fault.why", "高频用明显"),
    },
    {
      what: t("token.vsdiy.row.who.what", "适合谁"),
      us: t("token.vsdiy.row.who.us", "想稳定省心使用"),
      diy: t("token.vsdiy.row.who.diy", "想深度控制路由 / 合规 / 日志"),
      why: t("token.vsdiy.row.who.why", "公平承认取舍"),
    },
  ];

  return (
    <>
      {/* Desktop: 4-col table */}
      <div className="hidden md:block">
        <table
          className="w-full border-collapse"
          style={{ borderColor: "hsl(var(--border-soft))" }}
        >
          <thead>
            <tr
              className="text-left text-xs font-medium"
              style={{ borderBottom: "2px solid hsl(var(--border-soft))" }}
            >
              <th className="py-3 pr-4 text-muted-foreground font-medium">
                {t("token.vsdiy.col.what", "你关心的事")}
              </th>
              <th
                className="py-3 px-4 font-semibold"
                style={{ color: "hsl(var(--primary-deep))" }}
              >
                {t("token.vsdiy.col.us", "greentokey ¥99/月")}
              </th>
              <th className="py-3 px-4 text-muted-foreground font-medium">
                {t("token.vsdiy.col.diy", "自己接 3 家")}
              </th>
              <th className="py-3 pl-4 text-muted-foreground font-medium">
                {t("token.vsdiy.col.why", "为什么重要")}
              </th>
            </tr>
          </thead>
          <tbody className="text-sm">
            {rows.map((r, i) => (
              <tr
                key={i}
                style={{
                  borderBottom: "1px solid hsl(var(--border-soft) / 0.6)",
                }}
              >
                <td className="py-3.5 pr-4 font-medium">{r.what}</td>
                <td className="py-3.5 px-4">
                  <span className="inline-flex items-start gap-1.5">
                    <Check
                      className="w-3.5 h-3.5 mt-0.5 flex-shrink-0"
                      style={{ color: "hsl(var(--primary))" }}
                    />
                    <span style={{ color: "hsl(var(--primary-deep))" }}>
                      {r.us}
                    </span>
                  </span>
                </td>
                <td className="py-3.5 px-4 text-secondary-foreground/70">
                  {r.diy}
                </td>
                <td className="py-3.5 pl-4 text-xs text-muted-foreground">
                  {r.why}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Mobile: stacked cards per row */}
      <div className="md:hidden space-y-4">
        {rows.map((r, i) => (
          <div
            key={i}
            className="rounded-2xl p-4"
            style={{
              background: "hsl(var(--card))",
              border: "1px solid hsl(var(--border-soft))",
            }}
          >
            <p className="text-[11px] uppercase tracking-wider text-muted-foreground mb-2 font-medium">
              {r.what}
            </p>
            <div className="space-y-2 text-sm">
              <div className="flex items-start gap-2">
                <Check
                  className="w-3.5 h-3.5 mt-1 flex-shrink-0"
                  style={{ color: "hsl(var(--primary))" }}
                />
                <div>
                  <span
                    className="text-[10px] uppercase tracking-wider mr-1.5"
                    style={{ color: "hsl(var(--primary-deep))" }}
                  >
                    greentokey
                  </span>
                  <span style={{ color: "hsl(var(--primary-deep))" }}>
                    {r.us}
                  </span>
                </div>
              </div>
              <div className="flex items-start gap-2 text-secondary-foreground/70">
                <span className="w-3.5 mt-1 text-center text-[10px]">·</span>
                <div>
                  <span className="text-[10px] uppercase tracking-wider mr-1.5 text-muted-foreground">
                    DIY
                  </span>
                  <span>{r.diy}</span>
                </div>
              </div>
              <p className="text-[11px] text-muted-foreground pl-5.5">
                {r.why}
              </p>
            </div>
          </div>
        ))}
      </div>
    </>
  );
}

export default TokenPlans;
