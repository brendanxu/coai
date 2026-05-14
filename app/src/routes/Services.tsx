import { useState } from "react";
import { useTranslation } from "react-i18next";
import { ArrowRight } from "lucide-react";

import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";

/**
 * /services — service marketplace.
 *
 * Per mockup §4.1: graceful empty/placeholder state.
 * "内测中 · 5月底开放" — do NOT build a full service marketplace.
 * Two placeholder cards: 民宿小红书 SaaS + token-bundle.
 * CTAs wire to ContactDialog.
 */
export default function Services() {
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
          {/* Header */}
          <header className="text-center mb-14 md:mb-16">
            <span
              className="inline-block text-[11px] uppercase tracking-[0.18em] px-3 py-1 rounded-full font-medium mb-5"
              style={{
                background: "hsl(var(--accent-soft))",
                color: "hsl(var(--primary-deep))",
              }}
            >
              {t("services.placeholder.eyebrow", "内测中 · 5月底开放")}
            </span>
            <h1 className="font-display text-4xl md:text-5xl tracking-tight mb-5">
              {t("services.placeholder.heading", "服务市场")}
            </h1>
            <p className="text-base text-secondary-foreground/80 max-w-xl mx-auto leading-relaxed">
              {t(
                "services.placeholder.sub",
                "我们正在为你准备专属的 AI 服务，敬请期待。",
              )}
            </p>
          </header>

          {/* 2 placeholder service cards */}
          <div className="grid grid-cols-1 md:grid-cols-2 gap-6 max-w-3xl mx-auto mb-16">
            <PlaceholderCard
              badge={t("services.placeholder.card1.badge", "代运营 · 按月")}
              title={t("services.placeholder.card1.title", "民宿小红书 SaaS")}
              price={t("services.placeholder.card1.price", "¥1980/月")}
              body={t(
                "services.placeholder.card1.body",
                "AI 帮你写文案、出图、自动发布、应对评论私信。每月全托管，比本地 MCN 更稳更便宜。",
              )}
              onContact={() => setContactOpen(true)}
              t={t}
            />
            <PlaceholderCard
              badge={t("services.placeholder.card2.badge", "算力包 · 按次")}
              title={t("services.placeholder.card2.title", "Token 增量包")}
              price={t("services.placeholder.card2.price", "即将上线")}
              body={t(
                "services.placeholder.card2.body",
                "按需补充 credits，不想订月套餐时按次购买——接入同一 API Key，扣同一个钱包。",
              )}
              onContact={() => setContactOpen(true)}
              t={t}
            />
          </div>

          {/* Footer note */}
          <p className="text-center text-sm text-muted-foreground">
            {t(
              "services.placeholder.footer",
              "想提前体验？",
            )}{" "}
            <button
              type="button"
              onClick={() => setContactOpen(true)}
              className="underline underline-offset-4 hover:opacity-70 transition-opacity"
              style={{ color: "hsl(var(--primary))" }}
            >
              {t("services.placeholder.footer_cta", "联系我们加入内测")}
            </button>
          </p>
        </div>
        <Footer />
      </main>

      <ContactDialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        source="services"
        contextLabel={t("services.placeholder.contact_label", "服务市场内测")}
      />
    </>
  );
}

function PlaceholderCard({
  badge,
  title,
  price,
  body,
  onContact,
  t,
}: {
  badge: string;
  title: string;
  price: string;
  body: string;
  onContact: () => void;
  t: ReturnType<typeof useTranslation>["t"];
}) {
  return (
    <article
      className="rounded-3xl p-7 md:p-8 flex flex-col"
      style={{
        background: "hsl(var(--card))",
        border: "1px solid hsl(var(--border-soft))",
        boxShadow: "var(--shadow-xs)",
      }}
    >
      <span
        className="text-[10px] uppercase tracking-[0.14em] px-2 py-0.5 rounded-full font-medium mb-4 w-fit"
        style={{
          background: "hsl(var(--accent-soft))",
          color: "hsl(var(--primary-deep))",
        }}
      >
        {badge}
      </span>

      <h3 className="font-display text-xl mb-2 leading-tight">{title}</h3>

      <p className="font-display text-2xl font-semibold mb-3"
        style={{ color: "hsl(var(--primary))" }}
      >
        {price}
      </p>

      <p className="text-sm leading-relaxed text-secondary-foreground/85 mb-6 flex-1">
        {body}
      </p>

      <div className="flex gap-3">
        <button
          type="button"
          onClick={onContact}
          className="inline-flex items-center gap-1.5 text-sm font-medium hover:opacity-70 transition-opacity"
          style={{ color: "hsl(var(--primary))" }}
        >
          {t("services.placeholder.cta_detail", "了解详情")}
          <ArrowRight className="w-3.5 h-3.5" />
        </button>
        <button
          type="button"
          onClick={onContact}
          className="inline-flex items-center gap-1.5 text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
        >
          {t("services.placeholder.cta_contact", "联系我们")}
        </button>
      </div>
    </article>
  );
}
