import { Button } from "@/components/ui/button.tsx";
import { ArrowRight, Mail } from "lucide-react";
import { LucideIcon } from "lucide-react";

export type ServiceCardStatus = "live" | "coming-soon";

export type ServiceCardProps = {
  icon: LucideIcon;
  title: string;
  desc: string;
  status: ServiceCardStatus;
  /** Status badge label, e.g. "订阅含" / "Coming soon". */
  statusLabel: string;
  /** Primary CTA label, e.g. "试用" / "留邮箱抢先体验". */
  ctaLabel: string;
  onCta: () => void;
};

/**
 * Single tile on the marketing landing's service grid.
 *
 * Two visual states:
 *   - live        → solid primary button, "试用" → routes into the product
 *   - coming-soon → softer button with mail icon, "留邮箱" → opens WaitlistDialog
 *
 * The card itself doesn't know about waitlist or routing; it just calls
 * onCta. Welcome.tsx owns that wiring.
 */
export default function ServiceCard({
  icon: Icon,
  title,
  desc,
  status,
  statusLabel,
  ctaLabel,
  onCta,
}: ServiceCardProps) {
  const isLive = status === "live";

  return (
    <div className="rounded-lg border border-border bg-card p-6 flex flex-col gap-4 hover:bg-card-hover transition-colors">
      <div className="flex items-start justify-between gap-3">
        <div
          className="flex items-center justify-center w-12 h-12 rounded-md shrink-0"
          style={{
            background: "hsl(var(--accent))",
            color: "hsl(var(--accent-foreground))",
          }}
        >
          <Icon className="w-6 h-6" />
        </div>
        <span
          className={
            "text-xs px-2 py-1 rounded-full tracking-wide " +
            (isLive
              ? "bg-primary/10 text-primary border border-primary/20"
              : "bg-muted text-muted-foreground border border-border")
          }
        >
          {statusLabel}
        </span>
      </div>

      <div className="flex-1 space-y-2">
        <h3 className="font-display text-xl font-medium">{title}</h3>
        <p className="text-sm leading-relaxed text-secondary">{desc}</p>
      </div>

      <Button
        onClick={onCta}
        variant={isLive ? "default" : "outline"}
        className="w-full mt-2"
      >
        {isLive ? (
          <ArrowRight className="mr-2 w-4 h-4" />
        ) : (
          <Mail className="mr-2 w-4 h-4" />
        )}
        {ctaLabel}
      </Button>
    </div>
  );
}
