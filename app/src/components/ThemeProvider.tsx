import {
  createContext,
  useContext,
  useLayoutEffect,
  useState,
  ReactNode,
} from "react";
import { Moon, Sun, Monitor } from "lucide-react";

import { Button } from "./ui/button";
import { getMemory, setMemory } from "@/utils/memory.ts";
import { themeEvent } from "@/events/theme.ts";

const defaultTheme: Theme = "dark";

export type Theme = "dark" | "light" | "system";

type ThemeProviderProps = {
  children?: ReactNode;
  defaultTheme?: Theme;
};

type ThemeProviderState = {
  theme: Theme;
  setTheme: (theme: Theme) => void;
  toggleTheme?: () => void;
};

export function activeTheme(theme: Theme, options?: { persist?: boolean }) {
  const root = window.document.documentElement;

  root.classList.remove("light", "dark");
  let actualTheme = theme;
  if (theme === "system") {
    actualTheme = window.matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  }

  root.classList.add(actualTheme);
  // persist defaults true to preserve existing call sites (ThemeToggle etc.).
  // Marketing-route forcing in Index.tsx passes { persist: false } so the
  // user's app-route preference (e.g. dark) survives a marketing visit.
  if (options?.persist !== false) {
    setMemory("theme", theme);
  }
  themeEvent.emit(actualTheme);
}

export function getTheme() {
  return (getMemory("theme") as Theme) || defaultTheme;
}

// system -> dark -> light -> system
function getNextTheme(current: Theme): Theme {
  return current === "system"
    ? "dark"
    : current === "dark"
    ? "light"
    : "system";
}

const initialState: ThemeProviderState = {
  theme: "system",
  setTheme: (theme: Theme) => {
    activeTheme(theme);
  },
  toggleTheme: () => {
    const key = getMemory("theme");
    const current = (key.length > 0 ? (key as Theme) : defaultTheme) as Theme;
    const next = getNextTheme(current);
    activeTheme(next);
  },
};

const ThemeProviderContext = createContext<ThemeProviderState>(initialState);

export function ThemeProvider({
  defaultTheme = "dark",
  ...props
}: ThemeProviderProps) {
  const [theme, setTheme] = useState<Theme>(
    () => (getMemory("theme") as Theme) || defaultTheme,
  );

  // useLayoutEffect (synchronous before paint) so any theme flip happens
  // before the user sees a paint — batched with sibling layout effects.
  //
  // Strategy (per founder 2026-05-09 = "B 选项 2"):
  //   - User explicit toggle (theme state set via Header) always wins.
  //   - Marketing routes WITHOUT explicit pref:
  //       * 18:00-06:00 (local hour) → dark (sunset rule, primary)
  //       * Daytime + system prefers-color-scheme: dark → dark (respect OS)
  //       * Otherwise → light
  //   - App routes WITHOUT explicit pref → fall through to theme state
  //     (defaultTheme="dark", CoAI's original).
  //
  // Switching evaluates on mount/route-change only; not real time, to
  // avoid auto-flipping mid-read. Visual contrast in dark mode is
  // handled by --ink-foreground + --ink-accent inversions in
  // src/assets/globals.less .dark block.
  //
  // MARKETING_PATHS_FOR_THEME mirrors src/routes/Index.tsx MARKETING_PATHS
  // and index.html inline script — keep all three in sync.
  useLayoutEffect(() => {
    const root = window.document.documentElement;
    const MARKETING_PATHS_FOR_THEME = new Set([
      "/",
      "/pricing",
      "/contact",
      "/privacy",
      "/terms",
      "/about",
      "/pool",
      "/token-plans",
      "/services",
      "/services/mansu",
    ]);
    const isMarketing = MARKETING_PATHS_FOR_THEME.has(window.location.pathname);
    const userExplicit = !!getMemory("theme"); // toggled at least once

    function resolveMarketingTheme(): "light" | "dark" {
      const hour = new Date().getHours();
      if (hour < 6 || hour >= 18) return "dark"; // sunset rule
      if (window.matchMedia("(prefers-color-scheme: dark)").matches)
        return "dark";
      return "light";
    }

    root.classList.remove("light", "dark");

    let resolved: "light" | "dark";
    if (userExplicit) {
      resolved =
        theme === "system"
          ? window.matchMedia("(prefers-color-scheme: dark)").matches
            ? "dark"
            : "light"
          : (theme as "light" | "dark");
    } else if (isMarketing) {
      resolved = resolveMarketingTheme();
    } else {
      resolved =
        theme === "system"
          ? window.matchMedia("(prefers-color-scheme: dark)").matches
            ? "dark"
            : "light"
          : (theme as "light" | "dark");
    }

    root.classList.add(resolved);
  }, [theme]);

  const value = {
    theme,
    setTheme: (newTheme: Theme) => {
      activeTheme(newTheme);
      setTheme(newTheme);
    },
    toggleTheme: () => {
      const nextTheme: Theme = getNextTheme(theme);
      activeTheme(nextTheme);
      setTheme(nextTheme);
    },
  };

  return <ThemeProviderContext.Provider {...props} value={value} />;
}

export const useTheme = () => {
  const context = useContext(ThemeProviderContext);

  if (context === undefined)
    throw new Error("useTheme must be used within a ThemeProvider");

  return context;
};

export function ThemeToggle({
  className,
  size = "icon",
}: {
  className?: string;
  size?: "icon" | "icon-md";
}) {
  const { theme, toggleTheme } = useTheme();

  return (
    <Button
      variant="outline"
      size={size}
      onClick={() => toggleTheme?.()}
      className={`!m-0 ${className || ""}`}
    >
      <Sun
        className={`h-4 w-4 transition-all ${
          theme === "light"
            ? "relative rotate-0 scale-100"
            : "absolute -rotate-90 scale-0"
        }`}
      />
      <Moon
        className={`h-4 w-4 transition-all ${
          theme === "dark"
            ? "relative rotate-0 scale-100"
            : "absolute rotate-90 scale-0"
        }`}
      />
      <Monitor
        className={`h-4 w-4 transition-all ${
          theme === "system"
            ? "relative rotate-0 scale-100"
            : "absolute rotate-90 scale-0"
        }`}
      />
    </Button>
  );
}

export default ThemeToggle;
