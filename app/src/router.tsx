import {
  createBrowserRouter,
  Navigate,
  RouterProvider,
  useLocation,
  useNavigate,
} from "react-router-dom";
import Home from "./routes/Home.tsx";
import NotFound from "./routes/NotFound.tsx";
import Auth from "./routes/Auth.tsx";
import React, { Suspense, useEffect } from "react";
import { useDeeptrain } from "@/conf/env.ts";
import Register from "@/routes/Register.tsx";
import Forgot from "@/routes/Forgot.tsx";
import { lazyFactor } from "@/utils/loader.tsx";
import { useSelector } from "react-redux";
import { selectAdmin, selectAuthenticated, selectInit } from "@/store/auth.ts";
import Index from "@/routes/Index.tsx";
const Account = lazyFactor(() => import("@/routes/Account.tsx"));
const Pricing = lazyFactor(() => import("@/routes/Pricing.tsx"));
const Contact = lazyFactor(() => import("@/routes/Contact.tsx"));
// v0.11 design-restore — broader product surface (Token 套餐 + 服务市场 + 模型池)
const Pool = lazyFactor(() => import("@/routes/Pool.tsx"));
const TokenPlans = lazyFactor(() => import("@/routes/TokenPlans.tsx"));
const Services = lazyFactor(() => import("@/routes/Services.tsx"));
// v0.12 — legal pages (Tier 1 block 1)
const Privacy = lazyFactor(() => import("@/routes/Privacy.tsx"));
const Terms = lazyFactor(() => import("@/routes/Terms.tsx"));
// v0.12 Tier 1 block 2 — developer docs hub (single comprehensive page)
const Docs = lazyFactor(() => import("@/routes/Docs.tsx"));
// v0.12 Tier 1 block 4 — DIY agent runner (post-purchase)
const ServiceRun = lazyFactor(() => import("@/routes/ServiceRun.tsx"));

// v0.6 carbon — /dashboard kept (founder may use ESG narrative later);
// /methodology removed (long-form essay had no traffic and the Carbon
// surfaces that linked to it have been rewritten to drop the link).
const Dashboard = lazyFactor(() => import("@/routes/Dashboard.tsx"));

// v0.21 cleanup — /chat, /generate, /model, /article, /methodology all
// deleted. Two business lines (Token wholesale + Service market 民宿 SaaS)
// don't need a generic chat / image-gen / model-marketplace / writer /
// long-form methodology surface. See PKG-CLEANUP.

// PKG-5 — customer self-serve service order list (/orders).
const MyOrders = lazyFactor(() => import("@/routes/MyOrders.tsx"));

// PKG-N2 — single order detail (/orders/:order_no). Lands the customer
// self-serve loop: see what was delivered, request a refund, reorder.
const OrderDetail = lazyFactor(() => import("@/routes/OrderDetail.tsx"));

const AdminPage = lazyFactor(() => import("@/routes/Admin.tsx"));
const AdminSystem = lazyFactor(() => import("@/routes/admin/System.tsx"));
const AdminUsers = lazyFactor(() => import("@/routes/admin/Users.tsx"));
const AdminLogger = lazyFactor(() => import("@/routes/admin/Logger.tsx"));
// v0.12 Tier 1 block 5 — founder concierge workspace for 民宿 wedge
const AdminMansu = lazyFactor(() => import("@/routes/admin/Mansu.tsx"));
// PKG-4 (architecture §19) — per-channel routing admin pages (per-user routing merged into GtkAdmin).
const AdminChannelsRouting = lazyFactor(
  () => import("@/routes/admin/AdminChannels.tsx"),
);
// PKG-N1 — admin order management (/admin/orders). Closes the
// "founder uses curl to mark concierge orders paid" gap from the audit.
const AdminOrders = lazyFactor(() => import("@/routes/admin/AdminOrders.tsx"));
// Unified GTK admin panel (users + model-ratios + settings + sub2api hub).
const GtkAdmin = lazyFactor(() => import("@/routes/admin/GtkAdmin.tsx"));

// PKG-A-3 Wave 2 — user-side personal API token management
const TokensPage = lazyFactor(() => import("@/routes/setting/Tokens.tsx"));

// Phase 3+4 — checkout / payment-success / usage / live
const Checkout = lazyFactor(() => import("@/routes/Checkout.tsx"));
const PaymentSuccess = lazyFactor(() => import("@/routes/PaymentSuccess.tsx"));
const UsagePage = lazyFactor(() => import("@/routes/Usage.tsx"));
const LivePage = lazyFactor(() => import("@/routes/Live.tsx"));

