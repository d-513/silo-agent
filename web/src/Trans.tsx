import type { ReactElement } from "react";
import { Trans as Base } from "react-i18next";
import { i18n } from "./i18n.ts";

// A message with markup inside it. The key is the English sentence with the
// marked-up parts as tags, and `components` says what each tag is:
//
//   <Trans i18nKey="Set <code>debug: true</code> and reload." components={{ code: <code /> }} />
//
// so a translator moves the tag with the words it wraps. Use `t` for plain text.
//
// A value is text, never markup: a file called `<code>x</code>` must not turn
// into one of the components. So values go in escaped and come out unescaped.
export function Trans({ i18nKey, values, components }: { i18nKey: string; values?: Record<string, unknown>; components?: Record<string, ReactElement> }) {
  return <Base i18n={i18n} i18nKey={i18nKey} values={values && escaped(values)} components={components} shouldUnescape />;
}

function escaped(values: Record<string, unknown>): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const [k, v] of Object.entries(values)) {
    out[k] = typeof v === "string" ? v.replace(/&/g, "&amp;").replace(/</g, "&lt;").replace(/>/g, "&gt;") : v;
  }
  return out;
}
