// Usage.tsx — /usage (auth-gated)
//
// Usage analytics page with summary stats, 30-day trend bar chart (CSS),
// and by-model breakdown table with sortable columns.

import { useState, useEffect, useCallback } from "react";
import { useTranslation } from "react-i18next";
import axios from "axios";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";

type Range = "month" | "7d" | "30d";

interface UsageSummary {
  credits_used: number;
  total_calls: number;
  cache_saved_micro: number;
  actual_spent_micro: number;
}

interface TimeseriesPoint {
  date: string;
  cache_miss_credits: number;
  cache_hit_credits: number;
}

interface ModelRow {
  model_id: string;
  total_calls: number;
  input_tokens: number;
  output_tokens: number;
  cache_saved_micro: number;
  credits_used: number;
  actual_spent_micro: number;
}

type SortKey = keyof ModelRow;
type SortDir = "asc" | "desc";

function formatMicro(micro: number): string {
  return "¥" + (micro / 1e6).toFixed(2);
}

function StatCard({ label, value }: { label: string; value: string | number }) {
  return (
    <Card>
      <CardContent style={{ padding: "20px 16px" }}>
        <div style={{ fontSize: 12, opacity: 0.6, marginBottom: 6, fontFamily: "Manrope, sans-serif" }}>
          {label}
        </div>
        <div style={{ fontSize: 22, fontWeight: 700, fontFamily: "Manrope, sans-serif", color: "var(--accent, #2D5A3D)" }}>
          {value}
        </div>
      </CardContent>
    </Card>
  );
}

function BarChart({ data, emptyLabel }: { data: TimeseriesPoint[]; emptyLabel: string }) {
  const { t } = useTranslation();
  if (!data.length) {
    return (
      <div style={{ textAlign: "center", fontSize: 14, opacity: 0.5, padding: "32px 0" }}>
        {emptyLabel}
      </div>
    );
  }

  const last30 = data.slice(-30);
  const maxVal = Math.max(...last30.map((d) => d.cache_miss_credits + d.cache_hit_credits), 1);

  return (
    <div>
      <div style={{ display: "flex", gap: 12, marginBottom: 12, fontSize: 12, opacity: 0.65 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <div style={{ width: 12, height: 12, background: "var(--accent, #2D5A3D)", borderRadius: 2 }} />
          {t("usage.chart_legend_miss")}
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 6 }}>
          <div style={{ width: 12, height: 12, background: "rgba(45,90,61,0.3)", borderRadius: 2 }} />
          {t("usage.chart_legend_hit")}
        </div>
      </div>
      <div
        style={{
          display: "flex",
          alignItems: "flex-end",
          gap: 4,
          height: 100,
          overflowX: "auto",
        }}
      >
        {last30.map((d) => {
          const total = d.cache_miss_credits + d.cache_hit_credits;
          const totalH = (total / maxVal) * 100;
          const missH = (d.cache_miss_credits / maxVal) * 100;
          const hitH = (d.cache_hit_credits / maxVal) * 100;
          return (
            <div
              key={d.date}
              title={`${d.date}\nMiss: ${d.cache_miss_credits}\nHit: ${d.cache_hit_credits}`}
              style={{
                flex: 1,
                minWidth: 6,
                height: `${totalH}%`,
                display: "flex",
                flexDirection: "column",
                justifyContent: "flex-end",
              }}
            >
              <div style={{ height: `${hitH > 0 ? (hitH / (missH + hitH)) * 100 : 0}%`, background: "rgba(45,90,61,0.3)", minHeight: hitH > 0 ? 2 : 0 }} />
              <div style={{ height: `${missH > 0 ? (missH / (missH + hitH)) * 100 : 100}%`, background: "var(--accent, #2D5A3D)", minHeight: total > 0 ? 2 : 0 }} />
            </div>
          );
        })}
      </div>
      <div style={{ display: "flex", justifyContent: "space-between", fontSize: 11, opacity: 0.45, marginTop: 4 }}>
        <span>{last30[0]?.date ?? ""}</span>
        <span>{last30[last30.length - 1]?.date ?? ""}</span>
      </div>
    </div>
  );
}

