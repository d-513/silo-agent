// The languages Silo knows by code. Two lists, both mirrored from the server
// (internal/lang, whose test reads this file): the ones the console itself is
// translated into, and the ones a Bot can be told to write in.

/** Display languages, the default first. */
export const uiLanguages = ["en", "pl", "de"] as const;
export type UiLanguage = (typeof uiLanguages)[number];

/** A Bot with no language of its own: it answers in the language of the message. */
export const mixed = "mixed";

/** Every language a Bot can be set to. */
export const botLanguages = [
  "ar",
  "bg",
  "bn",
  "ca",
  "cs",
  "da",
  "de",
  "el",
  "en",
  "es",
  "et",
  "fa",
  "fi",
  "fr",
  "he",
  "hi",
  "hr",
  "hu",
  "id",
  "it",
  "ja",
  "ko",
  "lt",
  "lv",
  "nb",
  "nl",
  "pl",
  "pt",
  "pt-BR",
  "ro",
  "ru",
  "sk",
  "sl",
  "sr",
  "sv",
  "th",
  "tr",
  "uk",
  "vi",
  "zh-Hans",
  "zh-Hant",
] as const;

export function isUiLanguage(code: string | null | undefined): code is UiLanguage {
  return (uiLanguages as readonly string[]).includes(code ?? "");
}

function cap(s: string, locale: string): string {
  return s ? s[0].toLocaleUpperCase(locale) + s.slice(1) : s;
}

function displayName(code: string, locale: string): string {
  try {
    return cap(new Intl.DisplayNames([locale], { type: "language" }).of(code) ?? code, locale);
  } catch {
    return code;
  }
}

/** A language as its own speakers write it: "Polski", "Deutsch". */
export function nativeName(code: string): string {
  return displayName(code, code);
}

/** A language named in the reader's display language: "Polish", "polski". */
export function languageName(code: string, locale: string): string {
  return displayName(code, locale);
}

/** The Bot languages, sorted by their name in the reader's language. */
export function botLanguageOptions(locale: string): { value: string; label: string }[] {
  return botLanguages
    .map((code) => ({ value: code as string, label: languageName(code, locale) }))
    .sort((a, b) => a.label.localeCompare(b.label, locale));
}
