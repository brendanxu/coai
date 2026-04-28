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
 * v0.7-design: claudo-style service tile.
 * - Big rounded card (24px), warm paper background, soft umber shadow
 * - Hover lift -4px + shadow upgrade
 * - Icon tile with accent-soft tint (carbon-data-badge feel)
 * - Pill CTA — moss for live, ghost outline for coming-soon
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
    <article
      className={
        "service-card group relative flex flex-col h-full transition-all duration-200 ease-out " +
        "bg-card border border-border " +
        "hover:-translate-y-1 hover:shadow-[0_20px_60px_rgba(43,33,24,0.12)]"
      }
      style={{
        borderRadius: "var(--radius-card)",
        padding: "32px 28px 28px",
        boxShadow: "var(--shadow-xs)",
      }}
    >
      {/* Status chip — top-right floating */}
      <span
        className={
          "absolute top-5 right-5 inline-flex items-center text-[11px] font-medium tracking-wide rounded-full px-2.5 py-1 "
        }
        style={{
          background: isLive
            ? "hsl(var(--accent-soft))"
            : "hsl(var(--secondary))",
          color: isLive
            ? "hsl(var(--primary-deep))"
            : "hsl(var(--muted-foreground))",
        }}
      >
        {statusLabel}
      </span>

      {/* Icon tile */}
      <span
        className="inline-flex items-center justify-center mb-6"
        style={{
          width: "44px",
          height: "44px",
          borderRadius: "12px",
          background: "hsl(var(--accent-soft))",
          color: "hsl(var(--primary-deep))",
        }}
      >
        <Icon className="w-5 h-5" />
      </span>

      <h3 className="font-display text-2xl mb-3" style={{ letterSpacing: "-0.01em" }}>
        {title}
      </h3>
      <p
        className="text-sm flex-1 mb-6"
        style={{ color: "hsl(var(--muted-foreground))", lineHeight: 1.6 }}
      >
        {desc}
      </p>

      {/* Pill CTA */}
      <button
        onClick={onCta}
        className={
          "inline-flex items-center justify-center gap-1.5 self-start transition-colors duration-150 ease-out " +
          "text-sm font-medium "
        }
        style={{
          padding: "10px 20px",
          borderRadius: "var(--radius-pill)",
          background: isLive ? "hsl(var(--ink))" : "transparent",
          color: isLive
            ? "hsl(var(--ink-foreground))"
            : "hsl(var(--muted-foreground))",
          border: isLive ? "none" : "1px solid hsl(var(--border-hover))",
        }}
        onMouseEnter={(e) => {
          if (isLive) {
            e.currentTarget.style.background = "hsl(var(--ink-2))";
          } else {
            e.currentTarget.style.borderColor = "hsl(var(--primary))";
            e.currentTarget.style.color = "hsl(var(--primary-deep))";
          }
        }}
        onMouseLeave={(e) => {
          if (isLive) {
            e.currentTarget.style.background = "hsl(var(--ink))";
          } else {
            e.currentTarget.style.borderColor = "hsl(var(--border-hover))";
            e.currentTarget.style.color = "hsl(var(--muted-foreground))";
          }
        }}
      >
        {isLive ? <ArrowRight className="w-4 h-4" /> : <Mail className="w-4 h-4" />}
        {ctaLabel}
      </button>
    </article>
  );
}
