/**
 * TokensTab — Admin "令牌审计" tab.
 *
 * PKG-A-3 Wave 3, Tasks 3.2 – 3.5
 *
 * Features:
 *  - Global token list across all users (GET /gtk/v1/admin/tokens)
 *  - Filters: user_id search, status dropdown (supported by backend)
 *  - Force-revoke any token — no last-token guard (admin responsibility)
 *  - Audit log drawer (GET /gtk/v1/admin/tokens/:id/audit)
 *
 * Admin endpoint shapes (from newapi/admin_tokens.go):
 *  GET /gtk/v1/admin/tokens
 *    → { success: true, data: AdminTokenRow[] }
 *    AdminTokenRow = MaskedToken + coai_user_id
 *    Supports ?coai_user_id=<coai_user_id> filter (binding lookup on backend).
 *    Note: backend does NOT support ?status= or ?created_after= filters
 *    (global list fetches per-binding from NewAPI which has no server-side
 *    filter). Status filtering is done client-side here.
 *
 *  DELETE /gtk/v1/admin/tokens/:id?coai_user_id=<id>
 *    → { success: true }  (requires coai_user_id query param)
 *
 *  GET /gtk/v1/admin/tokens/:id/audit
 *    → { success: true, data: AuditEntry[] }  (may be empty if no audit rows)
 */

import { useState, useEffect, useCallback } from "react";
import axios from "axios";
import { toast } from "sonner";
import { Search, RefreshCw, ShieldOff, ClipboardList } from "lucide-react";
import { Badge } from "@/components/ui/badge.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select.tsx";
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
  Sheet,
  SheetContent,
  SheetHeader,
  SheetTitle,
} from "@/components/ui/sheet.tsx";

// ── Types ──────────────────────────────────────────────────────────────────

interface AdminTokenRow {
  id: number;
  user_id: number; // newapi user id
  coai_user_id: number;
  name: string;
  key: string; // masked
  status: number; // 1=active 2=revoked
  remain_quota: number;
  unlimited_quota: boolean;
  expired_time: number; // unix seconds; -1=never
}

interface AuditEntry {
  id: number;
  actor_id: number;
  action: string;
  resource_type: string;
  resource_id: number;
  detail: string;
  created_at: string; // RFC3339
}

type StatusFilter = "all" | "active" | "revoked";

// ── Helpers ────────────────────────────────────────────────────────────────

const STATUS_ACTIVE = 1;
const STATUS_REVOKED = 2;

function statusLabel(s: number): string {
  if (s === STATUS_ACTIVE) return "活跃";
  if (s === STATUS_REVOKED) return "已撤销";
  return "已过期";
}

function statusVariant(s: number): "default" | "secondary" | "destructive" {
  if (s === STATUS_ACTIVE) return "default";
  if (s === STATUS_REVOKED) return "destructive";
  return "secondary";
}

function formatDate(unix: number): string {
  if (unix === -1 || unix === 0) return "永不过期";
  return new Date(unix * 1000).toLocaleDateString("zh-CN");
}

function formatTs(iso: string): string {
  try {
    return new Date(iso).toLocaleString("zh-CN");
  } catch {
    return iso;
  }
}

// ── Audit Drawer ───────────────────────────────────────────────────────────

interface AuditDrawerProps {
  open: boolean;
  onOpenChange: (v: boolean) => void;
  tokenId: number;
  tokenName: string;
}

function AuditDrawer({ open, onOpenChange, tokenId, tokenName }: AuditDrawerProps) {
  const [records, setRecords] = useState<AuditEntry[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    setLoading(true);
    setError(null);
    setRecords([]);
    axios
      .get<{ success: boolean; data: AuditEntry[] }>(
        `/gtk/v1/admin/tokens/${tokenId}/audit`,
      )
      .then((r) => {
        if (r.data?.success) {
          setRecords(r.data.data ?? []);
        } else {
          setError("加载审计记录失败");
        }
      })
      .catch(() => setError("加载审计记录失败"))
      .finally(() => setLoading(false));
  }, [open, tokenId]);

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent side="right" className="w-[420px] sm:w-[520px] overflow-y-auto">
        <SheetHeader>
          <SheetTitle>
            操作审计 — {tokenName || `Token #${tokenId}`}
          </SheetTitle>
        </SheetHeader>

        <div className="mt-6">
          {loading && (
            <div className="text-sm text-muted-foreground py-8 text-center">
              加载中…
            </div>
          )}
          {error && (
            <div className="text-sm text-red-500 py-8 text-center">{error}</div>
          )}
          {!loading && !error && records.length === 0 && (
            <div className="text-sm text-muted-foreground py-8 text-center">
              暂无审计记录
            </div>
          )}
          {!loading && !error && records.length > 0 && (
            <ol className="space-y-3">
              {records.map((rec) => (
                <li
                  key={rec.id}
                  className="rounded-lg border px-4 py-3 text-sm"
                  style={{ borderColor: "rgba(255,252,247,0.09)" }}
                >
                  <div className="flex items-center justify-between mb-1">
                    <span
                      className="font-medium font-mono text-xs px-2 py-0.5 rounded"
                      style={{ background: "rgba(255,252,247,0.07)" }}
                    >
                      {rec.action}
                    </span>
                    <span className="text-xs text-muted-foreground">
                      {formatTs(rec.created_at)}
                    </span>
                  </div>
                  <div className="text-xs text-muted-foreground mt-1">
                    操作人 ID: {rec.actor_id}
                  </div>
                  {rec.detail && (
                    <div
                      className="mt-2 text-xs font-mono whitespace-pre-wrap break-all rounded px-2 py-1.5"
                      style={{ background: "rgba(255,252,247,0.04)" }}
                    >
                      {rec.detail}
                    </div>
                  )}
                </li>
              ))}
            </ol>
          )}
        </div>
      </SheetContent>
    </Sheet>
  );
}

