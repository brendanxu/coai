// v0.6.1 — shared shell for service cards on the logged-in home dashboard.
// ActiveServiceCard and ComingSoonServiceCard wrap their content in this so
// visual rhythm stays consistent (border, padding, hover, icon slot).

import React from "react";
import { cn } from "@/components/ui/lib/utils.ts";

type Props = {
  icon: React.ReactNode;
  title: string;
  subtitle?: string;
  children?: React.ReactNode;
  cta: React.ReactNode;
  className?: string;
  dimmed?: boolean;
};

export function ServiceCardShell({
  icon,
  title,
  subtitle,
  children,
  cta,
  className,
  dimmed = false,
}: Props) {
  return (
    <div
      className={cn(
        "rounded-lg border border-border bg-card p-5 transition-colors",
        dimmed ? "opacity-70" : "hover:bg-card-hover",
        className,
      )}
    >
      <div className="flex flex-col sm:flex-row sm:items-start gap-4">
        <div
          className="flex items-center justify-center w-10 h-10 rounded-md shrink-0"
          style={{
            background: "hsl(var(--accent))",
            color: "hsl(var(--accent-foreground))",
          }}
        >
          {icon}
        </div>
        <div className="flex-1 min-w-0">
          <div className="font-display text-lg font-medium leading-tight">
            {title}
          </div>
          {subtitle && (
            <div className="text-xs text-muted-foreground mt-0.5">
              {subtitle}
            </div>
          )}
          {children && (
            <div className="text-sm text-secondary mt-2 leading-relaxed">
              {children}
            </div>
          )}
        </div>
        <div className="shrink-0 sm:self-center">{cta}</div>
      </div>
    </div>
  );
}
