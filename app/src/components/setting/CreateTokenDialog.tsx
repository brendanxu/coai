/**
 * CreateTokenDialog — form to create a new personal API token.
 *
 * PKG-A-3 Wave 2, Task 2.3
 *
 * Flow:
 *  1. User fills name + expiry (+ optional quota).
 *  2. POST /api/gtk/v1/tokens
 *  3. On success → show one-time plaintext key reveal panel.
 *     - Big monospace key display + copy button
 *     - Red warning: "此 key 只显示一次"
 *     - "我已保存" button to dismiss (clears key from state)
 *  4. On MAX_TOKENS_REACHED → toast + close.
 *
 * The plaintext key is stored only in local component state and is cleared
 * immediately when the user clicks "我已保存" or the dialog closes.
 */

import { useState } from "react";
import axios from "axios";
import { useTranslation } from "react-i18next";
import { AlertTriangle, CheckCheck, Copy } from "lucide-react";
import { Checkbox } from "@/components/ui/checkbox.tsx";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import { Label } from "@/components/ui/label.tsx";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select.tsx";
import { toast } from "sonner";
import { CreatedToken } from "./types.ts";

interface CreateTokenDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}

type ExpireOption = "never" | "30d" | "90d" | "365d";

function expireOptionToUnix(opt: ExpireOption): number {
  if (opt === "never") return -1;
  const days = opt === "30d" ? 30 : opt === "90d" ? 90 : 365;
  return Math.floor(Date.now() / 1000) + days * 86400;
}

