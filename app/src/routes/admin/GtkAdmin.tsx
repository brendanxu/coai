// GtkAdmin — /admin/gtk
//
// Unified admin hub. Standalone full-page layout (NOT nested inside CoAI's
// AdminPage). Left sidebar nav + right content area with summary cards.

import { useEffect, useState } from "react";
import { useNavigate, useLocation } from "react-router-dom";
import axios from "axios";

interface SummaryData {
  channelCount: number;
  userCount: number;
  loading: boolean;
}

const NAV_ITEMS = [
  { label: "渠道管理", path: "/admin/channels-routing" },
  { label: "用户管理", path: "/admin/gtk-users" },
  { label: "模型计费", path: "/admin/gtk-model-ratios" },
  { label: "系统设置", path: "/admin/gtk-settings" },
];

function GtkAdmin() {
  const navigate = useNavigate();
  const location = useLocation();
  const [summary, setSummary] = useState<SummaryData>({
    channelCount: 0,
    userCount: 0,
    loading: true,
  });

  useEffect(() => {
    let cancelled = false;
    Promise.all([
      axios.get<{ success: boolean; data?: { channels: unknown[]; total: number } }>(
        "/gtk/v1/admin/channels"
      ).catch(() => null),
      axios.get<{ success: boolean; data?: { users: unknown[]; total: number } }>(
        "/gtk/v1/admin/users?page=1&page_size=1"
      ).catch(() => null),
    ]).then(([chRes, usrRes]) => {
      if (cancelled) return;
      setSummary({
        channelCount:
          chRes?.data?.success && chRes.data.data
            ? (chRes.data.data.channels?.length ?? chRes.data.data.total ?? 0)
            : 0,
        userCount:
          usrRes?.data?.success && usrRes.data.data
            ? usrRes.data.data.total ?? 0
            : 0,
        loading: false,
      });
    });
    return () => { cancelled = true; };
  }, []);

  const isActive = (path: string) => location.pathname === path;

  return (
    <div
      style={{
        display: "flex",
        minHeight: "100vh",
        fontFamily: "Manrope, sans-serif",
        background: "var(--bg, #F4EFE5)",
        color: "var(--ink, #2B2118)",
      }}
    >
      {/* Sidebar */}
      <aside
        style={{
          width: 200,
          flexShrink: 0,
          background: "rgba(0,0,0,0.04)",
          borderRight: "1px solid rgba(0,0,0,0.08)",
          padding: "32px 0",
          display: "flex",
          flexDirection: "column",
          gap: 4,
        }}
      >
        <div
          style={{
            fontFamily: "Fraunces, serif",
            fontSize: 16,
            fontWeight: 700,
            padding: "0 20px 24px",
            borderBottom: "1px solid rgba(0,0,0,0.08)",
            marginBottom: 8,
          }}
        >
          GTK 管理后台
        </div>
        {NAV_ITEMS.map((item) => (
          <button
            key={item.path}
            onClick={() => navigate(item.path)}
            style={{
              display: "block",
              width: "100%",
              textAlign: "left",
              padding: "10px 20px",
              border: "none",
              background: isActive(item.path)
                ? "var(--accent, #2D5A3D)"
                : "transparent",
              color: isActive(item.path) ? "#fff" : "var(--ink, #2B2118)",
              fontSize: 14,
              cursor: "pointer",
              borderRadius: 0,
              fontFamily: "Manrope, sans-serif",
              transition: "background 0.15s",
            }}
          >
            {item.label}
          </button>
        ))}
      </aside>

      {/* Content */}
      <main style={{ flex: 1, padding: 32 }}>
        <h1
          style={{
            fontFamily: "Fraunces, serif",
            fontSize: 24,
            fontWeight: 700,
            margin: "0 0 24px",
          }}
        >
          系统概览
        </h1>

        <div style={{ display: "flex", gap: 16, flexWrap: "wrap" }}>
          {/* Channel count card */}
          <div
            style={{
              background: "#fff",
              borderRadius: 12,
              padding: "20px 28px",
              minWidth: 160,
              boxShadow: "0 1px 4px rgba(0,0,0,0.06)",
            }}
          >
            <div style={{ fontSize: 12, opacity: 0.55, marginBottom: 6 }}>
              渠道数量
            </div>
            <div
              style={{
                fontSize: 32,
                fontFamily: "Fraunces, serif",
                fontWeight: 700,
                color: "var(--accent, #2D5A3D)",
              }}
            >
              {summary.loading ? "…" : summary.channelCount}
            </div>
          </div>

          {/* User count card */}
          <div
            style={{
              background: "#fff",
              borderRadius: 12,
              padding: "20px 28px",
              minWidth: 160,
              boxShadow: "0 1px 4px rgba(0,0,0,0.06)",
            }}
          >
            <div style={{ fontSize: 12, opacity: 0.55, marginBottom: 6 }}>
              用户数量
            </div>
            <div
              style={{
                fontSize: 32,
                fontFamily: "Fraunces, serif",
                fontWeight: 700,
                color: "var(--accent, #2D5A3D)",
              }}
            >
              {summary.loading ? "…" : summary.userCount}
            </div>
          </div>

          {/* System status card */}
          <div
            style={{
              background: "#fff",
              borderRadius: 12,
              padding: "20px 28px",
              minWidth: 160,
              boxShadow: "0 1px 4px rgba(0,0,0,0.06)",
            }}
          >
            <div style={{ fontSize: 12, opacity: 0.55, marginBottom: 6 }}>
              系统状态
            </div>
            <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <span
                style={{
                  display: "inline-block",
                  width: 10,
                  height: 10,
                  borderRadius: "50%",
                  background: "#22c55e",
                }}
              />
              <span
                style={{
                  fontSize: 14,
                  fontWeight: 600,
                  color: "#22c55e",
                }}
              >
                正常运行
              </span>
            </div>
          </div>
        </div>

        {/* Quick nav grid */}
        <div
          style={{
            marginTop: 32,
            display: "grid",
            gridTemplateColumns: "repeat(auto-fill, minmax(180px, 1fr))",
            gap: 12,
          }}
        >
          {NAV_ITEMS.map((item) => (
            <button
              key={item.path}
              onClick={() => navigate(item.path)}
              style={{
                background: "#fff",
                border: "1px solid rgba(0,0,0,0.08)",
                borderRadius: 10,
                padding: "16px 20px",
                textAlign: "left",
                cursor: "pointer",
                fontSize: 14,
                fontWeight: 500,
                fontFamily: "Manrope, sans-serif",
                color: "var(--ink, #2B2118)",
                transition: "box-shadow 0.15s",
              }}
              onMouseEnter={(e) =>
                ((e.currentTarget as HTMLButtonElement).style.boxShadow =
                  "0 2px 8px rgba(0,0,0,0.1)")
              }
              onMouseLeave={(e) =>
                ((e.currentTarget as HTMLButtonElement).style.boxShadow = "none")
              }
            >
              {item.label} →
            </button>
          ))}
        </div>
      </main>
    </div>
  );
}

export default GtkAdmin;
