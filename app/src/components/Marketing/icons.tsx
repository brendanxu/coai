import { MessagesSquare, Receipt, Film } from "lucide-react";

/**
 * Service icons used on the marketing landing's service grid. One icon per
 * service slug. Centralized so the same glyph can be reused later in
 * post-login dashboards if/when these services launch.
 */
export const ServiceIcons = {
  chat: MessagesSquare,
  "tax-filing": Receipt,
  "video-editing": Film,
} as const;

export type ServiceSlug = keyof typeof ServiceIcons;
