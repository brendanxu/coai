import { Outlet, useLocation } from "react-router-dom";
import ErrorBoundary from "@/components/ErrorBoundary.tsx";
import "@/assets/pages/home.less";
import { Button } from "@/components/ui/button.tsx";
import {
  ChevronDown,
  MessageCircle,
  PackageCheck,
  Shield,
  Wallet,
  LibraryBig,
  User,
  BarChart2,
  Radio,
} from "lucide-react";
import React, { useEffect, useLayoutEffect } from "react";
import Icon from "@/components/utils/Icon.tsx";
import router from "@/router.tsx";
import { useTranslation } from "react-i18next";
import { cn } from "@/components/ui/lib/utils.ts";
import { useDispatch, useSelector } from "react-redux";
import {
  selectAdmin,
  selectAuthenticated,
  validateToken,
} from "@/store/auth.ts";
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
import { activeTheme } from "@/components/ThemeProvider.tsx";
// ChatFloating component removed in v0.21 (commit 582dea6 "delete dead route
// files (Chat/Generation/Model/Article/Methodology + ChatFloating)"). The
// PKG-N3 home dashboard reorient for 民宿 made the always-on floating chat
// surface obsolete.

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
  const auth = useSelector(selectAuthenticated);
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
      {/* PKG-N4 — /orders is auth-gated by <AuthRequired> in router.tsx, so
          mirror that here: only show the toolbar entry when the caller is
          actually logged in (same pattern as /admin below). */}
      {auth && (
        <BarItem icon={<PackageCheck />} path={`/orders`} name={"orders"} />
      )}
      {auth && (
        <BarItem icon={<BarChart2 />} path={`/usage`} name={"usage"} />
      )}
      {auth && (
        <BarItem icon={<Radio />} path={`/live`} name={"live"} />
      )}
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

  // SPA-navigation theme refresh (2026-05-09 — auto-switch era).
  // ThemeProvider's useLayoutEffect handles cold-load + theme-state-change
  // route-aware theme resolution (marketing → system, app → stored). But
  // its effect deps are [theme] only — it doesn't re-run on route change.
  // So when the user SPA-navigates from /chat (stored=dark) to / (marketing),
  // ThemeProvider's effect doesn't re-fire and the dark class stays on html.
  //
  // This effect bridges that gap: on entering a marketing route via SPA
  // navigation, dispatch an empty themeEvent to trigger ThemeProvider
  // logic equivalent. {persist: false} preserves the user's app-route
  // preference in localStorage.
  useLayoutEffect(() => {
    if (!marketing) return;
    // Re-resolve the theme via ThemeProvider's same logic by toggling
    // through activeTheme. We pass the user's explicit choice if any,
    // else 'system' (which then resolves via prefers-color-scheme).
    const prev = getMemory("theme");
    if (prev === "light" || prev === "dark" || prev === "system") {
      activeTheme(prev as "light" | "dark" | "system", { persist: false });
    } else {
      activeTheme("system", { persist: false });
    }
    // No cleanup — ThemeProvider's own effect handles transitions out.
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
    </ErrorBoundary>
  );
}

export default Home;
