import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Key, Grid3x3, Network } from "lucide-react";

import Hero from "@/components/Marketing/Hero.tsx";
import MainServiceCards from "@/components/Marketing/MainServiceCards.tsx";
import ContactDialog from "@/components/Marketing/ContactDialog.tsx";
import Header from "@/components/Marketing/Header.tsx";
import Footer from "@/components/Marketing/Footer.tsx";

/**
 * greentokey marketing landing — v0.11 dual-rail restore.
 *
 * Restores the v0.7 Claude Design intent (docs/strategy/2026-04-30-redesign-v2.html):
 *   - Hero: dual-rail "一个 API Key, 一个智能体市场"
 *   - 3 main cards: Token 套餐 / 服务市场 / sub2API · 扩池
 *   - 3 value props: 一 Key 通模型 / 智能体即服务 / sub2API 扩池
 *
 * The 民宿 wedge (¥1980/月) is now ONE service in the marketplace
 * (/services), not the entire site identity. Customers who want the
 * 民宿 service follow Hero → 服务市场 → 民宿全托管.
 */
function Welcome() {
  const { t } = useTranslation();
  const [contactOpen, setContactOpen] = useState(false);

  return (
    <>
      <Header />
      <main
        className="flex-1 overflow-y-auto"
        style={{
          paddingLeft: "max(env(safe-area-inset-left), 0px)",
          paddingRight: "max(env(safe-area-inset-right), 0px)",
        }}
      >
        <div
          className="mx-auto px-6"
          style={{ maxWidth: "var(--max-content)" }}
        >
          {/* Hero */}
          <div className="py-16 md:py-24">
            <Hero />
          </div>

          {/* Main service cards (3 columns) */}
          <div className="pb-20 md:pb-28">
            <MainServiceCards />
          </div>

          {/* Value props — why this exists */}
          <div className="pb-24 md:pb-32">
            <h2 className="font-display text-2xl md:text-3xl text-center mb-3">
              {t("home.values.heading", "为什么这样设计")}
            </h2>
            <p className="text-center text-sm text-muted-foreground mb-12 max-w-xl mx-auto">
              {t(
                "home.values.sub",
                "底层是算力，上层是体验。Token 套餐让你直接调用，服务市场让你按结果买。",
              )}
            </p>
            <div className="grid grid-cols-1 md:grid-cols-3 gap-8 md:gap-12">
              <ValueProp
                icon={<Key className="w-6 h-6" strokeWidth={1.6} />}
                title={t("home.values.one-key.title", "一 Key 通模型")}
                body={t(
                  "home.values.one-key.body",
                  "NewAPI 在底层调度，你拿到的是一个 OpenAI 兼容 endpoint——切模型只改 model 字段，不再换账号、换 Key。",
                )}
              />
              <ValueProp
                icon={<Grid3x3 className="w-6 h-6" strokeWidth={1.6} />}
                title={t("home.values.agent.title", "智能体即服务")}
                body={t(
                  "home.values.agent.body",
                  "市场里的 DIY 智能体 = 我们调好的 Prompt + Token 包。你按次买，我们扣算力，效果稳定可复制。",
                )}
              />
              <ValueProp
                icon={<Network className="w-6 h-6" strokeWidth={1.6} />}
                title={t("home.values.sub2api.title", "sub2API 扩池(规划)")}
                body={t(
                  "home.values.sub2api.body",
                  "把 Claude / OpenAI 官方 API 桥接进 Token 池。聚合更全、合规接入，不挤占第三方中转的不稳定产能。",
                )}
              />
            </div>
          </div>
        </div>

        <Footer />
      </main>

      <ContactDialog
        open={contactOpen}
        onOpenChange={setContactOpen}
        source="home"
      />
    </>
  );
}

function ValueProp({
  icon,
  title,
  body,
}: {
  icon: React.ReactNode;
  title: string;
  body: string;
}) {
  return (
    <div className="space-y-3">
      <span
        className="inline-flex items-center justify-center w-10 h-10 rounded-xl"
        style={{ color: "hsl(var(--primary))" }}
      >
        {icon}
      </span>
      <h4 className="font-display text-lg font-medium">{title}</h4>
      <p className="text-sm leading-relaxed text-secondary-foreground/85">
        {body}
      </p>
    </div>
  );
}

export default Welcome;
