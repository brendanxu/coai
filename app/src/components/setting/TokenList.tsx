/**
 * TokenList — displays the user's masked API tokens.
 *
 * PKG-A-3 Wave 2, Tasks 2.2 + 2.4
 *
 * Features:
 *  - Masked key display with click-to-copy
 *  - Status badge (active / revoked / expired)
 *  - Dropdown per row: copy, rename, revoke, usage
 *  - Revoke: AlertDialog confirm; special warning when last active token
 *  - Rename: inline Dialog with name input
 *  - Usage: triggers TokenUsageDrawer
 */

import { useState } from "react";
import axios from "axios";
import { useTranslation } from "react-i18next";
import {
  CheckCheck,
  Copy,
  Edit2,
  MoreHorizontal,
  Trash2,
  BarChart2,
} from "lucide-react";
import { Badge } from "@/components/ui/badge.tsx";
import { Button } from "@/components/ui/button.tsx";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu.tsx";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog.tsx";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.tsx";
import { Input } from "@/components/ui/input.tsx";
import { Label } from "@/components/ui/label.tsx";
import { toast } from "sonner";
import { MaskedToken } from "./types.ts";
import TokenUsageDrawer from "./TokenUsageDrawer.tsx";

interface TokenListProps {
  tokens: MaskedToken[];
  onRefresh: () => void;
}

/** Token.status constants */
const STATUS_ACTIVE = 1;
const STATUS_REVOKED = 2;

function useTokenActions(onRefresh: () => void) {
  const { t } = useTranslation();

  const revoke = async (tokenId: number) => {
    try {
      await axios.delete(`/gtk/v1/tokens/${tokenId}`);
      toast.success(t("tokens.revoke.success", "令牌已撤销"));
      onRefresh();
    } catch (err: unknown) {
      const msg =
        axios.isAxiosError(err) && err.response?.data?.message === "CANNOT_REVOKE_LAST_TOKEN"
          ? t("tokens.revoke.last_token_warn", "这是您最后一个活跃令牌，撤销后无法使用 API")
          : t("tokens.errors.revoke_failed", "撤销失败，请稍后重试");
      toast.error(msg);
    }
  };

  const rename = async (tokenId: number, newName: string) => {
    if (!newName.trim()) return;
    try {
      await axios.patch(`/gtk/v1/tokens/${tokenId}`, { name: newName.trim() });
      toast.success(t("tokens.rename.success", "已重命名"));
      onRefresh();
    } catch {
      toast.error(t("tokens.errors.rename_failed", "重命名失败，请稍后重试"));
    }
  };

  return { revoke, rename };
}

export default function TokenList({ tokens, onRefresh }: TokenListProps) {
  const activeCount = tokens.filter((t) => t.status === STATUS_ACTIVE).length;

  return (
    <div className="space-y-3">
      {tokens.map((token) => (
        <TokenRow
          key={token.id}
          token={token}
          activeCount={activeCount}
          onRefresh={onRefresh}
        />
      ))}
    </div>
  );
}

