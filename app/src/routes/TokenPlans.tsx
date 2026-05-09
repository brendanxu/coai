import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import axios from "axios";
import {
  ArrowRight,
  BadgeCheck,
  Check,
  Code2,
  Gauge,
  Key,
  Network,
  RefreshCcw,
  ShieldCheck,
  Wallet,
} from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";

/**
 * /token-plans — Token 套餐 dedicated product page.
 *
 * Distinct from /services (service marketplace tiles). Token 套餐 = the
 * raw-compute axis of greentokey: one OpenAI-compatible Key into a pool
 * of GPT/Claude/DeepSeek/Qwen, billed in credits.
 *
 * v0.17 redesign rationale: previous version was a single ¥99 card and
 * read like one item from /services. Founder feedback (2026-05-09): need
 * a real product page with live pool + code sample + tier multiplier
 * breakdown so visitors immediately see this is infrastructure, not a
 * service offering.
 *
 * Sections:
 *   1. Hero — eyebrow + 2-line headline + sub
 *   2. Pricing card — ¥99 standard plan + features + CTA
 *   3. Live pool snapshot — /api/gtk/v1/pool (top 8 models + latency)
 *   4. Code sample — OpenAI SDK switching just the model field
 *   5. Tier multiplier strip — Light 0.5×, Standard 1×, Premium 3×
 *   6. Why 3 cards — one Key / credit abstraction / pool routing
 *   7. FAQ — billing / sub2API / privacy / cancellation
 *   8. Trust strip — concierge + transparent pricing + zero retention
 *
 * Checkout flow: lead-capture (founder follows up with payment +
 * LemonSqueezy provisioning). LS variants deferred per
 * memory/ls-variants-deferred.md.
 */
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