function Usage() {
  const { t } = useTranslation();
  const [range, setRange] = useState<Range>("month");
  const [summary, setSummary] = useState<UsageSummary | null>(null);
  const [timeseries, setTimeseries] = useState<TimeseriesPoint[]>([]);
  const [models, setModels] = useState<ModelRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [sortKey, setSortKey] = useState<SortKey>("total_calls");
  const [sortDir, setSortDir] = useState<SortDir>("desc");

  const fetchAll = useCallback(async (r: Range) => {
    setLoading(true);
    try {
      const [s, ts, m] = await Promise.all([
        axios.get<{ success: boolean; data?: UsageSummary }>(`/gtk/v1/usage/summary?range=${r}`),
        axios.get<{ success: boolean; data?: TimeseriesPoint[] }>(`/gtk/v1/usage/timeseries?range=${r}`),
        axios.get<{ success: boolean; data?: ModelRow[] }>(`/gtk/v1/usage/by-model?range=${r}`),
      ]);
      if (s.data?.success && s.data.data) setSummary(s.data.data);
      if (ts.data?.success && Array.isArray(ts.data.data)) setTimeseries(ts.data.data);
      if (m.data?.success && Array.isArray(m.data.data)) setModels(m.data.data);
    } catch (_e) {
      // silent — empty state shown
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchAll(range);
  }, [range, fetchAll]);

  const sortedModels = [...models].sort((a, b) => {
    const av = a[sortKey] as number;
    const bv = b[sortKey] as number;
    return sortDir === "asc" ? av - bv : bv - av;
  });

  const handleSort = (key: SortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
    } else {
      setSortKey(key);
      setSortDir("desc");
    }
  };

  const sortIcon = (key: SortKey) => {
    if (key !== sortKey) return " ↕";
    return sortDir === "asc" ? " ↑" : " ↓";
  };

  const ranges: Range[] = ["month", "7d", "30d"];

  return (
    <div
      style={{
        minHeight: "100vh",
        background: "var(--bg, #F4EFE5)",
        fontFamily: "Manrope, sans-serif",
        padding: "32px 16px",
      }}
    >
      <div style={{ maxWidth: 1000, margin: "0 auto", display: "flex", flexDirection: "column", gap: 20 }}>

        {/* Header */}
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 12 }}>
          <h1
            style={{
              fontFamily: "Fraunces, serif",
              fontSize: 24,
              fontWeight: 700,
              color: "var(--ink, #2B2118)",
              margin: 0,
            }}
          >
            {t("usage.title")}
          </h1>
          <div
            style={{
              display: "flex",
              border: "1px solid var(--border, #e2e8f0)",
              borderRadius: 8,
              overflow: "hidden",
            }}
          >
            {ranges.map((r) => (
              <button
                key={r}
                onClick={() => setRange(r)}
                style={{
                  padding: "6px 14px",
                  fontSize: 13,
                  fontFamily: "Manrope, sans-serif",
                  border: "none",
                  borderRight: r !== "30d" ? "1px solid var(--border, #e2e8f0)" : "none",
                  background: range === r ? "var(--accent, #2D5A3D)" : "transparent",
                  color: range === r ? "#fff" : "var(--ink, #2B2118)",
                  cursor: "pointer",
                  fontWeight: range === r ? 600 : 400,
                }}
              >
                {t(`usage.range_${r}` as any)}
              </button>
            ))}
          </div>
        </div>

        {/* Stats */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
            gap: 12,
          }}
        >
          <StatCard label={t("usage.stat_credits")} value={loading ? "…" : (summary?.credits_used ?? 0).toLocaleString()} />
          <StatCard label={t("usage.stat_calls")} value={loading ? "…" : (summary?.total_calls ?? 0).toLocaleString()} />
          <StatCard label={t("usage.stat_cache_saved")} value={loading ? "…" : formatMicro(summary?.cache_saved_micro ?? 0)} />
          <StatCard label={t("usage.stat_spent")} value={loading ? "…" : formatMicro(summary?.actual_spent_micro ?? 0)} />
        </div>

        {/* Chart */}
        <Card>
          <CardHeader>
            <CardTitle style={{ fontSize: 15 }}>{t("usage.chart_title")}</CardTitle>
          </CardHeader>
          <CardContent>
            <BarChart data={timeseries} emptyLabel={t("usage.empty_chart")} />
          </CardContent>
        </Card>

        {/* Table */}
        <Card>
          <CardHeader>
            <CardTitle style={{ fontSize: 15 }}>{t("usage.table_title")}</CardTitle>
          </CardHeader>
          <CardContent style={{ overflowX: "auto" }}>
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13, fontFamily: "Manrope, sans-serif" }}>
              <thead>
                <tr style={{ borderBottom: "1px solid var(--border, #e2e8f0)" }}>
                  <th style={{ textAlign: "left", padding: "8px 10px", fontWeight: 600, whiteSpace: "nowrap" }}>
                    {t("usage.col_model")}
                  </th>
                  {(
                    [
                      ["total_calls", t("usage.col_calls")],
                      ["input_tokens", t("usage.col_input")],
                      ["output_tokens", t("usage.col_output")],
                      ["cache_saved_micro", t("usage.col_cache_saved")],
                      ["credits_used", t("usage.col_credits")],
                      ["actual_spent_micro", t("usage.col_spent")],
                    ] as [SortKey, string][]
                  ).map(([key, label]) => (
                    <th
                      key={key}
                      onClick={() => handleSort(key)}
                      style={{
                        textAlign: "right",
                        padding: "8px 10px",
                        fontWeight: 600,
                        whiteSpace: "nowrap",
                        cursor: "pointer",
                        userSelect: "none",
                      }}
                    >
                      {key === "cache_saved_micro" ? (
                        <span style={{ display: "inline-flex", alignItems: "center", gap: 4 }}>
                          {label}
                          <span
                            title={t("usage.cache_help")}
                            style={{
                              fontSize: 11,
                              width: 16,
                              height: 16,
                              borderRadius: "50%",
                              border: "1px solid currentColor",
                              display: "inline-flex",
                              alignItems: "center",
                              justifyContent: "center",
                              cursor: "help",
                              opacity: 0.6,
                            }}
                          >
                            ?
                          </span>
                          {sortIcon(key)}
                        </span>
                      ) : (
                        `${label}${sortIcon(key)}`
                      )}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {sortedModels.length === 0 && !loading && (
                  <tr>
                    <td colSpan={7} style={{ textAlign: "center", padding: "24px 0", opacity: 0.5 }}>
                      {t("usage.empty_table")}
                    </td>
                  </tr>
                )}
                {sortedModels.map((m) => (
                  <tr
                    key={m.model_id}
                    style={{ borderBottom: "1px solid var(--border, #e2e8f0)" }}
                  >
                    <td style={{ padding: "8px 10px", fontWeight: 500 }}>{m.model_id}</td>
                    <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace" }}>
                      {m.total_calls.toLocaleString()}
                    </td>
                    <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace" }}>
                      {m.input_tokens.toLocaleString()}
                    </td>
                    <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace" }}>
                      {m.output_tokens.toLocaleString()}
                    </td>
                    <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace" }}>
                      {formatMicro(m.cache_saved_micro)}
                    </td>
                    <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace" }}>
                      {m.credits_used.toLocaleString()}
                    </td>
                    <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace" }}>
                      {formatMicro(m.actual_spent_micro)}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

export default Usage;
