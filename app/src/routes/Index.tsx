import { Outlet, useLocation } from "react-router-dom";
import ErrorBoundary from "@/components/ErrorBoundary.tsx";
import "@/assets/pages/home.less";
import { useEffect, useLayoutEffect } from "react";
import { useDispatch } from "react-redux";
import { validateToken } from "@/store/auth.ts";
import { tokenField } from "@/conf/bootstrap.ts";
import { getMemory } from "@/utils/memory.ts";
import type { AppDispatch } from "@/store/index.ts";
import NavBar from "@/components/app/NavBar.tsx";
import { activeTheme } from "@/components/ThemeProvider.tsx";
// ChatFloating component removed in v0.21 (commit 582dea6 "delete dead route
// files (Chat/Generation/Model/Article/Methodology + ChatFloating)"). The
// PKG-N3 home dashboard reorient for 民宿 made the always-on floating chat
// surface obsolete.
// ToolBar (vertical icon column) removed in PKG-INDEX-TOOLBAR-EXCISE — all
// targets (chat/model/wallet/orders/usage/live/account/admin) pointed to dead
// or post-Phase-1 routes. Customer nav consolidates on NavBar top bar.

// MARKETING_ROUTES — public-facing pages owned by the v0.7 design system.
// These render without CoAI's NavBar so 民宿主 don't see app
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

  // App layout — NavBar top bar + routed content.
  // Reserved for /chat /model /account /dashboard /admin/*.
  // ToolBar (vertical icon column) removed in PKG-INDEX-TOOLBAR-EXCISE.
  return (
    <ErrorBoundary>
      <NavBar />
      <div className={`main relative`}>
        <Outlet />
      </div>
    </ErrorBoundary>
  );
}

export default Home;
