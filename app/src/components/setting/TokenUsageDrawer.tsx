/**
 * TokenUsageDrawer — per-token usage breakdown in a right-side Sheet.
 *
 * PKG-A-3 Wave 2, Task 2.5
 *
 * Calls GET /api/gtk/v1/tokens/:id/usage and renders:
 *  - Summary stats (total calls, total tokens, input/output split)
 *  - Per-model breakdown as a Tailwind bar chart (no recharts needed;
 *    recharts is not installed in this project).
 *
 * Data source: NewAPI logs (Wave 1.5b — chat-completion traffic).
 */

import { useEffect, useState } from "react";
import axios from "axios";
import { useTranslation } from "react-i18next";
import {
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet.tsx";
import { TokenUsage, UsageByModel } from "./types.ts";

interface TokenUsageDrawerProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tokenId: number;
  tokenName: string;
}

export default function TokenUsageDrawer({
  open,
  onOpenChange,
  tokenId,
  tokenName,
}: TokenUsageDrawerProps) {
  const { t } = useTranslation();
  const [usage, setUsage] = useState<TokenUsage | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setError(null);
    setUsage(null);
    axios
      .get<{ success: boolean; data: TokenUsage }>(
        `/api/gtk/v1/tokens/${tokenId}/usage`,
      )
      .then((r) => {
        if (r.data?.success) {
          setUsage(r.data.data);
        } else {
          setError(t("tokens.usage.load_failed", "加载用量数据失败"));
        }
      })
      .catch(() => {
        setError(t("tokens.usage.load_failed", "加载用量数据失败"));
      })
      .finally(() => setLoading(false));
  }, [open, tokenId, t]);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="w-full sm:max-w-md overflow-y-auto"
        style={{ background: "hsl(var(--ink-background))" }}
      >
        <SheetHeader className="mb-6">
          <SheetTitle
            className="text-base"
            style={{ color: "hsl(var(--ink-foreground))" }}
          >
            {t("tokens.usage.drawer_title", "令牌用量")} · {tokenName}
          </SheetTitle>
        </SheetHeader>

        {loading && <UsageDrawerSkeleton />}

        {error && !loading && (
          <div
            className="rounded-xl px-4 py-3 text-sm"
            style={{
              background: "rgba(248,113,113,0.10)",
              color: "rgb(248,113,113)",
              border: "1px solid rgba(248,113,113,0.20)",
            }}
          >
            {error}
          </div>
        )}

        {usage && !loading && (
          <div className="space-y-8">
            {/* Summary stats */}
            <div className="grid grid-cols-2 gap-3">
              <StatCard
                label={t("tokens.usage.total_calls", "总调用次数")}
                value={usage.total_calls.toLocaleString()}
              />
              <StatCard
                label={t("tokens.usage.total_tokens", "总 Token 用量")}
                value={formatTokenCount(usage.total_tokens_used)}
              />
              <StatCard
                label={t("tokens.usage.input_tokens", "输入 Tokens")}
                value={formatTokenCount(usage.input_tokens)}
              />
              <StatCard
                label={t("tokens.usage.output_tokens", "输出 Tokens")}
                value={formatTokenCount(usage.output_tokens)}
              />
            </div>

            {/* Per-model breakdown */}
            {usage.by_model && usage.by_model.length > 0 && (
              <div>
                <p
                  className="text-xs uppercase tracking-[0.12em] mb-3"
                  style={{ color: "rgba(255,252,247,0.40)" }}
                >
                  {t("tokens.usage.by_model", "按模型分布")}
                </p>
                <ModelBarChart models={usage.by_model} />
              </div>
            )}

            {usage.total_calls === 0 && (
              <p
                className="text-sm text-center py-8"
                style={{ color: "rgba(255,252,247,0.35)" }}
              >
                {t("tokens.usage.no_data", "暂无调用记录")}
              </p>
            )}
          </div>
        )}
      </SheetContent>
    </Sheet>
  );
}

function StatCard({ label, value }: { label: string; value: string }) {
  return (
    <div
      className="rounded-xl px-4 py-3"
      style={{
        background: "rgba(255,252,247,0.05)",
        border: "1px solid rgba(255,252,247,0.08)",
      }}
    >
      <p
        className="text-[11px] uppercase tracking-[0.10em] mb-1"
        style={{ color: "rgba(255,252,247,0.40)" }}
      >
        {label}
      </p>
      <p
        className="text-lg font-semibold font-mono"
        style={{ color: "hsl(var(--ink-foreground))" }}
      >
        {value}
      </p>
    </div>
  );
}

function ModelBarChart({ models }: { models: UsageByModel[] }) {
  const maxCalls = Math.max(...models.map((m) => m.total_calls), 1);

  return (
    <div className="space-y-2.5">
      {models.map((m) => {
        const pct = Math.max((m.total_calls / maxCalls) * 100, 2);
        return (
          <div key={m.model_id}>
            <div className="flex items-center justify-between mb-1">
              <span
                className="text-xs font-mono truncate max-w-[60%]"
                style={{ color: "rgba(255,252,247,0.65)" }}
              >
                {m.model_id}
              </span>
              <span
                className="text-xs"
                style={{ color: "rgba(255,252,247,0.45)" }}
              >
                {m.total_calls.toLocaleString()} 次 ·{" "}
                {formatTokenCount(m.input_tokens + m.output_tokens)}
              </span>
            </div>
            <div
              className="h-1.5 rounded-full overflow-hidden"
              style={{ background: "rgba(255,252,247,0.08)" }}
            >
              <div
                className="h-full rounded-full transition-all duration-500"
                style={{
                  width: `${pct}%`,
                  background: "hsl(var(--ink-accent))",
                  opacity: 0.75,
                }}
              />
            </div>
          </div>
        );
      })}
    </div>
  );
}

function UsageDrawerSkeleton() {
  return (
    <div className="space-y-3">
      <div className="grid grid-cols-2 gap-3">
        {[1, 2, 3, 4].map((i) => (
          <div
            key={i}
            className="h-16 rounded-xl animate-pulse"
            style={{ background: "rgba(255,252,247,0.06)" }}
          />
        ))}
      </div>
      <div
        className="h-32 rounded-xl animate-pulse"
        style={{ background: "rgba(255,252,247,0.06)" }}
      />
    </div>
  );
}

function formatTokenCount(n: number): string {
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`;
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}K`;
  return n.toLocaleString();
}
