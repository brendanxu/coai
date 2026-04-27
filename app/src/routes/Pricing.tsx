/**
 * /pricing — public pricing page. Single-tier MVP ($15/mo Starter).
 *
 * Layout (matches Welcome.tsx idiom):
 *   1. Hero — headline + sub + tagline strip
 *   2. Pricing card — features list + UpgradeCTA dispatch
 *   3. Three value props (Wallet / Privacy / Planet)
 *   4. FAQ (4 questions, plain-language answers)
 *   5. Trust strip — payment processor + cancel anytime
 *
 * Mounting: registered in router.tsx as "/pricing" (public — no auth wall).
 * Anonymous visitors see the Upgrade CTA; the click flow detects unauth at
 * /api/payment/checkout and surfaces a "session expired, please sign in" toast
 * via UpgradeCTA's surfaceCheckoutError().
 */

import { useTranslation } from "react-i18next";
import {
  Check,
  KeyRound,
  Leaf,
  Shield,
  ShieldCheck,
  RefreshCcw,
  CreditCard,
} from "lucide-react";

import UpgradeCTA from "@/components/Pricing/UpgradeCTA.tsx";

function Pricing() {
  const { t } = useTranslation();

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
        {/* ---- Hero ---- */}
        <div className="text-center space-y-6 mb-12">
          <p className="text-sm tracking-widest uppercase text-muted-foreground">
            {t("pricing.tagline", "Sustainable · BYOK · Indie-built")}
          </p>
          <h1 className="text-4xl md:text-6xl leading-tight font-display">
            {t("pricing.headline-1", "One flat fee.")}{" "}
            <span style={{ color: "hsl(var(--primary))" }}>
              {t("pricing.headline-2", "Your keys.")}
            </span>
            <br />
            {t("pricing.headline-3", "Zero markup.")}
          </h1>
          <p className="text-lg text-secondary max-w-2xl mx-auto leading-relaxed">
            {t(
              "pricing.subheadline",
              "Bring your own OpenAI / Claude / Gemini keys. We give you the UI, the cost cap, the privacy layer — and visible carbon. You pay providers directly.",
            )}
          </p>
        </div>

        {/* ---- Pricing card ---- */}
        <div className="max-w-md mx-auto rounded-xl border border-border bg-card p-8 space-y-6 mb-20 shadow-sm">
          <div className="space-y-1">
            <p className="text-sm tracking-wide uppercase text-muted-foreground">
              {t("pricing.starter", "Starter")}
            </p>
            <div className="flex items-baseline gap-2">
              <span className="text-5xl font-display">$15</span>
              <span className="text-muted-foreground">
                /{t("pricing.month", "month")}
              </span>
            </div>
            <p className="text-sm text-muted-foreground">
              {t(
                "pricing.starter-tagline",
                "Everything you need. Nothing you don't.",
              )}
            </p>
          </div>

          <ul className="space-y-3 text-sm">
            <FeatureRow text={t("pricing.feature-byok", "Bring your own API keys (BYOK)")} />
            <FeatureRow text={t("pricing.feature-models", "All major models — GPT, Claude, Gemini, Llama")} />
            <FeatureRow text={t("pricing.feature-zero", "Zero prompt retention — we never see your data")} />
            <FeatureRow text={t("pricing.feature-cap", "Hard cost cap — no surprise bills")} />
            <FeatureRow text={t("pricing.feature-carbon", "Per-chat carbon footprint + monthly dashboard")} />
            <FeatureRow text={t("pricing.feature-cancel", "Cancel anytime — keep access until period ends")} />
          </ul>

          <div className="pt-2">
            <UpgradeCTA className="w-full" />
          </div>

          <p className="text-xs text-muted-foreground text-center">
            {t(
              "pricing.disclaimer",
              "Billed monthly. Powered by LemonSqueezy. Includes applicable tax.",
            )}
          </p>
        </div>

        {/* ---- 3 value props ---- */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-20">
          <ValueProp
            icon={<KeyRound className="w-5 h-5" />}
            title={t("pricing.value-wallet-title", "Wallet")}
            body={t(
              "pricing.value-wallet-body",
              "Compress $50–150/mo across ChatGPT Plus + Claude Pro + Cursor + scattered API into one flat fee. You bring the keys; we charge for the UI + ops.",
            )}
          />
          <ValueProp
            icon={<Shield className="w-5 h-5" />}
            title={t("pricing.value-privacy-title", "Privacy")}
            body={t(
              "pricing.value-privacy-body",
              "Configured zero-retention through the gateway. No prompt logging. Public methodology. Your conversations stay between you and your provider.",
            )}
          />
          <ValueProp
            icon={<Leaf className="w-5 h-5" />}
            title={t("pricing.value-planet-title", "Planet")}
            body={t(
              "pricing.value-planet-body",
              "Each chat shows ~CO₂g. Monthly sustainability dashboard. Optional Eco Mode routes to smaller models + cache. No greenwashing — methodology page is public.",
            )}
          />
        </div>

        {/* ---- FAQ ---- */}
        <div className="max-w-2xl mx-auto mb-16 space-y-6">
          <h2 className="text-2xl font-display text-center">
            {t("pricing.faq-title", "Questions you might have")}
          </h2>
          <FaqItem
            question={t(
              "pricing.faq-byok-q",
              "What does BYOK actually mean?",
            )}
            answer={t(
              "pricing.faq-byok-a",
              "You hold the API keys with each provider (OpenAI, Anthropic, Google, etc.) and pay them directly at their published rates — no markup from us. We're the UI + cost cap + privacy layer on top.",
            )}
          />
          <FaqItem
            question={t(
              "pricing.faq-cancel-q",
              "Can I cancel anytime?",
            )}
            answer={t(
              "pricing.faq-cancel-a",
              "Yes. Cancellation is one click in your account. You keep full access until the end of the period you've paid for, then drop to free tier — no surprise charges.",
            )}
          />
          <FaqItem
            question={t(
              "pricing.faq-refund-q",
              "Refunds?",
            )}
            answer={t(
              "pricing.faq-refund-a",
              "Email support within 14 days of your first charge for a no-questions refund. After that, we honor refunds case-by-case for service issues we caused.",
            )}
          />
          <FaqItem
            question={t(
              "pricing.faq-data-q",
              "What data do you store about my conversations?",
            )}
            answer={t(
              "pricing.faq-data-a",
              "Conversation metadata (timestamps, model, token counts) for billing transparency. No prompt content, no completion content. The gateway is configured with prompt logging off.",
            )}
          />
        </div>

        {/* ---- Trust strip ---- */}
        <div className="text-center pt-12 border-t border-border">
          <div className="flex flex-wrap justify-center gap-6 text-xs text-muted-foreground">
            <TrustItem icon={<CreditCard className="w-3.5 h-3.5" />} text={t("pricing.trust-ls", "Powered by LemonSqueezy")} />
            <TrustItem icon={<RefreshCcw className="w-3.5 h-3.5" />} text={t("pricing.trust-cancel", "Cancel anytime")} />
            <TrustItem icon={<ShieldCheck className="w-3.5 h-3.5" />} text={t("pricing.trust-no-lockin", "No vendor lock-in — your keys stay yours")} />
          </div>
        </div>
      </div>
    </div>
  );
}

