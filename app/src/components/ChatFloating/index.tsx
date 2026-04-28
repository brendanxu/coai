// v0.6.1 — global floating chat. Mounted once in routes/Index.tsx so it
// renders on every child route AND lives inside RouterProvider (so
// useLocation works — putting this in App.tsx would throw).
//
// State (open/closed) lives here, not in a global store, because the only
// two consumers are the Button and Panel rendered as siblings below.
// Per design decision: open state is NOT persisted — closes on every page
// load. Conversation state is persisted via the chat redux slice.
//
// Auto-close on /chat: prevents the ChatWrapper double-mount race when the
// user opens the panel on /pricing then navigates to /chat.

import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { ChatFloatingButton } from "./ChatFloatingButton.tsx";
import { ChatPanel } from "./ChatPanel.tsx";

export function ChatFloating() {
  const { pathname } = useLocation();
  const [open, setOpen] = useState(false);
  const onChat = pathname === "/chat";

  // If the user navigates to /chat with the panel open, close it so we
  // don't end up with two ChatWrappers mounted simultaneously.
  useEffect(() => {
    if (onChat && open) setOpen(false);
  }, [onChat, open]);

  return (
    <>
      <ChatFloatingButton hidden={onChat} onClick={() => setOpen(true)} />
      <ChatPanel open={open} onClose={() => setOpen(false)} />
    </>
  );
}
