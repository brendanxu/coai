/**
 * /pricing — Token rate reference table. Phase 2 rewrite.
 *
 * Per mockup §1.3: "所有花费都从一个钱包扣"
 * This page is the pricing reference table — NOT a sales page.
 * TokenPlans (/token-plans) is the sales page. CTAs here link back there.
 *
 * Sections:
 *   1. Header + sub-copy
 *   2. Facts strip: 4 pills
 *   3. Per-model rate table (hardcoded from mockup — backend extension deferred)
 *   4. Footer: CSV download stub + "回到 Token Plans" link
 *
 * i18n: pricing.* namespace replaced with pricing_table.* keys.
 * Old pricing-page.* (民宿) keys untouched in cn/en.json — they are still
 * used by the 民宿 SaaS service detail page (deferred).
 */

import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowRight, Download } from "lucide-react";

import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import { Button } from "@/components/ui/button.tsx";

type ModelRow = {
  model: string;
  vendor: string;
  context: string;
  priceIn: string;
  priceOut: string;
  creditsPerMOut: string;
  cache: boolean | "cache_control";
};

const MODEL_ROWS: ModelRow[] = [
  {
    model: "GPT-4o",
    vendor: "openai",
    context: "128k",
    priceIn: "18.20",
    priceOut: "72.80",
    creditsPerMOut: "3640",
    cache: true,
  },
  {
    model: "GPT-4o mini",
    vendor: "openai",
    context: "128k",
    priceIn: "1.10",
    priceOut: "4.40",
    creditsPerMOut: "220",
    cache: true,
  },
  {
    model: "Claude 3.5 Sonnet",
    vendor: "anthropic",
    context: "200k",
    priceIn: "21.60",
    priceOut: "108.00",
    creditsPerMOut: "5400",
    cache: "cache_control",
  },
  {
    model: "DeepSeek V3",
    vendor: "deepseek",
    context: "64k",
    priceIn: "1.00",
    priceOut: "4.00",
    creditsPerMOut: "200",
    cache: true,
  },
  {
    model: "DeepSeek R1",
    vendor: "deepseek",
    context: "64k",
    priceIn: "4.00",
    priceOut: "16.00",
    creditsPerMOut: "800",
    cache: true,
  },
  {
    model: "Qwen2.5-Max",
    vendor: "阿里",
    context: "32k",
    priceIn: "8.00",
    priceOut: "24.00",
    creditsPerMOut: "1200",
    cache: true,
  },
  {
    model: "Gemini 2.0 Flash",
    vendor: "google",
    context: "1M",
    priceIn: "0.72",
    priceOut: "2.88",
    creditsPerMOut: "144",
    cache: true,
  },
  {
    model: "Kimi K2",
    vendor: "moonshot",
    context: "200k",
    priceIn: "12.00",
    priceOut: "12.00",
    creditsPerMOut: "600",
    cache: true,
  },
];

