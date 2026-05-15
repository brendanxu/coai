import "@/assets/pages/package.less";
import { ScrollArea } from "@/components/ui/scroll-area.tsx";
import React, { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import axios from "axios";
import { cn } from "@/components/ui/lib/utils.ts";
import Avatar from "@/components/Avatar.tsx";
import { useDispatch, useSelector } from "react-redux";
import {
  logout,
  selectAuthenticated,
  selectInit,
  selectUsername,
} from "@/store/auth.ts";
import { Badge } from "@/components/ui/badge.tsx";
import { copyClipboard, useClipboard } from "@/utils/dom.ts";
import { useGroup } from "@/utils/groups.ts";
import { useTranslation } from "react-i18next";
import Icon from "@/components/utils/Icon.tsx";
import {
  CalendarClock,
  Cloud,
  CloudRain,
  Copy,
  ExternalLink,
  HandIcon,
  Plug,
  Power,
  RotateCw,
  Undo2,
  UserRoundCog,
  UserRoundIcon,
} from "lucide-react";
import { Button } from "@/components/ui/button.tsx";
import { useEffectAsync } from "@/utils/hook.ts";
import {
  getUserInfo,
  initialUserInfo,
  UserInfo,
} from "@/api/auth.ts";
import { CommonResponse, withNotify } from "@/api/common.ts";
import { goAuth } from "@/utils/app.ts";
import { quotaSelector } from "@/store/quota.ts";
import Tips from "@/components/Tips.tsx";
import { openWindow } from "@/utils/device.ts";
import { DeeptrainOnly } from "@/conf/deeptrain.tsx";
import { deeptrainEndpoint, docsEndpoint, HIDE_CREDIT_UI } from "@/conf/env.ts";
import { getApiKey, keySelector, regenerateApiKey } from "@/store/api.ts";
import { Input } from "@/components/ui/input.tsx";
import {
  AlertDialog,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog.tsx";
import { toast } from "sonner";

type AccountCardProps = {
  title: string;
  description: string;
  icon?: React.ReactElement;
  children: React.ReactNode;
  footer?: React.ReactNode;
  className?: string;
  classNameWrapper?: string;
};

function AccountCard({
  title,
  description,
  icon,
  children,
  footer,
  className,
  classNameWrapper,
}: AccountCardProps) {
  const { t } = useTranslation();

  return (
    <div
      className={cn(
        `flex flex-col bg-background rounded-lg shadow border overflow-hidden`,
        classNameWrapper,
      )}
    >
      <div
        className={`select-none inline-flex flex-row items-center h-fit w-full border-b px-4 py-2.5 bg-muted/20`}
      >
        <div className="flex items-center mr-2.5">
          {icon && (
            <Icon
              icon={icon}
              className="w-8 h-8 p-2 rounded-lg bg-muted text-secondary"
            />
          )}
        </div>
        <div className="flex flex-col">
          <p className="text-sm font-medium">{t(title)}</p>
          {description && (
            <p className="text-xs text-secondary">{t(description)}</p>
          )}
        </div>
      </div>
      <div className={cn("p-4", className)}>{children}</div>
      {footer && (
        <div className={`flex flex-row items-center px-4 pb-4 pt-2`}>
          {footer}
        </div>
      )}
    </div>
  );
}


// ─── CredentialsHero ─────────────────────────────────────────────────
// Phase 2 §3.2 — credentials hero card injected at the TOP of Account page.
// Fetches /gtk/v1/binding for the API key. If 404 (no plan), shows
// "购买 Token 套餐后自动生成" placeholder.

type BindingData = {
  api_key?: string;
};

type QuickStartTab = "openai" | "claudecode";

function CredentialsHero() {
  const { t } = useTranslation();
  const [apiKey, setApiKey] = useState<string | null | "loading">("loading");
  const [activeTab, setActiveTab] = useState<QuickStartTab>("openai");
  const [copiedField, setCopiedField] = useState<string | null>(null);

  const BASE_URL = "https://api.greentokey.com/v1";

  useEffect(() => {
    let mounted = true;
    axios
      .get<{ success: boolean; data?: BindingData }>("/gtk/v1/binding")
      .then((r) => {
        if (!mounted) return;
        setApiKey(r.data?.data?.api_key ?? null);
      })
      .catch(() => {
        if (mounted) setApiKey(null);
      });
    return () => {
      mounted = false;
    };
  }, []);

  const copyText = async (text: string, field: string) => {
    try {
      await navigator.clipboard.writeText(text);
      setCopiedField(field);
      setTimeout(() => setCopiedField(null), 1500);
      toast.success(t("api.copied", "已复制"));
    } catch {
      toast.error(t("token.quickstart.copy-failed", "复制失败，请手动选择"));
    }
  };

  const displayKey =
    apiKey && apiKey !== "loading"
      ? `${apiKey.slice(0, 8)}${"•".repeat(8)}${apiKey.slice(-4)}`
      : null;

  const snippets: Record<QuickStartTab, { label: string; code: string }> = {
    openai: {
      label: t("token.quickstart.tab.openai", "OpenAI SDK · node.js"),
      code: `import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "${BASE_URL}",
  apiKey:  "${apiKey && apiKey !== "loading" ? displayKey : "sk-tnx-xxxxxx"}",
});

client.chat.completions.create({
  model: "gpt-4o",
  messages: [...],
});`,
    },
    claudecode: {
      label: t("token.quickstart.tab.claudecode", "Claude Code CLI"),
      code: `export ANTHROPIC_BASE_URL="https://api.greentokey.com/anthropic"
export ANTHROPIC_API_KEY="${apiKey && apiKey !== "loading" ? displayKey : "sk-tnx-xxxxxx"}"

# 重启 shell 后 Claude Code 直接用
claude "解释这段代码"`,
    },
  };

  return (
    <div
      className="rounded-2xl overflow-hidden mb-4"
      style={{
        background: "hsl(var(--ink))",
        border: "1px solid hsl(var(--ink))",
        boxShadow: "var(--shadow-ink)",
      }}
    >
      {/* Header */}
      <div className="px-6 pt-6 pb-4">
        <div className="flex items-center justify-between mb-4">
          <div>
            <p
              className="text-[10px] uppercase tracking-[0.16em] mb-1"
              style={{ color: "rgba(255,252,247,0.55)" }}
            >
              {t("account.credentials.eyebrow", "主线 · 接入")}
            </p>
            <h2
              className="font-display text-xl"
              style={{ color: "hsl(var(--ink-foreground))" }}
            >
              {t("account.credentials.title", "你的 greentokey 凭据")}
            </h2>
          </div>
        </div>

        {/* Base URL field */}
        <div className="space-y-3">
          <CredField
            label="Base URL"
            value={BASE_URL}
            displayValue={BASE_URL}
            onCopy={() => copyText(BASE_URL, "url")}
            copied={copiedField === "url"}
            hint={t("account.credentials.url_hint", "改这一行就能跑")}
          />

          {/* API Key field */}
          {apiKey === "loading" ? (
            <div
              className="h-14 rounded-xl animate-pulse"
              style={{ background: "rgba(255,252,247,0.06)" }}
            />
          ) : apiKey ? (
            <CredField
              label="API Key"
              value={apiKey}
              displayValue={displayKey ?? ""}
              onCopy={() => copyText(apiKey, "key")}
              copied={copiedField === "key"}
              hint={t("account.credentials.key_hint", "首次显示，请保存")}
              accent
            />
          ) : (
            <div
              className="rounded-xl px-4 py-3 text-sm"
              style={{
                background: "rgba(255,252,247,0.06)",
                border: "1px solid rgba(255,252,247,0.10)",
                color: "rgba(255,252,247,0.55)",
              }}
            >
              {t(
                "account.credentials.no_key",
                "购买 Token 套餐后自动生成 API Key",
              )}
            </div>
          )}
        </div>
      </div>

      {/* Quick start code tabs */}
      <div
        style={{ borderTop: "1px solid rgba(255,252,247,0.08)" }}
      >
        {/* Tab bar */}
        <div
          className="flex items-center border-b"
          style={{ borderColor: "rgba(255,252,247,0.08)" }}
        >
          {(["openai", "claudecode"] as QuickStartTab[]).map((k) => (
            <button
              key={k}
              type="button"
              onClick={() => setActiveTab(k)}
              className="px-4 py-3 text-xs font-mono border-b-2 transition-colors"
              style={{
                color:
                  activeTab === k
                    ? "hsl(var(--ink-foreground))"
                    : "rgba(255,252,247,0.45)",
                borderColor:
                  activeTab === k ? "hsl(var(--ink-accent))" : "transparent",
              }}
            >
              {snippets[k].label}
            </button>
          ))}
          <button
            type="button"
            onClick={() => copyText(snippets[activeTab].code, "snippet")}
            className="ml-auto mr-3 inline-flex items-center gap-1.5 px-3 py-1 rounded-full text-xs font-medium transition-opacity hover:opacity-80"
            style={{
              background: "rgba(255,252,247,0.08)",
              color: "rgba(255,252,247,0.8)",
            }}
          >
            <Copy className="w-3 h-3" />
            <span className="hidden md:inline">
              {copiedField === "snippet"
                ? t("token.quickstart.copied", "已复制")
                : "Copy"}
            </span>
          </button>
        </div>

        {/* Code block */}
        <pre
          className="px-5 py-4 text-xs font-mono leading-relaxed overflow-x-auto whitespace-pre"
          style={{ color: "hsl(var(--ink-foreground))" }}
        >
          <code>{snippets[activeTab].code}</code>
        </pre>
      </div>
    </div>
  );
}

function CredField({
  label,
  displayValue,
  onCopy,
  copied,
  hint,
  accent,
}: {
  label: string;
  displayValue: string;
  value: string; // consumed by caller's onCopy closure; not read inside component
  onCopy: () => void;
  copied: boolean;
  hint?: string;
  accent?: boolean;
}) {
  return (
    <div
      className="flex items-center gap-3 rounded-xl px-4 py-3"
      style={{
        background: "rgba(255,252,247,0.06)",
        border: "1px solid rgba(255,252,247,0.10)",
      }}
    >
      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span
            className="text-[10px] uppercase tracking-[0.12em]"
            style={{ color: "rgba(255,252,247,0.45)" }}
          >
            {label}
          </span>
          {hint && (
            <span
              className="text-[10px]"
              style={{ color: "rgba(255,252,247,0.35)" }}
            >
              · {hint}
            </span>
          )}
        </div>
        <p
          className="font-mono text-xs mt-0.5 truncate"
          style={{
            color: accent
              ? "hsl(var(--ink-accent))"
              : "hsl(var(--ink-foreground))",
          }}
        >
          {displayValue}
        </p>
      </div>
      <button
        type="button"
        onClick={onCopy}
        className="shrink-0 p-1.5 rounded-lg transition-colors"
        style={{
          background: "rgba(255,252,247,0.06)",
          border: "1px solid rgba(255,252,247,0.10)",
          color: copied ? "hsl(var(--ink-accent))" : "rgba(255,252,247,0.55)",
        }}
        aria-label={`Copy ${label}`}
      >
        <Copy className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}

// ─── Account ────────────────────────────────────────────────────────────

function Account() {
  const { t } = useTranslation();
  const dispatch = useDispatch();
  const init = useSelector(selectInit);
  const username = useSelector(selectUsername);
  const auth = useSelector(selectAuthenticated);
  const quota = useSelector(quotaSelector);
  const copy = useClipboard();
  const group = useGroup(true);

  const apiKey = useSelector(keySelector);
  const [loadingApiKey, setLoadingApiKey] = useState(false);
  const [openResetApiKey, setOpenResetApiKey] = useState(false);

  const getSystemKey = async () => {
    if (!init) return;

    setLoadingApiKey(true);
    await getApiKey(dispatch);
    setLoadingApiKey(false);
  };

  useEffectAsync(getSystemKey, [init]);

  async function copySystemKey() {
    await copyClipboard(apiKey);
    toast.success(t("api.copied"), {
      description: t("api.copied-description"),
    });
  }

  async function resetSystemKey() {
    const resp = await regenerateApiKey(dispatch);
    withNotify(t, resp as CommonResponse, true);

    if (resp.status) {
      setOpenResetApiKey(false);
    }
  }

  const [info, setInfo] = React.useState<UserInfo>({
    ...initialUserInfo,
  });

  const updateUserInfo = async () => {
    if (!auth) {
      return;
    }

    const resp = await getUserInfo();
    console.log(`[account api] get user info:`, resp);
    withNotify(t, resp);

    if (resp.status) {
      setInfo(resp.data);
    }
  };
  useEffectAsync(updateUserInfo, [auth]);

  return (
    <ScrollArea
      className={`relative w-full h-full flex flex-col bg-background`}
    >
      <div
        className={`px-4 py-6 md:py-12 lg:py-16 h-full flex flex-col w-full max-w-3xl mx-auto space-y-4`}
      >
        {/* Phase 2 §3.2 — credentials hero card injected at top */}
        <CredentialsHero />

        <AccountCard
          icon={<UserRoundIcon />}
          title={"account.my-account"}
          description={t("account.my-account-description")}
          footer={
            !auth ? (
              <Button
                classNameWrapper={`ml-auto`}
                className={`flex flex-row items-center`}
                onClick={goAuth}
              >
                <HandIcon className={`h-4 w-4 mr-1.5`} />
                {t("login")}
              </Button>
            ) : (
              <Button
                classNameWrapper={`ml-auto`}
                className={`flex flex-row items-center`}
                onClick={() => dispatch(logout())}
              >
                <Undo2 className={`h-4 w-4 mr-1.5`} />
                {t("logout")}
              </Button>
            )
          }
        >
          <div className="flex flex-col space-y-4">
            <div className="flex items-center space-x-4">
              <Avatar
                username={username}
                className="w-16 h-16 shrink-0 shadow text-lg rounded-full"
              />
              <div className="flex flex-row w-full">
                <div className="flex flex-col w-fit">
                  <p
                    className="text-xl font-semibold cursor-pointer select-none"
                    onClick={() => copy(username)}
                  >
                    {auth ? username : t("anonymous")}
                  </p>
                  <p className="text-sm text-muted-foreground">#{info.id}</p>
                </div>
              </div>
            </div>

            <div className="flex flex-wrap gap-2">
              <Badge className="px-3 py-1 text-sm font-medium">
                {t(`admin.channels.groups.${group}`)}
              </Badge>
              <Badge
                variant="outline"
                className="px-3 py-1 text-sm font-medium"
              >
                {t(`account.registerDays`, {
                  days: Math.ceil(info.register_days),
                })}
              </Badge>
            </div>
          </div>
          {!HIDE_CREDIT_UI && (
            <div className="mt-6 grid grid-cols-1 md:grid-cols-3 gap-4">
              <div className="bg-card shadow-sm rounded-lg p-4 transition-all border">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-sm font-medium text-muted-foreground">
                    {t("account.current-quota")}
                  </span>
                  <Cloud className="w-10 h-10 p-2 rounded-lg bg-muted/40 text-secondary stroke-[1]" />
                </div>
                <p className="text-md">{quota.toFixed(2)}</p>
              </div>
              <div className="bg-card shadow-sm rounded-lg p-4 transition-all border">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-sm font-medium text-muted-foreground">
                    {t("account.used-quota")}
                  </span>
                  <CloudRain className="w-10 h-10 p-2 rounded-lg bg-muted/40 text-secondary stroke-[1]" />
                </div>
                <p className="text-md">{info.used_quota.toFixed(2)}</p>
              </div>
              <div className="bg-card shadow-sm rounded-lg p-4 transition-all border">
                <div className="flex items-center justify-between mb-2">
                  <span className="text-sm font-medium text-muted-foreground">
                    {t("account.plan-total-month")}
                  </span>
                  <CalendarClock className="w-10 h-10 p-2 rounded-lg bg-muted/40 text-secondary stroke-[1]" />
                </div>
                <div className="flex items-center">
                  <p className="text-md mr-2">{info.plan_total_month}</p>
                  <Tips
                    className="text-muted-foreground hover:text-foreground transition-colors"
                    content={t("account.plan-total-month-tips")}
                  />
                </div>
              </div>
            </div>
          )}
        </AccountCard>
        <DeeptrainOnly>
          <AccountCard
            title={"account.deeptrain"}
            description={t("account.deeptrain-description")}
            icon={<UserRoundCog />}
            footer={
              auth ? (
                <Button
                  className={`flex flex-row items-center`}
                  classNameWrapper={`ml-auto`}
                  onClick={() => openWindow(`${deeptrainEndpoint}/home`)}
                >
                  <ExternalLink className={`h-4 w-4 mr-1.5`} />
                  {t("manage")}
                </Button>
              ) : (
                <Button classNameWrapper={`ml-auto`} onClick={goAuth}>
                  <HandIcon className={`h-4 w-4 mr-1.5`} />
                  {t("login")}
                </Button>
              )
            }
          >
            <div className={`flex flex-row items-center space-x-2`}>
              <img
                src={`${deeptrainEndpoint}/favicon.ico`}
                alt={``}
                className={`w-12 h-12 select-none cursor-pointer`}
                onClick={() => openWindow(`${deeptrainEndpoint}/home`)}
              />
              <div className={`inline-flex flex-col`}>
                <p className={`text-common text-sm font-bold`}>DeepTrain SSO</p>
                <p className={`text-secondary text-xs`}>
                  {t("account.deeptrain-description")}
                </p>
              </div>
            </div>
          </AccountCard>
        </DeeptrainOnly>
        <AccountCard
          title={"api.title"}
          description={t("account.api-description")}
          icon={<Plug />}
        >
          <div className={`api-dialog`}>
            {/* v0.12 block 3 — base_url hint above sk-key field. CoAI's
                default UX assumes devs read external docs; greentokey
                surfaces the one piece of config they always forget. */}
            <div className="mb-2 text-xs text-muted-foreground">
              {t("api.base-url-label", "Base URL")}
              <code className="ml-2 px-1.5 py-0.5 rounded bg-muted text-foreground font-mono">
                https://api.greentokey.com/v1
              </code>
            </div>
            <div className={`api-wrapper flex flex-row space-x-1`}>
              <Button
                variant={`outline`}
                size={`icon-sm`}
                className={`shrink-0`}
                onClick={getSystemKey}
              >
                <RotateCw
                  className={cn("h-3.5 w-3.5", loadingApiKey && "animate-spin")}
                />
              </Button>
              <Input
                type={`password`}
                value={apiKey}
                readOnly={true}
                classNameWrapper={`grow`}
                className={`text-xs h-8`}
              />
              <Button
                variant={`default`}
                className={`shrink-0`}
                size={`icon-sm`}
                onClick={copySystemKey}
              >
                <Copy className={`h-3.5 w-3.5`} />
              </Button>
            </div>
            <div className={`flex flex-row mt-2 items-center justify-center`}>
              <AlertDialog
                open={openResetApiKey}
                onOpenChange={setOpenResetApiKey}
              >
                <AlertDialogTrigger asChild>
                  <Button
                    variant={`destructive`}
                    size={`default-sm`}
                    className={`text-xs mr-2`}
                  >
                    <Power className={`h-3.5 w-3.5 mr-2`} />
                    {t("api.reset")}
                  </Button>
                </AlertDialogTrigger>
                <AlertDialogContent>
                  <AlertDialogHeader>
                    <AlertDialogTitle>{t("api.reset")}</AlertDialogTitle>
                    <AlertDialogDescription>
                      {t("api.reset-description")}
                    </AlertDialogDescription>
                  </AlertDialogHeader>
                  <AlertDialogFooter>
                    <Button
                      variant={`destructive`}
                      loading={true}
                      onClick={resetSystemKey}
                      unClickable
                    >
                      {t("confirm")}
                    </Button>
                    <AlertDialogCancel>{t("cancel")}</AlertDialogCancel>
                  </AlertDialogFooter>
                </AlertDialogContent>
              </AlertDialog>

              {/* v0.12 block 3 — internal Link for same-origin /docs;
                  external <a target=_blank> only when admin overrode
                  docsEndpoint to a hosted destination. */}
              <Button
                variant={`outline`}
                size={`default-sm`}
                className={`text-xs`}
                asChild
              >
                {docsEndpoint.startsWith("/") ? (
                  <Link to={docsEndpoint}>
                    <ExternalLink className={`h-3.5 w-3.5 mr-2`} />
                    {t("api.learn-more")}
                  </Link>
                ) : (
                  <a href={docsEndpoint} target={`_blank`} rel="noreferrer">
                    <ExternalLink className={`h-3.5 w-3.5 mr-2`} />
                    {t("api.learn-more")}
                  </a>
                )}
              </Button>
            </div>
          </div>
        </AccountCard>
      </div>
    </ScrollArea>
  );
}

export default Account;
