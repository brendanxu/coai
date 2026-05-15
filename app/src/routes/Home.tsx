// Home (/) is a router gate, not a chat shell.
//   - !init  → null               (auth state still hydrating)
//   - auth   → <HomeDashboard />   (logged-in service home)
//   - else   → <Welcome />         (logged-out marketing)

import { useSelector } from "react-redux";
import { selectAuthenticated, selectInit } from "@/store/auth.ts";
import Welcome from "@/routes/Welcome.tsx";
import { HomeDashboard } from "@/components/Dashboard/HomeDashboard.tsx";

function Home() {
  const auth = useSelector(selectAuthenticated);
  const init = useSelector(selectInit);

  // Hold render until auth state is known so logged-in users don't see a
  // Welcome flash. Spinner already mounts globally in App.tsx.
  if (!init) return null;
  if (auth) return <HomeDashboard />;
  return <Welcome />;
}

export default Home;
