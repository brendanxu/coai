import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "react-router-dom";
import { ArrowRight } from "lucide-react";
import axios from "axios";

import { Button } from "@/components/ui/button.tsx";

/**
 * Dual-rail hero per v0.7 Claude Design.
 *
 * Left rail: copy + dual CTAs ("查看 Token 套餐" + "逛服务市场")
 * Right rail: live model pool snapshot pulled from /api/gtk/v1/pool
 *
 * Matches the structure in docs/strategy/2026-04-30-redesign-v2.html
 * lines 872-904 (gt-hero / gt-rail). Uses existing color tokens
 * (--primary moss / --background cream / --foreground ink) so visual
 * comes for free.
 */
type PoolModel = {
  model: string;
  provider: string;
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

export default function Hero() {
  const { t } = useTranslation();
  const [pool, setPool] = useState<PoolSnapshot | null>(null);

  useEffect(() => {
    let mounted = true;
    axios
      .get("/gtk/v1/pool")
      .then((r) => {
        if (mounted && r.data?.success) setPool(r.data.data);
      })
      .catch(() => {
        // Silent fall-through — rail shows skeleton/placeholder
      });
    return () => {
      mounted = false;
    };
  }, []);

  return (
    <section className="grid grid-cols-1 lg:grid-cols-[1.4fr_1fr] gap-10 lg:gap-14 items-stretch">
      {/* ═══ LEFT RAIL — copy + CTAs ═══════════════════════════════════ */}
      <div className="space-y-7 self-center">
        <div className="inline-flex items-center gap-2 text-xs uppercase tracking-[0.18em] text-muted-foreground">
          <span className="w-1.5 h-1.5 rounded-full bg-[hsl(var(--primary))]" />
          {t(
            "hero.eyebrow",
            "一个 Key 调一池模型 · 一个市场买一组服务",
          )}
        </div>

        <h1 className="font-display text-4xl md:text-5xl lg:text-6xl leading-[1.05] tracking-tight">
          {t("hero.line1", "聚合 AI 算力，")}
          <br />
          <em
            className="not-italic"
            style={{
              fontStyle: "italic",
              color: "hsl(var(--primary))",
              fontWeight: 400,
            }}
          >
            {t("hero.line2", "开箱即用")}
          </em>
          {t("hero.line3", " 的智能体服务。")}
        </h1>

        <p className="text-base md:text-lg text-secondary-foreground/80 max-w-xl leading-relaxed">
          {t(
            "hero.sub",
            "买 Token 套餐：一个 API Key 通吃 GPT、Claude、DeepSeek、Qwen 等十余款模型，基于 NewAPI，sub2API 即将接入官方 API。买服务：小红书代运营、资料制作、DIY 智能体——按次计费，底层仍是算力。",
          )}
        </p>

        <div className="flex flex-wrap gap-3">
          <Link to="/token-plans">
            <Button size="lg" className="rounded-full px-5 gap-2">
              {t("hero.cta-token", "查看 Token 套餐")}
              <ArrowRight className="w-4 h-4" />
            </Button>
          </Link>
          <Link to="/services">
            <Button size="lg" variant="outline" className="rounded-full px-5">
              {t("hero.cta-market", "逛服务市场")}
            </Button>
          </Link>
        </div>

        <div className="flex flex-wrap gap-x-8 gap-y-2 text-sm pt-2">
          <Trust
            num={pool ? `${pool.total_models}+` : "14+"}
            label={t("hero.trust.models", "模型在池")}
          />
          <Trust num="3" label={t("hero.trust.services", "类服务上架")} />
          <Trust num="1" label={t("hero.trust.key", "Key 全调用")} />
        </div>
      </div>

      {/* ═══ RIGHT RAIL — live model pool ══════════════════════════════ */}
      <aside
        className="rounded-3xl p-7 md:p-8 self-stretch"
        style={{
          background: "hsl(var(--ink))",
          color: "hsl(var(--ink-foreground))",
          boxShadow: "var(--shadow-ink)",
        }}
      >
        <div className="flex items-center justify-between text-xs uppercase tracking-[0.16em] mb-5 opacity-80">
          <span>{t("hero.rail.label", "Live grid")}</span>
          <span className="inline-flex items-center gap-1.5">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-300 opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-green-300" />
            </span>
            {t("hero.rail.realtime", "Realtime")}
          </span>
        </div>

        <h3 className="font-display text-2xl mb-1">
          {t("hero.rail.heading", "模型池实时")}
        </h3>
        <p className="font-mono text-xs opacity-70 mb-6">
          {pool
            ? t("hero.rail.delta", "{{count}} 个模型在线 · 平均延迟 {{ms}}ms", {
                count: pool.total_models,
                ms: pool.avg_latency_ms || "—",
              })
            : t("hero.rail.loading", "加载中…")}
        </p>

        <div className="flex flex-col gap-2 mb-6 font-mono text-xs">
          {(pool?.models || []).slice(0, 5).map((m) => (
            <div
              key={m.model}
              className="flex justify-between items-center gap-2"
            >
              <span style={{ color: "#BFD9C5" }}>● {m.model}</span>
              <span className="opacity-60">
                {m.average_latency_ms ? `${m.average_latency_ms}ms` : "—"}
              </span>
            </div>
          ))}
          {pool && pool.models.length > 5 && (
            <div className="flex justify-between items-center gap-2 opacity-50">
              <span>○ +{pool.models.length - 5} more</span>
              <span>···</span>
            </div>
          )}
          {!pool &&
            ["gpt-4o", "claude-sonnet-4", "deepseek-v3", "qwen-max"].map(
              (m) => (
                <div
                  key={m}
                  className="flex justify-between items-center gap-2 opacity-50"
                >
                  <span style={{ color: "#BFD9C5" }}>● {m}</span>
                  <span>—</span>
                </div>
              ),
            )}
        </div>

        <Link to="/token-plans">
          <Button
            variant="secondary"
            className="w-full rounded-full font-medium gap-1"
          >
            {t("hero.rail.cta", "复制我的 API Key")}
            <span className="opacity-70 ml-1">→</span>
          </Button>
        </Link>
      </aside>
    </section>
  );
}

function Trust({ num, label }: { num: string; label: string }) {
  return (
    <div className="flex items-baseline gap-2">
      <span
        className="font-display text-xl font-semibold"
        style={{ color: "hsl(var(--primary))" }}
      >
        {num}
      </span>
      <span className="text-muted-foreground">{label}</span>
    </div>
  );
}