function TokenPlans() {
  const { t } = useTranslation();
  const [contactOpen, setContactOpen] = useState(false);
  const [pool, setPool] = useState<PoolSnapshot | null>(null);

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

  return (
    <>
      <Header />
      <main className="flex-1 overflow-y-auto">
        <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
          {/* ─── 1. Hero ─────────────────────────────────────────────── */}
          <header className="text-center mb-12 md:mb-16">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
              {t("token.eyebrow", "Token 套餐 · 模型池接入")}
            </p>
            <h1 className="font-display text-4xl md:text-6xl tracking-tight mb-5 leading-[1.05]">
              {t("token.heading.line1", "一个 Key,")}
              <br />
              <span style={{ color: "hsl(var(--primary))" }}>
                {t("token.heading.line2", "一池模型。")}
              </span>
            </h1>
            <p className="text-base md:text-lg text-secondary-foreground/80 max-w-2xl mx-auto leading-relaxed">
              {t(
                "token.sub",
                "OpenAI 兼容 endpoint,Bearer 鉴权。GPT / Claude / DeepSeek / Qwen 之间切换只改 model 字段——不换账号、不换 Key。底层 NewAPI 路由,sub2API 桥接官方 API 进行中。",
              )}
            </p>
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
                    "切模型只改 model 字段——路由 + 计费自动",
                  )}
                />
                <FeatureRow
                  text={t(
                    "token.plan.feat.4",
                    "Light / Standard / Premium 分档:DeepSeek/Qwen 0.5×、Claude-haiku 1×、GPT-4o/Sonnet 3×",
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
                <ArrowRight className="w-4 h-4 ml-1.5" />
              </Button>
              <p className="text-xs text-center text-muted-foreground mt-3">
                {t(
                  "token.plan.disclaimer",
                  "首批客户 founder 一对一沟通 · 微信/支付宝/银行转账皆可 · LemonSqueezy 海外卡支付即将开通",
                )}
              </p>
            </div>
          </div>

          {/* ─── 3. Live pool snapshot ───────────────────────────────── */}
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

          {/* ─── 4. Code sample ──────────────────────────────────────── */}
          <section className="mb-16 md:mb-20">
            <div className="text-center mb-8">
              <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-2">
                {t("token.code.eyebrow", "OpenAI SDK 即插即用")}
              </p>
              <h3 className="font-display text-2xl md:text-3xl">
                {t("token.code.heading", "切模型 = 改一行字。")}
              </h3>
            </div>

            <div
              className="rounded-2xl overflow-hidden"
              style={{
                background: "hsl(var(--ink))",
                border: "1px solid hsl(var(--border-soft))",
              }}
            >
              <div
                className="flex items-center gap-2 px-4 py-3 text-xs"
                style={{
                  borderBottom: "1px solid hsl(var(--ink-foreground) / 0.08)",
                  color: "hsl(var(--ink-foreground) / 0.65)",
                }}
              >
                <Code2 className="w-3.5 h-3.5" />
                <span className="font-mono">node.js · OpenAI SDK</span>
              </div>
              <pre
                className="p-5 md:p-6 text-xs md:text-sm font-mono leading-relaxed overflow-x-auto"
                style={{ color: "hsl(var(--ink-foreground))" }}
              >
                <code>
                  <span style={{ color: "hsl(var(--ink-accent))" }}>
                    {"import"}
                  </span>{" "}
                  OpenAI{" "}
                  <span style={{ color: "hsl(var(--ink-accent))" }}>from</span>{" "}
                  <span style={{ opacity: 0.85 }}>{`"openai"`}</span>;
                  {"\n\n"}
                  <span style={{ color: "hsl(var(--ink-accent))" }}>
                    {"const"}
                  </span>{" "}
                  client = <span style={{ color: "hsl(var(--ink-accent))" }}>new</span>{" "}
                  OpenAI({"{\n"}
                  {"  baseURL: "}
                  <span style={{ opacity: 0.85 }}>
                    {`"https://api.greentokey.com/v1"`}
                  </span>
                  ,{"\n"}
                  {"  apiKey:  "}
                  <span style={{ opacity: 0.85 }}>{`"sk-xxxxxx"`}</span>
                  ,{"\n"}
                  {"});\n\n"}
                  <span style={{ color: "hsl(var(--ink-accent))" }}>{"// "}</span>
                  <span style={{ color: "hsl(var(--ink-accent))" }}>
                    {t(
                      "token.code.comment",
                      "切到 GPT-4o 只改 model 字段 — 计费自动按 Premium 档",
                    )}
                  </span>
                  {"\n"}
                  client.chat.completions.create({"{\n"}
                  {"  model: "}
                  <span style={{ opacity: 0.85 }}>
                    {`"gpt-4o"`}
                  </span>
                  ,    {/* swap to "deepseek-v3" / "claude-sonnet-4" / ... */}
                  {"\n"}
                  {"  messages: [...],\n"}
                  {"});"}
                </code>
              </pre>
            </div>
            <p className="text-xs text-muted-foreground text-center mt-4">
              {t(
                "token.code.hint",
                "endpoint 100% OpenAI 兼容 · Stream / function-call / vision 全支持",
              )}
            </p>
          </section>

          {/* ─── 5. Tier multiplier strip ───────────────────────────── */}
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

          {/* ─── 6. Why 3 cards ─────────────────────────────────────── */}
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
                title={t("token.why.three.title", "Pool 自动调度")}
                body={t(
                  "token.why.three.body",
                  "底层 NewAPI 路由:对单次调用,自动选可用且最便宜的渠道。某家挂了你看不见。",
                )}
              />
            </div>
          </section>

          {/* ─── 7. FAQ ──────────────────────────────────────────────── */}
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

          {/* ─── 8. Trust strip ─────────────────────────────────────── */}
          <div className="text-center pt-12 border-t border-border">
            <div className="flex flex-wrap justify-center gap-6 text-xs text-muted-foreground">
              <TrustItem
                icon={<BadgeCheck className="w-3.5 h-3.5" />}
                text={t("token.trust.concierge", "首批客户 founder 一对一开通")}
              />
              <TrustItem
                icon={<Gauge className="w-3.5 h-3.5" />}
                text={t("token.trust.transparent", "三档计费 0 隐藏成本")}
              />
              <TrustItem
                icon={<ShieldCheck className="w-3.5 h-3.5" />}
                text={t("token.trust.privacy", "调用内容 0 持久化")}
              />
              <TrustItem
                icon={<RefreshCcw className="w-3.5 h-3.5" />}
                text={t("token.trust.cancel", "随时取消 · 余额用完为止")}
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
    <div
      className="rounded-2xl p-6"
      style={{
        background: "hsl(var(--card))",
        border: "1px solid hsl(var(--border-soft))",
      }}
    >
      <div
        className="inline-flex items-center justify-center w-10 h-10 rounded-xl mb-4"
        style={{
          background: "hsl(var(--accent-soft))",
          color: "hsl(var(--primary))",
        }}
      >
        {icon}
      </div>
      <h4 className="font-display text-lg font-medium mb-2">{title}</h4>
      <p className="text-sm leading-relaxed text-secondary-foreground/85">
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
      className="rounded-2xl p-6 flex flex-col gap-3"
      style={{
        background: highlight ? "hsl(var(--accent-soft))" : "hsl(var(--card))",
        border: highlight
          ? "1px solid hsl(var(--primary) / 0.35)"
          : "1px solid hsl(var(--border-soft))",
        boxShadow: highlight ? "var(--shadow-xs)" : "none",
      }}
    >
      <span
        className="text-[11px] uppercase tracking-[0.14em] font-medium"
        style={{ color: "hsl(var(--primary-deep))" }}
      >
        {badge}
      </span>
      <p className="text-sm leading-relaxed text-secondary-foreground/85">
        {models}
      </p>
      <div className="flex items-baseline gap-2 mt-auto pt-2">
        <span className="font-display text-2xl font-semibold">{rate}</span>
      </div>
      <p className="text-xs text-muted-foreground">{volume}</p>
    </div>
  );
}

function FaqItem({ question, answer }: { question: string; answer: string }) {
  return (
    <div className="space-y-2">
      <h3 className="font-medium text-base">{question}</h3>
      <p className="text-sm text-secondary-foreground/85 leading-relaxed">
        {answer}
      </p>
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

export default TokenPlans;
