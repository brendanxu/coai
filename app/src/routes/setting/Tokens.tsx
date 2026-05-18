/**
 * /setting/tokens — Personal API Token management page.
 *
 * PKG-A-3 Wave 2, Task 2.1
 *
 * Users can list, create, rename, revoke, and inspect usage of their
 * sk-tnx-xxx personal access tokens. Tokens are managed via the
 * greentokey backend, which delegates to NewAPI under the hood.
 *
 * Backend endpoints (Wave 1, all ready):
 *   GET    /api/gtk/v1/tokens          list (masked)
 *   POST   /api/gtk/v1/tokens          create (one-time plaintext)
 *   PATCH  /api/gtk/v1/tokens/:id      rename / update expire / quota
 *   DELETE /api/gtk/v1/tokens/:id      revoke (soft delete)
 *   GET    /api/gtk/v1/tokens/:id/usage per-token usage
 */

import { useCallback, useEffect, useState } from "react";
import axios from "axios";
import { useTranslation } from "react-i18next";
import { KeyRound, Plus } from "lucide-react";
import { Button } from "@/components/ui/button.tsx";
import { toast } from "sonner";
import TokenList from "@/components/setting/TokenList.tsx";
import CreateTokenDialog from "@/components/setting/CreateTokenDialog.tsx";
import { MaskedToken } from "@/components/setting/types.ts";

export default function Tokens() {
  const { t } = useTranslation();

  const [tokens, setTokens] = useState<MaskedToken[]>([]);
  const [loading, setLoading] = useState(true);
  const [createOpen, setCreateOpen] = useState(false);

  const fetchTokens = useCallback(() => {
    setLoading(true);
    axios
      .get<{ success: boolean; data: MaskedToken[] }>("/gtk/v1/tokens")
      .then((r) => {
        if (r.data?.success) {
          setTokens(r.data.data ?? []);
        }
      })
      .catch(() => {
        toast.error(t("tokens.errors.load_failed", "加载令牌列表失败"));
      })
      .finally(() => setLoading(false));
  }, [t]);

  useEffect(() => {
    fetchTokens();
  }, [fetchTokens]);

  return (
    <div
      className="min-h-screen"
      style={{ background: "hsl(var(--ink-background))" }}
    >
      <div className="mx-auto max-w-3xl px-6 py-12 space-y-8">
        {/* Page header */}
        <div className="flex items-start justify-between gap-4">
          <div>
            <p
              className="text-xs uppercase tracking-[0.16em] mb-1"
              style={{ color: "rgba(255,252,247,0.45)" }}
            >
              {t("tokens.eyebrow", "API 访问")}
            </p>
            <h1
              className="text-2xl font-semibold"
              style={{ color: "hsl(var(--ink-foreground))" }}
            >
              {t("tokens.title", "我的令牌")}
            </h1>
            <p
              className="text-sm mt-1"
              style={{ color: "rgba(255,252,247,0.55)" }}
            >
              {t(
                "tokens.subtitle",
                "管理您的 sk-tnx-xxx API 密钥，最多 10 个",
              )}
            </p>
          </div>
          <Button
            onClick={() => setCreateOpen(true)}
            className="shrink-0"
            style={{
              background: "hsl(var(--ink-accent))",
              color: "hsl(var(--ink-background))",
            }}
          >
            <Plus className="w-4 h-4 mr-2" />
            {t("tokens.create.label", "新建令牌")}
          </Button>
        </div>

        {/* Token list / empty state */}
        {loading ? (
          <TokenListSkeleton />
        ) : tokens.length === 0 ? (
          <TokenEmptyState onCreateClick={() => setCreateOpen(true)} />
        ) : (
          <TokenList
            tokens={tokens}
            onRefresh={fetchTokens}
          />
        )}
      </div>

      <CreateTokenDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        onCreated={fetchTokens}
      />
    </div>
  );
}

function TokenListSkeleton() {
  return (
    <div className="space-y-3">
      {[1, 2, 3].map((i) => (
        <div
          key={i}
          className="h-16 rounded-xl animate-pulse"
          style={{ background: "rgba(255,252,247,0.06)" }}
        />
      ))}
    </div>
  );
}

function TokenEmptyState({ onCreateClick }: { onCreateClick: () => void }) {
  const { t } = useTranslation();
  return (
    <div
      className="flex flex-col items-center justify-center py-20 rounded-2xl text-center"
      style={{
        background: "rgba(255,252,247,0.03)",
        border: "1px dashed rgba(255,252,247,0.12)",
      }}
    >
      <KeyRound
        className="w-10 h-10 mb-4"
        style={{ color: "rgba(255,252,247,0.25)" }}
      />
      <p
        className="text-base font-medium mb-1"
        style={{ color: "hsl(var(--ink-foreground))" }}
      >
        {t("tokens.empty.title", "还没有令牌")}
      </p>
      <p
        className="text-sm mb-6"
        style={{ color: "rgba(255,252,247,0.45)" }}
      >
        {t(
          "tokens.empty.cta",
          "创建第一个令牌开始使用 API",
        )}
      </p>
      <Button
        onClick={onCreateClick}
        style={{
          background: "hsl(var(--ink-accent))",
          color: "hsl(var(--ink-background))",
        }}
      >
        <Plus className="w-4 h-4 mr-2" />
        {t("tokens.create.label", "新建令牌")}
      </Button>
    </div>
  );
}
