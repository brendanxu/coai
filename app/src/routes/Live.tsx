// Live.tsx — /live (auth-gated)
//
// Real-time token usage stream. Polls /gtk/v1/usage/recent every 3s,
// appending rows to the top. Max 200 rows (FIFO). Pause/Resume control.

import { useState, useEffect, useRef, useCallback } from "react";
import { useTranslation } from "react-i18next";
import axios from "axios";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";

const POLL_INTERVAL = 3000;
const MAX_ROWS = 200;

type CacheClass = "cache_miss" | "cache_hit_write" | "cache_hit_read";

interface LiveRow {
  id: string | number;
  timestamp: number;
  model_id: string;
  provider: string;
  cache_class: CacheClass;
  input_tokens: number;
  output_tokens: number;
  client_charge_micro: number;
  latency_ms: number;
}

function formatTime(ts: number): string {
  const d = new Date(ts * 1000);
  return [
    d.getHours().toString().padStart(2, "0"),
    d.getMinutes().toString().padStart(2, "0"),
    d.getSeconds().toString().padStart(2, "0"),
  ].join(":");
}

function formatCost(micro: number): string {
  return "¥" + (micro / 1e6).toFixed(4);
}

function isHit(cls: CacheClass): boolean {
  return cls === "cache_hit_read" || cls === "cache_hit_write";
}

function CacheChip({ cls }: { cls: CacheClass }) {
  const { t } = useTranslation();
  const hit = isHit(cls);
  return (
    <span
      style={{
        fontSize: 11,
        fontWeight: 600,
        padding: "2px 6px",
        borderRadius: 4,
        background: hit ? "rgba(45,90,61,0.12)" : "rgba(180,83,9,0.12)",
        color: hit ? "var(--accent, #2D5A3D)" : "#b45309",
      }}
    >
      {hit ? t("live.cache_hit") : t("live.cache_miss")}
    </span>
  );
}

