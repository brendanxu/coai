// GtkSettings — /admin/gtk-settings
//
// System settings page. Fetches all NewAPI options and groups them into sections.
// No i18n — hardcoded Chinese.

import { useEffect, useState } from "react";
import axios from "axios";
import { toast } from "sonner";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";

interface Option {
  key: string;
  value: string;
}

// Keys grouped into named sections for display.
const SECTION_AUTH = ["PasswordLoginEnabled", "RegisterEnabled", "EmailVerificationEnabled"];
const SECTION_RATE = ["GlobalApiRateLimitNum", "GlobalApiRateLimitDuration"];

// Bool-style options that should render as toggle chips.
const BOOL_OPTIONS = new Set([
  "PasswordLoginEnabled",
  "RegisterEnabled",
  "EmailVerificationEnabled",
  "EmailDomainWhitelistEnabled",
]);

function isBoolOption(key: string): boolean {
  return BOOL_OPTIONS.has(key);
}

function BoolToggle({
  value,
  onChange,
}: {
  value: string;
  onChange: (v: string) => void;
}) {
  const isTrue = value === "true";
  return (
    <div style={{ display: "flex", gap: 0 }}>
      {(["true", "false"] as const).map((v) => (
        <button
          key={v}
          onClick={() => onChange(v)}
          style={{
            padding: "5px 14px",
            border: "1px solid rgba(0,0,0,0.12)",
            borderRight: v === "true" ? "none" : "1px solid rgba(0,0,0,0.12)",
            borderRadius: v === "true" ? "6px 0 0 6px" : "0 6px 6px 0",
            background:
              (v === "true" && isTrue) || (v === "false" && !isTrue)
                ? "var(--accent, #2D5A3D)"
                : "transparent",
            color:
              (v === "true" && isTrue) || (v === "false" && !isTrue)
                ? "#fff"
                : "var(--ink, #2B2118)",
            fontSize: 13,
            cursor: "pointer",
            fontFamily: "Manrope, sans-serif",
          }}
        >
          {v === "true" ? "开启" : "关闭"}
        </button>
      ))}
    </div>
  );
}

interface SectionProps {
  title: string;
  keys: string[];
  options: Option[];
  localValues: Record<string, string>;
  saving: Record<string, boolean>;
  onValueChange: (key: string, value: string) => void;
  onSave: (key: string) => void;
}

function SettingsSection({
  title,
  keys,
  options,
  localValues,
  saving,
  onValueChange,
  onSave,
}: SectionProps) {
  const sectionOptions = options.filter((o) => keys.includes(o.key));

  if (sectionOptions.length === 0) return null;

  return (
    <div
      style={{
        background: "#fff",
        borderRadius: 10,
        padding: 20,
        marginBottom: 16,
        boxShadow: "0 1px 4px rgba(0,0,0,0.05)",
      }}
    >
      <h2
        style={{
          fontFamily: "Fraunces, serif",
          fontSize: 16,
          fontWeight: 700,
          margin: "0 0 16px",
        }}
      >
        {title}
      </h2>
      <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
        {sectionOptions.map((opt) => {
          const current = localValues[opt.key] ?? opt.value;
          return (
            <div
              key={opt.key}
              style={{ display: "flex", alignItems: "center", gap: 14, flexWrap: "wrap" }}
            >
              <label
                style={{
                  fontSize: 13,
                  fontFamily: "JetBrains Mono, monospace",
                  minWidth: 240,
                  opacity: 0.8,
                }}
              >
                {opt.key}
              </label>
              {isBoolOption(opt.key) ? (
                <BoolToggle
                  value={current}
                  onChange={(v) => onValueChange(opt.key, v)}
                />
              ) : (
                <Input
                  value={current}
                  onChange={(e) => onValueChange(opt.key, e.target.value)}
                  style={{ maxWidth: 200, height: 30, fontSize: 13 }}
                />
              )}
              <Button
                size="sm"
                disabled={saving[opt.key]}
                onClick={() => onSave(opt.key)}
                style={{
                  fontSize: 12,
                  background: "var(--accent, #2D5A3D)",
                  color: "#fff",
                }}
              >
                {saving[opt.key] ? "…" : "保存"}
              </Button>
            </div>
          );
        })}
      </div>
    </div>
  );
}

