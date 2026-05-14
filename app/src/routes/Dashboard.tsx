// /dashboard — dual mode: Token API (new) + 民宿 SaaS (existing carbon dashboard).
// localStorage key: gtk_dashboardMode, default = "token"
//
// Token mode (Phase 2):
//   - 4 stats cards: credits used / call count / cache saved / actual spent
//   - Quick start card: base URL + API key
//   - Live pool grid (reuses axios /gtk/v1/pool pattern from TokenPlans)
//
// 民宿 mode: ALL existing carbon Dashboard content untouched — just
// conditionally rendered behind the toggle.

import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useDispatch, useSelector } from "react-redux";
import { useTranslation } from "react-i18next";
import { toast } from "sonner";
import axios from "axios";
import { Copy } from "lucide-react";

import {
  selectCarbonSummary,
  selectCarbonSummaryLoading,
  setSummary,
  setSummaryLoading,
} from "@/store/carbon.ts";
import { getCarbonSummary } from "@/api/carbon.ts";
import { CarbonBar } from "@/components/Carbon/CarbonBar.tsx";
import { EquivalentNarrative } from "@/components/Carbon/EquivalentNarrative.tsx";
import { ShareCard } from "@/components/Carbon/ShareCard.tsx";
import { LeafIcon } from "@/components/Carbon/icons.tsx";
import { formatCO2 } from "@/components/Carbon/tier.ts";
import { Button } from "@/components/ui/button.tsx";
import { ArrowDown, ArrowUp, ArrowRight } from "lucide-react";
import type { AppDispatch } from "@/store/index.ts";
import { cn } from "@/components/ui/lib/utils.ts";

// ─── Types ─────────────────────────────────────────────────────────────

type DashboardMode = "token" | "mansion";
const DASHBOARD_MODE_KEY = "gtk_dashboardMode";

type UsageSummary = {
  credits_used: number;
  total_calls: number;
  cache_saved_micro: number;
  actual_spent_micro: number;
};

type PoolModel = {
  model: string;
  provider_label: string;
  is_sub2api?: boolean;
  average_latency_ms?: number;
  credit_tier?: string;
};

type PoolSnapshot = {
  total_models: number;
  enabled_channels: number;
  avg_latency_ms: number;
  models: PoolModel[];
};

type BindingResp = {
  success: boolean;
  data?: { api_key?: string };
};

// ─── Helpers ───────────────────────────────────────────────────────────

function microToYuan(micro: number): string {
  return "¥" + (micro / 1_000_000).toFixed(2);
}

// ─── Token Dashboard ────────────────────────────────────────────────────

