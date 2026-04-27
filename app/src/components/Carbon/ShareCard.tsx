// v0.6 carbon — generates a shareable PNG of the user's monthly carbon usage.
// Uses html-to-image (already a CoAI dep — no new install).
//
// The card is rendered off-screen (absolute, top -9999px) so it never affects
// the visible layout. Click the trigger button → render → PNG download.

import { useRef, useState } from "react";
import { toPng } from "html-to-image";
import { toast } from "sonner";
import { Download } from "lucide-react";
import type { CarbonSummary } from "@/api/carbon.ts";
import { LeafIcon } from "./icons.tsx";
import { formatCO2 } from "./tier.ts";
import { Button } from "@/components/ui/button.tsx";

type Props = {
  summary: CarbonSummary;
};

export function ShareCard({ summary }: Props) {
  const cardRef = useRef<HTMLDivElement>(null);
  const [generating, setGenerating] = useState(false);

  const handleDownload = async () => {
    if (!cardRef.current) return;
    setGenerating(true);
    try {
      const dataUrl = await toPng(cardRef.current, {
        cacheBust: true,
        pixelRatio: 2,
      });
      const link = document.createElement("a");
      link.href = dataUrl;
      link.download = `greentokey-carbon-${summary.month}.png`;
      link.click();
    } catch (err) {
      toast.error("Couldn't generate share card. Try again.");
      // eslint-disable-next-line no-console
      console.error(err);
    } finally {
      setGenerating(false);
    }
  };

  return (
    <>
      <Button
        type="button"
        variant="secondary"
        onClick={handleDownload}
        disabled={generating}
        className="gap-2"
      >
        <Download size={14} />
        {generating ? "Generating…" : "Share card"}
      </Button>

      {/* Off-screen render target — html-to-image captures from real DOM */}
      <div
        ref={cardRef}
        aria-hidden="true"
        style={{
          position: "absolute",
          left: -9999,
          top: -9999,
          width: 600,
          padding: 48,
          background: "linear-gradient(180deg, #1F3A2E 0%, #0F1A14 100%)",
          color: "#E8E5DE",
          fontFamily: "Inter, system-ui, sans-serif",
          borderRadius: 16,
        }}
      >
        {/* Header — brand */}
        <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 32 }}>
          <LeafIcon size={20} className="text-[#A8C9B0]" />
          <span style={{ fontSize: 18, fontWeight: 600, letterSpacing: "-0.01em" }}>
            greentokey
          </span>
        </div>

        {/* Hero number */}
        <div style={{ marginBottom: 8 }}>
          <div style={{ fontSize: 64, fontWeight: 600, lineHeight: 1, fontVariantNumeric: "tabular-nums" }}>
            {formatCO2(summary.total_g)}
          </div>
          <div style={{ fontSize: 14, opacity: 0.7, marginTop: 8 }}>
            CO₂e — {summary.month}
          </div>
        </div>

        {/* Top model row(s) */}
        <div style={{ marginTop: 32, marginBottom: 32 }}>
          {summary.by_model.slice(0, 3).map((m) => (
            <div
              key={m.model}
              style={{
                display: "flex",
                justifyContent: "space-between",
                fontSize: 13,
                fontFamily: "JetBrains Mono, monospace",
                padding: "6px 0",
                borderBottom: "1px solid rgba(232,229,222,0.1)",
              }}
            >
              <span>{m.model}</span>
              <span style={{ opacity: 0.7 }}>
                {formatCO2(m.g)} · {m.pct.toFixed(0)}%
              </span>
            </div>
          ))}
        </div>

        {/* Footer — disclaimer + URL */}
        <div style={{ fontSize: 11, opacity: 0.55, marginTop: 32 }}>
          Estimate ±{summary.error_margin_pct}% · Coefficient v{summary.coefficient_version}
          <br />
          greentokey.com/methodology
        </div>
      </div>
    </>
  );
}
