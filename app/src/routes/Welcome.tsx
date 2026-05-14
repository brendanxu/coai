import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Key, Grid3x3, Network } from "lucide-react";
import { Link } from "react-router-dom";

import Hero from "@/components/Marketing/Hero.tsx";
import MainServiceCards from "@/components/Marketing/MainServiceCards.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";


/**
 * greentokey marketing landing — v0.24 dual hero mode.
 *
 * Mode toggle pill at top: "Token API" / "民宿 SaaS"
 *   - localStorage key: gtk_homeMode (default = "token")
 * Token mode: developer-first hero + live pool rail + 4 mcards + values strip
 * 民宿 mode: existing v0.11 民宿 content untouched, conditionally rendered
 */

type HomeMode = "token" | "mansion";

const HOME_MODE_KEY = "gtk_homeMode";

function Welcome() {
  const { t } = useTranslation();
  const [contactOpen, setContactOpen] = useState(false);
  const [mode, setMode] = useState<HomeMode>(() => {
    try {
      const stored = localStorage.getItem(HOME_MODE_KEY);
      return stored === "mansion" ? "mansion" : "token";
    } catch {
      return "token";
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem(HOME_MODE_KEY, mode);
    } catch {
      // ignore storage errors
    }
  }, [mode]);

  return (
    <>
      <Header />
      <main
        className="flex-1 overflow-y-auto"
        style={{
          paddingLeft: "max(env(safe-area-inset-left), 0px)",
          paddingRight: "max(env(safe-area-inset-right), 0px)",
        }}
      >
        {/* ── Mode toggle pill ─────────────────────────────────────── */}
        <div className="flex justify-center pt-8 pb-2">
          <div
            className="inline-flex rounded-full p-0.5 gap-0.5"
            style={{
              background: "hsl(var(--muted))",
              border: "1px solid hsl(var(--border))",
            }}
            role="group"
            aria-label={t("welcome.mode_toggle.label", "首页模式切换")}
          >
            <button
              type="button"
              onClick={() => setMode("token")}
              className="px-4 py-1.5 rounded-full text-xs font-medium transition-colors"
              style={
                mode === "token"
                  ? {
                      background: "hsl(var(--accent))",
                      color: "#fff",
                    }
                  : {
                      background: "transparent",
                      color: "hsl(var(--muted-foreground))",
                    }
              }
            >
              {t("welcome.mode_toggle.token", "Token API")}
            </button>
            <button
              type="button"
              onClick={() => setMode("mansion")}
              className="px-4 py-1.5 rounded-full text-xs font-medium transition-colors"
              style={
                mode === "mansion"
                  ? {
                      background: "hsl(var(--accent))",
                      color: "#fff",
                    }
                  : {
                      background: "transparent",
                      color: "hsl(var(--muted-foreground))",
                    }
              }
            >
              {t("welcome.mode_toggle.mansion", "民宿 SaaS")}
            </button>
          </div>
        </div>

        {/* ── Token mode ────────────────────────────────────────────── */}
        {mode === "token" && (
          <div
            className="mx-auto px-6"
            style={{ maxWidth: "var(--max-content)" }}
          >
            {/* Token hero: copy + live pool rail */}
            <div className="py-12 md:py-20">
              <Hero />
            </div>

            {/* 4 marketing cards (mcards) */}
            <div className="pb-16 md:pb-20">
              <div className="grid grid-cols-1 md:grid-cols-2 gap-5 md:gap-6">
                <MCard
                  dark
                  badge={t("welcome.mcard.token.badge", "主线 · 已上线")}
                  title={t("welcome.mcard.token.title", "Token 套餐")}
                  accent={t("welcome.mcard.token.accent", "一个 Key 调一池")}
                  body={t(
                    "home.mcard.token.body",
                    "¥99/月，5000 credits。所有模型走同一 endpoint，按上游计价，prompt cache 自动透传。微信/支付宝/卡均可付。",
                  )}
                  features={[
                    t("welcome.mcard.token.f1", "OpenAI-compatible base URL"),
                    t("welcome.mcard.token.f2", "14+ 模型 · 一个 Key 直调"),
                    t("welcome.mcard.token.f3", "实时用量 + cache 节省视图"),
                  ]}
                  price={t("welcome.mcard.token.price", "¥99")}
                  priceSub={t("welcome.mcard.token.priceSub", "/月")}
                  cta={t("welcome.mcard.token.cta", "立即开通 →")}
                  ctaTo="/token-plans"
                />
                <MCard
                  soon
                  badge={t("welcome.mcard.market.badge", "主线 · 占位")}
                  title={t("welcome.mcard.market.title", "服务市场")}
                  accent={t("welcome.mcard.market.accent", "买一组现成服务")}
                  body={t(
                    "home.mcard.market.body",
                    "「智能体 + Token 包」按次卖——小红书代运营、资料制作、DIY 智能体。底层仍是算力，扣同一个钱包。",
                  )}
                  features={[
                    t("welcome.mcard.market.f1", "调好的 Prompt + workflow"),
                    t("welcome.mcard.market.f2", "一次成稿 · 不用调模型"),
                    t("welcome.mcard.market.f3", "共享同一钱包"),
                  ]}
                  cta={t("welcome.mcard.market.cta", "订阅上线提醒")}
                  ctaTo="/services"
                  pricePlaceholder={t("welcome.mcard.market.pricePlaceholder", "敬请期待")}
                />
              </div>
            </div>

            {/* "支付丝滑" values strip */}
            <div className="pb-20 md:pb-28">
              <div className="text-center mb-10">
                <h2 className="font-display text-2xl md:text-3xl mb-3">
                  {t("welcome.values.heading", "为什么这样设计")}
                </h2>
                <p className="text-sm text-muted-foreground max-w-xl mx-auto">
                  {t(
                    "home.values.sub",
                    "底层是算力，上层是体验。Token 套餐让你直接调用，服务市场让你按结果买。",
                  )}
                </p>
              </div>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-8 md:gap-12">
                <ValueProp
                  icon={<Key className="w-6 h-6" strokeWidth={1.6} />}
                  title={t("welcome.values.one-key.title", "一 Key 通模型")}
                  body={t(
                    "home.values.one-key.body",
                    "NewAPI 在底层调度，你拿到的是一个 OpenAI 兼容 endpoint——切模型只改 model 字段，不再换账号、换 Key。",
                  )}
                />
                <ValueProp
                  icon={<Grid3x3 className="w-6 h-6" strokeWidth={1.6} />}
                  title={t("welcome.values.agent.title", "智能体即服务")}
                  body={t(
                    "home.values.agent.body",
                    "市场里的 DIY 智能体 = 我们调好的 Prompt + Token 包。你按次买，我们扣算力，效果稳定可复制。",
                  )}
                />
                <ValueProp
                  icon={<Network className="w-6 h-6" strokeWidth={1.6} />}
                  title={t("welcome.values.sub2api.title", "sub2API 扩池(规划)")}
                  body={t(
                    "home.values.sub2api.body",
                    "把 Claude / OpenAI 官方 API 桥接进 Token 池。聚合更全、合规接入，不挤占第三方中转的不稳定产能。",
                  )}
                />
              </div>
            </div>
          </div>
        )}

        {/* ── 民宿 mode — keep ALL existing content untouched ──────── */}
        {mode === "mansion" && (
          <div
            className="mx-auto px-6"
            style={{ maxWidth: "var(--max-content)" }}
          >
            {/* Original Hero (dual-rail, developer-focused — reused) */}
            <div className="py-16 md:py-24">
              <Hero />
            </div>

            {/* Original service cards */}
            <div className="pb-20 md:pb-28">
              <MainServiceCards />
            </div>

            {/* Original value props */}
            <div className="pb-24 md:pb-32">
              <h2 className="font-display text-2xl md:text-3xl text-center mb-3">
                {t("welcome.values.heading", "为什么这样设计")}
              </h2>
              <p className="text-center text-sm text-muted-foreground mb-12 max-w-xl mx-auto">
                {t(
                  "home.values.sub",
                  "底层是算力，上层是体验。Token 套餐让你直接调用，服务市场让你按结果买。",
                )}
              </p>
              <div className="grid grid-cols-1 md:grid-cols-3 gap-8 md:gap-12">
                <ValueProp
                  icon={<Key className="w-6 h-6" strokeWidth={1.6} />}
                  title={t("welcome.values.one-key.title", "一 Key 通模型")}
                  body={t(
                    "home.values.one-key.body",
                    "NewAPI 在底层调度，你拿到的是一个 OpenAI 兼容 endpoint——切模型只改 model 字段，不再换账号、换 Key。",
                  )}
                />
                <ValueProp
                  icon={<Grid3x3 className="w-6 h-6" strokeWidth={1.6} />}
                  title={t("welcome.values.agent.title", "智能体即服务")}
                  body={t(
                    "home.values.agent.body",
                    "市场里的 DIY 智能体 = 我们调好的 Prompt + Token 包。你按次买，我们扣算力，效果稳定可复制。",
                  )}
                />
                <ValueProp
                  icon={<Network className="w-6 h-6" strokeWidth={1.6} />}
                  title={t("welcome.values.sub2api.title", "sub2API 扩池(规划)")}
                  body={t(
                    "home.values.sub2api.body",
                    "把 Claude / OpenAI 官方 API 桥接进 Token 池。聚合更全、合规接入，不挤占第三方中转的不稳定产能。",
                  )}
                />
              </div>
            </div>
          </div>
        )}

        <Footer />
      </main>

      <ContactDialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        source="home"
      />
    </>
  );
}

