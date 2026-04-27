// v0.6 carbon — /methodology route. Long-form transparency essay.
// Reference: Cal.com transparency, Plausible blog, Patagonia footprint.
// Founder-signed (literally — Brendan writes the trust-critical copy).
// "What we don't measure" section is the anti-greenwashing moat.

import { useEffect } from "react";
import { Link } from "react-router-dom";
import { useDispatch, useSelector } from "react-redux";
import {
  selectCarbonFactors,
  setFactors,
  setFactorsLoading,
} from "@/store/carbon.ts";
import { getCarbonFactors } from "@/api/carbon.ts";
import { MethodologyTable } from "@/components/Carbon/MethodologyTable.tsx";
import { Callout } from "@/components/Carbon/Callout.tsx";
import type { AppDispatch } from "@/store/index.ts";

export default function Methodology() {
  const dispatch = useDispatch<AppDispatch>();
  const factors = useSelector(selectCarbonFactors);

  useEffect(() => {
    if (factors) return;
    let cancelled = false;
    dispatch(setFactorsLoading(true));
    getCarbonFactors()
      .then((f) => {
        if (!cancelled) dispatch(setFactors(f));
      })
      .catch(() => {
        if (!cancelled) dispatch(setFactorsLoading(false));
      });
    return () => {
      cancelled = true;
    };
  }, [dispatch, factors]);

  return (
    <article className="mx-auto max-w-3xl px-6 py-16 leading-relaxed">
      {/* Breadcrumb */}
      <div className="text-sm text-muted-foreground mb-8">
        <Link to="/" className="hover:underline">greentokey</Link>
        <span className="mx-1">›</span>
        <Link to="/dashboard" className="hover:underline">carbon</Link>
        <span className="mx-1">›</span>
        <span>methodology</span>
      </div>

      {/* Headline */}
      <h1 className="font-display text-5xl md:text-6xl leading-tight tracking-tight mb-6">
        How we estimate carbon footprint
      </h1>

      <p className="text-lg text-muted-foreground mb-12">
        Last updated April 2026. Estimates carry a ±{factors?.error_margin_pct ?? 20}%
        margin — we&apos;d rather be honest than precise.
      </p>

      {/* Disclaimer callout — early + prominent */}
      <Callout variant="warning">
        <strong>This is an estimate, not a measurement.</strong> Real per-request
        compute varies with batch size, GPU, and grid mix. We use per-model
        midpoints and disclose the ±20% margin everywhere we show a number. If you
        need audited measurements, this isn&apos;t the tool — yet.
      </Callout>

      {/* The formula */}
      <h2 className="font-display text-3xl mt-16 mb-4">The formula</h2>
      <pre className="bg-muted rounded-md p-4 font-mono text-sm overflow-x-auto">
{`CO₂g = tokens × coefficient(model, region) / 1000`}
      </pre>
      <p className="mt-4">
        Tokens come straight from the upstream provider&apos;s response (the same
        number your billing reflects). Coefficients are per-model, per-region
        midpoints sourced from public LLM-carbon studies. Region defaults to a
        global weighted average (~430 gCO₂e/kWh) when the upstream API
        doesn&apos;t expose data center location.
      </p>

      {/* Coefficients */}
      <h2 className="font-display text-3xl mt-16 mb-4">Coefficients</h2>
      <p className="mb-6">
        Every model we route is in this table. If a model is missing, the badge
        shows &quot;~?g&quot; rather than guessing.
      </p>
      <MethodologyTable factors={factors?.factors ?? []} />
      {factors?.calibration_note && (
        <p className="text-sm text-muted-foreground italic mt-4">
          {factors.calibration_note}
        </p>
      )}

      {/* What we don't measure — the moat */}
      <h2 className="font-display text-3xl mt-16 mb-4">What we don&apos;t measure</h2>
      <p className="mb-4">
        Honest software discloses its blind spots. Here are ours:
      </p>
      <ul className="list-disc list-inside space-y-2 marker:text-[hsl(var(--secondary))]">
        {(factors?.what_we_dont_measure ?? []).map((bullet, i) => (
          <li key={i}>{bullet}</li>
        ))}
      </ul>

      {/* Sources */}
      <h2 className="font-display text-3xl mt-16 mb-4">Sources</h2>
      <ul className="space-y-3">
        {(factors?.sources ?? []).map((s, i) => (
          <li key={i}>
            <a
              href={s.url}
              target="_blank"
              rel="noopener noreferrer"
              className="underline hover:no-underline font-medium"
            >
              {s.name}
            </a>
            {s.note && (
              <span className="text-sm text-muted-foreground"> — {s.note}</span>
            )}
          </li>
        ))}
      </ul>

      {/* Eco Mode */}
      <h2 className="font-display text-3xl mt-16 mb-4">Eco Mode</h2>
      <p>
        When you turn on the leaf icon next to your message input, future chats
        route to a smaller variant of the same model family (GPT-4 → GPT-4o-mini,
        Claude Sonnet → Haiku, etc.) when quality difference is acceptable. The
        substitution happens server-side, so your billing also reflects the
        cheaper model — not just the carbon. Long-press the toggle to skip Eco
        for one chat.
      </p>
      <p className="mt-4">
        Routing rules and expected savings live in{" "}
        <code className="font-mono text-sm">data/eco_routing.json</code>, the same
        source the middleware reads. The savings claim ({" "}
        <span className="font-mono text-sm">expected_savings_pct</span>) is computed
        from this coefficient table — there&apos;s a unit test that asserts they stay
        in sync.
      </p>

      {/* Footer signature */}
      <div className="mt-20 pt-6 border-t border-border italic text-muted-foreground">
        — Brendan, founder of greentokey. Last reviewed April 2026.
      </div>
    </article>
  );
}