function TokenDashboard() {
  const { t } = useTranslation();
  const [usage, setUsage] = useState<UsageSummary | null>(null);
  const [usageLoading, setUsageLoading] = useState(true);
  const [pool, setPool] = useState<PoolSnapshot | null>(null);
  const [apiKey, setApiKey] = useState<string | null>(null);
  const [copying, setCopying] = useState<"url" | "key" | null>(null);

  useEffect(() => {
    let mounted = true;

    // Usage summary
    setUsageLoading(true);
    axios
      .get<{ success: boolean; data?: UsageSummary }>(
        "/gtk/v1/usage/summary?range=month",
      )
      .then((r) => {
        if (mounted && r.data?.success && r.data.data) {
          setUsage(r.data.data);
        }
      })
      .catch(() => {/* silent — skeleton shown */})
      .finally(() => {
        if (mounted) setUsageLoading(false);
      });

    // Pool snapshot
    axios
      .get("/gtk/v1/pool")
      .then((r) => {
        if (mounted && r.data?.success) setPool(r.data.data);
      })
      .catch(() => {/* silent */});

    // API key binding
    axios
      .get<BindingResp>("/gtk/v1/binding")
      .then((r) => {
        if (mounted && r.data?.success && r.data.data?.api_key) {
          setApiKey(r.data.data.api_key);
        }
      })
      .catch(() => {/* 404 = no plan yet, show placeholder */});

    return () => {
      mounted = false;
    };
  }, []);

  const copyText = async (text: string, which: "url" | "key") => {
    try {
      await navigator.clipboard.writeText(text);
      setCopying(which);
      setTimeout(() => setCopying(null), 1500);
    } catch {
      toast.error(t("token.quickstart.copy-failed", "复制失败，请手动选择"));
    }
  };

  const BASE_URL = "https://api.greentokey.com/v1";

  const stats = [
    {
      label: t("dashboard.token.stat.credits_used", "本月 credits 已用"),
      value: usageLoading ? "—" : (usage?.credits_used ?? "—").toString(),
      sub: null,
    },
    {
      label: t("dashboard.token.stat.total_calls", "本月调用次数"),
      value: usageLoading ? "—" : (usage?.total_calls ?? "—").toString(),
      sub: null,
    },
    {
      label: t("dashboard.token.stat.cache_saved", "cache 节省"),
      value: usageLoading
        ? "—"
        : usage
        ? microToYuan(usage.cache_saved_micro)
        : "—",
      sub: null,
    },
    {
      label: t("dashboard.token.stat.actual_spent", "实付支出"),
      value: usageLoading
        ? "—"
        : usage
        ? microToYuan(usage.actual_spent_micro)
        : "—",
      sub: null,
    },
  ];

  return (
    <div className="mx-auto max-w-4xl px-6 py-12 space-y-10">
      {/* Page header */}
      <div>
        <p className="text-xs uppercase tracking-[0.16em] text-muted-foreground mb-2">
          {t("dashboard.token.eyebrow", "Token API · 本月")}
        </p>
        <h1 className="font-display text-3xl tracking-tight">
          {t("dashboard.token.heading", "仪表盘")}
        </h1>
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
        {stats.map((s) => (
          <div
            key={s.label}
            className="rounded-2xl p-5"
            style={{
              background: "hsl(var(--card))",
              border: "1px solid hsl(var(--border-soft))",
            }}
          >
            <p className="text-xs text-muted-foreground mb-2">{s.label}</p>
            {usageLoading ? (
              <div className="h-7 w-16 bg-muted/40 rounded animate-pulse" />
            ) : (
              <p className="font-display text-2xl tabular-nums">{s.value}</p>
            )}
          </div>
        ))}
      </div>

      {/* Quick start card */}
      <div
        className="rounded-2xl p-6"
        style={{
          background: "hsl(var(--card))",
          border: "1px solid hsl(var(--border-soft))",
        }}
      >
        <div className="flex items-center justify-between mb-4">
          <h3 className="font-display text-lg">
            {t("dashboard.token.quickstart.heading", "快速开始 · OpenAI-compatible")}
          </h3>
          <span className="text-xs text-muted-foreground">
            {t("dashboard.token.quickstart.sub", "已为你创建 API Key")}
          </span>
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 gap-3">
          {/* Base URL */}
          <div
            className="flex items-center gap-3 rounded-xl px-4 py-3"
            style={{
              background: "hsl(var(--muted) / 0.5)",
              border: "1px solid hsl(var(--border))",
            }}
          >
            <div className="flex-1 min-w-0">
              <p className="text-[10px] uppercase tracking-[0.12em] text-muted-foreground mb-0.5">
                base_url
              </p>
              <p className="font-mono text-xs truncate">{BASE_URL}</p>
            </div>
            <button
              type="button"
              onClick={() => copyText(BASE_URL, "url")}
              className="shrink-0 p-1.5 rounded-lg transition-colors hover:bg-muted"
              aria-label="Copy base URL"
            >
              <Copy
                className="w-3.5 h-3.5"
                style={{
                  color: copying === "url"
                    ? "hsl(var(--primary))"
                    : "hsl(var(--muted-foreground))",
                }}
              />
            </button>
          </div>

          {/* API Key */}
          <div
            className="flex items-center gap-3 rounded-xl px-4 py-3"
            style={{
              background: "hsl(var(--muted) / 0.5)",
              border: "1px solid hsl(var(--border))",
            }}
          >
            <div className="flex-1 min-w-0">
              <p className="text-[10px] uppercase tracking-[0.12em] text-muted-foreground mb-0.5">
                api_key
              </p>
              {apiKey ? (
                <p
                  className="font-mono text-xs truncate"
                  style={{ color: "hsl(var(--primary))" }}
                >
                  {apiKey.slice(0, 8)}{"•".repeat(8)}{apiKey.slice(-4)}
                </p>
              ) : (
                <p className="text-xs text-muted-foreground italic">
                  {t(
                    "dashboard.token.quickstart.no_key",
                    "购买 Token 套餐后自动生成",
                  )}
                </p>
              )}
            </div>
            {apiKey && (
              <button
                type="button"
                onClick={() => copyText(apiKey, "key")}
                className="shrink-0 p-1.5 rounded-lg transition-colors hover:bg-muted"
                aria-label="Copy API key"
              >
                <Copy
                  className="w-3.5 h-3.5"
                  style={{
                    color: copying === "key"
                      ? "hsl(var(--primary))"
                      : "hsl(var(--muted-foreground))",
                  }}
                />
              </button>
            )}
          </div>
        </div>

        {!apiKey && (
          <div className="mt-4 text-center">
            <Link to="/token-plans">
              <Button size="sm" className="rounded-full gap-1.5">
                {t("dashboard.token.quickstart.buy_cta", "购买 Token 套餐")}
                <ArrowRight className="w-3.5 h-3.5" />
              </Button>
            </Link>
          </div>
        )}
      </div>

      {/* Live pool grid */}
      <section
        className="rounded-2xl p-6 md:p-8"
        style={{
          background: "hsl(var(--ink))",
          color: "hsl(var(--ink-foreground))",
          boxShadow: "var(--shadow-ink)",
        }}
      >
        <div className="flex items-center justify-between text-xs uppercase tracking-[0.16em] mb-4 opacity-80">
          <span>{t("token.pool.label", "Live grid")}</span>
          <span className="inline-flex items-center gap-1.5">
            <span className="relative flex h-2 w-2">
              <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-green-300 opacity-75" />
              <span className="relative inline-flex rounded-full h-2 w-2 bg-green-300" />
            </span>
            {t("token.pool.realtime", "Realtime")}
          </span>
        </div>

        <h3 className="font-display text-xl mb-1">
          {t("token.pool.heading", "你买到的就是这个池子。")}
        </h3>
        <p className="font-mono text-xs opacity-70 mb-6">
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

        <div className="grid grid-cols-1 md:grid-cols-2 gap-x-8 gap-y-2 font-mono text-xs">
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
              </span>
              <span className="opacity-60 flex-shrink-0">
                {m.average_latency_ms ? `${m.average_latency_ms}ms` : "—"}
              </span>
            </div>
          ))}
          {!pool &&
            ["gpt-4o", "claude-sonnet-4", "deepseek-v3", "qwen-max"].map((m) => (
              <div
                key={m}
                className="flex justify-between items-center gap-3 py-1 opacity-50"
              >
                <span style={{ color: "hsl(var(--ink-accent))" }}>● {m}</span>
                <span>—</span>
              </div>
            ))}
        </div>

        <div className="mt-6">
          <Link
            to="/pool"
            className="inline-flex items-center gap-1.5 text-sm font-medium hover:gap-2 transition-all opacity-90 hover:opacity-100"
            style={{ color: "hsl(var(--ink-accent))" }}
          >
            {t("token.pool.cta", "查看完整模型池 + 渠道状态")}
            <ArrowRight className="w-3.5 h-3.5" />
          </Link>
        </div>
      </section>
    </div>
  );
}