function TokenRow({
  token,
  activeCount,
  onRefresh,
}: {
  token: MaskedToken;
  activeCount: number;
  onRefresh: () => void;
}) {
  const { t } = useTranslation();
  const [copied, setCopied] = useState(false);
  const [revokeOpen, setRevokeOpen] = useState(false);
  const [renameOpen, setRenameOpen] = useState(false);
  const [renameValue, setRenameValue] = useState(token.name);
  const [renameLoading, setRenameLoading] = useState(false);
  const [usageOpen, setUsageOpen] = useState(false);

  const { revoke, rename } = useTokenActions(onRefresh);

  const isLast = activeCount === 1 && token.status === STATUS_ACTIVE;

  const copyKey = async () => {
    try {
      await navigator.clipboard.writeText(token.key);
      setCopied(true);
      toast.success(t("tokens.copy.success", "已复制"));
      setTimeout(() => setCopied(false), 1500);
    } catch {
      toast.error(t("tokens.copy.failed", "复制失败，请手动选择"));
    }
  };

  const handleRevoke = async () => {
    await revoke(token.id);
    setRevokeOpen(false);
  };

  const handleRename = async () => {
    setRenameLoading(true);
    await rename(token.id, renameValue);
    setRenameLoading(false);
    setRenameOpen(false);
  };

  return (
    <>
      <div
        className="flex items-center gap-3 rounded-xl px-4 py-3"
        style={{
          background: "rgba(255,252,247,0.05)",
          border: "1px solid rgba(255,252,247,0.09)",
        }}
      >
        {/* Name + key */}
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 mb-0.5">
            <span
              className="text-sm font-medium truncate"
              style={{ color: "hsl(var(--ink-foreground))" }}
            >
              {token.name || t("tokens.unnamed", "未命名")}
            </span>
            <TokenStatusBadge status={token.status} />
          </div>
          <div className="flex items-center gap-1.5">
            <span
              className="font-mono text-xs"
              style={{ color: "rgba(255,252,247,0.45)" }}
            >
              {token.key}
            </span>
            <button
              type="button"
              onClick={copyKey}
              className="p-0.5 rounded transition-colors hover:opacity-80"
              style={{ color: copied ? "hsl(var(--ink-accent))" : "rgba(255,252,247,0.35)" }}
              aria-label={t("tokens.copy", "复制")}
            >
              {copied ? (
                <CheckCheck className="w-3 h-3" />
              ) : (
                <Copy className="w-3 h-3" />
              )}
            </button>
          </div>
        </div>

        {/* Quota / expire info */}
        <div className="hidden sm:flex flex-col items-end shrink-0 mr-2">
          <span
            className="text-xs"
            style={{ color: "rgba(255,252,247,0.45)" }}
          >
            {token.unlimited_quota
              ? t("tokens.quota.unlimited", "无限额度")
              : `${token.remain_quota.toLocaleString()} credits`}
          </span>
          <span
            className="text-[10px]"
            style={{ color: "rgba(255,252,247,0.30)" }}
          >
            {token.expired_time === -1
              ? t("tokens.expire.never", "永不过期")
              : formatExpiry(token.expired_time)}
          </span>
        </div>

        {/* Actions dropdown */}
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <Button
              variant="ghost"
              size="icon"
              className="shrink-0 h-8 w-8"
              style={{ color: "rgba(255,252,247,0.45)" }}
              aria-label="Token actions"
            >
              <MoreHorizontal className="w-4 h-4" />
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={copyKey}>
              <Copy className="w-3.5 h-3.5 mr-2" />
              {t("tokens.copy", "复制")}
            </DropdownMenuItem>
            <DropdownMenuItem
              onClick={() => {
                setRenameValue(token.name);
                setRenameOpen(true);
              }}
              disabled={token.status !== STATUS_ACTIVE}
            >
              <Edit2 className="w-3.5 h-3.5 mr-2" />
              {t("tokens.rename", "改名")}
            </DropdownMenuItem>
            <DropdownMenuItem onClick={() => setUsageOpen(true)}>
              <BarChart2 className="w-3.5 h-3.5 mr-2" />
              {t("tokens.usage", "用量")}
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={() => setRevokeOpen(true)}
              disabled={token.status !== STATUS_ACTIVE}
              className="text-red-400 focus:text-red-400"
            >
              <Trash2 className="w-3.5 h-3.5 mr-2" />
              {t("tokens.revoke", "撤销")}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </div>

      {/* Revoke confirm */}
      <AlertDialog open={revokeOpen} onOpenChange={setRevokeOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>
              {t("tokens.revoke.confirm", "确定要撤销此 token？")}
            </AlertDialogTitle>
            <AlertDialogDescription>
              {isLast ? (
                <span className="text-red-400 font-medium">
                  {t(
                    "tokens.revoke.last_token_warn",
                    "这是您最后一个活跃令牌，撤销后将无法使用 API。请先创建新令牌再撤销此令牌。",
                  )}
                </span>
              ) : (
                t(
                  "tokens.revoke.description",
                  "撤销后使用此 key 的 API 调用将返回 401。此操作无法撤销。",
                )
              )}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("cancel", "取消")}</AlertDialogCancel>
            {!isLast && (
              <AlertDialogAction
                onClick={handleRevoke}
                className="bg-red-600 hover:bg-red-700"
              >
                {t("tokens.revoke", "撤销")}
              </AlertDialogAction>
            )}
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Rename dialog */}
      <Dialog open={renameOpen} onOpenChange={setRenameOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{t("tokens.rename.title", "重命名令牌")}</DialogTitle>
          </DialogHeader>
          <div className="py-2">
            <Label htmlFor="rename-input" className="text-sm mb-2 block">
              {t("tokens.create.name_label", "名称")}
            </Label>
            <Input
              id="rename-input"
              value={renameValue}
              onChange={(e) => setRenameValue(e.target.value)}
              maxLength={64}
              placeholder={t("tokens.name_placeholder", "例如：生产环境")}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleRename();
              }}
            />
          </div>
          <DialogFooter>
            <Button variant="ghost" onClick={() => setRenameOpen(false)}>
              {t("cancel", "取消")}
            </Button>
            <Button
              onClick={handleRename}
              disabled={!renameValue.trim() || renameLoading}
            >
              {renameLoading ? t("tokens.rename.saving", "保存中…") : t("tokens.rename.save", "保存")}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Usage drawer */}
      <TokenUsageDrawer
        open={usageOpen}
        onOpenChange={setUsageOpen}
        tokenId={token.id}
        tokenName={token.name}
      />
    </>
  );
}

function TokenStatusBadge({ status }: { status: number }) {
  const { t } = useTranslation();
  if (status === STATUS_ACTIVE) {
    return (
      <Badge
        className="text-[10px] py-0 px-1.5"
        style={{
          background: "rgba(74,222,128,0.15)",
          color: "rgb(74,222,128)",
          border: "1px solid rgba(74,222,128,0.30)",
        }}
      >
        {t("tokens.status.active", "活跃")}
      </Badge>
    );
  }
  if (status === STATUS_REVOKED) {
    return (
      <Badge
        className="text-[10px] py-0 px-1.5"
        style={{
          background: "rgba(248,113,113,0.12)",
          color: "rgb(248,113,113)",
          border: "1px solid rgba(248,113,113,0.25)",
        }}
      >
        {t("tokens.status.revoked", "已撤销")}
      </Badge>
    );
  }
  // status 3 = expired
  return (
    <Badge
      className="text-[10px] py-0 px-1.5"
      style={{
        background: "rgba(251,191,36,0.12)",
        color: "rgb(251,191,36)",
        border: "1px solid rgba(251,191,36,0.25)",
      }}
    >
      {t("tokens.status.expired", "已过期")}
    </Badge>
  );
}

function formatExpiry(unixSec: number): string {
  if (unixSec <= 0) return "永不过期";
  const d = new Date(unixSec * 1000);
  return `${d.getFullYear()}/${String(d.getMonth() + 1).padStart(2, "0")}/${String(d.getDate()).padStart(2, "0")} 过期`;
}
