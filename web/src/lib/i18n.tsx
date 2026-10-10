import { createContext, useContext } from "react";
import type { Language } from "./types";
import definitions from "../../../internal/i18n/languages.json";
export const languages = definitions as { code: Language; label: string }[];
export function isLanguage(value: string | null): value is Language {
  return languages.some(({ code }) => code === value);
}
export type Catalog = Record<string, string>;
export function format(
  catalog: Catalog,
  key: string,
  params: Record<string, string | number> = {},
) {
  return (catalog[key] ?? key).replace(/\{(\w+)\}/g, (match, name: string) =>
    String(params[name] ?? match),
  );
}
export async function loadCatalog(
  language?: Language,
): Promise<{ language: Language; catalog: Catalog }> {
  // The server matches the browser's Accept-Language header on this first request.
  const response = await fetch(`/locales/${language ?? "default"}.json`);
  if (!response.ok) throw new Error("catalog_unavailable");
  const resolved = response.headers.get("Content-Language");
  return {
    language: isLanguage(resolved) ? resolved : "en",
    catalog: await response.json(),
  };
}
export const I18nContext = createContext({
  language: "en" as Language,
  catalog: {} as Catalog,
  setLanguage: (() => {}) as (language: Language) => void,
});
export function useI18n() {
  const context = useContext(I18nContext);
  return {
    ...context,
    t: (key: string, params?: Record<string, string | number>) =>
      format(context.catalog, key, params),
  };
}
export function readPreference(key: "language" | "theme") {
  try {
    return localStorage.getItem(`mailwake.${key}`);
  } catch {
    return null;
  }
}
export function savePreference(key: "language" | "theme", value: string) {
  try {
    localStorage.setItem(`mailwake.${key}`, value);
  } catch {
    /* Preferences are optional in restricted browsers. */
  }
}
