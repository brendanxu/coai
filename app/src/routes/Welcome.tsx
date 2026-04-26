import { Button } from "@/components/ui/button.tsx";
import router from "@/router.tsx";
import { ArrowRight, KeyRound, Leaf, Shield } from "lucide-react";

/**
 * greentokey welcome / landing — shown to logged-out visitors who haven't yet
 * dismissed the welcome screen. Hero + 3 value props + 2 CTAs.
 *
 * Layered above chat UI: visitor either signs up / logs in (full features),
 * or hits "免费试用一次" which sets a localStorage flag and lets them in to
 * the anonymous chat path that CoAI already supports.
 */
function Welcome() {
  const enterAnonymous = () => {
    localStorage.setItem("skip_welcome", "1");
    // soft reload to re-render Home with chat content
    window.location.reload();
  };

  const goLogin = () => router.navigate("/login");
  const goRegister = () => router.navigate("/register");

  return (
    <div className="flex-1 overflow-y-auto">
      <div className="max-w-5xl mx-auto px-6 py-12 md:py-20">
        {/* Hero */}
        <div className="text-center space-y-6 mb-16">
          <p className="text-sm tracking-widest uppercase text-muted-foreground">
            Sustainable · BYOK · Indie-built
          </p>
          <h1 className="text-5xl md:text-7xl leading-tight font-display">
            your{" "}
            <span style={{ color: "hsl(var(--primary))" }}>green key</span>
            <br />
            to AI
          </h1>
          <p className="text-lg md:text-xl text-secondary max-w-2xl mx-auto leading-relaxed">
            Bring your own API keys. See your real-time carbon footprint.
            We never see your prompts.
          </p>

          <div className="flex flex-col sm:flex-row gap-3 justify-center pt-4">
            <Button
              size="lg"
              onClick={goRegister}
              className="px-8 py-6 text-base"
            >
              立即注册 <ArrowRight className="ml-2 w-4 h-4" />
            </Button>
            <Button
              variant="outline"
              size="lg"
              onClick={goLogin}
              className="px-8 py-6 text-base"
            >
              已有账户 · 登录
            </Button>
          </div>

          <button
            onClick={enterAnonymous}
            className="text-sm text-muted-foreground underline underline-offset-4 hover:text-foreground transition-colors mt-2"
          >
            或者 不注册先试一下 →
          </button>
        </div>

        {/* 3 value props */}
        <div className="grid grid-cols-1 md:grid-cols-3 gap-6 mb-20">
          <FeatureCard
            icon={<KeyRound className="w-6 h-6" />}
            title="BYOK"
            subtitle="One key, every model"
            body="把你散在 ChatGPT Plus + Claude Pro + Cursor + 散装 API 的成本，压成一套 flat \$15/mo，你自带的 keys，0 markup。"
          />
          <FeatureCard
            icon={<Shield className="w-6 h-6" />}
            title="Zero Retention"
            subtitle="Your prompts stay yours"
            body="我们永不存你的 prompt。NewAPI 网关明文配置 + 公开方法学。Hard cost cap 防止单月费用爆炸。"
          />
          <FeatureCard
            icon={<Leaf className="w-6 h-6" />}
            title="Carbon Visible"
            subtitle="AI that doesn't cost the earth"
            body="每条 chat 后看见 ~CO₂g 估算。月度可持续 dashboard。Eco Mode 自动路由到小模型 + cache。"
          />
        </div>

        {/* Brand statement footer */}
        <div className="text-center text-sm text-muted-foreground space-y-2 pt-12 border-t border-border">
          <p className="font-display text-base text-secondary">
            One key. Transparent compute. Visible carbon.
          </p>
          <p>
            Built for indie hackers / solo founders / vibe coders who care.
            Open source CoAI fork · Apache 2.0.
          </p>
        </div>
      </div>
    </div>
  );
}

type FeatureCardProps = {
  icon: React.ReactNode;
  title: string;
  subtitle: string;
  body: string;
};

function FeatureCard({ icon, title, subtitle, body }: FeatureCardProps) {
  return (
    <div className="rounded-lg border border-border bg-card p-6 space-y-3 hover:bg-card-hover transition-colors">
      <div className="flex items-center gap-3">
        <div
          className="flex items-center justify-center w-10 h-10 rounded-md"
          style={{
            background: "hsl(var(--accent))",
            color: "hsl(var(--accent-foreground))",
          }}
        >
          {icon}
        </div>
        <div>
          <div className="font-display text-lg font-medium">{title}</div>
          <div className="text-xs text-muted-foreground tracking-wide">
            {subtitle}
          </div>
        </div>
      </div>
      <p className="text-sm leading-relaxed text-secondary">{body}</p>
    </div>
  );
}

export default Welcome;