function ValueProp({
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
      <span
        className="inline-flex items-center justify-center w-10 h-10 rounded-xl"
        style={{ color: "hsl(var(--primary))" }}
      >
        {icon}
      </span>
      <h4 className="font-display text-lg font-medium">{title}</h4>
      <p className="text-sm leading-relaxed text-secondary-foreground/85">
        {body}
      </p>
    </div>
  );
}

// MCard — dual main service card used in Token mode hero section.
// Matches mockup §1.1 "mcards" layout: dark variant for Token 套餐,
// light variant for 服务市场 (with SOON badge).
type MCardProps = {
  dark?: boolean;
  soon?: boolean;
  badge: string;
  title: string;
  accent: string;
  body: string;
  features: string[];
  price?: string;
  priceSub?: string;
  pricePlaceholder?: string;
  cta: string;
  ctaTo: string;
};

function MCard({
  dark,
  soon,
  badge,
  title,
  accent,
  body,
  features,
  price,
  priceSub,
  pricePlaceholder,
  cta,
  ctaTo,
}: MCardProps) {
  return (
    <article
      className="relative rounded-3xl p-7 md:p-8 flex flex-col"
      style={{
        background: dark ? "hsl(var(--ink))" : "hsl(var(--card))",
        color: dark ? "hsl(var(--ink-foreground))" : undefined,
        border: dark
          ? "1px solid hsl(var(--ink))"
          : "1px solid hsl(var(--border-soft))",
        boxShadow: dark ? "var(--shadow-ink)" : "var(--shadow-xs)",
      }}
    >
      {soon && (
        <span
          className="absolute top-5 right-5 text-[10px] uppercase tracking-[0.14em] px-2 py-0.5 rounded-full font-medium"
          style={{
            background: "hsl(var(--accent-soft))",
            color: "hsl(var(--primary-deep))",
          }}
        >
          SOON
        </span>
      )}

      <span
        className="inline-block text-[10px] uppercase tracking-[0.14em] px-2 py-0.5 rounded-full font-medium mb-4 w-fit"
        style={{
          background: dark
            ? "hsl(var(--ink-foreground) / 0.12)"
            : "hsl(var(--accent-soft))",
          color: dark
            ? "hsl(var(--ink-foreground) / 0.7)"
            : "hsl(var(--primary-deep))",
        }}
      >
        {badge}
      </span>

      <h3
        className="font-display text-2xl md:text-3xl leading-tight mb-1"
        style={{ color: dark ? "hsl(var(--ink-foreground))" : undefined }}
      >
        {title}
        <br />
        <em
          style={{
            fontStyle: "italic",
            color: dark ? "hsl(var(--ink-accent))" : "hsl(var(--primary))",
            fontWeight: 400,
          }}
        >
          {accent}
        </em>
      </h3>

      <p
        className="text-sm leading-relaxed mb-5 mt-3"
        style={{
          color: dark
            ? "hsl(var(--ink-foreground) / 0.75)"
            : "hsl(var(--secondary-foreground) / 0.85)",
        }}
      >
        {body}
      </p>

      <ul className="space-y-2 mb-6 text-sm flex-1">
        {features.map((f, i) => (
          <li key={i} className="flex items-start gap-2">
            <svg
              className="w-4 h-4 mt-0.5 flex-shrink-0"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
              style={{
                color: dark ? "hsl(var(--ink-accent))" : "hsl(var(--primary))",
              }}
            >
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span
              style={{
                color: dark
                  ? "hsl(var(--ink-foreground) / 0.85)"
                  : undefined,
              }}
            >
              {f}
            </span>
          </li>
        ))}
      </ul>

      <div className="flex items-center gap-3 mt-auto pt-2">
        {price ? (
          <span
            className="font-display text-2xl font-semibold"
            style={{
              color: dark ? "hsl(var(--ink-foreground))" : undefined,
            }}
          >
            {price}
            <small
              className="font-sans text-sm font-normal ml-0.5"
              style={{
                color: dark
                  ? "hsl(var(--ink-foreground) / 0.55)"
                  : "hsl(var(--muted-foreground))",
              }}
            >
              {priceSub}
            </small>
          </span>
        ) : (
          <span
            className="text-sm"
            style={{
              color: dark
                ? "hsl(var(--ink-foreground) / 0.55)"
                : "hsl(var(--muted-foreground))",
            }}
          >
            {pricePlaceholder}
          </span>
        )}
        <Link
          to={ctaTo}
          className="ml-auto inline-flex items-center gap-1.5 px-5 py-2.5 rounded-full text-sm font-medium transition-opacity hover:opacity-80"
          style={{
            background: dark
              ? "hsl(var(--ink-foreground) / 0.12)"
              : "hsl(var(--accent))",
            color: dark ? "hsl(var(--ink-foreground))" : "#fff",
          }}
        >
          {cta}
        </Link>
      </div>
    </article>
  );
}

export default Welcome;
