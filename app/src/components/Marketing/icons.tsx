import { Sparkles, Send, MessagesSquare, BarChart3 } from "lucide-react";

/**
 * Service icons for v0.8 民宿 SaaS landing.
 * 4 environments of the closed-loop:
 *  1. content   — AI 内容生成（含风格学习 few-shot）
 *  2. publish   — 老板手机端轻量 helper 自动发布
 *  3. engage    — 评论 + 私信 AI 草稿
 *  4. roi       — 订单归因 dashboard（Phase 1 手动 + Phase 2 自动）
 *
 * v0.7 三服务（chat/tax-filing/video-editing）已废弃，老 slug 保留只为
 * 兼容历史 i18n key（如有引用可优雅 degrade，不阻塞 build）。
 */
export const ServiceIcons = {
  // v0.8 民宿闭环（主线）
  content: Sparkles,
  publish: Send,
  engage: MessagesSquare,
  roi: BarChart3,
  // v0.7 indie wedge（已弃，保留映射避免历史 import 报错）
  chat: MessagesSquare,
  "tax-filing": Sparkles,
  "video-editing": Send,
} as const;

export type ServiceSlug = keyof typeof ServiceIcons;
