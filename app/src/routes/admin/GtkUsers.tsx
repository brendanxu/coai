// GtkUsers — /admin/gtk-users
//
// User management page. Lists NewAPI users with quota adjust + status toggle.
// No i18n — hardcoded Chinese.

import { useEffect, useState, useCallback } from "react";
import axios from "axios";
import { toast } from "sonner";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";

interface NewAPIUser {
  id: number;
  username: string;
  display_name: string;
  email: string;
  role: number;
  status: number;
  quota: number;
  used_quota: number;
  created_time: number;
}

function roleName(role: number): string {
  if (role >= 100) return "超管";
  if (role >= 10) return "管理员";
  return "用户";
}

function statusLabel(status: number): string {
  return status === 1 ? "启用" : "停用";
}

function formatQuota(q: number): string {
  if (q >= 1_000_000) return (q / 1_000_000).toFixed(2) + "M";
  if (q >= 1_000) return (q / 1_000).toFixed(1) + "K";
  return String(q);
}

function GtkUsers() {
  const [users, setUsers] = useState<NewAPIUser[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [keyword, setKeyword] = useState("");
  const [searchInput, setSearchInput] = useState("");
  const [loading, setLoading] = useState(false);

  // Inline quota edit state: { [userId]: string }
  const [quotaInputs, setQuotaInputs] = useState<Record<number, string>>({});
  const [quotaSaving, setQuotaSaving] = useState<Record<number, boolean>>({});
  const [statusSaving, setStatusSaving] = useState<Record<number, boolean>>({});

  const PAGE_SIZE = 20;

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const r = await axios.get<{
        success: boolean;
        data?: { users: NewAPIUser[]; total: number };
      }>(
        `/gtk/v1/admin/users?page=${page}&page_size=${PAGE_SIZE}&keyword=${encodeURIComponent(keyword)}`
      );
      if (r.data?.success && r.data.data) {
        setUsers(r.data.data.users ?? []);
        setTotal(r.data.data.total ?? 0);
      }
    } catch (_e) {
      toast.error("加载用户列表失败");
    } finally {
      setLoading(false);
    }
  }, [page, keyword]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleSearch = () => {
    setKeyword(searchInput.trim());
    setPage(1);
  };

  const handleQuotaSave = async (userId: number) => {
    const raw = quotaInputs[userId];
    if (raw === undefined || raw === "") return;
    const quota = parseInt(raw, 10);
    if (isNaN(quota) || quota < 0) {
      toast.error("配额必须是非负整数");
      return;
    }
    setQuotaSaving((s) => ({ ...s, [userId]: true }));
    try {
      await axios.put(`/gtk/v1/admin/users/${userId}/quota`, { quota });
      toast.success("配额已更新");
      setQuotaInputs((s) => { const n = { ...s }; delete n[userId]; return n; });
      refresh();
    } catch (_e) {
      toast.error("配额更新失败");
    } finally {
      setQuotaSaving((s) => ({ ...s, [userId]: false }));
    }
  };

  const handleStatusToggle = async (user: NewAPIUser) => {
    const newStatus = user.status === 1 ? 0 : 1;
    setStatusSaving((s) => ({ ...s, [user.id]: true }));
    try {
      await axios.put(`/gtk/v1/admin/users/${user.id}/status`, {
        status: newStatus,
      });
      toast.success(newStatus === 1 ? "用户已启用" : "用户已停用");
      refresh();
    } catch (_e) {
      toast.error("状态更新失败");
    } finally {
      setStatusSaving((s) => ({ ...s, [user.id]: false }));
    }
  };

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  return (
    <div
      style={{
        fontFamily: "Manrope, sans-serif",
        padding: "24px 16px",
        color: "var(--ink, #2B2118)",
      }}
    >
      <h1
        style={{
          fontFamily: "Fraunces, serif",
          fontSize: 22,
          fontWeight: 700,
          margin: "0 0 20px",
        }}
      >
        用户管理
      </h1>

      {/* Search row */}
      <div
        style={{ display: "flex", gap: 10, marginBottom: 16, flexWrap: "wrap" }}
      >
        <Input
          value={searchInput}
          onChange={(e) => setSearchInput(e.target.value)}
          onKeyDown={(e) => e.key === "Enter" && handleSearch()}
          placeholder="搜索用户名 / 邮箱…"
          style={{ maxWidth: 280 }}
        />
        <Button
          onClick={handleSearch}
          style={{ background: "var(--accent, #2D5A3D)", color: "#fff" }}
        >
          搜索
        </Button>
        <Button variant="outline" onClick={() => { setSearchInput(""); setKeyword(""); setPage(1); }}>
          重置
        </Button>
        <Button variant="outline" onClick={refresh}>
          刷新
        </Button>
        {loading && (
          <span style={{ fontSize: 13, opacity: 0.5, alignSelf: "center" }}>
            加载中…
          </span>
        )}
      </div>

      {/* Table */}
      <div style={{ overflowX: "auto" }}>
        <table
          style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}
        >
          <thead>
            <tr
              style={{
                borderBottom: "1px solid rgba(0,0,0,0.1)",
                background: "rgba(0,0,0,0.02)",
              }}
            >
              {["ID", "用户名", "邮箱", "角色", "状态", "配额余量", "已用配额", "操作"].map(
                (h) => (
                  <th
                    key={h}
                    style={{
                      padding: "8px 10px",
                      textAlign: "left",
                      fontWeight: 600,
                      whiteSpace: "nowrap",
                    }}
                  >
                    {h}
                  </th>
                )
              )}
            </tr>
          </thead>
          <tbody>
            {!loading && users.length === 0 && (
              <tr>
                <td
                  colSpan={8}
                  style={{
                    textAlign: "center",
                    padding: "40px 0",
                    opacity: 0.4,
                  }}
                >
                  暂无用户
                </td>
              </tr>
            )}
            {users.map((u) => (
              <tr
                key={u.id}
                style={{
                  borderBottom: "1px solid rgba(0,0,0,0.06)",
                  verticalAlign: "middle",
                }}
              >
                <td
                  style={{
                    padding: "8px 10px",
                    opacity: 0.45,
                    fontFamily: "JetBrains Mono, monospace",
                  }}
                >
                  {u.id}
                </td>
                <td style={{ padding: "8px 10px", fontWeight: 500 }}>
                  {u.username}
                  {u.display_name && u.display_name !== u.username && (
                    <span style={{ fontSize: 11, opacity: 0.5, marginLeft: 6 }}>
                      ({u.display_name})
                    </span>
                  )}
                </td>
                <td style={{ padding: "8px 10px", opacity: 0.7 }}>
                  {u.email || "—"}
                </td>
                <td style={{ padding: "8px 10px" }}>
                  <span
                    style={{
                      fontSize: 11,
                      background:
                        u.role >= 10
                          ? "rgba(45,90,61,0.12)"
                          : "rgba(0,0,0,0.06)",
                      color:
                        u.role >= 10 ? "var(--accent, #2D5A3D)" : "inherit",
                      borderRadius: 4,
                      padding: "2px 6px",
                    }}
                  >
                    {roleName(u.role)}
                  </span>
                </td>
                <td style={{ padding: "8px 10px" }}>
                  <span
                    style={{
                      fontSize: 12,
                      color: u.status === 1 ? "var(--accent, #2D5A3D)" : "#dc2626",
                      fontWeight: 500,
                    }}
                  >
                    {statusLabel(u.status)}
                  </span>
                </td>
                <td
                  style={{
                    padding: "8px 10px",
                    fontFamily: "JetBrains Mono, monospace",
                    fontSize: 12,
                  }}
                >
                  {formatQuota(u.quota)}
                </td>
                <td
                  style={{
                    padding: "8px 10px",
                    fontFamily: "JetBrains Mono, monospace",
                    fontSize: 12,
                  }}
                >
                  {formatQuota(u.used_quota)}
                </td>
                <td style={{ padding: "8px 10px" }}>
                  <div
                    style={{
                      display: "flex",
                      gap: 6,
                      alignItems: "center",
                      flexWrap: "wrap",
                    }}
                  >
                    {/* Quota adjust */}
                    <Input
                      type="number"
                      value={quotaInputs[u.id] ?? ""}
                      onChange={(e) =>
                        setQuotaInputs((s) => ({ ...s, [u.id]: e.target.value }))
                      }
                      placeholder="新配额"
                      style={{ width: 90, height: 28, fontSize: 12 }}
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={
                        quotaSaving[u.id] ||
                        quotaInputs[u.id] === undefined ||
                        quotaInputs[u.id] === ""
                      }
                      onClick={() => handleQuotaSave(u.id)}
                      style={{ fontSize: 12, height: 28 }}
                    >
                      {quotaSaving[u.id] ? "…" : "调整配额"}
                    </Button>

                    {/* Status toggle */}
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={statusSaving[u.id]}
                      onClick={() => handleStatusToggle(u)}
                      style={{
                        fontSize: 12,
                        height: 28,
                        color: u.status === 1 ? "#dc2626" : "var(--accent, #2D5A3D)",
                        borderColor:
                          u.status === 1 ? "#dc2626" : "var(--accent, #2D5A3D)",
                      }}
                    >
                      {statusSaving[u.id]
                        ? "…"
                        : u.status === 1
                        ? "停用"
                        : "启用"}
                    </Button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {/* Pagination */}
      {totalPages > 1 && (
        <div
          style={{
            display: "flex",
            gap: 6,
            marginTop: 16,
            alignItems: "center",
            flexWrap: "wrap",
          }}
        >
          <span style={{ fontSize: 13, opacity: 0.6 }}>
            共 {total} 条，第 {page} / {totalPages} 页
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={page <= 1}
            onClick={() => setPage((p) => p - 1)}
            style={{ fontSize: 12 }}
          >
            上一页
          </Button>
          {Array.from({ length: Math.min(totalPages, 7) }, (_, i) => i + 1).map(
            (p) => (
              <Button
                key={p}
                size="sm"
                variant={p === page ? "default" : "outline"}
                onClick={() => setPage(p)}
                style={{
                  fontSize: 12,
                  background:
                    p === page ? "var(--accent, #2D5A3D)" : "transparent",
                  color: p === page ? "#fff" : "var(--ink, #2B2118)",
                }}
              >
                {p}
              </Button>
            )
          )}
          <Button
            size="sm"
            variant="outline"
            disabled={page >= totalPages}
            onClick={() => setPage((p) => p + 1)}
            style={{ fontSize: 12 }}
          >
            下一页
          </Button>
        </div>
      )}
    </div>
  );
}

export default GtkUsers;
