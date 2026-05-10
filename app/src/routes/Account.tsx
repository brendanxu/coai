import "@/assets/pages/package.less";
import { ScrollArea } from "@/components/ui/scroll-area.tsx";
import React, { useState } from "react";
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

              <Button
                variant={`outline`}
                size={`default-sm`}
                className={`text-xs`}
                asChild
              >
                <a href={docsEndpoint} target={`_blank`}>
                  <ExternalLink className={`h-3.5 w-3.5 mr-2`} />
                  {t("api.learn-more")}
                </a>
              </Button>
            </div>
          </div>
        </AccountCard>
        {/* v0.21 cleanup — "Shared conversations" card removed. Sharing was a
            chat-history feature; with /chat gone there is nothing to share.
            Token wholesale customers still have API key management above;
            service market customers manage orders via /orders. */}
      </div>
    </ScrollArea>
  );
}

export default Account;
