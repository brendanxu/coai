import {
  createBrowserRouter,
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
import License from "@/routes/admin/License.tsx";

const Model = lazyFactor(() => import("@/routes/Model.tsx"));
const Wallet = lazyFactor(() => import("@/routes/Wallet.tsx"));
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

const Generation = lazyFactor(() => import("@/routes/Generation.tsx"));
const Sharing = lazyFactor(() => import("@/routes/Sharing.tsx"));
const Article = lazyFactor(() => import("@/routes/Article.tsx"));

// v0.6 carbon
const Dashboard = lazyFactor(() => import("@/routes/Dashboard.tsx"));
const Methodology = lazyFactor(() => import("@/routes/Methodology.tsx"));

// v0.6.1 — dedicated /chat route (chat moved off /)
const Chat = lazyFactor(() => import("@/routes/Chat.tsx"));

const AdminPage = lazyFactor(() => import("@/routes/Admin.tsx"));
const AdminDashboard = lazyFactor(() => import("@/routes/admin/DashBoard.tsx"));
const AdminMarket = lazyFactor(() => import("@/routes/admin/Market.tsx"));
const AdminChannel = lazyFactor(() => import("@/routes/admin/Channel.tsx"));
const AdminSystem = lazyFactor(() => import("@/routes/admin/System.tsx"));
const AdminLicense = lazyFactor(() => import("@/routes/admin/License.tsx"));
const AdminCharge = lazyFactor(() => import("@/routes/admin/Charge.tsx"));
const AdminUsers = lazyFactor(() => import("@/routes/admin/Users.tsx"));
const AdminBroadcast = lazyFactor(() => import("@/routes/admin/Broadcast.tsx"));
const AdminSubscription = lazyFactor(
  () => import("@/routes/admin/Subscription.tsx"),
);
const AdminLogger = lazyFactor(() => import("@/routes/admin/Logger.tsx"));
// v0.12 Tier 1 block 5 — founder concierge workspace for 民宿 wedge
const AdminMansu = lazyFactor(() => import("@/routes/admin/Mansu.tsx"));

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
      // v0.6.1 — dedicated chat route. Anonymous users with skip_welcome=1
      // are redirected here from Home. Logged-in users land on the dashboard
      // and reach chat via the ToolBar icon, dashboard CTA, or floating button.
      {
        id: "chat",
        path: "chat",
        element: (
          <Suspense>
            <Chat />
          </Suspense>
        ),
      },
      {
        id: "model",
        path: "model",
        element: (
          <Suspense>
            <Model />
          </Suspense>
        ),
      },
      // /wallet route — hidden when HIDE_CREDIT_UI=true (greentokey BYOK has
      // no internal credit/quota model). Direct access to /wallet falls through
      // to the catch-all NotFound. Restore by setting VITE_HIDE_CREDIT_UI=false.
      ...(import.meta.env.VITE_HIDE_CREDIT_UI === "false"
        ? [
            {
              id: "wallet",
              path: "wallet",
              element: (
                <Suspense>
                  <Wallet />
                </Suspense>
              ),
            },
          ]
        : []),
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
      {
        id: "methodology",
        path: "methodology",
        element: (
          <Suspense>
            <Methodology />
          </Suspense>
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
            element: (
              <Suspense>
                <AdminDashboard />
              </Suspense>
            ),
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
            id: "admin-market",
            path: "market",
            element: (
              <Suspense>
                <AdminMarket />
              </Suspense>
            ),
          },
          {
            id: "admin-channel",
            path: "channel",
            element: (
              <Suspense>
                <AdminChannel />
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
            id: "admin-warm-up",
            path: "warmup",
            element: (
              <Suspense>
                <License />
              </Suspense>
            ),
          },
          {
            id: "admin-license",
            path: "license",
            element: (
              <Suspense>
                <AdminLicense />
              </Suspense>
            ),
          },
          {
            id: "admin-charge",
            path: "charge",
            element: (
              <Suspense>
                <AdminCharge />
              </Suspense>
            ),
          },
          {
            id: "admin-broadcast",
            path: "broadcast",
            element: (
              <Suspense>
                <AdminBroadcast />
              </Suspense>
            ),
          },
          {
            id: "admin-subscription",
            path: "subscription",
            element: (
              <Suspense>
                <AdminSubscription />
              </Suspense>
            ),
          },
          {
            id: "admin-record",
            path: "record",
            element: (
              <Suspense>
                <License />
              </Suspense>
            ),
          },
          {
            id: "admin-payment",
            path: "pay",
            element: (
              <Suspense>
                <License />
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
        ],
        ErrorBoundary: NotFound,
      },
      {
        id: "generation",
        path: "/generate",
        element: (
          <AuthRequired>
            <Suspense>
              <Generation />
            </Suspense>
          </AuthRequired>
        ),
        ErrorBoundary: NotFound,
      },
      {
        id: "article",
        path: "/article",
        element: (
          <AuthRequired>
            <Suspense>
              <Article />
            </Suspense>
          </AuthRequired>
        ),
        ErrorBoundary: NotFound,
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
  {
    id: "share",
    path: "/share/:hash",
    element: (
      <Suspense>
        <Sharing />
      </Suspense>
    ),
    ErrorBoundary: NotFound,
  },
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
