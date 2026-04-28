import { Leaf, Shield, Layers } from "lucide-react";
import { useTranslation } from "react-i18next";
import router from "@/router.tsx";

/**
 * "Why greentokey" — three small cards beneath the service grid that frame
 * the brand promise: visible carbon, zero retention, multi-service bundle.
 *
 * The carbon prop links to /methodology so visitors can verify the claim
 * before they pay (this is the moat — "transparent" only matters if it's
 * checkable).
 */
export default function ValueProps() {
  const { t } = useTranslation();

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-20">
      <ValueCard
        icon={<Leaf className="w-5 h-5" />}
        title={t("landing.why.carbon.title", "Carbon visible")}
        body={t(
          "landing.why.carbon.body",
          "Every chat shows a real-time CO₂ estimate. Monthly dashboard. Public methodology.",
        )}
        linkLabel={t("landing.why.carbon.link", "Methodology →")}
        onLink={() => router.navigate("/methodology")}
      />
      <ValueCard
        icon={<Shield className="w-5 h-5" />}
        title={t("landing.why.privacy.title", "Zero prompt retention")}
        body={t(
          "landing.why.privacy.body",
          "Your prompts never persist. Open-source gateway. Hard cost cap so a runaway loop can't drain your balance.",
        )}
      />
      <ValueCard
        icon={<Layers className="w-5 h-5" />}
        title={t("landing.why.bundle.title", "One subscription, many services")}
        body={t(
          "landing.why.bundle.body",
          "Pay once. Use chat today. Tax filing and auto-edit unlock for you the moment they ship.",
        )}
      />
    </div>
  );
}

type ValueCardProps = {
  icon: React.ReactNode;
  title: string;
  body: string;
  linkLabel?: string;
  onLink?: () => void;
};

function ValueCard({ icon, title, body, linkLabel, onLink }: ValueCardProps) {
  return (
    <div className="rounded-lg border border-border bg-card p-6 space-y-3 hover:bg-card-hover transition-colors">
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
        <div className="font-display text-base font-medium">{title}</div>
      </div>
      <p className="text-sm leading-relaxed text-secondary">{body}</p>
      {linkLabel && onLink && (
        <button
          onClick={onLink}
          className="text-sm text-primary hover:underline underline-offset-4"
        >
          {linkLabel}
        </button>
      )}
    </div>
  );
}
