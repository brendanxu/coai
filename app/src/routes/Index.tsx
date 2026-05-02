import { Outlet, useLocation } from "react-router-dom";
import ErrorBoundary from "@/components/ErrorBoundary.tsx";
import "@/assets/pages/home.less";
import { Button } from "@/components/ui/button.tsx";
import {
  ChevronDown,
  MessageCircle,
  Shield,
  Wallet,
  LibraryBig,
  User,
} from "lucide-react";
import React from "react";
import Icon from "@/components/utils/Icon.tsx";
import router from "@/router.tsx";
import { useTranslation } from "react-i18next";
import { cn } from "@/components/ui/lib/utils.ts";
import { useSelector } from "react-redux";
import { selectAdmin } from "@/store/auth.ts";
import {
  hideToolbarSelector,
  hideToolbarTextSelector,
} from "@/store/settings.ts";
import { isMobile, useMobile } from "@/utils/device.ts";
import {
  Tooltip,
  TooltipContent,
  TooltipProvider,
  TooltipTrigger,
} from "@/components/ui/tooltip.tsx";
import NavBar from "@/components/app/NavBar.tsx";
import { HIDE_CREDIT_UI } from "@/conf/env.ts";
// v0.6.1 — global floating chat must mount inside RouterProvider so it can
// call useLocation. Index.tsx is the layout that wraps every child route,
// making it the natural mount point.
import { ChatFloating } from "@/components/ChatFloating/index.tsx";

type BarItemProps = {
  icon: React.ReactElement;
  path: string;
  name: string;
};

function isPrefix(current: string, path: string): boolean {
  if (location.pathname === path) return true;
  if (location.pathname + "/" === path) return true;

  return path.length > 1 && current.startsWith(path + "/");
}

function BarItem({ icon, path, name }: BarItemProps) {
  const { t } = useTranslation();
  const location = useLocation();
  const active = isPrefix(location.pathname, path);

  const hidden = useSelector(hideToolbarTextSelector);
  const mobile = useMobile();

  const [open, setOpen] = React.useState(false);

  const onClick = async () => {
    await router.navigate(path);
  };

  return (
    <div className={`inline-flex flex-col`}>
      <TooltipProvider delayDuration={100}>
        <Tooltip open={open} onOpenChange={setOpen}>
          <TooltipTrigger asChild>
            <Button
              size="icon"
              variant={active ? "default" : "outline"}
              onClick={onClick}
            >
              <Icon icon={icon} className="h-4 w-4 stroke-[1.75]" />
            </Button>
          </TooltipTrigger>
          <TooltipContent
            side={mobile ? "top" : "right"}
            align="center"
            className={`z-[100]`}
          >
            {t(`bar.${name}`)}
          </TooltipContent>
        </Tooltip>
      </TooltipProvider>
      <div
        className={cn(
          `toolbar-text text-secondary text-center text-xs mt-1.5 cursor-pointer select-none`,
          active && `text-common`,
          hidden && `hidden`,
        )}
        onClick={onClick}
      >
        {t(`bar.${name}`)}
      </div>
    </div>
  );
}

function ToolBar() {
  const admin = useSelector(selectAdmin);
  const hideToolbar = useSelector(hideToolbarSelector);
  const [stacked, setStacked] = React.useState(hideToolbar || isMobile());

  return (
    <div className={cn("toolbar", stacked && "stacked")}>
      <div
        className={cn("bar-kit", stacked && "stacked")}
        onClick={(e) => {
          e.stopPropagation();
          e.preventDefault();
          setStacked(!stacked);
        }}
      >
        <ChevronDown className={`h-3.5 w-3.5`} />
      </div>
      {/* v0.6.1 — chat moved off /. The toolbar's "chat" affordance now
          targets /chat directly. Without this, clicking the message-circle
          icon would land on the dashboard (the page you came from). */}
      <BarItem icon={<MessageCircle />} path={`/chat`} name={"chat"} />
      <BarItem icon={<LibraryBig />} path={`/model`} name={"model"} />
      {/* <BarItem icon={<Compass />} path={`/preset`} name={"preset"} /> */}
      {!HIDE_CREDIT_UI && (
        <BarItem icon={<Wallet />} path={`/wallet`} name={"wallet"} />
      )}
      {/* <BarItem icon={<DraftingCompass />} path={`/key`} name={"key"} /> */}
      {/* <BarItem icon={<PieChart />} path={`/log`} name={"log"} /> */}
      <BarItem icon={<User />} path={`/account`} name={"account"} />
      {admin && <BarItem icon={<Shield />} path={`/admin`} name={"admin"} />}
    </div>
  );
}

// MARKETING_ROUTES — public-facing pages owned by the v0.7 design system.
// These render without CoAI's NavBar / ToolBar so 民宿主 don't see app
// cruft (chat / model / account icons that mean nothing to them).
//
// Each marketing page wraps its own content in <Header /> + <Footer />
// from components/Marketing/. The CoAI sidebar + top bar are reserved
// for app routes (chat / model / account / dashboard / admin).
//
// Add a new public marketing path here when adding /privacy /terms /
// /about / /blog etc. — keeps the rule single-source.
const MARKETING_PATHS = new Set([
  "/",
  "/pricing",
  "/contact",
  "/privacy",
  "/terms",
  "/about",
]);

function isMarketingRoute(pathname: string): boolean {
  return MARKETING_PATHS.has(pathname);
}

function Home() {
  const location = useLocation();
  const marketing = isMarketingRoute(location.pathname);

  if (marketing) {
    // Pure marketing layout — NO NavBar, NO ToolBar, NO floating chat.
    // The route's own Header + Footer (from components/Marketing/) own
    // the entire viewport. Customers see a coherent marketing site.
    return (
      <ErrorBoundary>
        <Outlet />
      </ErrorBoundary>
    );
  }

  // App layout — CoAI's full chrome (top bar + left sidebar + floating
  // chat). Reserved for /chat /model /account /dashboard /admin/*.
  return (
    <ErrorBoundary>
      <NavBar />
      <div className={`main relative`}>
        <ToolBar />
        <Outlet />
      </div>
      {/* v0.6.1 — global floating chat. Renders on every app route.
          Auto-hides on /chat where the full chat UI is already on screen. */}
      <ChatFloating />
    </ErrorBoundary>
  );
}

export default Home;
