import { tokenField } from "@/conf/bootstrap.ts";
import { useEffect, useReducer } from "react";
import Loader from "@/components/Loader.tsx";
import "@/assets/pages/auth.less";
import { validateToken } from "@/store/auth.ts";
import { useDispatch } from "react-redux";
import router from "@/router.tsx";
import { useTranslation } from "react-i18next";
import { getQueryParam } from "@/utils/path.ts";

// HI-02 (REVIEW.md 2026-05-13): after successful login, return to the page
// the user was trying to reach (e.g. /token-plans for a checkout-after-login
// flow). The `next` query param is set by upstream callers like
// TokenPlans.tsx on 401. We accept ONLY same-origin relative paths to defend
// against open-redirect attacks — anything else falls back to "/".
//
// Rejected patterns:
//   //evil.com/...           — scheme-relative, treated as cross-origin
//   http://evil.com/...      — absolute URL
//   javascript:alert(1)      — pseudo-scheme injection
//   <anything with spaces>   — pre-encoded leak / malformed
//   empty / missing          — default to "/"
export const SAFE_NEXT_PATH = /^\/(?!\/)[^\s<>"]*$/;

export function nextPathFromQuery(): string {
  const next = (getQueryParam("next") || "").trim();
  if (!next || !SAFE_NEXT_PATH.test(next)) return "/";
  return next;
}
import { setMemory } from "@/utils/memory.ts";
import { appLogo, appName, useDeeptrain } from "@/conf/env.ts";

import { goAuth } from "@/utils/app.ts";
import { Label } from "@/components/ui/label.tsx";
import { Input } from "@/components/ui/input.tsx";
import Require, { LengthRangeRequired } from "@/components/Require.tsx";
import { Button } from "@/components/ui/button.tsx";
import { formReducer, isTextInRange } from "@/utils/form.ts";
import { doLogin, LoginForm } from "@/api/auth.ts";
import { getErrorMessage, isEnter } from "@/utils/base.ts";
import { ScrollArea } from "@/components/ui/scroll-area.tsx";
import { toast } from "sonner";

function DeepAuth() {
  const { t } = useTranslation();
  const dispatch = useDispatch();
  const token = getQueryParam("token").trim();

  useEffect(() => {
    if (!token.length) {
      toast.warning(t("invalid-token"), {
        description: t("invalid-token-prompt"),
        action: {
          label: t("try-again"),
          onClick: goAuth,
        },
      });

      setTimeout(goAuth, 2500);
      return;
    }

    setMemory(tokenField, token);

    doLogin({ token })
      .then((data) => {
        if (!data.status) {
          toast.error(t("login-failed"), {
            description: t("login-failed-prompt", { reason: data.error }),
            action: {
              label: t("try-again"),
              onClick: goAuth,
            },
          });
        } else
          validateToken(dispatch, data.token, async () => {
            toast.success(t("login-success"), {
              description: t("login-success-prompt"),
            });

            await router.navigate(nextPathFromQuery());
          });
      })
      .catch((err) => {
        console.debug(err);

        toast.error(t("server-error"), {
          description: `${t("server-error-prompt")}\n${err.message}`,
          action: {
            label: t("try-again"),
            onClick: goAuth,
          },
        });
      });
  }, []);

  return (
    <div className={`auth`}>
      <Loader prompt={t("login")} />
    </div>
  );
}

function Login() {
  const { t } = useTranslation();
  const globalDispatch = useDispatch();
  const [form, dispatch] = useReducer(formReducer<LoginForm>(), {
    username: sessionStorage.getItem("username") || "",
    password: sessionStorage.getItem("password") || "",
  });

  // Detect ?next= param for checkout-after-login context copy
  const nextPath = nextPathFromQuery();
  const isCheckoutNext = nextPath.startsWith("/token-plans") || nextPath.startsWith("/checkout");

  const onSubmit = async () => {
    if (
      !isTextInRange(form.username, 1, 255) ||
      !isTextInRange(form.password, 6, 36)
    )
      return;

    try {
      const resp = await doLogin(form);
      if (!resp.status) {
        toast.warning(t("login-failed"), {
          description: t("login-failed-prompt", { reason: resp.error }),
        });
        return;
      }

      toast.success(t("login-success"), {
        description: t("login-success-prompt"),
      });

      if (
        form.username.trim() === "root" &&
        form.password.trim() === "coai123456"
      ) {
        toast.warning(t("admin.default-password"), {
          description: t("admin.default-password-prompt"),
          duration: 15000,
        });
      }

      validateToken(globalDispatch, resp.token);
      await router.navigate(nextPathFromQuery());
    } catch (err) {
      console.debug(err);
      toast.error(t("server-error"), {
        description: `${t("server-error-prompt")}\n${getErrorMessage(err)}`,
      });
    }
  };

  useEffect(() => {
    // listen to enter key and auto submit
    const listener = async (e: KeyboardEvent) => {
      if (isEnter(e)) await onSubmit();
    };

    document.addEventListener("keydown", listener);
    return () => document.removeEventListener("keydown", listener);
  }, []);

  return (
    <ScrollArea className="w-full h-full">
      <div className="min-h-screen grid md:grid-cols-[1fr_1fr] lg:grid-cols-[1.1fr_1fr]">
        {/* ── Left panel — brand (hidden on small screens) ─────────── */}
        <div
          className="hidden md:flex flex-col justify-between p-10 lg:p-14"
          style={{
            background: "hsl(var(--accent))",
            color: "#fff",
          }}
        >
          <div className="flex items-center gap-2">
            <img
              className="w-8 h-8 rounded-lg"
              src={appLogo}
              alt={appName}
            />
            <span className="font-display text-lg font-semibold tracking-tight">
              {appName}
            </span>
          </div>

          <div className="space-y-6">
            <p
              className="text-xs uppercase tracking-[0.18em] font-medium"
              style={{ color: "rgba(255,255,255,0.65)" }}
            >
              {t("auth.split.eyebrow", "登录")}
            </p>
            <h2
              className="font-display text-3xl lg:text-4xl leading-[1.15] tracking-tight"
              style={{ color: "#fff" }}
            >
              {t("auth.split.heading", "欢迎回来,")}
              <br />
              {t("auth.split.heading2", "从你停下的")}
              <em
                className="not-italic"
                style={{ color: "rgba(255,255,255,0.75)", fontStyle: "italic" }}
              >
                {" "}{t("auth.split.heading3", "地方继续。")}
              </em>
            </h2>
            <p
              className="text-sm leading-relaxed max-w-xs"
              style={{ color: "rgba(255,255,255,0.75)" }}
            >
              {isCheckoutNext
                ? t(
                    "auth.split.sub_checkout",
                    "登录后会自动回到你刚才看的 Token 套餐页，几秒内完成支付。",
                  )
                : t(
                    "auth.split.sub",
                    "一个账号，调用全部模型。支付宝 / 微信 / 卡都可以付。",
                  )}
            </p>

            {isCheckoutNext && (
              <div
                className="inline-flex items-center gap-2 rounded-lg px-3 py-2 text-xs"
                style={{
                  background: "rgba(255,255,255,0.12)",
                  color: "rgba(255,255,255,0.85)",
                }}
              >
                <svg
                  className="w-3.5 h-3.5 flex-shrink-0"
                  viewBox="0 0 24 24"
                  fill="none"
                  stroke="currentColor"
                  strokeWidth="2"
                  strokeLinecap="round"
                >
                  <path d="M9 14 4 9l5-5" />
                  <path d="M4 9h10.5a5.5 5.5 0 0 1 5.5 5.5v0a5.5 5.5 0 0 1-5.5 5.5H11" />
                </svg>
                {t("auth.split.returnto", "登录后将跳回")} <strong>/token-plans</strong>
              </div>
            )}
          </div>

          {/* Stats chips at bottom */}
          <div className="flex flex-wrap gap-3">
            {[
              t("auth.split.stat1", "1.2k+ 注册用户"),
              t("auth.split.stat2", "14 款现货模型"),
              t("auth.split.stat3", "99.9% 可用率"),
            ].map((stat) => (
              <span
                key={stat}
                className="text-xs px-3 py-1.5 rounded-full font-medium"
                style={{
                  background: "rgba(255,255,255,0.15)",
                  color: "rgba(255,255,255,0.9)",
                }}
              >
                {stat}
              </span>
            ))}
          </div>
        </div>

        {/* ── Right panel — login form ──────────────────────────────── */}
        <div className="flex flex-col items-center justify-center px-6 py-10 md:px-10">
          {/* Mobile-only logo */}
          <div className="flex items-center gap-2 mb-8 md:hidden">
            <img className="w-8 h-8 rounded-lg" src={appLogo} alt={appName} />
            <span className="font-display text-lg font-semibold">
              {appName}
            </span>
          </div>

          <div className="w-full max-w-sm">
            <div className="mb-6">
              <h1 className="font-display text-2xl tracking-tight mb-1">
                {t("login")}
              </h1>
              <p className="text-sm text-muted-foreground">
                {isCheckoutNext
                  ? t("auth.split.form_sub_checkout", "登录并继续完成支付")
                  : t("auth.split.form_sub", "登录你的 greentokey 账号")}
              </p>
            </div>

            <div className="space-y-4">
              <div className="space-y-1.5">
                <Label>
                  <Require />
                  {t("auth.username-or-email")}
                  <LengthRangeRequired
                    content={form.username}
                    min={1}
                    max={255}
                    hideOnEmpty={true}
                  />
                </Label>
                <Input
                  placeholder={t("auth.username-or-email-placeholder")}
                  value={form.username}
                  onChange={(e) =>
                    dispatch({ type: "update:username", payload: e.target.value })
                  }
                />
              </div>

              <div className="space-y-1.5">
                <Label>
                  <Require />
                  {t("auth.password")}
                  <LengthRangeRequired
                    content={form.password}
                    min={6}
                    max={36}
                    hideOnEmpty={true}
                  />
                </Label>
                <Input
                  placeholder={t("auth.password-placeholder")}
                  value={form.password}
                  type="password"
                  onChange={(e) =>
                    dispatch({ type: "update:password", payload: e.target.value })
                  }
                />
              </div>

              <Button
                tapScale={0.975}
                classNameWrapper="mt-2"
                onClick={onSubmit}
                className="w-full"
                loading={true}
              >
                {isCheckoutNext
                  ? t("auth.split.cta_checkout", "登录并继续支付 →")
                  : t("login")}
              </Button>
            </div>

            <div className="mt-6 space-y-2 text-sm text-center text-muted-foreground">
              <div>
                {t("auth.no-account")}
                <a
                  className="ml-1 underline underline-offset-4 cursor-pointer hover:text-foreground"
                  onClick={() => router.navigate("/register")}
                >
                  {t("auth.register")}
                </a>
              </div>
              <div>
                {t("auth.forgot-password")}
                <a
                  className="ml-1 underline underline-offset-4 cursor-pointer hover:text-foreground"
                  onClick={() => router.navigate("/forgot")}
                >
                  {t("auth.reset-password")}
                </a>
              </div>
            </div>
          </div>
        </div>
      </div>
    </ScrollArea>
  );
}

function Auth() {
  return useDeeptrain ? <DeepAuth /> : <Login />;
}

export default Auth;
