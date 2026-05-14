// AdminChannels — /admin/channels-routing
//
// Full channel management UI: list, add, edit, delete, test.
// Uses the Stream A admin API endpoints (already live in prod).
//
// No i18n — admin is founder-only, Chinese hardcoded is fine.

import { useEffect, useState, useCallback } from "react";
import axios from "axios";
import { toast } from "sonner";
import {
  Card,
  CardContent,
  CardHeader,
  CardTitle,
} from "@/components/ui/card.tsx";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";
import { Label } from "@/components/ui/label.tsx";

// Channel shape from backend
interface Channel {
  id: number;
  type: number;
  name: string;
  key: string;
  base_url: string;
  models: string[];
  group: string;
  model_mapping: string;
  model_ratio: string;
  priority: number;
  weight: number;
  status: number;
  used_quota: number;
  balance: number;
  response_time: number;
  test_time: number;
}

const CHANNEL_TYPES: { value: number; label: string }[] = [
  { value: 1, label: "OpenAI (1)" },
  { value: 14, label: "Anthropic (14)" },
  { value: 24, label: "Gemini (24)" },
  { value: 36, label: "SunoAPI (36)" },
  { value: 43, label: "DeepSeek (43)" },
  { value: 45, label: "Sub2API (45)" },
];

interface ChannelFormState {
  type: number;
  name: string;
  key: string;
  base_url: string;
  models: string;
  group: string;
}

const defaultForm: ChannelFormState = {
  type: 1,
  name: "",
  key: "",
  base_url: "",
  models: "",
  group: "",
};

type StatusFilter = "all" | "enabled" | "disabled";

function StatusDot({ status }: { status: number }) {
  const color = status === 1 ? "#2D5A3D" : status === 0 ? "#dc2626" : "#9ca3af";
  return (
    <span
      style={{
        display: "inline-block",
        width: 8,
        height: 8,
        borderRadius: "50%",
        background: color,
        marginRight: 6,
        flexShrink: 0,
      }}
    />
  );
}

function ModelList({ models }: { models: string[] }) {
  const visible = models.slice(0, 3);
  const extra = models.length - 3;
  return (
    <div style={{ display: "flex", flexWrap: "wrap", gap: 4 }}>
      {visible.map((m) => (
        <span
          key={m}
          style={{
            fontSize: 11,
            background: "rgba(0,0,0,0.06)",
            borderRadius: 4,
            padding: "1px 6px",
            fontFamily: "JetBrains Mono, monospace",
          }}
        >
          {m}
        </span>
      ))}
      {extra > 0 && (
        <span style={{ fontSize: 11, opacity: 0.55 }}>+{extra} 更多</span>
      )}
    </div>
  );
}

