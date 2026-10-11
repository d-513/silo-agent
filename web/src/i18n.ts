import i18next from "i18next";
import { useSyncExternalStore } from "react";
import { isUiLanguage, type UiLanguage } from "./languages.ts";
import en from "./locales/en.ts";

// The console's display language. English is the source: a message's key is
// its English text, so `t("Save")` reads as what it shows and an untranslated
// string falls back to itself. Polish and German are maps from that text
// (locales/), loaded when first needed.
//
// `t` is a plain function, not a hook: pure helpers (format.ts, statusLook.ts)
// call it too. A component does not subscribe to the language; App re-keys the
// routes when it changes, which is once in a long while. So never call `t` at
// module level: the module is evaluated once, before the language is known.
// Mark such a string with `msg` and translate it where it is shown.
//
// The language is the user's setting (Account → Language, `User.language`).
// It is mirrored to localStorage so the signed-out pages and the first paint
// of the next visit read it without asking the server.

export const langKey = "silo.lang";

export const i18n = i18next.createInstance();
void i18n.init({
  lng: "en",
  fallbackLng: "en",
  resources: { en: { translation: en } },
  // Keys are sentences: neither "." nor ":" divides one.
  keySeparator: false,
  nsSeparator: false,
  // React escapes what it renders.
  interpolation: { escapeValue: false },
  initAsync: false,
});

/** The message in the display language. `{{name}}` takes a value; `count` picks the plural form. */
export function t(key: string, values?: Record<string, unknown>): string {
  return i18n.t(key, values) as string;
}

/** Marks a message that is translated later, where it is shown: `t(row.label)`. */
export function msg<T extends string>(s: T): T {
  return s;
}

const loaders: Record<Exclude<UiLanguage, "en">, () => Promise<{ default: Record<string, string> }>> = {
  pl: () => import("./locales/pl.ts"),
  de: () => import("./locales/de.ts"),
};

export function storedLanguage(raw: string | null | undefined): UiLanguage | null {
  return isUiLanguage(raw) ? raw : null;
}

let language: UiLanguage = "en";
const subs = new Set<() => void>();

export function getLanguage(): UiLanguage {
  return language;
}

function subscribe(f: () => void) {
  subs.add(f);
  return () => subs.delete(f);
}

export function useLanguage(): UiLanguage {
  return useSyncExternalStore(subscribe, getLanguage);
}

// The newest request wins: a slow bundle must not land over a later pick.
let wanted: UiLanguage = "en";

/** Switches the display language, loading its messages first. */
export async function setLanguage(next: UiLanguage): Promise<void> {
  wanted = next;
  if (next !== "en" && !i18n.hasResourceBundle(next, "translation")) {
    const bundle = await loaders[next]();
    i18n.addResourceBundle(next, "translation", bundle.default);
  }
  if (wanted !== next) return;
  try {
    localStorage.setItem(langKey, next);
  } catch {
    /* private window: still works for this tab */
  }
  if (next === language) return;
  await i18n.changeLanguage(next);
  language = next;
  if (typeof document !== "undefined") document.documentElement.lang = next;
  subs.forEach((f) => f());
}

/** Takes the language of the user who is signed in (`User.language`); anything unknown is English. */
export function followUser(code: string | undefined): Promise<void> {
  return setLanguage(storedLanguage(code) ?? "en").catch(() => {});
}

/** The language this device last showed, before anyone is known to be signed in. */
export function bootLanguage(): Promise<void> {
  let raw: string | null = null;
  try {
    raw = localStorage.getItem(langKey);
  } catch {
    /* no storage */
  }
  return setLanguage(storedLanguage(raw) ?? "en").catch(() => {});
}

/**
 * The locale dates and numbers are written in: the display language, in the
 * browser's own regional flavour when it is the same language (en-GB keeps its
 * 24-hour clock).
 */
export function locale(): string {
  const own = typeof navigator === "undefined" ? "" : (navigator.language ?? "");
  return own.toLowerCase().split("-")[0] === language ? own : language;
}
