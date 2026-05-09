import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import axios from "axios";
import { ArrowRight } from "lucide-react";

import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";

/**
 * /services — service marketplace listing.
 *
 * Pulls from /api/gtk/v1/services (gtk_service catalog). Currently
 * surfaces 3 seeded services from service/seed.go:
 *   - xhs-single-post (¥19, diy_agent)
 *   - xhs-monthly-pack (¥299, content_pack)
 *   - mansu-managed-ops (¥1,980/month, managed_ops) — links to
 *     /services/mansu (= the legacy /pricing page) for full detail.
 */
type PublicService = {
  slug: string;
  name: string;
  description?: string;
  category: string;
  price_cny_cents: number;
  price_display_cny: string;
  included_credits: number;
  billing_type: string;
  display_order: number;
};

const CATEGORY_LABELS: Record<string, string> = {
  diy_agent: "DIY 智能体",
  content_pack: "资料制作包",
  managed_ops: "代运营",
};

const BILLING_LABELS: Record<string, string> = {
  one_time: "一次性",
  monthly: "按月",
  per_use: "按次",
};

export default function Services() {
  const { t } = useTranslation();
  const [items, setItems] = useState<PublicService[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;
    axios
      .get("/gtk/v1/services")
      .then((r) => {
        if (mounted && r.data?.success) {
          setItems(r.data.data?.services || []);
        } else if (mounted) {
          setError("加载失败");
        }
      })
      .catch(() => mounted && setError("网络异常"));
    return () => {
      mounted = false;
    };
  }, []);

  return (
    <>
      <Header />
      <main className="flex-1 overflow-y-auto">
        <div
          className="mx-auto px-6 py-16 md:py-24"
          style={{ maxWidth: "var(--max-content)" }}
        >
          <header className="text-center mb-14 md:mb-20">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
              {t("services.eyebrow", "服务市场")}
            </p>
            <h1 className="font-display text-4xl md:text-5xl tracking-tight mb-5">
              {t("services.heading", "AI 服务，按结果买。")}
            </h1>
            <p className="text-base md:text-lg text-secondary-foreground/80 max-w-2xl mx-auto leading-relaxed">
              {t(
                "services.sub",
                "服务 = 我们调好的 Prompt + Token 包。底层仍是算力，但你看到的是「单图小红书内容 ¥19」「月度 30 篇 ¥299」「全闭环代运营 ¥1980/月」这种可定价的结果。",
              )}
            </p>
          </header>

          {error && (
            <div className="rounded-2xl bg-destructive/10 text-destructive p-6 text-center">
              {error}
            </div>
          )}

          {!items && !error && (
            <div className="text-center text-muted-foreground py-16">
              {t("services.loading", "加载中…")}
            </div>
          )}

          {items && items.length === 0 && (
            <div className="text-center text-muted-foreground py-16">
              {t("services.empty", "敬请期待——首批服务即将上架")}
            </div>
          )}

          {items && items.length > 0 && (
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
              {items.map((s) => (
                <ServiceTile key={s.slug} svc={s} />
              ))}
            </div>
          )}
        </div>
        <Footer />
      </main>
    </>
  );
}

function ServiceTile({ svc }: { svc: PublicService }) {
  const { t } = useTranslation();
  // mansu-managed-ops gets a deep-link to its dedicated detail page;
  // others currently route to /contact for concierge follow-up.
  const detailHref =
    svc.slug === "mansu-managed-ops" ? "/services/mansu" : "/contact";

  return (
    <article
      className="rounded-3xl p-7 md:p-8 flex flex-col"
      style={{
        background: "hsl(var(--card))",
        border: "1px solid hsl(var(--border-soft))",
        boxShadow: "var(--shadow-xs)",
      }}
    >
      <div className="flex items-baseline justify-between mb-4">
        <span
          className="text-[10px] uppercase tracking-[0.14em] px-2 py-0.5 rounded-full font-medium"
          style={{
            background: "hsl(var(--accent-soft))",
            color: "hsl(var(--primary-deep))",
          }}
        >
          {CATEGORY_LABELS[svc.category] || svc.category}
        </span>
        <span className="text-xs text-muted-foreground">
          {BILLING_LABELS[svc.billing_type] || svc.billing_type}
        </span>
      </div>

      <h3 className="font-display text-xl mb-3 leading-tight">{svc.name}</h3>

      {svc.description && (
        <p className="text-sm leading-relaxed text-secondary-foreground/85 mb-6 flex-1">
          {svc.description}
        </p>
      )}

      <div className="flex items-baseline gap-2 mb-5">
        <span className="font-display text-3xl font-semibold">
          {svc.price_display_cny}
        </span>
        {svc.billing_type === "monthly" && (
          <span className="text-sm text-muted-foreground">/ 月</span>
        )}
      </div>

      <Link
        to={detailHref}
        className="inline-flex items-center gap-1.5 text-sm font-medium hover:gap-2 active:gap-2 active:opacity-70 transition-all"
        style={{ color: "hsl(var(--primary))" }}
      >
        {t("services.tile.cta", "了解详情")}
        <ArrowRight className="w-3.5 h-3.5" />
      </Link>
    </article>
  );
}