// ─── 民宿 (Carbon) Dashboard — UNCHANGED ───────────────────────────────

function Hero({ totalG, deltaPct, firstMonth, lastMonthG }: {
  totalG: number;
  deltaPct: number;
  firstMonth: boolean;
  lastMonthG: number;
}) {
  const isImprovement = deltaPct < 0;

  return (
    <div className="flex flex-col md:flex-row md:items-end md:gap-8">
      <div>
        <div className="text-6xl md:text-7xl font-display leading-none tabular-nums tracking-tight">
          {formatCO2(totalG)}
        </div>
        <div className="text-sm text-muted-foreground mt-3">CO₂ this month</div>
      </div>
      {!firstMonth && lastMonthG > 0 && (
        <div className="mt-4 md:mt-0 md:mb-2 flex items-center gap-1.5">
          {isImprovement ? (
            <ArrowDown size={16} className="text-[hsl(var(--success))]" />
          ) : (
            <ArrowUp size={16} className="text-[hsl(var(--gold))]" />
          )}
          <span
            className={cn(
              "tabular-nums font-medium",
              isImprovement
                ? "text-[hsl(var(--success))]"
                : "text-[hsl(var(--gold))]",
            )}
          >
            ~{Math.abs(deltaPct).toFixed(0)}%
          </span>
          <span className="text-muted-foreground text-sm">vs last month</span>
        </div>
      )}
      {firstMonth && (
        <div className="mt-4 md:mt-0 md:mb-2 text-sm text-muted-foreground italic">
          Just getting started
        </div>
      )}
    </div>
  );
}

function CarbonEmptyState() {
  return (
    <div className="text-center py-24">
      <LeafIcon size={48} className="mx-auto text-[hsl(var(--secondary))] mb-6" />
      <h2 className="text-2xl font-display mb-3">No carbon to report yet</h2>
      <p className="text-muted-foreground max-w-md mx-auto mb-8">
        Send your first message and we&apos;ll start estimating the impact of every
        chat — and showing you the trend.
      </p>
      <Link to="/chat">
        <Button>Start chatting →</Button>
      </Link>
    </div>
  );
}

