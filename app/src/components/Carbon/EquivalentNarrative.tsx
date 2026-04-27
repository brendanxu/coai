// v0.6 carbon — equivalent narrative card.
// Translates abstract grams into felt experience ("≈ 1.2 km of city driving").
// Used on /dashboard below the hero number. Fraunces/display feel.

import equivalents from "@/data/carbon_equivalents.json";
import { Callout } from "./Callout.tsx";
import { cn } from "@/components/ui/lib/utils.ts";

type Locale = "en" | "zh";

type Tier = {
  max_g: number;
  en: string[];
  zh: string[];
  g_per_km_city_car?: number;
  g_per_km_train?: number;
  g_per_mile_small_petrol?: number;
};

function pickTemplate(co2g: number, locale: Locale): { template: string; tier: Tier } | null {
  const tiers = equivalents.tiers as Tier[];
  for (const t of tiers) {
    if (co2g <= t.max_g) {
      const arr = locale === "zh" ? t.zh : t.en;
      if (arr && arr.length > 0) {
        return { template: arr[0], tier: t };
      }
    }
  }
  return null;
}

function renderTemplate(template: string, tier: Tier, co2g: number): string {
  // Compute substitutions from tier metadata
  const km =
    tier.g_per_km_city_car !== undefined
      ? co2g / tier.g_per_km_city_car
      : tier.g_per_km_train !== undefined
        ? co2g / tier.g_per_km_train
        : 0;
  const miles =
    tier.g_per_mile_small_petrol !== undefined
      ? co2g / tier.g_per_mile_small_petrol
      : 0;

  return template
    .replace("{km:f1}", km.toFixed(1))
    .replace("{km:f0}", km.toFixed(0))
    .replace("{miles:f1}", miles.toFixed(1))
    .replace("{miles:f0}", miles.toFixed(0));
}

type Props = {
  co2g: number;
  locale?: Locale;
  className?: string;
};

export function EquivalentNarrative({ co2g, locale = "en", className }: Props) {
  const picked = pickTemplate(co2g, locale);
  if (!picked) return null;
  const text = renderTemplate(picked.template, picked.tier, co2g);

  return (
    <Callout variant="accent" className={cn("font-display text-2xl italic leading-relaxed", className)}>
      {text}
    </Callout>
  );
}
