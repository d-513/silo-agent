// What a quote is, as plain functions: how one is made from a selection, how
// it is named on screen, and how it travels. No React here, so it is tested
// under node.
import type { Quote } from "../fold/types.ts";
import { t } from "../i18n.ts";

export type { Quote } from "../fold/types.ts";

// One quote waiting in the composer: the quote, plus a picture of a cropped
// region (a data URL, shown on the chip only).
export type DraftQuote = Quote & { id: string; thumb?: string };

export const MAX_QUOTES = 8;
export const MAX_QUOTE_TEXT = 4000;

type ProtoQuote = { source: string; ref: string; label: string; text: string; locator: string; image: string };

export function quoteFromProto(q: ProtoQuote): Quote {
  const source = q.source === "file" || q.source === "artifact" ? q.source : "message";
  return { source, ref: q.ref, label: q.label, text: q.text, locator: q.locator, image: q.image || undefined };
}

export function quoteToProto(q: Quote): ProtoQuote {
  return { source: q.source, ref: q.ref, label: q.label, text: q.text, locator: q.locator, image: q.image ?? "" };
}

// lineLocator says which lines of full a selection covers ("lines 4–9",
// "line 4"), or "" when the selection is not found in it as written.
export function lineLocator(full: string, selected: string): string {
  const s = selected.trim();
  if (!s || !full) return "";
  const at = full.indexOf(s);
  if (at < 0) return "";
  const first = full.slice(0, at).split("\n").length;
  const last = first + s.split("\n").length - 1;
  return first === last ? t("line {{n}}", { n: first }) : t("lines {{a}}–{{b}}", { a: first, b: last });
}

// regionLocator is a crop as fractions of the image: x,y,w,h.
export function regionLocator(r: { x: number; y: number; w: number; h: number }): string {
  const f = (n: number) => String(Math.round(Math.min(1, Math.max(0, n)) * 1000) / 1000);
  return [r.x, r.y, r.w, r.h].map(f).join(",");
}

// quoteTitle is what a quote is called on screen.
export function quoteTitle(q: Pick<Quote, "source" | "ref" | "label">): string {
  if (q.source === "message") return q.ref === "own" ? t("Your message") : t("Reply");
  return q.label || q.ref;
}

// clip keeps a quote under the server's cap, cutting at a line where it can.
export function clip(text: string, max = MAX_QUOTE_TEXT): string {
  const s = text.trim();
  if (s.length <= max) return s;
  const cut = s.lastIndexOf("\n", max);
  return `${s.slice(0, cut > max / 2 ? cut : max)}…`;
}

let seq = 0;

// addQuote puts q in a draft's list: the same text from the same place once,
// and never more than the cap.
export function addQuote(list: DraftQuote[], q: Quote & { thumb?: string }): DraftQuote[] {
  const text = clip(q.text);
  if (!text && !q.image) return list;
  if (list.some((x) => x.source === q.source && x.ref === q.ref && x.text === text && x.image === q.image)) return list;
  if (list.length >= MAX_QUOTES) return list;
  return [...list, { ...q, text, id: `q${++seq}` }];
}
