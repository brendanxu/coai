// v0.6.1 — service card for a "coming soon" product (e.g., AI 报税, 自动剪辑).
// CTA points to the marketing waitlist with ?service=<id>. Marketing window
// owns the destination URL — Product window only ships the link with the
// query param, Marketing wires the form to pre-fill the user's email.

import React from "react";
import { Button } from "@/components/ui/button.tsx";
import { ServiceCardShell } from "./ServiceCardShell.tsx";

type Props = {
  icon: React.ReactNode;
  title: string;
  subtitle?: string;
  body?: React.ReactNode;
  ctaLabel: string;
  serviceId: string; // appended as ?service=<id> to /#waitlist anchor
};

export function ComingSoonServiceCard({
  icon,
  title,
  subtitle,
  body,
  ctaLabel,
  serviceId,
}: Props) {
  // Marketing landing page owns the #waitlist anchor + form. We ship the
  // link with a service hint; Marketing wires the rest.
  const href = `/?service=${encodeURIComponent(serviceId)}#waitlist`;

  return (
    <ServiceCardShell
      icon={icon}
      title={title}
      subtitle={subtitle}
      dimmed
      cta={
        <Button asChild size="sm" variant="outline">
          <a href={href}>{ctaLabel}</a>
        </Button>
      }
    >
      {body}
    </ServiceCardShell>
  );
}