function ChannelModal({
  initial,
  onClose,
  onSave,
}: {
  initial?: Channel;
  onClose: () => void;
  onSave: (form: ChannelFormState) => Promise<void>;
}) {
  const [form, setForm] = useState<ChannelFormState>(
    initial
      ? {
          type: initial.type,
          name: initial.name,
          key: initial.key,
          base_url: initial.base_url ?? "",
          models: (initial.models ?? []).join(", "),
          group: initial.group ?? "",
        }
      : { ...defaultForm }
  );
  const [showKey, setShowKey] = useState(false);
  const [saving, setSaving] = useState(false);

  const handleSave = async () => {
    if (!form.name.trim()) {
      toast.error("渠道名称不能为空");
      return;
    }
    if (!form.key.trim()) {
      toast.error("API Key 不能为空");
      return;
    }
    setSaving(true);
    try {
      await onSave(form);
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      style={{
        position: "fixed",
        inset: 0,
        background: "rgba(0,0,0,0.45)",
        zIndex: 1000,
        display: "flex",
        alignItems: "center",
        justifyContent: "center",
        padding: 16,
      }}
      onClick={(e) => {
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div
        style={{
          background: "var(--bg, #F4EFE5)",
          borderRadius: 12,
          padding: 28,
          width: "100%",
          maxWidth: 480,
          maxHeight: "90vh",
          overflowY: "auto",
          display: "flex",
          flexDirection: "column",
          gap: 16,
        }}
      >
        <h2
          style={{
            fontFamily: "Fraunces, serif",
            fontSize: 20,
            fontWeight: 700,
            margin: 0,
          }}
        >
          {initial ? "编辑渠道" : "添加渠道"}
        </h2>

        <div>
          <Label htmlFor="ch-type" style={{ display: "block", marginBottom: 4 }}>
            类型
          </Label>
          <select
            id="ch-type"
            value={form.type}
            onChange={(e) => setForm((f) => ({ ...f, type: Number(e.target.value) }))}
            style={{
              width: "100%",
              padding: "8px 10px",
              borderRadius: 6,
              border: "1px solid var(--border, #e2e8f0)",
              background: "transparent",
              fontSize: 14,
              fontFamily: "Manrope, sans-serif",
              color: "var(--ink, #2B2118)",
            }}
          >
            {CHANNEL_TYPES.map((ct) => (
              <option key={ct.value} value={ct.value}>
                {ct.label}
              </option>
            ))}
          </select>
        </div>

        <div>
          <Label htmlFor="ch-name" style={{ display: "block", marginBottom: 4 }}>
            名称
          </Label>
          <Input
            id="ch-name"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            placeholder="渠道名称"
          />
        </div>

        {form.type !== 45 && (
          <div>
            <Label htmlFor="ch-key" style={{ display: "block", marginBottom: 4 }}>
              API Key
            </Label>
            <div style={{ display: "flex", gap: 8 }}>
              <Input
                id="ch-key"
                type={showKey ? "text" : "password"}
                value={form.key}
                onChange={(e) => setForm((f) => ({ ...f, key: e.target.value }))}
                placeholder="sk-..."
                style={{ flex: 1 }}
              />
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowKey((s) => !s)}
                style={{ flexShrink: 0 }}
              >
                {showKey ? "隐藏" : "显示"}
              </Button>
            </div>
          </div>
        )}

        {form.type === 45 ? (
          <>
            <div>
              <Label htmlFor="ch-base-url" style={{ display: "block", marginBottom: 4 }}>
                Sub2API Base URL
              </Label>
              <Input
                id="ch-base-url"
                value={form.base_url}
                onChange={(e) => setForm((f) => ({ ...f, base_url: e.target.value }))}
                placeholder="https://api.sub2api.com"
              />
            </div>
            <div>
              <Label htmlFor="ch-key-sub2api" style={{ display: "block", marginBottom: 4 }}>
                Sub2API Token
              </Label>
              <div style={{ display: "flex", gap: 8 }}>
                <Input
                  id="ch-key-sub2api"
                  type={showKey ? "text" : "password"}
                  value={form.key}
                  onChange={(e) => setForm((f) => ({ ...f, key: e.target.value }))}
                  placeholder="Sub2API token"
                  style={{ flex: 1 }}
                />
                <Button
                  variant="outline"
                  size="sm"
                  onClick={() => setShowKey((s) => !s)}
                  style={{ flexShrink: 0 }}
                >
                  {showKey ? "隐藏" : "显示"}
                </Button>
              </div>
            </div>
          </>
        ) : (
          <div>
            <Label htmlFor="ch-base-url" style={{ display: "block", marginBottom: 4 }}>
              Base URL (可选)
            </Label>
            <Input
              id="ch-base-url"
              value={form.base_url}
              onChange={(e) => setForm((f) => ({ ...f, base_url: e.target.value }))}
              placeholder="https://api.example.com"
            />
          </div>
        )}

        <div>
          <Label htmlFor="ch-models" style={{ display: "block", marginBottom: 4 }}>
            模型列表 (逗号分隔)
          </Label>
          <textarea
            id="ch-models"
            value={form.models}
            onChange={(e) => setForm((f) => ({ ...f, models: e.target.value }))}
            placeholder="gpt-4o, gpt-4o-mini, claude-3-5-sonnet"
            rows={3}
            style={{
              width: "100%",
              padding: "8px 10px",
              borderRadius: 6,
              border: "1px solid var(--border, #e2e8f0)",
              background: "transparent",
              fontSize: 13,
              fontFamily: "JetBrains Mono, monospace",
              resize: "vertical",
              outline: "none",
              color: "var(--ink, #2B2118)",
            }}
          />
        </div>

        <div>
          <Label htmlFor="ch-group" style={{ display: "block", marginBottom: 4 }}>
            分组 (可选)
          </Label>
          <Input
            id="ch-group"
            value={form.group}
            onChange={(e) => setForm((f) => ({ ...f, group: e.target.value }))}
            placeholder="default"
          />
        </div>

        <div style={{ display: "flex", gap: 10, justifyContent: "flex-end" }}>
          <Button variant="outline" onClick={onClose} disabled={saving}>
            取消
          </Button>
          <Button
            onClick={handleSave}
            disabled={saving}
            style={{ background: "var(--accent, #2D5A3D)", color: "#fff" }}
          >
            {saving ? "保存中…" : "保存"}
          </Button>
        </div>
      </div>
    </div>
  );
}

function AdminChannels() {
  const [channels, setChannels] = useState<Channel[]>([]);
  const [loading, setLoading] = useState(false);
  const [typeFilter, setTypeFilter] = useState<number | "all">("all");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");
  const [modalOpen, setModalOpen] = useState(false);
  const [editTarget, setEditTarget] = useState<Channel | undefined>(undefined);
  const [testResults, setTestResults] = useState<Record<number, string>>({});

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const r = await axios.get<{ success: boolean; data?: { channels: Channel[] } }>(
        "/gtk/v1/admin/channels"
      );
      if (r.data?.success && Array.isArray(r.data.data?.channels)) {
        setChannels(r.data.data!.channels);
      }
    } catch (_e) {
      toast.error("加载渠道列表失败");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const handleAdd = () => {
    setEditTarget(undefined);
    setModalOpen(true);
  };

  const handleEdit = (ch: Channel) => {
    setEditTarget(ch);
    setModalOpen(true);
  };

  const handleDelete = async (id: number, name: string) => {
    if (!window.confirm(`确认删除渠道「${name}」？此操作不可撤销。`)) return;
    try {
      await axios.delete(`/gtk/v1/admin/channels/${id}`);
      toast.success(`渠道「${name}」已删除`);
      refresh();
    } catch (_e) {
      toast.error("删除失败");
    }
  };

  const handleTest = async (id: number) => {
    setTestResults((prev) => ({ ...prev, [id]: "测试中…" }));
    try {
      const r = await axios.post<{ success: boolean; data?: { response_time?: number } }>(
        `/gtk/v1/admin/channels/${id}/test`
      );
      if (r.data?.success) {
        const ms = r.data.data?.response_time;
        setTestResults((prev) => ({
          ...prev,
          [id]: ms != null ? `✓ ${ms}ms` : "✓ OK",
        }));
      } else {
        setTestResults((prev) => ({ ...prev, [id]: "✗ 失败" }));
      }
    } catch (_e) {
      setTestResults((prev) => ({ ...prev, [id]: "✗ 错误" }));
    }
  };

  const handleSave = async (form: ChannelFormState) => {
    const payload = {
      type: form.type,
      name: form.name,
      key: form.key,
      base_url: form.base_url || "",
      models: form.models
        .split(",")
        .map((m) => m.trim())
        .filter(Boolean),
      group: form.group || "default",
    };

    try {
      if (editTarget) {
        await axios.put(`/gtk/v1/admin/channels/${editTarget.id}`, payload);
        toast.success(`渠道「${form.name}」已更新`);
      } else {
        await axios.post("/gtk/v1/admin/channels", payload);
        toast.success(`渠道「${form.name}」已创建`);
      }
      setModalOpen(false);
      refresh();
    } catch (_e) {
      toast.error(editTarget ? "更新失败" : "创建失败");
      throw _e; // re-throw so modal can stop spinner
    }
  };

  // Filtered channels
  const filtered = channels.filter((ch) => {
    if (typeFilter !== "all" && ch.type !== typeFilter) return false;
    if (statusFilter === "enabled" && ch.status !== 1) return false;
    if (statusFilter === "disabled" && ch.status !== 0) return false;
    return true;
  });

  const usedTypes = Array.from(new Set(channels.map((c) => c.type)));

  return (
    <div style={{ fontFamily: "Manrope, sans-serif", padding: "24px 16px" }}>
      <Card>
        <CardHeader>
          <CardTitle style={{ display: "flex", alignItems: "center", justifyContent: "space-between" }}>
            <span style={{ fontFamily: "Fraunces, serif" }}>渠道管理</span>
            <Button
              onClick={handleAdd}
              style={{ background: "var(--accent, #2D5A3D)", color: "#fff", fontSize: 13 }}
            >
              + 添加渠道
            </Button>
          </CardTitle>
        </CardHeader>
        <CardContent>
          {/* Filter row */}
          <div style={{ display: "flex", gap: 10, marginBottom: 16, flexWrap: "wrap", alignItems: "center" }}>
            <select
              value={typeFilter}
              onChange={(e) => setTypeFilter(e.target.value === "all" ? "all" : Number(e.target.value))}
              style={{
                padding: "5px 10px",
                borderRadius: 6,
                border: "1px solid var(--border, #e2e8f0)",
                background: "transparent",
                fontSize: 13,
                fontFamily: "Manrope, sans-serif",
                color: "var(--ink, #2B2118)",
              }}
            >
              <option value="all">全部类型</option>
              {usedTypes.map((t) => {
                const label = CHANNEL_TYPES.find((ct) => ct.value === t)?.label ?? `类型 ${t}`;
                return (
                  <option key={t} value={t}>
                    {label}
                  </option>
                );
              })}
            </select>

            <div style={{ display: "flex", border: "1px solid var(--border, #e2e8f0)", borderRadius: 6, overflow: "hidden" }}>
              {(["all", "enabled", "disabled"] as StatusFilter[]).map((s, i, arr) => (
                <button
                  key={s}
                  onClick={() => setStatusFilter(s)}
                  style={{
                    padding: "5px 12px",
                    fontSize: 13,
                    border: "none",
                    borderRight: i < arr.length - 1 ? "1px solid var(--border, #e2e8f0)" : "none",
                    background: statusFilter === s ? "var(--accent, #2D5A3D)" : "transparent",
                    color: statusFilter === s ? "#fff" : "var(--ink, #2B2118)",
                    cursor: "pointer",
                    fontFamily: "Manrope, sans-serif",
                  }}
                >
                  {s === "all" ? "全部" : s === "enabled" ? "启用" : "禁用"}
                </button>
              ))}
            </div>

            {loading && <span style={{ fontSize: 13, opacity: 0.5 }}>加载中…</span>}
          </div>

          {/* Table */}
          <div style={{ overflowX: "auto" }}>
            <table style={{ width: "100%", borderCollapse: "collapse", fontSize: 13 }}>
              <thead>
                <tr style={{ borderBottom: "1px solid var(--border, #e2e8f0)", background: "rgba(0,0,0,0.02)" }}>
                  <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>#</th>
                  <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>名称</th>
                  <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>类型</th>
                  <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>状态</th>
                  <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>模型</th>
                  <th style={{ padding: "8px 10px", textAlign: "right", fontWeight: 600 }}>余额</th>
                  <th style={{ padding: "8px 10px", textAlign: "right", fontWeight: 600 }}>用量</th>
                  <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>操作</th>
                </tr>
              </thead>
              <tbody>
                {filtered.length === 0 && !loading && (
                  <tr>
                    <td colSpan={8} style={{ textAlign: "center", padding: "32px 0", opacity: 0.45 }}>
                      暂无渠道
                    </td>
                  </tr>
                )}
                {filtered.map((ch) => {
                  const typeName = CHANNEL_TYPES.find((ct) => ct.value === ch.type)?.label ?? `type ${ch.type}`;
                  return (
                    <tr key={ch.id} style={{ borderBottom: "1px solid var(--border, #e2e8f0)", verticalAlign: "middle" }}>
                      <td style={{ padding: "8px 10px", opacity: 0.5 }}>{ch.id}</td>
                      <td style={{ padding: "8px 10px", fontWeight: 500 }}>{ch.name}</td>
                      <td style={{ padding: "8px 10px" }}>
                        <span style={{ fontSize: 11, background: "rgba(0,0,0,0.06)", borderRadius: 4, padding: "2px 6px" }}>
                          {typeName}
                        </span>
                      </td>
                      <td style={{ padding: "8px 10px" }}>
                        <div style={{ display: "flex", alignItems: "center" }}>
                          <StatusDot status={ch.status} />
                          <span style={{ fontSize: 12 }}>
                            {ch.status === 1 ? "启用" : ch.status === 0 ? "禁用" : "未知"}
                          </span>
                        </div>
                      </td>
                      <td style={{ padding: "8px 10px" }}>
                        <ModelList models={ch.models ?? []} />
                      </td>
                      <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace", fontSize: 12 }}>
                        {ch.balance?.toFixed(2) ?? "—"}
                      </td>
                      <td style={{ padding: "8px 10px", textAlign: "right", fontFamily: "JetBrains Mono, monospace", fontSize: 12 }}>
                        {ch.used_quota?.toLocaleString() ?? "—"}
                      </td>
                      <td style={{ padding: "8px 10px" }}>
                        <div style={{ display: "flex", gap: 6, alignItems: "center", flexWrap: "wrap" }}>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleTest(ch.id)}
                            style={{ fontSize: 12 }}
                          >
                            测试
                          </Button>
                          {testResults[ch.id] && (
                            <span
                              style={{
                                fontSize: 12,
                                color: testResults[ch.id].startsWith("✓")
                                  ? "var(--accent, #2D5A3D)"
                                  : "#dc2626",
                              }}
                            >
                              {testResults[ch.id]}
                            </span>
                          )}
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleEdit(ch)}
                            style={{ fontSize: 12 }}
                          >
                            编辑
                          </Button>
                          <Button
                            size="sm"
                            variant="outline"
                            onClick={() => handleDelete(ch.id, ch.name)}
                            style={{ fontSize: 12, color: "#dc2626", borderColor: "#dc2626" }}
                          >
                            删除
                          </Button>
                        </div>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </CardContent>
      </Card>

      {modalOpen && (
        <ChannelModal
          initial={editTarget}
          onClose={() => setModalOpen(false)}
          onSave={handleSave}
        />
      )}
    </div>
  );
}

export default AdminChannels;