// ----------------------------------------------------------------------------
// Sub-components
// ----------------------------------------------------------------------------

function FeatureRow({ text }: { text: string }) {
  return (
    <li className="flex items-start gap-3">
      <Check
        className="w-4 h-4 mt-0.5 flex-shrink-0"
        style={{ color: "hsl(var(--primary))" }}
      />
      <span className="text-secondary leading-relaxed">{text}</span>
    </li>
  );
}

type ValuePropProps = {
  icon: React.ReactNode;
  title: string;
  body: string;
};

function ValueProp({ icon, title, body }: ValuePropProps) {
  return (
    <div className="rounded-lg border border-border bg-card p-6 space-y-3">
      <div className="flex items-center gap-3">
        <div
          className="flex items-center justify-center w-9 h-9 rounded-md"
          style={{
            background: "hsl(var(--accent))",
            color: "hsl(var(--accent-foreground))",
          }}
        >
          {icon}
        </div>
        <div className="font-display text-lg font-medium">{title}</div>
      </div>
      <p className="text-sm leading-relaxed text-secondary">{body}</p>
    </div>
  );
}

function FaqItem({ question, answer }: { question: string; answer: string }) {
  return (
    <div className="space-y-2">
      <h3 className="font-medium text-base">{question}</h3>
      <p className="text-sm text-secondary leading-relaxed">{answer}</p>
    </div>
  );
}

function TrustItem({ icon, text }: { icon: React.ReactNode; text: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      {icon}
      {text}
    </span>
  );
}

export default Pricing;
