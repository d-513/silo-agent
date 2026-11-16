import { msg } from "./i18n.ts";

// What the Memories page narrows its list to. "all" is what the Bot can be
// told or may come to be told: the active memories and the noticed ones.
export type Filter = "all" | "preference" | "lesson" | "fact" | "noticed" | "retired";

type Row = { kind: string; status: string };

// FILTERS are the chips, in order. One that is not `always` shows only while
// some memory would be under it.
export const FILTERS: { key: Filter; label: string; always?: boolean }[] = [
  { key: "all", label: msg("All"), always: true },
  { key: "preference", label: msg("Preferences") },
  { key: "lesson", label: msg("Lessons") },
  { key: "fact", label: msg("Facts") },
  { key: "noticed", label: msg("Noticed") },
  { key: "retired", label: msg("Retired") },
];

// shows reports whether a memory is listed under a filter. A retired one is
// listed only under Retired, a noticed one under All and Noticed.
export function shows(f: Filter, m: Row): boolean {
  const retired = m.status === "retired";
  switch (f) {
    case "all":
      return !retired;
    case "noticed":
      return m.status === "tentative";
    case "retired":
      return retired;
    default:
      return m.status === "active" && (m.kind || "fact") === f;
  }
}

// kindWord is the word a row carries for its kind; a fact carries none.
export function kindWord(kind: string): string {
  if (kind === "preference") return msg("preference");
  if (kind === "lesson") return msg("lesson");
  return "";
}