// ── Main TokensTab ─────────────────────────────────────────────────────────

export function TokensTab() {
  const [allTokens, setAllTokens] = useState<AdminTokenRow[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // Filters — user_id is sent to backend; status is client-side
  const [userSearch, setUserSearch] = useState("");
  const [pendingUserSearch, setPendingUserSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  // Force-revoke dialog
  const [revokeTarget, setRevokeTarget] = useState<AdminTokenRow | null>(null);
  const [revoking, setRevoking] = useState(false);

  // Audit drawer
  const [auditTarget, setAuditTarget] = useState<AdminTokenRow | null>(null);

  const load = useCallback(
    (uid?: string) => {
      setLoading(true);
      setError(null);
      const params = uid ? `?coai_user_id=${uid}` : "";
      axios
        .get<{ success: boolean; data: AdminTokenRow[] }>(
          `/gtk/v1/admin/tokens${params}`,
        )
        .then((r) => {
          if (r.data?.success) {
            setAllTokens(r.data.data ?? []);
          } else {
            setError("加载令牌列表失败");
          }
        })
        .catch(() => setError("加载令牌列表失败"))
        .finally(() => setLoading(false));
    },
    [],
  );

  useEffect(() => {
    load();
  }, [load]);

  // Client-side status filter
  const visibleTokens = allTokens.filter((t) => {
    if (statusFilter === "all") return true;
    if (statusFilter === "active") return t.status === STATUS_ACTIVE;
    if (statusFilter === "revoked") return t.status === STATUS_REVOKED;
    return true;
  });

  const handleUserSearch = () => {
    const uid = pendingUserSearch.trim();
    setUserSearch(uid);
    load(uid || undefined);
  };

  const handleReset = () => {
    setPendingUserSearch("");
    setUserSearch("");
    setStatusFilter("all");
    load();
  };

  const handleForceRevoke = async () => {
    if (!revokeTarget) return;
    setRevoking(true);
    try {
      await axios.delete(
        `/gtk/v1/admin/tokens/${revokeTarget.id}?coai_user_id=${revokeTarget.coai_user_id}`,
      );
      toast.success("令牌已强制撤销");
      setRevokeTarget(null);
      // Refresh list (same filter as current)
      load(userSearch || undefined);
    } catch (err: unknown) {
      const msg =
        axios.isAxiosError(err) && err.response?.data?.message
          ? err.response.data.message
          : "撤销失败，请稍后重试";
      toast.error(msg);
    } finally {
      setRevoking(false);
    }
  };

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h2 className="text-lg font-semibold">令牌审计</h2>
          <p className="text-sm text-muted-foreground mt-0.5">
            全局 token 列表 · admin 可强制撤销任意 token
          </p>
        </div>
        <button
          type="button"
          onClick={() => load(userSearch || undefined)}
          className="btn btn-ghost btn-sm flex items-center gap-1.5"
          style={{ fontSize: 13 }}
        >
          <RefreshCw className="w-3.5 h-3.5" />
          刷新
        </button>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap gap-3 items-center">
        {/* User ID search — sent to backend */}
        <div className="flex gap-2 items-center">
          <div className="relative">
            <Search className="absolute left-2.5 top-1/2 -translate-y-1/2 w-3.5 h-3.5 text-muted-foreground pointer-events-none" />
            <Input
              className="pl-8 h-8 w-52 text-sm"
              placeholder="用户 ID 搜索"
              value={pendingUserSearch}
              onChange={(e) => setPendingUserSearch(e.target.value)}
              onKeyDown={(e) => e.key === "Enter" && handleUserSearch()}
            />
          </div>
          <Button variant="outline" size="sm" className="h-8" onClick={handleUserSearch}>
            搜索
          </Button>
        </div>

        {/* Status filter — client-side (backend global list has no status param) */}
        <Select
          value={statusFilter}
          onValueChange={(v) => setStatusFilter(v as StatusFilter)}
        >
          <SelectTrigger className="h-8 w-36 text-sm">
            <SelectValue placeholder="全部状态" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">全部状态</SelectItem>
            <SelectItem value="active">活跃</SelectItem>
            <SelectItem value="revoked">已撤销</SelectItem>
          </SelectContent>
        </Select>

        {/* Note: 创建时间筛选 — backend global list does not support created_after/before params,
            so date range filter is omitted to avoid full-table client-side hacks. */}

        {(userSearch || statusFilter !== "all") && (
          <Button variant="ghost" size="sm" className="h-8 text-muted-foreground" onClick={handleReset}>
            重置
          </Button>
        )}

        <span className="ml-auto text-xs text-muted-foreground">
          {loading ? "加载中…" : `共 ${visibleTokens.length} 条`}
        </span>
      </div>

      {/* Table */}
      {error ? (
        <div className="rounded-xl border px-6 py-8 text-center text-sm text-red-500">
          {error}
        </div>
      ) : loading ? (
        <div className="rounded-xl border px-6 py-12 text-center text-sm text-muted-foreground">
          加载中…
        </div>
      ) : visibleTokens.length === 0 ? (
        <div className="rounded-xl border px-6 py-12 text-center text-sm text-muted-foreground">
          暂无令牌记录
        </div>
      ) : (
        <div className="overflow-x-auto rounded-xl border" style={{ borderColor: "rgba(255,252,247,0.09)" }}>
          <table className="w-full text-sm">
            <thead>
              <tr style={{ borderBottom: "1px solid rgba(255,252,247,0.09)" }}>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">用户 ID</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">名称</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">Masked Key</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">状态</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">额度</th>
                <th className="text-left px-4 py-3 font-medium text-muted-foreground">过期时间</th>
                <th className="text-right px-4 py-3 font-medium text-muted-foreground">操作</th>
              </tr>
            </thead>
            <tbody>
              {visibleTokens.map((tok) => (
                <tr
                  key={tok.id}
                  style={{ borderBottom: "1px solid rgba(255,252,247,0.05)" }}
                >
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                    #{tok.coai_user_id}
                  </td>
                  <td className="px-4 py-3 font-medium max-w-[140px] truncate">
                    {tok.name || <span className="text-muted-foreground italic">未命名</span>}
                  </td>
                  <td className="px-4 py-3 font-mono text-xs text-muted-foreground">
                    {tok.key}
                  </td>
                  <td className="px-4 py-3">
                    <Badge variant={statusVariant(tok.status)}>
                      {statusLabel(tok.status)}
                    </Badge>
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {tok.unlimited_quota
                      ? "无限"
                      : tok.remain_quota.toLocaleString()}
                  </td>
                  <td className="px-4 py-3 text-xs text-muted-foreground">
                    {formatDate(tok.expired_time)}
                  </td>
                  <td className="px-4 py-3">
                    <div className="flex justify-end gap-2">
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 gap-1.5 text-xs"
                        onClick={() => setAuditTarget(tok)}
                        title="查看审计"
                      >
                        <ClipboardList className="w-3.5 h-3.5" />
                        审计
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        className="h-7 gap-1.5 text-xs text-red-400 hover:text-red-400"
                        disabled={tok.status !== STATUS_ACTIVE}
                        onClick={() => setRevokeTarget(tok)}
                        title="强制撤销"
                      >
                        <ShieldOff className="w-3.5 h-3.5" />
                        撤销
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {/* Force-revoke confirm dialog */}
      <AlertDialog
        open={!!revokeTarget}
        onOpenChange={(v) => !v && setRevokeTarget(null)}
      >
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>强制撤销 token？</AlertDialogTitle>
            <AlertDialogDescription>
              用户 <strong>#{revokeTarget?.coai_user_id}</strong> 的 token{" "}
              <strong>&quot;{revokeTarget?.name || "未命名"}&quot;</strong>{" "}
              将立即失效，使用此 key 的 API 调用将返回 401。
              <br />
              <span className="text-yellow-500 font-medium">
                此操作无法撤销，且不受最后一个 token 保护限制。
              </span>
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={revoking}>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleForceRevoke}
              disabled={revoking}
              className="bg-red-600 hover:bg-red-700"
            >
              {revoking ? "撤销中…" : "确定撤销"}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Audit log drawer */}
      {auditTarget && (
        <AuditDrawer
          open={!!auditTarget}
          onOpenChange={(v) => !v && setAuditTarget(null)}
          tokenId={auditTarget.id}
          tokenName={auditTarget.name}
        />
      )}
    </div>
  );
}
