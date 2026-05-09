import { Trees, Smartphone, BadgeCheck } from "lucide-react";
import { useTranslation } from "react-i18next";

/**
 * "为啥找我们做" — v0.8 民宿 wedge 三个核心价值点。
 *
 * 替换之前的 carbon / privacy / bundle (indie hacker 卖点)，新的 3 点是
 * 民宿主真正在乎的东西：
 *  1. 替代 MCN — 更稳更便宜（直接 PK 现有 ¥2-5K/月本地工作室）
 *  2. 民宿垂直 — 懂洱海 / 旅拍 / 季节 / 客群（vs 通用 AI 不懂民宿）
 *  3. 老板自己掌控 — 账号 100% 你的，零 RPA 风险（解决信任）
 */
export default function ValueProps() {
  const { t } = useTranslation();

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-20">
      <ValueCard
        icon={<BadgeCheck className="w-5 h-5" />}
        title={t("landing.why.replace-mcn.title", "替代本地 MCN")}
        body={t(
          "landing.why.replace-mcn.body",
          "你给本地工作室付 ¥2-5K/月，3 个月退订是常事。我们用 AI 把那套活儿做实，¥1980/月不分级，效果不稳定时 30 天 50% 退款。",
        )}
      />
      <ValueCard
        icon={<Trees className="w-5 h-5" />}
        title={t("landing.why.vertical.title", "民宿垂直 AI")}
        body={t(
          "landing.why.vertical.body",
          "我们不是通用 AI 工具。AI 学过的是大理民宿爆款的语料：洱海、苍山、旅拍、蜜月、亲子、季节性玩法。先做大理一个区域，做透了再扩。",
        )}
      />
      <ValueCard
        icon={<Smartphone className="w-5 h-5" />}
        title={t("landing.why.your-account.title", "账号 100% 你掌控")}
        body={t(
          "landing.why.your-account.body",
          "我们不接管你的小红书账号。内容生成后推到你手机草稿箱，你一键确认即发。封号风险低，账号永远是你的。",
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
    <div className="rounded-lg border border-border bg-card p-6 space-y-3 hover:bg-card-hover active:bg-card-hover transition-colors">
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
          className="text-sm text-primary hover:underline active:underline underline-offset-4"
        >
          {linkLabel}
        </button>
      )}
    </div>
  );
}
