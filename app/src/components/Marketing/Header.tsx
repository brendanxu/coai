import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link, useLocation } from "react-router-dom";
import { Menu, X, Globe } from "lucide-react";

import { Button } from "@/components/ui/button.tsx";
import { cn } from "@/components/ui/lib/utils.ts";
import { setLanguage } from "@/i18n.ts";

// Lightweight 2-locale switcher: zh/en. We only display these two
// in marketing — other locales (ja/ru/tw) are CoAI legacy, not part
// of greentokey's go-to-market plan.
//
// Uses the project's existing setLanguage helper (i18n.ts) so the
// memory backend + i18n.changeLanguage stay in sync. setLanguage also
// validates against supportedLanguages.
function LangToggle() {
  const { i18n } = useTranslation();
  const isEn = (i18n.language || "").toLowerCase().startsWith("en");
  const next = isEn ? "cn" : "en";
  const onClick = () => {
    setLanguage(i18n, next);
  };
  return (
    <button
      type="button"
      onClick={onClick}
      className="hidden md:inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full text-xs text-muted-foreground hover:text-foreground hover:bg-muted transition-colors"
      aria-label="切换语言 / Toggle language"
    >
      <Globe className="w-3.5 h-3.5" />
      {isEn ? "中文" : "EN"}
    </button>
  );
}

/**
 * Marketing-side top navigation header.
 *
 * Why this component is separate from app's NavBar (Index.tsx):
 *   - NavBar is for logged-in app users (chat / model / account icons).
 *     民宿主 hitting greentokey.com don't have accounts and don't want
 *     to see chat playground icons.
 *   - This Header speaks customer-language: 首页 / 服务 / 定价 / 联系
 *     and routes to marketing pages (NOT to /chat or /admin).
 *
 * Mobile: hamburger menu reveals the same links as a vertical drawer.
 *
 * Sticky on scroll so the CTA stays in reach as the customer reads.
 */
export default function Header() {
  const { t } = useTranslation();
  const location = useLocation();
  const [mobileOpen, setMobileOpen] = useState(false);

  // Nav matches v0.7 Claude Design (redesign-v2.html line 864):
  // Token 套餐 / 服务市场 / 模型池 / 仪表盘 / 文档.
  // 仪表盘 + 文档 are post-login views; we surface them only after auth.
  const navItems = [
    { to: "/token-plans", label: t("nav.token", "Token 套餐") },
    { to: "/services", label: t("nav.services", "服务市场") },
    { to: "/pool", label: t("nav.pool", "模型池") },
    { to: "/contact", label: t("nav.contact", "联系") },
  ];

  // Match exact path (or hash) so / doesn't always look "active" when
  // viewing /pricing.
  const isActive = (to: string) => {
    if (to.startsWith("/#")) {
      return location.pathname === "/" && location.hash === to.slice(1);
    }
    return location.pathname === to;
  };

  return (
    <header className="sticky top-0 z-40 w-full border-b border-border/50 bg-background/85 backdrop-blur-md">
      <div className="max-w-6xl mx-auto px-6 h-14 flex items-center justify-between">
        {/* Logo */}
        <Link to="/" className="flex items-center gap-2 font-display font-medium">
          <img src="/logo.svg" alt="greentokey" className="h-7 w-7" />
          <span className="text-base">greentokey</span>
        </Link>

        {/* Desktop nav */}
        <nav className="hidden md:flex items-center gap-1">
          {navItems.map((item) => (
            <Link
              key={item.to}
              to={item.to}
              className={cn(
                "px-3 py-1.5 rounded-md text-sm transition-colors",
                isActive(item.to)
                  ? "text-foreground font-medium"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted",
              )}
            >
              {item.label}
            </Link>
          ))}
        </nav>

        {/* Lang toggle + CTA + mobile toggle */}
        <div className="flex items-center gap-2">
          <LangToggle />
          <Link to="/contact" className="hidden sm:inline-flex">
            <Button size="sm" className="rounded-full px-4">
              {t("nav.cta", "预约 demo")}
            </Button>
          </Link>
          <button
            type="button"
            onClick={() => setMobileOpen(!mobileOpen)}
            className="md:hidden p-2 -mr-2 rounded-md hover:bg-muted"
            aria-label="菜单 / Menu"
          >
            {mobileOpen ? <X className="w-5 h-5" /> : <Menu className="w-5 h-5" />}
          </button>
        </div>
      </div>

      {/* Mobile drawer */}
      {mobileOpen && (
        <div className="md:hidden border-t border-border/50 bg-background">
          <nav className="px-6 py-3 flex flex-col gap-1">
            {navItems.map((item) => (
              <Link
                key={item.to}
                to={item.to}
                onClick={() => setMobileOpen(false)}
                className={cn(
                  "px-3 py-2 rounded-md text-sm",
                  isActive(item.to)
                    ? "text-foreground font-medium bg-muted"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted",
                )}
              >
                {item.label}
              </Link>
            ))}
            <Link to="/contact" onClick={() => setMobileOpen(false)} className="mt-2">
              <Button className="w-full rounded-full">
                {t("nav.cta", "预约 demo")}
              </Button>
            </Link>
          </nav>
        </div>
      )}
    </header>
  );
}
