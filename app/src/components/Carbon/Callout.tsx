// v0.6 carbon — reusable callout component.
// Used on the Methodology page for the ±20% disclaimer + on Dashboard
// for the equivalent narrative.
import React from "react";
import { cn } from "@/components/ui/lib/utils.ts";

type Variant = "warning" | "info" | "accent";

type Props = {
  variant?: Variant;
  children: React.ReactNode;
  className?: string;
};

export function Callout({ variant = "info", children, className }: Props) {
  const variantClasses = {
    warning: "bg-[hsl(var(--gold)/0.15)] border-[hsl(var(--gold))]",
    info: "bg-card border-border",
    accent: "bg-[hsl(var(--accent)/0.15)] border-[hsl(var(--primary))]",
  }[variant];

  return (
    <div
      className={cn(
        "rounded-md border-l-4 p-4 my-6 leading-relaxed",
        variantClasses,
        className,
      )}
    >
      {children}
    </div>
  );
}