function GtkSettings() {
  const [options, setOptions] = useState<Option[]>([]);
  const [loading, setLoading] = useState(false);
  const [localValues, setLocalValues] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<Record<string, boolean>>({});

  useEffect(() => {
    loadOptions();
  }, []);

  async function loadOptions() {
    setLoading(true);
    try {
      const r = await axios.get<{
        success: boolean;
        data?: { options: Option[] };
      }>("/gtk/v1/admin/system-options");
      if (r.data?.success && r.data.data) {
        setOptions(r.data.data.options ?? []);
        // Pre-populate local values
        const vals: Record<string, string> = {};
        for (const opt of r.data.data.options ?? []) {
          vals[opt.key] = opt.value;
        }
        setLocalValues(vals);
      }
    } catch (_e) {
      toast.error("加载系统设置失败");
    } finally {
      setLoading(false);
    }
  }

  const handleValueChange = (key: string, value: string) => {
    setLocalValues((prev) => ({ ...prev, [key]: value }));
  };

  const handleSave = async (key: string) => {
    const value = localValues[key] ?? "";
    setSaving((s) => ({ ...s, [key]: true }));
    try {
      await axios.put("/gtk/v1/admin/system-options", { key, value });
      toast.success(`${key} 已保存`);
      // Update the options array to reflect saved value.
      setOptions((prev) =>
        prev.map((o) => (o.key === key ? { ...o, value } : o))
      );
    } catch (_e) {
      toast.error(`保存 ${key} 失败`);
    } finally {
      setSaving((s) => ({ ...s, [key]: false }));
    }
  };

  // "Other" = options not in any named section
  const knownKeys = new Set([...SECTION_AUTH, ...SECTION_RATE]);
  const otherOptions = options.filter((o) => !knownKeys.has(o.key));

  return (
    <div
      style={{
        fontFamily: "Manrope, sans-serif",
        padding: "24px 16px",
        color: "var(--ink, #2B2118)",
        maxWidth: 800,
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 20,
          gap: 12,
        }}
      >
        <h1
          style={{
            fontFamily: "Fraunces, serif",
            fontSize: 22,
            fontWeight: 700,
            margin: 0,
          }}
        >
          系统设置
        </h1>
        <Button variant="outline" onClick={loadOptions} disabled={loading}>
          {loading ? "加载中…" : "刷新"}
        </Button>
      </div>

      {loading && (
        <div style={{ opacity: 0.5, marginBottom: 16 }}>加载中…</div>
      )}

      <SettingsSection
        title="注册与登录"
        keys={SECTION_AUTH}
        options={options}
        localValues={localValues}
        saving={saving}
        onValueChange={handleValueChange}
        onSave={handleSave}
      />

      <SettingsSection
        title="限速"
        keys={SECTION_RATE}
        options={options}
        localValues={localValues}
        saving={saving}
        onValueChange={handleValueChange}
        onSave={handleSave}
      />

      {/* Other options */}
      {otherOptions.length > 0 && (
        <div
          style={{
            background: "#fff",
            borderRadius: 10,
            padding: 20,
            boxShadow: "0 1px 4px rgba(0,0,0,0.05)",
          }}
        >
          <h2
            style={{
              fontFamily: "Fraunces, serif",
              fontSize: 16,
              fontWeight: 700,
              margin: "0 0 16px",
            }}
          >
            其他设置
          </h2>
          <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
            {otherOptions.map((opt) => {
              const current = localValues[opt.key] ?? opt.value;
              return (
                <div
                  key={opt.key}
                  style={{
                    display: "flex",
                    alignItems: "center",
                    gap: 14,
                    flexWrap: "wrap",
                  }}
                >
                  <label
                    style={{
                      fontSize: 12,
                      fontFamily: "JetBrains Mono, monospace",
                      minWidth: 240,
                      opacity: 0.7,
                    }}
                  >
                    {opt.key}
                  </label>
                  <Input
                    value={current}
                    onChange={(e) => handleValueChange(opt.key, e.target.value)}
                    style={{ maxWidth: 220, height: 28, fontSize: 12 }}
                  />
                  <Button
                    size="sm"
                    disabled={saving[opt.key]}
                    onClick={() => handleSave(opt.key)}
                    style={{
                      fontSize: 12,
                      background: "var(--accent, #2D5A3D)",
                      color: "#fff",
                    }}
                  >
                    {saving[opt.key] ? "…" : "保存"}
                  </Button>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </div>
  );
}

export default GtkSettings;
