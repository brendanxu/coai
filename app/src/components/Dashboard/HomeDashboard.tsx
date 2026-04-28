// v0.6.1 — logged-in home (/) dashboard. Replaces the chat-first home.
// v0.6.1.1 hotfix: removed redundant Chat Playground card. Chat is the
// floating widget (bottom-right), not a service tile — those two were felt
// as "the same thing" by founder during smoke test.
// Dashboard now primarily surfaces the differentiated AI services.
//
// File named HomeDashboard.tsx to avoid collision with routes/Dashboard.tsx
// (the v0.6 carbon report at /dashboard).

import { useTranslation } from "react-i18next";
import { Receipt, Film, MessageSquare } from "lucide-react";
import { UsageOverview } from "./UsageOverview.tsx";
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

          {/* Subtle inline hint — chat is the floating widget, not a card. */}
          <div className="flex items-center gap-2 text-xs text-muted-foreground/80 px-1 py-1">
            <MessageSquare className="w-3.5 h-3.5" />
            <span>{t("dashboard.chat-floating-hint", "多模型对话已就绪 · 点右下角 🌱 随时开聊")}</span>
          </div>

          <div className="space-y-3">
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
