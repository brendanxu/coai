import React, { useEffect } from "react";
import { Message } from "@/api/types.tsx";
import { useSelector } from "react-redux";
import {
  listenMessageEvent,
  selectCurrent,
  selectModel,
  useMessages,
} from "@/store/chat.ts";
import MessageSegment from "@/components/Message.tsx";
import { ScrollArea } from "@/components/ui/scroll-area.tsx";
import { AnimatePresence, motion } from "framer-motion";
import { CarbonBadge } from "@/components/Carbon/CarbonBadge.tsx";

type ChatInterfaceProps = {
  scrollable: boolean;
  setTarget: (target: HTMLDivElement | null) => void;
};

const shouldRenderMessage = (message: Message): boolean => {
  if (message.role === "tool") {
    return false;
  }

  if (message.role && message.role.startsWith("virtualRole::")) {
    return false;
  }
  
  return true;
};

function ChatInterface({ scrollable, setTarget }: ChatInterfaceProps) {
  const ref = React.useRef<HTMLDivElement>(null);
  const messages: Message[] = useMessages();
  const process = listenMessageEvent();
  const current: number = useSelector(selectCurrent);
  // v0.6 carbon: fallback for CarbonBadge when message-level model isn't set
  const currentModel: string = useSelector(selectModel);
  const [selected, setSelected] = React.useState(-1);

  const renderableMessages = React.useMemo(() => {
    return messages.filter(shouldRenderMessage);
  }, [messages]);

  useEffect(() => {
    if (!ref.current || !scrollable) return;
    const el = ref.current;
    el.scrollTop = el.scrollHeight;
  }, [renderableMessages, scrollable]);

  useEffect(() => {
    setTarget(ref.current);
  }, [ref, setTarget]);

  return (
    <ScrollArea className="chat-content" ref={ref}>
      <AnimatePresence>
        <motion.div className="chat-messages-wrapper">
          {renderableMessages.map((message) => {
            const originalIndex = messages.findIndex(m => m === message);
            
            return (
              <motion.div
                key={`message-${originalIndex}`}
                initial={{ opacity: 0, y: 20 }}
                animate={{ opacity: 1, y: 0 }}
                exit={{ opacity: 0, y: -20 }}
                transition={{
                  type: "spring",
                  stiffness: 500,
                  damping: 30,
                  mass: 1,
                  delay:
                    message.role === "assistant" || message.role === "system"
                      ? 0.25
                      : 0,
                }}
              >
                <MessageSegment
                  message={message}
                  end={originalIndex === messages.length - 1}
                  onEvent={(event: string, index?: number, message?: string) => {
                    process({ id: current, event, index: index ?? originalIndex, message });
                  }}
                  index={originalIndex}
                  selected={selected === originalIndex}
                  onFocus={() => setSelected(originalIndex)}
                  onFocusLeave={() => setSelected(-1)}
                />
                {/* v0.6 carbon: badge under each completed AI message.
                    Skipped during streaming (skeleton shown by CarbonBadge itself).
                    Tokens fall back to (content.length / 4) approximation when
                    upstream message-level usage isn't populated yet — see
                    v0.7 polish in api/types.tsx. */}
                {message.role === "assistant" && message.end !== false && (
                  <div className="ml-2 mt-1">
                    <CarbonBadge
                      tokens={message.tokens ?? Math.max(1, Math.round(message.content.length / 4))}
                      model={message.model ?? currentModel}
                      streaming={false}
                    />
                  </div>
                )}
              </motion.div>
            );
          })}
        </motion.div>
      </AnimatePresence>
    </ScrollArea>
  );
}

export default ChatInterface;
