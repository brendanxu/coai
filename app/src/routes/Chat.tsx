import "@/assets/pages/chat.less";
import ChatWrapper from "@/components/home/ChatWrapper.tsx";
import SideBar from "@/components/home/SideBar.tsx";

// greentokey v0.6.1 — chat moved off /. Dedicated /chat route owns the
// SideBar + ChatWrapper layout. Home (/) is now the service dashboard for
// authenticated users. Floating chat button on every other route opens
// ChatWrapper in a side panel; on /chat itself the button is hidden.
function Chat() {
  return (
    <div className="home-page flex flex-row flex-1">
      <SideBar />
      <ChatWrapper />
    </div>
  );
}

export default Chat;