function Pricing() {
  const { t } = useTranslation();

  const facts = [
    {
      label: t("pricing_table.fact.sub.label", "订阅"),
      value: t("pricing_table.fact.sub.value", "¥99/月 · 5000 credits"),
    },
    {
      label: t("pricing_table.fact.overage.label", "超额"),
      value: t("pricing_table.fact.overage.value", "¥0.020/credit · 自动充值"),
    },
    {
      label: t("pricing_table.fact.refund.label", "退款"),
      value: t("pricing_table.fact.refund.value", "7 天未消费可退"),
    },
    {
      label: t("pricing_table.fact.invoice.label", "发票"),
      value: t("pricing_table.fact.invoice.value", "支持电子普票"),
    },
  ];

  return (
    <div className="flex-1 overflow-y-auto">
      <Header />
      <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">

        {/* ── Header ────────────────────────────────────────────────── */}
        <div className="grid md:grid-cols-[1.4fr_1fr] gap-8 mb-10">
          <div>
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
              {t("pricing_table.eyebrow", "定价")}
            </p>
            <h1 className="font-display text-3xl md:text-4xl tracking-tight mb-4 leading-tight">
              {t("pricing_table.heading1", "所有花费都")}
              <em
                className="not-italic"
                style={{ fontStyle: "italic", color: "hsl(var(--primary))" }}
              >
                {t("pricing_table.heading2", "从一个钱包扣")}
              </em>
              {t("pricing_table.heading3", "。")}
            </h1>
            <p className="text-sm text-secondary-foreground/80 leading-relaxed max-w-lg">
              {t(
                "pricing_table.sub",
                "主线只有一个 Token 套餐档。其余按使用量扣 credits，服务市场上线后按次计费，共用同一余额。",
              )}
            </p>
          </div>
          <div
            className="self-end rounded-xl p-4 text-sm"
            style={{
              background: "hsl(var(--card))",
              border: "1px solid hsl(var(--border-soft))",
            }}
          >
            <p className="text-xs uppercase tracking-[0.14em] text-muted-foreground mb-2">
              {t("pricing_table.note.label", "主页提醒")}
            </p>
            <p className="text-xs text-secondary-foreground/80 leading-relaxed">
              {t(
                "pricing_table.note.body",
                "本页与 /token-plans 文案不冲突——TokenPlans 是销售页，Pricing 是参照表。点击 CTA 都会跳到 TokenPlans 完成购买。",
              )}
            </p>
          </div>
        </div>

        {/* ── Facts strip ───────────────────────────────────────────── */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-3 mb-10">
          {facts.map((f) => (
            <div
              key={f.label}
              className="rounded-xl p-4"
              style={{
                background: "hsl(var(--card))",
                border: "1px solid hsl(var(--border-soft))",
              }}
            >
              <p className="text-[10px] uppercase tracking-[0.14em] text-muted-foreground mb-1.5">
                {f.label}
              </p>
              <p className="font-display text-lg leading-tight">{f.value}</p>
            </div>
          ))}
        </div>

        {/* ── Rate table ────────────────────────────────────────────── */}
        <div
          className="rounded-2xl overflow-hidden mb-8"
          style={{
            border: "1px solid hsl(var(--border-soft))",
          }}
        >
          {/* Desktop */}
          <div className="hidden md:block overflow-x-auto">
            <table className="w-full text-sm border-collapse">
              <thead>
                <tr
                  className="text-left"
                  style={{
                    background: "hsl(var(--muted) / 0.5)",
                    borderBottom: "1px solid hsl(var(--border-soft))",
                  }}
                >
                  <th className="px-5 py-3 font-medium text-xs text-muted-foreground">
                    {t("pricing_table.col.model", "模型")}
                  </th>
                  <th className="px-4 py-3 font-medium text-xs text-muted-foreground">
                    {t("pricing_table.col.vendor", "厂商")}
                  </th>
                  <th className="px-4 py-3 font-medium text-xs text-muted-foreground">
                    {t("pricing_table.col.context", "上下文")}
                  </th>
                  <th className="px-4 py-3 font-medium text-xs text-muted-foreground text-right">
                    {t("pricing_table.col.price_in", "¥ / 1M in")}
                  </th>
                  <th className="px-4 py-3 font-medium text-xs text-muted-foreground text-right">
                    {t("pricing_table.col.price_out", "¥ / 1M out")}
                  </th>
                  <th className="px-4 py-3 font-medium text-xs text-muted-foreground text-right">
                    {t("pricing_table.col.credits", "credits / 1M out")}
                  </th>
                  <th className="px-4 py-3 font-medium text-xs text-muted-foreground">
                    {t("pricing_table.col.cache", "cache")}
                  </th>
                </tr>
              </thead>
              <tbody>
                {MODEL_ROWS.map((row, i) => (
                  <tr
                    key={row.model}
                    className="hover:bg-muted/20 transition-colors"
                    style={
                      i < MODEL_ROWS.length - 1
                        ? { borderBottom: "1px solid hsl(var(--border-soft) / 0.6)" }
                        : undefined
                    }
                  >
                    <td className="px-5 py-3.5 font-medium">{row.model}</td>
                    <td className="px-4 py-3.5 font-mono text-xs text-muted-foreground">
                      {row.vendor}
                    </td>
                    <td className="px-4 py-3.5 font-mono text-xs text-muted-foreground">
                      {row.context}
                    </td>
                    <td className="px-4 py-3.5 font-mono text-xs text-right tabular-nums">
                      {row.priceIn}
                    </td>
                    <td className="px-4 py-3.5 font-mono text-xs text-right tabular-nums">
                      {row.priceOut}
                    </td>
                    <td className="px-4 py-3.5 font-mono text-xs text-right tabular-nums">
                      {row.creditsPerMOut}
                    </td>
                    <td className="px-4 py-3.5">
                      {row.cache === "cache_control" ? (
                        <span
                          className="inline-block text-[10px] px-2 py-0.5 rounded-full font-medium"
                          style={{
                            background: "hsl(var(--accent-soft))",
                            color: "hsl(var(--primary-deep))",
                          }}
                        >
                          cache_control
                        </span>
                      ) : row.cache ? (
                        <span
                          className="inline-block text-[10px] px-2 py-0.5 rounded-full font-medium"
                          style={{
                            background: "hsl(var(--accent-soft))",
                            color: "hsl(var(--primary-deep))",
                          }}
                        >
                          {t("pricing_table.cache.yes", "支持")}
                        </span>
                      ) : (
                        <span className="text-muted-foreground text-xs">—</span>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          {/* Mobile stacked cards */}
          <div className="md:hidden divide-y divide-border">
            {MODEL_ROWS.map((row) => (
              <div key={row.model} className="px-5 py-4">
                <div className="flex items-start justify-between gap-3 mb-2">
                  <div>
                    <p className="font-medium text-sm">{row.model}</p>
                    <p className="font-mono text-xs text-muted-foreground mt-0.5">
                      {row.vendor} · {row.context}
                    </p>
                  </div>
                  {row.cache && (
                    <span
                      className="text-[10px] px-2 py-0.5 rounded-full font-medium flex-shrink-0"
                      style={{
                        background: "hsl(var(--accent-soft))",
                        color: "hsl(var(--primary-deep))",
                      }}
                    >
                      {row.cache === "cache_control" ? "cache_control" : t("pricing_table.cache.yes", "支持")}
                    </span>
                  )}
                </div>
                <div className="grid grid-cols-3 gap-2 text-xs font-mono tabular-nums text-right">
                  <div>
                    <p className="text-muted-foreground mb-0.5 text-[10px] text-left">¥/1M in</p>
                    <p>{row.priceIn}</p>
                  </div>
                  <div>
                    <p className="text-muted-foreground mb-0.5 text-[10px] text-left">¥/1M out</p>
                    <p>{row.priceOut}</p>
                  </div>
                  <div>
                    <p className="text-muted-foreground mb-0.5 text-[10px] text-left">credits/1M</p>
                    <p>{row.creditsPerMOut}</p>
                  </div>
                </div>
              </div>
            ))}
          </div>
        </div>

        {/* ── Footer actions ────────────────────────────────────────── */}
        <div className="flex flex-wrap items-center justify-end gap-3">
          <Button variant="ghost" size="sm" disabled className="gap-1.5">
            <Download className="w-3.5 h-3.5" />
            {t("pricing_table.csv_download", "下载完整价表 .csv")}
          </Button>
          <Link to="/token-plans">
            <Button size="sm" className="gap-1.5 rounded-full">
              {t("pricing_table.back_to_plans", "回到 Token Plans 开通")}
              <ArrowRight className="w-3.5 h-3.5" />
            </Button>
          </Link>
        </div>
      </div>

      <Footer />
    </div>
  );
}

export default Pricing;
