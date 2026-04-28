// v0.6.1 — service card for an active product (e.g., Chat Playground).
// "Active" means the user can use it RIGHT NOW; their subscription includes it.

import React from "react";
import { Link } from "react-router-dom";
import { ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button.tsx";
import { ServiceCardShell } from "./ServiceCardShell.tsx";

type Props = {
  icon: React.ReactNode;
  title: string;
  subtitle?: string;
  body?: React.ReactNode;
  ctaLabel: string;
  to: string;
};

export function ActiveServiceCard({
  icon,
  title,
  subtitle,
  body,
  ctaLabel,
  to,
}: Props) {
  return (
    <ServiceCardShell
      icon={icon}
      title={title}
      subtitle={subtitle}
      cta={
        <Link to={to}>
          <Button size="sm" className="gap-1">
            {ctaLabel}
            <ArrowRight className="w-3.5 h-3.5" />
          </Button>
        </Link>
      }
    >
      {body}
    </ServiceCardShell>
  );
}
