// v0.6.1 — fixed bottom-right button that opens the floating chat panel.
// Hidden when prop hidden=true (controlled by ChatFloating root via
// useLocation — hidden on /chat where the full chat UI is already visible).

import { useTranslation } from "react-i18next";
import { motion } from "framer-motion";
import { Leaf } from "lucide-react";
import { cn } from "@/components/ui/lib/utils.ts";

type Props = {
  hidden: boolean;
  onClick: () => void;
};

export function ChatFloatingButton({ hidden, onClick }: Props) {
  const { t } = useTranslation();
  if (hidden) return null;

  return (
    <motion.button
      type="button"
      onClick={onClick}
      aria-label={t("chat-floating.open")}
      title={t("chat-floating.open")}
      initial={{ opacity: 0, scale: 0.8 }}
      animate={{ opacity: 1, scale: 1 }}
      transition={{ duration: 0.25, ease: "easeOut" }}
      whileHover={{ scale: 1.05 }}
      whileTap={{ scale: 0.95 }}
      className={cn(
        "fixed bottom-6 right-6 z-50",
        "w-12 h-12 rounded-full shadow-lg",
        "flex items-center justify-center",
        "bg-[hsl(var(--primary))] text-[hsl(var(--primary-foreground))]",
        "hover:shadow-xl transition-shadow",
        "focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-[hsl(var(--primary))]",
      )}
    >
      <Leaf className="w-5 h-5" />
    </motion.button>
  );
}
