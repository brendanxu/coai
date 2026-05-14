// GtkModelRatios — /admin/gtk-model-ratios
//
// Editable model billing ratio table. Fetches/saves the NewAPI ModelPrice option.
// No i18n — hardcoded Chinese.

import { useEffect, useState } from "react";
import axios from "axios";
import { toast } from "sonner";
import { Button } from "@/components/ui/button.tsx";
import { Input } from "@/components/ui/input.tsx";

// $1 = 500,000 NewAPI quota units. Ratio 1 = base rate = $0.002 per 1M tokens.
const BASE_RATE_PER_M = 0.002; // USD per 1M tokens at ratio=1

const DEFAULT_RATIOS: Record<string, number> = {
  "gpt-4o": 15,
  "gpt-4o-mini": 0.075,
  "claude-3-5-sonnet-20241022": 9,
  "deepseek-chat": 0.135,
  "deepseek-reasoner": 4,
  "qwen-max": 2.4,
  "gemini-2.0-flash": 0.075,
  "moonshot-v1-8k": 1,
};

interface RatioRow {
  model: string;
  ratio: number;
}

function GtkModelRatios() {
  const [rows, setRows] = useState<RatioRow[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  // New model addition state
  const [newModel, setNewModel] = useState("");
  const [newRatio, setNewRatio] = useState("");

  useEffect(() => {
    loadRatios();
  }, []);

  async function loadRatios() {
    setLoading(true);
    try {
      const r = await axios.get<{
        success: boolean;
        data?: { ratios: Record<string, number> };
      }>("/gtk/v1/admin/model-ratios");
      if (r.data?.success && r.data.data) {
        const fetched = r.data.data.ratios ?? {};
        const merged =
          Object.keys(fetched).length === 0 ? DEFAULT_RATIOS : fetched;
        setRows(
          Object.entries(merged)
            .map(([model, ratio]) => ({ model, ratio }))
            .sort((a, b) => b.ratio - a.ratio)
        );
      }
    } catch (_e) {
      toast.error("加载模型比例失败，使用默认值");
      setRows(
        Object.entries(DEFAULT_RATIOS)
          .map(([model, ratio]) => ({ model, ratio }))
          .sort((a, b) => b.ratio - a.ratio)
      );
    } finally {
      setLoading(false);
    }
  }

  const handleRatioChange = (index: number, value: string) => {
    setRows((prev) =>
      prev.map((r, i) =>
        i === index ? { ...r, ratio: parseFloat(value) || 0 } : r
      )
    );
  };

  const handleAddModel = () => {
    const model = newModel.trim();
    if (!model) {
      toast.error("模型名称不能为空");
      return;
    }
    const ratio = parseFloat(newRatio);
    if (isNaN(ratio) || ratio < 0) {
      toast.error("Ratio 必须是非负数");
      return;
    }
    if (rows.some((r) => r.model === model)) {
      toast.error(`模型 ${model} 已存在`);
      return;
    }
    setRows((prev) => [...prev, { model, ratio }]);
    setNewModel("");
    setNewRatio("");
  };

  const handleDeleteRow = (index: number) => {
    setRows((prev) => prev.filter((_, i) => i !== index));
  };

  const handleSaveAll = async () => {
    const ratios: Record<string, number> = {};
    for (const row of rows) {
      if (row.model.trim()) {
        ratios[row.model.trim()] = row.ratio;
      }
    }
    setSaving(true);
    try {
      await axios.put("/gtk/v1/admin/model-ratios", { ratios });
      toast.success("模型计费比例已保存");
    } catch (_e) {
      toast.error("保存失败");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div
      style={{
        fontFamily: "Manrope, sans-serif",
        padding: "24px 16px",
        color: "var(--ink, #2B2118)",
      }}
    >
      <div
        style={{
          display: "flex",
          alignItems: "center",
          justifyContent: "space-between",
          marginBottom: 20,
          flexWrap: "wrap",
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
          模型计费比例
        </h1>
        <div style={{ display: "flex", gap: 10 }}>
          <Button variant="outline" onClick={loadRatios} disabled={loading}>
            {loading ? "加载中…" : "刷新"}
          </Button>
          <Button
            onClick={handleSaveAll}
            disabled={saving}
            style={{ background: "var(--accent, #2D5A3D)", color: "#fff" }}
          >
            {saving ? "保存中…" : "保存全部"}
          </Button>
        </div>
      </div>

      {/* Table */}
      <div style={{ overflowX: "auto", marginBottom: 16 }}>
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
              <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>
                模型名称
              </th>
              <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>
                Ratio 值
              </th>
              <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>
                等价 ¥/1M tokens
              </th>
              <th style={{ padding: "8px 10px", textAlign: "left", fontWeight: 600 }}>
                操作
              </th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && !loading && (
              <tr>
                <td
                  colSpan={4}
                  style={{ textAlign: "center", padding: "32px 0", opacity: 0.4 }}
                >
                  暂无数据
                </td>
              </tr>
            )}
            {rows.map((row, i) => {
              // Cost in USD per 1M tokens = ratio * BASE_RATE_PER_M
              // Approx CNY: * 7.2
              const cnyPerM = row.ratio * BASE_RATE_PER_M * 7.2;
              return (
                <tr
                  key={row.model}
                  style={{ borderBottom: "1px solid rgba(0,0,0,0.06)" }}
                >
                  <td
                    style={{
                      padding: "8px 10px",
                      fontFamily: "JetBrains Mono, monospace",
                      fontSize: 12,
                    }}
                  >
                    {row.model}
                  </td>
                  <td style={{ padding: "8px 10px" }}>
                    <Input
                      type="number"
                      value={row.ratio}
                      min={0}
                      step={0.001}
                      onChange={(e) => handleRatioChange(i, e.target.value)}
                      style={{ width: 110, height: 30, fontSize: 13 }}
                    />
                  </td>
                  <td
                    style={{
                      padding: "8px 10px",
                      fontFamily: "JetBrains Mono, monospace",
                      fontSize: 12,
                      opacity: 0.7,
                    }}
                  >
                    ¥{cnyPerM.toFixed(4)}
                  </td>
                  <td style={{ padding: "8px 10px" }}>
                    <Button
                      size="sm"
                      variant="outline"
                      onClick={() => handleDeleteRow(i)}
                      style={{ fontSize: 12, color: "#dc2626", borderColor: "#dc2626" }}
                    >
                      删除
                    </Button>
                  </td>
                </tr>
              );
            })}

            {/* Add new row */}
            <tr style={{ borderTop: "2px solid rgba(0,0,0,0.08)" }}>
              <td style={{ padding: "8px 10px" }}>
                <Input
                  value={newModel}
                  onChange={(e) => setNewModel(e.target.value)}
                  placeholder="gpt-4o-new"
                  style={{ height: 30, fontSize: 12 }}
                />
              </td>
              <td style={{ padding: "8px 10px" }}>
                <Input
                  type="number"
                  value={newRatio}
                  onChange={(e) => setNewRatio(e.target.value)}
                  placeholder="15"
                  min={0}
                  step={0.001}
                  style={{ width: 110, height: 30, fontSize: 13 }}
                />
              </td>
              <td style={{ padding: "8px 10px", opacity: 0.4, fontSize: 12 }}>
                —
              </td>
              <td style={{ padding: "8px 10px" }}>
                <Button
                  size="sm"
                  variant="outline"
                  onClick={handleAddModel}
                  style={{ fontSize: 12 }}
                >
                  + 添加模型
                </Button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>

      {/* Info strip */}
      <div
        style={{
          fontSize: 12,
          opacity: 0.6,
          background: "rgba(0,0,0,0.04)",
          borderRadius: 8,
          padding: "10px 14px",
          lineHeight: 1.6,
        }}
      >
        <strong>Ratio 说明：</strong>NewAPI 基准 1 credit = $0.002。Ratio=15
        意为该模型按 15x 基准计费，即 $0.03/1M tokens ≈ ¥0.216/1M tokens（汇率
        7.2 估算）。
      </div>
    </div>
  );
}

export default GtkModelRatios;