const router = createBrowserRouter([
  {
    id: "index",
    path: "/",
    Component: Index,
    ErrorBoundary: NotFound,
    children: [
      {
        id: "not-found",
        path: "*",
        element: <NotFound />,
      },
      {
        id: "home",
        path: "",
        element: <Home />,
      },
      // /wallet route removed (excise-1c-sweep — Wallet.tsx deleted)
      // {
      //   id: "log",
      //   path: "log",
      //   element: (
      //     <Suspense>
      //       <License />
      //     </Suspense>
      //   ),
      // },
      // {
      //   id: "preset",
      //   path: "preset",
      //   element: (
      //     <Suspense>
      //       <Preset />
      //     </Suspense>
      //   ),
      // },
      // {
      //   id: "key",
      //   path: "key",
      //   element: (
      //     <Suspense>
      //       <License />
      //     </Suspense>
      //   ),
      // },
      {
        id: "account",
        path: "account",
        element: (
          <Suspense>
            <Account />
          </Suspense>
        ),
      },
      // v0.6 carbon routes
      {
        id: "dashboard",
        path: "dashboard",
        element: (
          <AuthRequired>
            <Suspense>
              <Dashboard />
            </Suspense>
          </AuthRequired>
        ),
      },
      // PKG-5 — /orders. Customer-scoped service order list.
      // Backend: GET /gtk/v1/orders + GET /gtk/v1/orders/:order_no.
      {
        id: "my-orders",
        path: "orders",
        element: (
          <AuthRequired>
            <Suspense>
              <MyOrders />
            </Suspense>
          </AuthRequired>
        ),
      },
      // PKG-N2 — /orders/:order_no. Single order detail page (customer
      // self-serve refund/reorder, run progress polling).
      // Backend: GET/POST /gtk/v1/orders/:order_no/...
      {
        id: "order-detail",
        path: "orders/:order_no",
        element: (
          <AuthRequired>
            <Suspense>
              <OrderDetail />
            </Suspense>
          </AuthRequired>
        ),
      },
      // Phase 3+4 — /checkout (auth-gated payment confirmation)
      {
        id: "checkout",
        path: "checkout",
        element: (
          <AuthRequired>
            <Suspense>
              <Checkout />
            </Suspense>
          </AuthRequired>
        ),
      },
      // Phase 3+4 — /payment/success (public, reads auth)
      {
        id: "payment-success",
        path: "payment/success",
        element: (
          <Suspense>
            <PaymentSuccess />
          </Suspense>
        ),
      },
      // Phase 3+4 — /usage (auth-gated usage analytics)
      {
        id: "usage",
        path: "usage",
        element: (
          <AuthRequired>
            <Suspense>
              <UsagePage />
            </Suspense>
          </AuthRequired>
        ),
      },
      // Phase 3+4 — /live (auth-gated realtime token flow)
      {
        id: "live",
        path: "live",
        element: (
          <AuthRequired>
            <Suspense>
              <LivePage />
            </Suspense>
          </AuthRequired>
        ),
      },
      // PKG-A-3 — /setting/tokens (personal API token management)
      {
        id: "setting-tokens",
        path: "setting/tokens",
        element: (
          <AuthRequired>
            <Suspense>
              <TokensPage />
            </Suspense>
          </AuthRequired>
        ),
      },
      {
        id: "pricing",
        path: "pricing",
        element: (
          <Suspense>
            <Pricing />
          </Suspense>
        ),
      },
      // v0.10.2 — public lead-capture page (民宿主 demo 预约)
      {
        id: "contact",
        path: "contact",
        element: (
          <Suspense>
            <Contact />
          </Suspense>
        ),
      },
      // v0.11 design-restore — Token 套餐 / 服务市场 / 模型池 routes.
      // /services/mansu deep-link goes to legacy /pricing (民宿 detail page).
      {
        id: "pool",
        path: "pool",
        element: (
          <Suspense>
            <Pool />
          </Suspense>
        ),
      },
      {
        id: "token-plans",
        path: "token-plans",
        element: (
          <Suspense>
            <TokenPlans />
          </Suspense>
        ),
      },
      {
        id: "services",
        path: "services",
        element: (
          <Suspense>
            <Services />
          </Suspense>
        ),
      },
      {
        id: "services-mansu",
        path: "services/mansu",
        element: (
          <Suspense>
            <Pricing />
          </Suspense>
        ),
      },
      // v0.12 Tier 1 block 1 — public legal pages (ICP filing prerequisite,
      // payment platform requirement, customer trust)
      {
        id: "privacy",
        path: "privacy",
        element: (
          <Suspense>
            <Privacy />
          </Suspense>
        ),
      },
      {
        id: "terms",
        path: "terms",
        element: (
          <Suspense>
            <Terms />
          </Suspense>
        ),
      },
      // v0.12 Tier 1 block 2 — developer docs (publicly reachable; not
      // in primary nav since marketing audience is 民宿主, not devs)
      {
        id: "docs",
        path: "docs",
        element: (
          <Suspense>
            <Docs />
          </Suspense>
        ),
      },
      // v0.12 Tier 1 block 4 — DIY agent runner. order_no carries
      // the order identity; backend may not be live at v0.12 ship —
      // the page falls back to a demo placeholder on 404.
      {
        id: "service-run",
        path: "services/run/:order_no",
        element: (
          <Suspense>
            <ServiceRun />
          </Suspense>
        ),
      },
      {
        id: "login",
        path: "/login",
        element: (
          <AuthForbidden>
            <Auth />
          </AuthForbidden>
        ),
        ErrorBoundary: NotFound,
      },
      {
        id: "admin",
        path: "/admin",
        element: (
          <AdminRequired>
            <Suspense>
              <AdminPage />
            </Suspense>
          </AdminRequired>
        ),
        children: [
          {
            id: "admin-dashboard",
            path: "",
            element: <Navigate to="/admin/gtk" replace />,
          },
          {
            id: "admin-users",
            path: "users",
            element: (
              <Suspense>
                <AdminUsers />
              </Suspense>
            ),
          },
          {
            id: "admin-system",
            path: "system",
            element: (
              <Suspense>
                <AdminSystem />
              </Suspense>
            ),
          },
          {
            id: "admin-logger",
            path: "logger",
            element: (
              <Suspense>
                <AdminLogger />
              </Suspense>
            ),
          },
          // v0.12 Tier 1 block 5 — 民宿 concierge workspace (founder-only)
          {
            id: "admin-mansu",
            path: "mansu",
            element: (
              <Suspense>
                <AdminMansu />
              </Suspense>
            ),
          },
          // /admin/channels-routing — PKG-4 per-channel routing admin.
          {
            id: "admin-channels-routing",
            path: "channels-routing",
            element: (
              <Suspense>
                <AdminChannelsRouting />
              </Suspense>
            ),
          },
          // PKG-N1 — service order management (mark-paid / refund) UI.
          {
            id: "admin-orders",
            path: "orders",
            element: (
              <Suspense>
                <AdminOrders />
              </Suspense>
            ),
          },
        ],
        ErrorBoundary: NotFound,
      },
      // Unified GTK admin panel — standalone, no CoAI admin shell.
      {
        id: "admin-gtk",
        path: "/admin/gtk",
        element: (
          <AdminRequired>
            <Suspense>
              <GtkAdmin />
            </Suspense>
          </AdminRequired>
        ),
      },
      ...(useDeeptrain
        ? []
        : [
            {
              id: "register",
              path: "/register",
              element: (
                <AuthForbidden>
                  <Register />
                </AuthForbidden>
              ),
              ErrorBoundary: NotFound,
            },
            {
              id: "forgot",
              path: "/forgot",
              element: (
                <AuthForbidden>
                  <Forgot />
                </AuthForbidden>
              ),
              ErrorBoundary: NotFound,
            },
          ]),
    ],
  },
  // /share/:hash route removed (excise-1c-sweep — Sharing.tsx deleted)
]);

export function AuthRequired({ children }: { children: React.ReactNode }) {
  const init = useSelector(selectInit);
  const authenticated = useSelector(selectAuthenticated);
  const navigate = useNavigate();
  const location = useLocation();

  useEffect(() => {
    if (init && !authenticated) {
      navigate("/login", { state: { from: location.pathname } });
    }
  }, [init, authenticated]);

  return <>{children}</>;
}

export function AuthForbidden({ children }: { children: React.ReactNode }) {
  const init = useSelector(selectInit);
  const authenticated = useSelector(selectAuthenticated);
  const navigate = useNavigate();
  const location = useLocation();

  useEffect(() => {
    if (init && authenticated) {
      navigate("/", { state: { from: location.pathname } });
    }
  }, [init, authenticated]);

  return <>{children}</>;
}

export function AdminRequired({ children }: { children: React.ReactNode }) {
  const init = useSelector(selectInit);
  const admin = useSelector(selectAdmin);
  const navigate = useNavigate();
  const location = useLocation();

  useEffect(() => {
    if (init && !admin) {
      navigate("/", { state: { from: location.pathname } });
    }
  }, [init, admin]);

  return <>{children}</>;
}

export function AppRouter() {
  return <RouterProvider router={router} />;
}

export default router;
