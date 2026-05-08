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
import React, { useEffect, useLayoutEffect } from "react";
import Icon from "@/components/utils/Icon.tsx";
import router from "@/router.tsx";
import { useTranslation } from "react-i18next";
import { cn } from "@/components/ui/lib/utils.ts";
import { useDispatch, useSelector } from "react-redux";
import { selectAdmin, validateToken } from "@/store/auth.ts";
import { tokenField } from "@/conf/bootstrap.ts";
import { getMemory } from "@/utils/memory.ts";
import type { AppDispatch } from "@/store/index.ts";
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
import { activeTheme, getTheme } from "@/components/ThemeProvider.tsx";
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
  // v0.11 design-restore additions
  "/pool",
  "/token-plans",
  "/services",
  "/services/mansu",
]);

function isMarketingRoute(pathname: string): boolean {
  return MARKETING_PATHS.has(pathname);
}

function Home() {
  const location = useLocation();
  const dispatch: AppDispatch = useDispatch();
  const marketing = isMarketingRoute(location.pathname);

  // Auth state hydration on app start. Was previously bootstrapped from
  // NavBar.tsx's useEffect, but marketing routes don't render NavBar
  // anymore, so the gate at routes/Home.tsx (returns null until init=true)
  // would hang forever. Call validateToken here so init flips to true
  // regardless of which layout branch we render. Idempotent — repeats
  // are harmless.
  useEffect(() => {
    validateToken(dispatch, getMemory(tokenField));
  }, [dispatch]);

  // v0.11 — force light theme on marketing routes via the existing
  // ThemeProvider plumbing. CoAI's defaultTheme is "dark"; visitors
  // with no localStorage land on dark, which breaks the v0.7 light-first
  // Claude Design (warm cream + ink umber + moss accent). On marketing
  // exit, restore whatever the user had before (or system default).
  //
  // Use activeTheme() so the .light/.dark class on documentElement +
  // memory + themeEvent stay in sync — manual class swaps left the
  // ThemeProvider out of sync on hot navigation.
  //
  // FOUC fix (2026-05-08): pass { persist: false } so a marketing visit
  // does NOT overwrite the user's app-route theme preference in
  // localStorage. Cold loads are also gated by index.html's inline
  // script, which sets the .light class before React mounts. This
  // useLayoutEffect handles SPA navigation (app → marketing) without
  // an inter-paint flash.
  useLayoutEffect(() => {
    if (!marketing) return;
    const prev = getTheme();
    activeTheme("light", { persist: false });
    return () => {
      activeTheme(prev, { persist: false });
    };
  }, [marketing]);

  if (marketing) {
    // Pure marketing layout — NO NavBar, NO ToolBar, NO floating chat.
    // The route's own Header + Footer (from components/Marketing/) own
    // the entire viewport. Customers see a coherent marketing site.
    //
    // Wrapper provides the full viewport sizing that .main used to
    // contribute (height/width). Marketing pages use flex-col so
    // Footer can sit at bottom of short pages.
    return (
      <ErrorBoundary>
        <div className="flex flex-col min-h-screen w-full">
          <Outlet />
        </div>
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
