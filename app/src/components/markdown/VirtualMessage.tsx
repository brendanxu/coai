// excise-1c-sweep: VirtualMessage stubbed — chat store removed.
// Link.tsx still imports this; reference links use a passthrough span.
import React, { useState } from "react";

type VirtualMessageProps = {
  message: string;
  children: React.ReactNode;
};

export function VirtualMessage({ message, children }: VirtualMessageProps) {
  const [isHovered, setIsHovered] = useState(false);

  // reference:: links open in a new tab
  const segments = message.split("::");
  const isReference = segments[0] === "reference";
  if (isReference) {
    const handleClick = (e: React.MouseEvent) => {
      e.preventDefault();
      let targetUrl = decodeURIComponent(segments.slice(1).join("::"));
      if (!targetUrl.startsWith("http://") && !targetUrl.startsWith("https://")) {
        targetUrl = "https://" + targetUrl;
      }
      window.open(targetUrl, "_blank", "noopener,noreferrer");
    };

    return (
      <span
        className={`inline-flex items-center justify-center h-[18px] px-[6px] py-0
                  text-[12px] font-normal
                  ${isHovered ? "bg-[#d0d0d0] text-[#202020]" : "bg-[#e5e5e5] text-[#404040]"}
                  rounded-[9px] cursor-pointer relative -top-[2px] ml-1
                  font-variant-numeric tabular-nums align-middle flex-shrink-0
                  transition-colors duration-150`}
        onClick={handleClick}
        onMouseEnter={() => setIsHovered(true)}
        onMouseLeave={() => setIsHovered(false)}
      >
        {children}
      </span>
    );
  }

  // All other virtual actions (midjourney buttons etc.) — render as plain span.
  // Chat store is gone so the Dialog/send flow is non-functional; passthrough only.
  return <span className="virtual-action mx-1">{children}</span>;
}
