// v0.6.1 — Home (/) is now a router gate, not a chat shell.
// Logic:
//   - !init                     → null   (auth state still hydrating)
//   - auth                      → <HomeDashboard /> (logged-in service home)
//   - !auth && skipWelcome      → redirect to /chat (anon trial path)
//   - else                      → <Welcome />        (logged-out marketing)
//
// /chat is now the dedicated chat route. Marketing window owns Welcome.tsx.

import { useEffect } from "react";
import { useSelector } from "react-redux";
import { useNavigate } from "react-router-dom";
import { selectAuthenticated, selectInit } from "@/store/auth.ts";
import Welcome from "@/routes/Welcome.tsx";
import { HomeDashboard } from "@/components/Dashboard/HomeDashboard.tsx";

function Home() {
  const auth = useSelector(selectAuthenticated);
  const init = useSelector(selectInit);
  const navigate = useNavigate();
  // Anonymous users who tapped "试一下" on Welcome have skip_welcome=1 in
  // localStorage. Redirect them to /chat so the dashboard isn't blocked
  // behind a Welcome flash and they get the chat UX they came for.
  const skipWelcome =
    typeof window !== "undefined" &&
    !!window.localStorage.getItem("skip_welcome");

  useEffect(() => {
    if (init && !auth && skipWelcome) {
      navigate("/chat", { replace: true });
    }
  }, [init, auth, skipWelcome, navigate]);

  // Hold render until auth state is known so logged-in users don't see a
  // Welcome flash. Spinner already mounts globally in App.tsx.
  if (!init) return null;

  if (auth) return <HomeDashboard />;

  // Anonymous skip_welcome users will redirect via the effect above.
  // Until the navigation lands, render nothing to avoid Welcome flash.
  if (skipWelcome) return null;

  return <Welcome />;
}

export default Home;
