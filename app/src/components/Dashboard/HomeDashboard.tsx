// v0.6.1 — logged-in home (/) dashboard. Replaces the chat-first home.
// Composition: UsageOverview banner → 3 service cards → carbon footer.
//
// File named HomeDashboard.tsx to avoid collision with routes/Dashboard.tsx
// (the v0.6 carbon report at /dashboard).

import { useTranslation } from "react-i18next";
import { MessageSquare, Receipt, Film } from "lucide-react";
import { UsageOverview } from "./UsageOverview.tsx";
import { ActiveServiceCard } from "./ActiveServiceCard.tsx";
import { ComingSoonServiceCard } from "./ComingSoonServiceCard.tsx";
import { MonthlyCarbonSummary } from "./MonthlyCarbonSummary.tsx";

export function HomeDashboard() {
  const { t } = useTranslation();

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-3xl mx-auto px-6 py-10 space-y-8">
        <UsageOverview />

        <section className="space-y-3">
          <h2 className="text-sm font-medium text-muted-foreground uppercase tracking-wider">
            {t("dashboard.your-services")}
          </h2>
          <div className="space-y-3">
            <ActiveServiceCard
              icon={<MessageSquare className="w-5 h-5" />}
              title={t("dashboard.svc-chat-title")}
              subtitle={t("dashboard.svc-chat-subtitle")}
              body={t("dashboard.svc-chat-body")}
              ctaLabel={t("dashboard.svc-chat-cta")}
              to="/chat"
            />
            <ComingSoonServiceCard
              icon={<Receipt className="w-5 h-5" />}
              title={t("dashboard.svc-tax-title")}
              subtitle={t("dashboard.coming-soon")}
              body={t("dashboard.svc-tax-body")}
              ctaLabel={t("dashboard.notify-me")}
              serviceId="tax"
            />
            <ComingSoonServiceCard
              icon={<Film className="w-5 h-5" />}
              title={t("dashboard.svc-editor-title")}
              subtitle={t("dashboard.coming-soon")}
              body={t("dashboard.svc-editor-body")}
              ctaLabel={t("dashboard.notify-me")}
              serviceId="editor"
            />
          </div>
        </section>

        <MonthlyCarbonSummary />
      </div>
    </div>
  );
}
