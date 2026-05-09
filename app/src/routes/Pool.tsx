import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import axios from "axios";

import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";

/**
 * /pool — public live model-pool snapshot. Customers and devs see
 * what's actually in the Token 套餐 pool right now (model list +
 * latency + tier + sub2API badge). Pulls from /api/gtk/v1/pool.
 *
 * Per v0.7 Claude Design intent: 模型池 is one of the top-nav targets
 * (redesign-v2.html line 864). Transparent infrastructure → trust.
 */
type Model = {
  model: string;
  provider: string;
  provider_label: string;
  is_sub2api?: boolean;
  average_latency_ms?: number;
  credit_tier?: string;
  credit_tier_label?: string;
  credit_per_call?: number;
};

type Channel = {
  id: number;
  name: string;
  provider: string;
  provider_label: string;
  is_sub2api?: boolean;
  status: number;
};

type Snapshot = {
  generated_at: string;
  total_models: number;
  enabled_channels: number;
  avg_latency_ms: number;
  models: Model[];
  channels: Channel[];
};

export default function Pool() {
  const { t } = useTranslation();
  const [snap, setSnap] = useState<Snapshot | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let mounted = true;
    axios
      .get("/gtk/v1/pool")
      .then((r) => {
        if (mounted && r.data?.success) setSnap(r.data.data);
        else if (mounted) setError("加载失败");
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
          <header className="mb-12 md:mb-16 text-center">
            <p className="text-xs uppercase tracking-[0.18em] text-muted-foreground mb-3">
              {t("pool.eyebrow", "Live grid · Realtime")}
            </p>
            <h1 className="font-display text-4xl md:text-5xl tracking-tight mb-4">
              {t("pool.heading", "模型池")}
            </h1>
            <p className="text-secondary-foreground/80 max-w-2xl mx-auto">
              {t(
                "pool.sub",
                "你买 Token 套餐后，一个 API Key 能调用以下模型。底层基于 NewAPI 路由，自动选最便宜可用的渠道。延迟为最近 1 小时滑动平均。",
              )}
            </p>
          </header>

          {error && (
            <div className="rounded-2xl bg-destructive/10 text-destructive p-6 text-center">
              {error}
            </div>
          )}

          {!snap && !error && (
            <div className="text-center text-muted-foreground py-16">
              {t("pool.loading", "加载中…")}
            </div>
          )}

          {snap && (
            <>
              {/* Stats banner */}
              <div className="grid grid-cols-3 gap-3 md:gap-8 mb-12 md:mb-16">
                <Stat
                  num={String(snap.total_models)}
                  label={t("pool.stats.models", "模型在线")}
                />
                <Stat
                  num={String(snap.enabled_channels)}
                  label={t("pool.stats.channels", "渠道在用")}
                />
                <Stat
                  num={snap.avg_latency_ms ? `${snap.avg_latency_ms}ms` : "—"}
                  label={t("pool.stats.latency", "平均延迟")}
                />
              </div>

              {/* Model table */}
              <div
                className="rounded-3xl overflow-hidden"
                style={{
                  background: "hsl(var(--card))",
                  border: "1px solid hsl(var(--border-soft))",
                  boxShadow: "var(--shadow-xs)",
                }}
              >
                {/* Header row — desktop only. On mobile each row is a stacked card. */}
                <div className="hidden md:grid grid-cols-[2fr_1.5fr_0.8fr_0.7fr] gap-4 px-6 py-4 text-xs font-medium uppercase tracking-wider text-muted-foreground border-b border-border-soft">
                  <span>{t("pool.col.model", "模型")}</span>
                  <span>{t("pool.col.provider", "供应商")}</span>
                  <span>{t("pool.col.tier", "档位")}</span>
                  <span className="text-right">
                    {t("pool.col.latency", "延迟")}
                  </span>
                </div>
                {snap.models.map((m) => (
                  <div
                    key={m.model}
                    className="border-b border-border-soft last:border-0"
                  >
                    {/* Mobile (<md): stacked card — model name on top, meta row below. */}
                    <div className="md:hidden px-5 py-3 flex flex-col gap-1.5">
                      <div className="font-mono text-sm break-all">
                        {m.model}
                      </div>
                      <div className="flex items-center gap-2 text-xs flex-wrap">
                        <span className="text-secondary-foreground/85">
                          {m.provider_label || m.provider}
                        </span>
                        {m.is_sub2api && (
                          <span
                            className="text-[10px] uppercase px-1.5 py-0.5 rounded font-medium"
                            style={{
                              background: "hsl(var(--accent-soft))",
                              color: "hsl(var(--primary-deep))",
                            }}
                          >
                            sub2API
                          </span>
                        )}
                        <span className="text-muted-foreground">·</span>
                        <span className="text-muted-foreground">
                          {m.credit_tier_label || m.credit_tier || "—"}
                        </span>
                        <span className="ml-auto font-mono text-muted-foreground">
                          {m.average_latency_ms
                            ? `${m.average_latency_ms}ms`
                            : "—"}
                        </span>
                      </div>
                    </div>
                    {/* Desktop (≥md): grid row matching header columns. */}
                    <div className="hidden md:grid grid-cols-[2fr_1.5fr_0.8fr_0.7fr] gap-4 px-6 py-3.5 text-sm">
                      <span className="font-mono">{m.model}</span>
                      <span className="text-secondary-foreground/85">
                        {m.provider_label || m.provider}
                        {m.is_sub2api && (
                          <span
                            className="ml-2 text-[10px] uppercase px-1.5 py-0.5 rounded font-medium"
                            style={{
                              background: "hsl(var(--accent-soft))",
                              color: "hsl(var(--primary-deep))",
                            }}
                          >
                            sub2API
                          </span>
                        )}
                      </span>
                      <span className="text-xs text-muted-foreground">
                        {m.credit_tier_label || m.credit_tier || "—"}
                      </span>
                      <span className="text-right font-mono text-xs text-muted-foreground">
                        {m.average_latency_ms
                          ? `${m.average_latency_ms}ms`
                          : "—"}
                      </span>
                    </div>
                  </div>
                ))}
              </div>

              <p className="text-xs text-center text-muted-foreground mt-6">
                {t("pool.disclaimer", "Snapshot 取自 ")}
                <span className="font-mono">{snap.generated_at}</span>
              </p>
            </>
          )}
        </div>
        <Footer />
      </main>
    </>
  );
}

function Stat({ num, label }: { num: string; label: string }) {
  return (
    <div
      className="rounded-2xl p-3 md:p-6 text-center"
      style={{
        background: "hsl(var(--card))",
        border: "1px solid hsl(var(--border-soft))",
      }}
    >
      <div
        className="font-display text-xl md:text-4xl font-medium mb-1 md:mb-1.5 leading-tight"
        style={{ color: "hsl(var(--primary))" }}
      >
        {num}
      </div>
      <div className="text-[10px] md:text-xs uppercase tracking-wider text-muted-foreground leading-tight">
        {label}
      </div>
    </div>
  );
}