export default function CreateTokenDialog({
  open,
  onOpenChange,
  onCreated,
}: CreateTokenDialogProps) {
  const { t } = useTranslation();

  // Form state
  const [name, setName] = useState("");
  const [expire, setExpire] = useState<ExpireOption>("never");
  const [unlimited, setUnlimited] = useState(true);
  const [quota, setQuota] = useState("");
  const [loading, setLoading] = useState(false);

  // One-time reveal state
  const [createdToken, setCreatedToken] = useState<CreatedToken | null>(null);
  const [copied, setCopied] = useState(false);

  const isRevealPhase = createdToken !== null;
  const nameError = name.trim().length === 0
    ? ""
    : name.length > 64
    ? t("tokens.create.name_too_long", "名称不超过 64 字符")
    : "";

  const handleCreate = async () => {
    if (!name.trim()) return;
    setLoading(true);
    try {
      const parsedQuota = parseInt(quota, 10);
      const hasExplicitQuota = !unlimited && !isNaN(parsedQuota) && parsedQuota > 0;
      const resp = await axios.post<{ success: boolean; data: CreatedToken }>(
        "/gtk/v1/tokens",
        {
          name: name.trim(),
          expired_time: expireOptionToUnix(expire),
          unlimited_quota: unlimited,
          remain_quota: hasExplicitQuota ? parsedQuota : 0,
          has_explicit_quota: hasExplicitQuota,
        },
      );
      if (resp.data?.success && resp.data.data) {
        setCreatedToken(resp.data.data);
        onCreated(); // refresh list in background
      }
    } catch (err: unknown) {
      if (axios.isAxiosError(err) && err.response?.data?.message === "MAX_TOKENS_REACHED") {
        toast.error(t("tokens.errors.max_reached", "已达上限 10 个 token"));
        onOpenChange(false);
      } else {
        toast.error(t("tokens.errors.create_failed", "创建失败，请稍后重试"));
      }
    } finally {
      setLoading(false);
    }
  };

  const handleCopyKey = async () => {
    if (!createdToken) return;
    try {
      await navigator.clipboard.writeText(createdToken.key);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
      toast.success(t("tokens.copy.success", "已复制"));
    } catch {
      toast.error(t("tokens.copy.failed", "复制失败，请手动选择"));
    }
  };

  const handleConfirmSaved = () => {
    // Immediately clear the plaintext from state before closing
    setCreatedToken(null);
    setCopied(false);
    resetForm();
    onOpenChange(false);
  };

  const resetForm = () => {
    setName("");
    setExpire("never");
    setUnlimited(true);
    setQuota("");
    setLoading(false);
    setCreatedToken(null);
    setCopied(false);
  };

  const handleOpenChange = (val: boolean) => {
    if (!val) {
      // Clear sensitive data on close regardless of phase
      resetForm();
    }
    onOpenChange(val);
  };

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="max-w-md">
        {!isRevealPhase ? (
          <>
            <DialogHeader>
              <DialogTitle>
                {t("tokens.create.title", "新建 API 令牌")}
              </DialogTitle>
            </DialogHeader>

            <div className="space-y-4 py-2">
              {/* Name */}
              <div className="space-y-1.5">
                <Label htmlFor="token-name">
                  {t("tokens.create.name_label", "名称")}
                  <span className="text-red-400 ml-0.5">*</span>
                </Label>
                <Input
                  id="token-name"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder={t("tokens.name_placeholder", "例如：生产环境")}
                  maxLength={64}
                  autoFocus
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && name.trim()) handleCreate();
                  }}
                />
                {nameError && (
                  <p className="text-xs text-red-400">{nameError}</p>
                )}
                <p
                  className="text-[11px]"
                  style={{ color: "rgba(255,252,247,0.35)" }}
                >
                  {name.length}/64
                </p>
              </div>

              {/* Expiry */}
              <div className="space-y-1.5">
                <Label htmlFor="token-expire">
                  {t("tokens.create.expire_label", "过期时间")}
                </Label>
                <Select
                  value={expire}
                  onValueChange={(v) => setExpire(v as ExpireOption)}
                >
                  <SelectTrigger id="token-expire">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="never">
                      {t("tokens.create.never", "永不过期")}
                    </SelectItem>
                    <SelectItem value="30d">30 天</SelectItem>
                    <SelectItem value="90d">90 天</SelectItem>
                    <SelectItem value="365d">1 年</SelectItem>
                  </SelectContent>
                </Select>
              </div>

              {/* Unlimited quota checkbox */}
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <Checkbox
                    id="token-unlimited"
                    checked={unlimited}
                    onCheckedChange={(v) => setUnlimited(!!v)}
                  />
                  <Label htmlFor="token-unlimited" className="cursor-pointer">
                    {t("tokens.create.unlimited", "无限额度 / Unlimited")}
                  </Label>
                </div>
                {!unlimited && (
                  <div className="space-y-1">
                    <Label htmlFor="token-quota" className="text-xs" style={{ color: "rgba(255,252,247,0.55)" }}>
                      {t("tokens.create.quota_label", "额度 (credits)")}
                    </Label>
                    <input
                      id="token-quota"
                      type="number"
                      min={1}
                      value={quota}
                      onChange={(e) => setQuota(e.target.value)}
                      placeholder="e.g. 500000"
                      className="w-full rounded-md border px-3 py-1.5 text-sm"
                      style={{
                        background: "rgba(255,252,247,0.06)",
                        border: "1px solid rgba(255,252,247,0.15)",
                        color: "hsl(var(--ink-foreground))",
                      }}
                    />
                  </div>
                )}
              </div>
            </div>

            <DialogFooter>
              <Button variant="ghost" onClick={() => handleOpenChange(false)}>
                {t("cancel", "取消")}
              </Button>
              <Button
                onClick={handleCreate}
                disabled={!name.trim() || !!nameError || loading}
              >
                {loading
                  ? t("tokens.create.creating", "创建中…")
                  : t("tokens.create", "新建令牌")}
              </Button>
            </DialogFooter>
          </>
        ) : (
          /* One-time key reveal phase */
          <>
            <DialogHeader>
              <DialogTitle>
                {t("tokens.created.title", "令牌已创建")}
              </DialogTitle>
            </DialogHeader>

            <div className="space-y-4 py-2">
              {/* Red warning */}
              <div
                className="flex items-start gap-2 rounded-lg px-3 py-2.5"
                style={{
                  background: "rgba(248,113,113,0.10)",
                  border: "1px solid rgba(248,113,113,0.25)",
                }}
              >
                <AlertTriangle
                  className="w-4 h-4 shrink-0 mt-0.5"
                  style={{ color: "rgb(248,113,113)" }}
                />
                <p className="text-sm" style={{ color: "rgb(248,113,113)" }}>
                  {t(
                    "tokens.create.plaintext_warning",
                    "此 key 只显示一次，请立即复制保存。关闭后将无法再看到。",
                  )}
                </p>
              </div>

              {/* Token name label */}
              <p
                className="text-xs"
                style={{ color: "rgba(255,252,247,0.45)" }}
              >
                {t("tokens.created.name_label", "令牌名称")}：
                <span style={{ color: "hsl(var(--ink-foreground))" }}>
                  {createdToken.name}
                </span>
              </p>

              {/* Key display */}
              <div
                className="flex items-center gap-3 rounded-xl px-4 py-3"
                style={{
                  background: "rgba(255,252,247,0.06)",
                  border: "1px solid rgba(255,252,247,0.12)",
                }}
              >
                <code
                  className="flex-1 text-sm font-mono break-all select-all"
                  style={{ color: "hsl(var(--ink-accent))" }}
                >
                  {createdToken.key}
                </code>
                <button
                  type="button"
                  onClick={handleCopyKey}
                  className="shrink-0 p-2 rounded-lg transition-colors hover:opacity-80"
                  style={{
                    background: "rgba(255,252,247,0.08)",
                    border: "1px solid rgba(255,252,247,0.12)",
                    color: copied
                      ? "hsl(var(--ink-accent))"
                      : "rgba(255,252,247,0.55)",
                  }}
                  aria-label={t("tokens.copy", "复制")}
                >
                  {copied ? (
                    <CheckCheck className="w-4 h-4" />
                  ) : (
                    <Copy className="w-4 h-4" />
                  )}
                </button>
              </div>
            </div>

            <DialogFooter>
              <Button
                onClick={handleConfirmSaved}
                style={{
                  background: "hsl(var(--ink-accent))",
                  color: "hsl(var(--ink-background))",
                }}
              >
                {t("tokens.create.success_action", "我已保存")}
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}
