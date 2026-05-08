import i18n from "i18next";
import { initReactI18next } from "react-i18next";
import { getMemory, setMemory } from "@/utils/memory.ts";
import cn from "@/resources/i18n/cn.json";
import en from "@/resources/i18n/en.json";
import ru from "@/resources/i18n/ru.json";
import ja from "@/resources/i18n/ja.json";
import tw from "@/resources/i18n/tw.json";

// the translations
// (tip move them in a JSON file and import them,
// or even better, manage them separated from your code: https://react.i18next.com/guides/multiple-translation-files)

const resources = {
  cn: { translation: cn },
  en: { translation: en },
  ru: { translation: ru },
  ja: { translation: ja },
  tw: { translation: tw },
};

export const langsProps: Record<string, string> = {
  cn: "简体中文",
  tw: "正體中文",
  en: "English",
  ru: "Русский",
  ja: "日本語",
};

export const supportedLanguages = Object.keys(resources);
export const defaultLanguage = "cn";

i18n
  .use(initReactI18next)
  .init({
    resources,
    lng: getLanguage(),
    fallbackLng: "en",
    interpolation: {
      escapeValue: false, // react already safes from xss
    },
  })
  .then(() => console.debug(`[i18n] initialized (language: ${i18n.language})`));

export default i18n;

export function getLanguage(): string {
  // 1. User's explicit choice (Header lang toggle) always wins.
  const storage = getMemory("language");
  if (storage && supportedLanguages.includes(storage)) {
    return storage;
  }

  // 2. Wedge phase (2026-05-08 → ?): the current target market is mainland
  //    China (大理 民宿主 SaaS, ¥1980/月). Default to Chinese regardless of
  //    browser locale — most visitors arrive via WeChat link or founder
  //    outreach and ARE Chinese, even if their phone OS happens to be
  //    English. English-speaking visitors can toggle via the Header lang
  //    switch (also visible in mobile drawer).
  //
  // 3. Chinese variants honored: zh-TW / zh-HK / zh-MO -> tw (Traditional).
  //    All other zh-* -> cn (Simplified, the wedge default).
  //
  // 4. Future PKG-I18N-EXPAND (deferred to overseas launch) adds Cloudflare
  //    cf-ipcountry GeoIP detection — IP in CN -> cn, else browser-language
  //    based with new locales (de / ms / vi / etc.) shipped alongside real
  //    translations. Don't add empty stub locales here; they'd promise
  //    coverage we don't yet have.
  const browser = (navigator.language || "").toLowerCase();
  if (
    browser.startsWith("zh-tw") ||
    browser.startsWith("zh-hk") ||
    browser.startsWith("zh-mo")
  ) {
    return "tw";
  }

  return defaultLanguage; // "cn"
}

export function setLanguage(i18n: any, lang: string): void {
  if (supportedLanguages.includes(lang)) {
    i18n
      .changeLanguage(lang)
      .then(() =>
        console.debug(`[i18n] language changed (language: ${i18n.language})`),
      );
    setMemory("language", lang);
    return;
  }
  console.warn(`[i18n] language ${lang} is not supported`);
}