function MansionDashboard() {
  const dispatch = useDispatch<AppDispatch>();
  const { t } = useTranslation();
  const summary = useSelector(selectCarbonSummary);
  const loading = useSelector(selectCarbonSummaryLoading);

  useEffect(() => {
    let cancelled = false;
    dispatch(setSummaryLoading(true));
    getCarbonSummary()
      .then((s) => {
        if (!cancelled) dispatch(setSummary(s));
      })
      .catch((e) => {
        console.error("[carbon] summary fetch failed", e);
        if (!cancelled) {
          dispatch(setSummaryLoading(false));
          toast.error(
            t(
              "carbon.summary_load_failed",
              "Couldn't load your carbon report. Please retry.",
            ),
          );
        }
      });
    return () => {
      cancelled = true;
    };
  }, [dispatch, t]);

  if (loading && !summary) {
    return (
      <div className="mx-auto max-w-2xl px-6 py-12 space-y-12">
        <div className="h-24 w-48 bg-muted/40 rounded animate-pulse" />
        <div className="h-6 w-64 bg-muted/30 rounded animate-pulse" />
        <div className="h-32 bg-muted/20 rounded animate-pulse" />
      </div>
    );
  }

  if (!summary || summary.total_g === 0) {
    return (
      <div className="mx-auto max-w-2xl px-6 py-16">
        <h1 className="font-display text-3xl mb-2">{summary?.month ?? "Carbon"}</h1>
        <CarbonEmptyState />
      </div>
    );
  }

  const maxPct = Math.max(...summary.by_model.map((m) => m.pct), 1);

  return (
    <div className="mx-auto max-w-2xl px-6 py-12 space-y-12">
      <div className="text-sm text-muted-foreground">
        <Link to="/" className="hover:underline">greentokey</Link>
        <span className="mx-1">›</span>
        <span>carbon report</span>
      </div>
      <h1 className="font-display text-3xl tracking-tight">{summary.month}</h1>

      <Hero
        totalG={summary.total_g}
        deltaPct={summary.delta_pct}
        firstMonth={summary.first_month}
        lastMonthG={summary.last_month_g}
      />

      {summary.by_model.length > 1 && (
        <section>
          <h2 className="font-display text-xl mb-3 text-muted-foreground">By model</h2>
          <div>
            {summary.by_model.map((m) => (
              <CarbonBar
                key={m.model}
                model={m.model}
                g={m.g}
                pct={m.pct}
                maxPct={maxPct}
              />
            ))}
          </div>
        </section>
      )}

      <EquivalentNarrative co2g={summary.total_g} />

      <div className="flex flex-wrap items-center gap-3">
        <ShareCard summary={summary} />
        <Link to="/methodology">
          <Button variant="ghost" className="gap-1">
            Methodology
            <ArrowRight size={14} />
          </Button>
        </Link>
      </div>

      <div className="border-t border-border pt-6 text-xs text-muted-foreground tabular-nums leading-relaxed">
        Estimate ±{summary.error_margin_pct}%. Coefficient v{summary.coefficient_version}.{" "}
        <Link to="/methodology" className="underline hover:no-underline">
          See methodology
        </Link>
        .
      </div>
    </div>
  );
}

// ─── Root Dashboard ──────────────────────────────────────────────────────

export default function Dashboard() {
  const { t } = useTranslation();
  const [mode, setMode] = useState<DashboardMode>(() => {
    try {
      const stored = localStorage.getItem(DASHBOARD_MODE_KEY);
      return stored === "mansion" ? "mansion" : "token";
    } catch {
      return "token";
    }
  });

  useEffect(() => {
    try {
      localStorage.setItem(DASHBOARD_MODE_KEY, mode);
    } catch {
      // ignore
    }
  }, [mode]);

  return (
    <div className="w-full h-full overflow-y-auto">
      {/* Mode toggle pill */}
      <div className="flex justify-center pt-6 pb-0">
        <div
          className="inline-flex rounded-full p-0.5 gap-0.5"
          style={{
            background: "hsl(var(--muted))",
            border: "1px solid hsl(var(--border))",
          }}
          role="group"
          aria-label={t("dashboard.mode_toggle.label", "仪表盘模式切换")}
        >
          <button
            type="button"
            onClick={() => setMode("token")}
            className="px-4 py-1.5 rounded-full text-xs font-medium transition-colors"
            style={
              mode === "token"
                ? { background: "hsl(var(--accent))", color: "#fff" }
                : { background: "transparent", color: "hsl(var(--muted-foreground))" }
            }
          >
            {t("dashboard.mode_toggle.token", "Token API")}
          </button>
          <button
            type="button"
            onClick={() => setMode("mansion")}
            className="px-4 py-1.5 rounded-full text-xs font-medium transition-colors"
            style={
              mode === "mansion"
                ? { background: "hsl(var(--accent))", color: "#fff" }
                : { background: "transparent", color: "hsl(var(--muted-foreground))" }
            }
          >
            {t("dashboard.mode_toggle.mansion", "民宿 SaaS")}
          </button>
        </div>
      </div>

      {mode === "token" ? <TokenDashboard /> : <MansionDashboard />}
    </div>
  );
}
