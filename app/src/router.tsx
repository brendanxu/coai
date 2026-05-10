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

const Generation = lazyFactor(() => import("@/routes/Generation.tsx"));
const Sharing = lazyFactor(() => import("@/routes/Sharing.tsx"));
const Article = lazyFactor(() => import("@/routes/Article.tsx"));

// v0.6 carbon
const Dashboard = lazyFactor(() => import("@/routes/Dashboard.tsx"));
const Methodology = lazyFactor(() => import("@/routes/Methodology.tsx"));

// v0.6.1 — dedicated /chat route (chat moved off /)
const Chat = lazyFactor(() => import("@/routes/Chat.tsx"));

// PKG-5 — customer self-serve service order list (/orders).
const MyOrders = lazyFactor(() => import("@/routes/MyOrders.tsx"));

// PKG-N2 — single order detail (/orders/:order_no). Lands the customer
// self-serve loop: see what was delivered, request a refund, reorder.
const OrderDetail = lazyFactor(() => import("@/routes/OrderDetail.tsx"));

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
// PKG-4 (architecture §19) — per-user channel routing admin pages.
const AdminUserRouting = lazyFactor(
  () => import("@/routes/admin/UserRouting.tsx"),
);
const AdminChannelsRouting = lazyFactor(
  () => import("@/routes/admin/AdminChannels.tsx"),
);

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
          // PKG-4 (architecture §19): per-user channel routing admin.
          {
            id: "admin-user-routing",
            path: "user-routing",
            element: (
              <Suspense>
                <AdminUserRouting />
              </Suspense>
            ),
          },
          // /admin/channels-routing (NOT /admin/channels — that path is
          // unused but the singular /admin/channel is taken by the
          // CoAI-upstream channel admin page).
          {
            id: "admin-channels-routing",
            path: "channels-routing",
            element: (
              <Suspense>
                <AdminChannelsRouting />
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
