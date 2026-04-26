import "@/assets/pages/chat.less";
import ChatWrapper from "@/components/home/ChatWrapper.tsx";
import SideBar from "@/components/home/SideBar.tsx";
import { useSelector } from "react-redux";
import { selectAuthenticated, selectInit } from "@/store/auth.ts";
import Welcome from "@/routes/Welcome.tsx";

function Home() {
  const auth = useSelector(selectAuthenticated);
  const init = useSelector(selectInit);
  // greentokey: show Welcome to logged-out visitors who haven't dismissed it.
  // After they click "免费试用一次", `skip_welcome` is set + page reloads,
  // delivering them to the standard CoAI anonymous chat path below.
  const skipWelcome = !!localStorage.getItem("skip_welcome");

  // Wait for auth state to initialize so we don't flash Welcome at logged-in users
  if (init && !auth && !skipWelcome) {
    return <Welcome />;
  }

  return (
    <div className={`home-page flex flex-row flex-1`}>
      <SideBar />
      <ChatWrapper />
    </div>
  );
}

export default Home;
