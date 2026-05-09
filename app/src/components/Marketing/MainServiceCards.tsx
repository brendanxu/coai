import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { Key, Grid3x3, Network } from "lucide-react";

/**
 * 3 main service cards on the homepage. Matches v0.7 redesign-v2.html
 * lines 906-934 (Token 套餐 / 服务市场 / sub2API · 扩池).
 *
 * Layout: 3-column grid on desktop, 1-column on mobile. Each card uses
 * the cream paper-white card surface (--card) with moss accent on icon.
 */
export default function MainServiceCards() {
  const { t } = useTranslation();

  return (
    <section className="grid grid-cols-1 md:grid-cols-3 gap-5 md:gap-6">
      <ServiceCard
        icon={<Key className="w-6 h-6" />}
        title={t("home.cards.token.title", "Token 套餐")}
        body={t(
          "home.cards.token.body",
          "一个 API Key，接入 GPT / Claude / DeepSeek / Qwen 等十余款模型。基于 NewAPI 路由，按 Token 计费，余额可见。",
        )}
        ctaLabel={t("home.cards.token.cta", "查看套餐")}
        to="/token-plans"
      />
      <ServiceCard
        icon={<Grid3x3 className="w-6 h-6" />}
        title={t("home.cards.market.title", "服务市场")}
        body={t(
          "home.cards.market.body",
          "小红书代运营、资料制作、DIY 智能体——已上架。后者本质是「智能体 + Token 包」按次卖，底层仍是算力。",
        )}
        ctaLabel={t("home.cards.market.cta", "逛市场")}
        to="/services"
      />
      <ServiceCard
        icon={<Network className="w-6 h-6" />}
        title={t("home.cards.sub2api.title", "sub2API · 扩池")}
        body={t(
          "home.cards.sub2api.body",
          "把 Claude / OpenAI 官方 API 通过 sub2API 接入到 Token 池里——用一个 Key 调更全的模型，无需切换账户。",
        )}
        ctaLabel={t("home.cards.sub2api.cta", "加入等待列表")}
        to="/contact"
        soon
      />
    </section>
  );
}

type CardProps = {
  icon: React.ReactNode;
  title: string;
  body: string;
  ctaLabel: string;
  to: string;
  soon?: boolean;
};

function ServiceCard({ icon, title, body, ctaLabel, to, soon }: CardProps) {
  return (
    <article
      className="relative rounded-3xl p-7 md:p-8 transition-shadow hover:shadow-md active:shadow-md"
      style={{
        background: "hsl(var(--card))",
        boxShadow: "var(--shadow-xs)",
        border: "1px solid hsl(var(--border-soft))",
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
          即将上线
        </span>
      )}
      <span
        className="inline-flex items-center justify-center w-11 h-11 rounded-2xl mb-5"
        style={{
          background: "hsl(var(--accent-soft))",
          color: "hsl(var(--primary-deep))",
        }}
      >
        {icon}
      </span>
      <h3 className="font-display text-xl mb-2.5">{title}</h3>
      <p className="text-sm leading-relaxed text-secondary-foreground/85 mb-5">
        {body}
      </p>
      <Link
        to={to}
        className="inline-flex items-center gap-1.5 text-sm font-medium hover:gap-2 active:gap-2 active:opacity-70 transition-all"
        style={{ color: "hsl(var(--primary))" }}
      >
        {ctaLabel}
        <span className="opacity-70">→</span>
      </Link>
    </article>
  );
}