function Live() {
  const { t } = useTranslation();
  const [rows, setRows] = useState<LiveRow[]>([]);
  const [paused, setPaused] = useState(false);
  const [modelFilter, setModelFilter] = useState("");
  const [cacheFilter, setCacheFilter] = useState<"all" | "hit" | "miss">("all");

  const lastTsRef = useRef<number>(Math.floor(Date.now() / 1000) - 60);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pausedRef = useRef(false);

  // Keep ref in sync for use inside closure
  pausedRef.current = paused;

  const poll = useCallback(async () => {
    if (pausedRef.current) return;

    try {
      const r = await axios.get<{ success: boolean; data?: LiveRow[] }>(
        `/gtk/v1/usage/recent?since=${lastTsRef.current}`
      );
      if (r.data?.success && Array.isArray(r.data.data) && r.data.data.length > 0) {
        const newRows = r.data.data;
        // Update last timestamp to most recent
        const maxTs = Math.max(...newRows.map((row) => row.timestamp));
        lastTsRef.current = maxTs;

        setRows((prev) => {
          const combined = [...newRows, ...prev];
          return combined.slice(0, MAX_ROWS);
        });
      }
    } catch (_e) {
      // network error — continue polling
    }

    timerRef.current = setTimeout(poll, POLL_INTERVAL);
  }, []);

  useEffect(() => {
    poll();
    return () => {
      if (timerRef.current) clearTimeout(timerRef.current);
    };
  }, [poll]);

  // When unpaused, resume polling
  useEffect(() => {
    if (!paused) {
      if (timerRef.current) clearTimeout(timerRef.current);
      timerRef.current = setTimeout(poll, 100);
    }
  }, [paused, poll]);

  const handleClear = () => {
    setModelFilter("");
    setCacheFilter("all");
  };

  const filteredRows = rows.filter((row) => {
    if (modelFilter && !row.model_id.toLowerCase().includes(modelFilter.toLowerCase())) return false;
    if (cacheFilter === "hit" && !isHit(row.cache_class)) return false;
    if (cacheFilter === "miss" && isHit(row.cache_class)) return false;
    return true;
  });

  // Session stats
  const totalCalls = rows.length;
  const hitCount = rows.filter((r) => isHit(r.cache_class)).length;
  const hitRate = totalCalls > 0 ? ((hitCount / totalCalls) * 100).toFixed(1) : "0.0";
  const totalCost = rows.reduce((sum, r) => sum + r.client_charge_micro, 0);

  const cacheOptions: Array<{ key: "all" | "hit" | "miss"; label: string }> = [
    { key: "all", label: t("live.filter_all") },
    { key: "hit", label: t("live.filter_hit") },
    { key: "miss", label: t("live.filter_miss") },
  ];

  return (
    <div
      style={{
        minHeight: "100vh",
        background: "var(--bg, #F4EFE5)",
        fontFamily: "Manrope, sans-serif",
        padding: "24px 16px",
      }}
    >
      <div
        style={{
          maxWidth: 1100,
          margin: "0 auto",
          display: "flex",
          flexDirection: "column",
          gap: 16,
        }}
      >
        {/* Top bar */}
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", flexWrap: "wrap", gap: 12 }}>
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <h1
              style={{
                fontFamily: "Fraunces, serif",
                fontSize: 22,
                fontWeight: 700,
                color: "var(--ink, #2B2118)",
                margin: 0,
              }}
            >
              {t("live.title")}
            </h1>
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 6,
                fontSize: 13,
                padding: "4px 10px",
                borderRadius: 20,
                border: `1px solid ${paused ? "#999" : "var(--accent, #2D5A3D)"}`,
                color: paused ? "#999" : "var(--accent, #2D5A3D)",
              }}
            >
              <span
                style={{
                  width: 8,
                  height: 8,
                  borderRadius: "50%",
                  background: paused ? "#999" : "var(--accent, #2D5A3D)",
                  animation: paused ? "none" : "pulse 1.5s ease-in-out infinite",
                  display: "inline-block",
                }}
              />
              {paused ? t("live.status_paused") : t("live.status_live")}
            </div>
          </div>
          <Button
            variant="outline"
            size="sm"
            onClick={() => setPaused((p) => !p)}
          >
            {paused ? t("live.resume") : t("live.pause")}
          </Button>
        </div>

        {/* Filter row */}
        <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
          <Input
            placeholder={t("live.filter_model")}
            value={modelFilter}
            onChange={(e) => setModelFilter(e.target.value)}
            style={{ maxWidth: 200, fontSize: 13 }}
          />
          <div style={{ display: "flex", border: "1px solid var(--border, #e2e8f0)", borderRadius: 6, overflow: "hidden" }}>
            {cacheOptions.map(({ key, label }) => (
              <button
                key={key}
                onClick={() => setCacheFilter(key)}
                style={{
                  padding: "5px 12px",
                  fontSize: 13,
                  border: "none",
                  borderRight: key !== "miss" ? "1px solid var(--border, #e2e8f0)" : "none",
                  background: cacheFilter === key ? "var(--accent, #2D5A3D)" : "transparent",
                  color: cacheFilter === key ? "#fff" : "var(--ink, #2B2118)",
                  cursor: "pointer",
                  fontFamily: "Manrope, sans-serif",
                }}
              >
                {label}
              </button>
            ))}
          </div>
          <Button variant="ghost" size="sm" onClick={handleClear}>
            {t("live.filter_clear")}
          </Button>
        </div>

        <div style={{ display: "flex", gap: 16, alignItems: "flex-start" }}>
          {/* Rows list */}
          <div style={{ flex: 1 }}>
            <Card>
              <CardContent style={{ padding: 0 }}>
                <div style={{ overflowX: "auto" }}>
                  <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 12, fontFamily: "Manrope, sans-serif" }}>
                    <thead>
                      <tr style={{ borderBottom: "1px solid var(--border, #e2e8f0)", background: "rgba(0,0,0,0.02)" }}>
                        <th style={{ padding: "8px 12px", textAlign: "left", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_time")}</th>
                        <th style={{ padding: "8px 12px", textAlign: "left", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_model")}</th>
                        <th style={{ padding: "8px 12px", textAlign: "left", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_provider")}</th>
                        <th style={{ padding: "8px 12px", textAlign: "left", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_cache")}</th>
                        <th style={{ padding: "8px 12px", textAlign: "right", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_tokens")}</th>
                        <th style={{ padding: "8px 12px", textAlign: "right", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_cost")}</th>
                        <th style={{ padding: "8px 12px", textAlign: "right", fontWeight: 600, whiteSpace: "nowrap" }}>{t("live.col_latency")}</th>
                      </tr>
                    </thead>
                    <tbody>
                      {filteredRows.length === 0 && (
                        <tr>
                          <td colSpan={7} style={{ textAlign: "center", padding: "40px 0", opacity: 0.45 }}>
                            {t("live.empty")}
                          </td>
                        </tr>
                      )}
                      {filteredRows.map((row) => (
                        <tr
                          key={row.id}
                          style={{ borderBottom: "1px solid var(--border, #e2e8f0)", verticalAlign: "middle" }}
                        >
                          <td style={{ padding: "7px 12px", fontFamily: "JetBrains Mono, monospace", fontSize: 11, opacity: 0.7 }}>
                            {formatTime(row.timestamp)}
                          </td>
                          <td style={{ padding: "7px 12px", maxWidth: 160, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                            {row.model_id}
                          </td>
                          <td style={{ padding: "7px 12px" }}>
                            <span style={{ fontSize: 11, background: "rgba(0,0,0,0.06)", borderRadius: 4, padding: "2px 6px" }}>
                              {row.provider}
                            </span>
                          </td>
                          <td style={{ padding: "7px 12px" }}>
                            <CacheChip cls={row.cache_class} />
                          </td>
                          <td style={{ padding: "7px 12px", textAlign: "right", fontFamily: "JetBrains Mono, monospace", fontSize: 11 }}>
                            {row.input_tokens.toLocaleString()} / {row.output_tokens.toLocaleString()}
                          </td>
                          <td style={{ padding: "7px 12px", textAlign: "right", fontFamily: "JetBrains Mono, monospace", fontSize: 11 }}>
                            {formatCost(row.client_charge_micro)}
                          </td>
                          <td style={{ padding: "7px 12px", textAlign: "right", fontFamily: "JetBrains Mono, monospace", fontSize: 11 }}>
                            {row.latency_ms}ms
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
              </CardContent>
            </Card>
          </div>

          {/* Right rail — session stats */}
          <div style={{ width: 200, flexShrink: 0, position: "sticky", top: 16 }}>
            <Card>
              <CardHeader>
                <CardTitle style={{ fontSize: 14 }}>{t("live.session_title")}</CardTitle>
              </CardHeader>
              <CardContent style={{ display: "flex", flexDirection: "column", gap: 12 }}>
                {totalCalls === 0 ? (
                  <div style={{ fontSize: 13, opacity: 0.5 }}>{t("live.empty")}</div>
                ) : (
                  <>
                    <div>
                      <div style={{ fontSize: 11, opacity: 0.55, marginBottom: 2 }}>{t("live.session_calls")}</div>
                      <div style={{ fontSize: 20, fontWeight: 700, color: "var(--accent, #2D5A3D)", fontFamily: "JetBrains Mono, monospace" }}>
                        {totalCalls.toLocaleString()}
                      </div>
                    </div>
                    <div>
                      <div style={{ fontSize: 11, opacity: 0.55, marginBottom: 2 }}>{t("live.session_hit_rate")}</div>
                      <div style={{ fontSize: 20, fontWeight: 700, color: "var(--accent, #2D5A3D)", fontFamily: "JetBrains Mono, monospace" }}>
                        {hitRate}%
                      </div>
                    </div>
                    <div>
                      <div style={{ fontSize: 11, opacity: 0.55, marginBottom: 2 }}>{t("live.session_cost")}</div>
                      <div style={{ fontSize: 18, fontWeight: 700, color: "var(--accent, #2D5A3D)", fontFamily: "JetBrains Mono, monospace" }}>
                        {formatCost(totalCost)}
                      </div>
                    </div>
                  </>
                )}
              </CardContent>
            </Card>
          </div>
        </div>
      </div>

      <style>{`
        @keyframes pulse {
          0%, 100% { opacity: 1; }
          50% { opacity: 0.4; }
        }
      `}</style>
    </div>
  );
}

export default Live;
