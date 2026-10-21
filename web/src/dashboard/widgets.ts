// What the Dashboard's widgets read from the server's JSON, as pure functions:
// the page draws, these decide. The node test (widgets_test.ts) holds them.

export type Size = "s" | "m" | "l";
export type Cols = 1 | 2 | 4;

/** A JSON object from the server, or {} for anything else. */
export function obj(json: string | undefined): Record<string, unknown> {
  if (!json) return {};
  try {
    const v = JSON.parse(json);
    return v && typeof v === "object" && !Array.isArray(v) ? (v as Record<string, unknown>) : {};
  } catch {
    return {};
  }
}

/** A list of objects out of an unknown value. */
export function items(v: unknown): Record<string, unknown>[] {
  return Array.isArray(v) ? v.filter((x): x is Record<string, unknown> => !!x && typeof x === "object" && !Array.isArray(x)) : [];
}

/** A string or a number as text; anything else (a yes/no too: the page words it) is "". */
export function text(v: unknown): string {
  if (typeof v === "string") return v;
  if (typeof v === "number" && Number.isFinite(v)) return String(v);
  return "";
}

export function num(v: unknown): number | null {
  if (typeof v === "number" && Number.isFinite(v)) return v;
  if (typeof v === "string" && v.trim() !== "" && Number.isFinite(Number(v))) return Number(v);
  return null;
}

/** How many columns the board has at a width: four on a wide page, two, one. */
export function columnsFor(width: number): Cols {
  if (width >= 760) return 4;
  if (width >= 480) return 2;
  return 1;
}

/** The columns a widget spans: a quarter, half, or the row. */
export function span(size: string, cols: Cols): number {
  const want = size === "s" ? 1 : size === "l" ? 4 : 2;
  return Math.min(want, cols);
}

/** A span of time in one unit, for the page to word: 4 m, 2 h, 3 d. */
export type Span = { n: number; unit: "m" | "h" | "d" };

/** How old a widget's data is, for its corner: "now" under a minute, null when unknown. */
export function age(iso: string, now: Date = new Date()): Span | "now" | null {
  const ms = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(ms)) return null;
  const s = Math.max(0, (now.getTime() - ms) / 1000);
  if (s < 60) return "now";
  if (s < 3600) return { n: Math.floor(s / 60), unit: "m" };
  if (s < 86400) return { n: Math.floor(s / 3600), unit: "h" };
  return { n: Math.floor(s / 86400), unit: "d" };
}

/** A probe interval in seconds as a span, null when there is none. */
export function interval(sec: number): Span | null {
  if (sec <= 0) return null;
  if (sec % 86400 === 0) return { n: sec / 86400, unit: "d" };
  if (sec % 3600 === 0) return { n: sec / 3600, unit: "h" };
  return { n: Math.round(sec / 60), unit: "m" };
}

export type Action = { id: string; label: string; kind: "run" | "hand"; row?: boolean; confirm?: boolean };

export type WidgetHandlers = {
  onSelect: () => void;
  onRefresh: () => Promise<void>;
  onState: (stateJson: string) => void;
  // apart: a hand action opens a chat of its own instead of the main one
  // (Ctrl/⌘-click, like a link in a new tab).
  onAction: (a: Action, args: Record<string, unknown>, apart?: boolean) => Promise<void>;
};

/** The buttons a widget's spec declares. */
export function actions(specJson: string): Action[] {
  return items(obj(specJson).actions)
    .map((a) => ({
      id: text(a.id),
      label: text(a.label),
      kind: a.kind === "hand" ? ("hand" as const) : ("run" as const),
      row: a.row === true,
      confirm: a.confirm === true,
    }))
    .filter((a) => a.id && a.label);
}

/** Polyline points for a sparkline in a w×h box, inset by pad. */
export function sparkPoints(series: number[], w: number, h: number, pad = 3): [number, number][] {
  const vals = series.filter((x) => Number.isFinite(x));
  if (vals.length < 2) return [];
  const lo = Math.min(...vals);
  const hi = Math.max(...vals);
  const range = hi - lo || 1;
  const step = (w - pad * 2) / (vals.length - 1);
  return vals.map((v, i) => [pad + i * step, pad + (h - pad * 2) * (1 - (v - lo) / range)]);
}

/** The bar heights of a bar chart, 0..1 of the tallest (negative values are 0). */
export function bars(values: number[]): number[] {
  const hi = Math.max(0, ...values);
  return values.map((v) => (hi > 0 ? Math.max(0, v) / hi : 0));
}

/**
 * Where the "now" rule sits in a timeline: the number of items already past.
 * -1 when no item is today (the rule would say nothing) or a time cannot be
 * read.
 */
export function nowSplit(times: string[], now: Date = new Date()): number {
  let past = 0;
  let today = false;
  for (const t of times) {
    const ms = Date.parse(t);
    if (!Number.isFinite(ms)) return -1;
    const d = new Date(ms);
    if (d.toDateString() === now.toDateString()) today = true;
    if (ms <= now.getTime()) past++;
  }
  return today ? past : -1;
}

export type Tone = "ok" | "warn" | "down" | "off";

export function tone(state: unknown): Tone {
  return state === "warn" || state === "down" || state === "off" ? state : "ok";
}

/** A link a widget may open: http(s) only. */
export function safeUrl(u: unknown): string {
  if (typeof u !== "string") return "";
  try {
    const p = new URL(u);
    return p.protocol === "https:" || p.protocol === "http:" ? p.href : "";
  } catch {
    return "";
  }
}

/** The ids the owner checked on a checklist. */
export function checked(stateJson: string): string[] {
  const c = obj(stateJson).checked;
  return Array.isArray(c) ? c.map(String) : [];
}

/** The state after the owner checks or unchecks one item. */
export function toggle(stateJson: string, id: string): string {
  const st = obj(stateJson);
  const cur = checked(stateJson);
  st.checked = cur.includes(id) ? cur.filter((x) => x !== id) : [...cur, id];
  return JSON.stringify(st);
}

export type Receipt = { text: string; ok: boolean; at: string };

export function receipt(json: string): Receipt | null {
  const r = obj(json);
  if (!text(r.text)) return null;
  return { text: text(r.text), ok: r.ok !== false, at: text(r.at) };
}

/** Move the widget `from` to where `to` is now. */
export function reorder(ids: string[], from: string, to: string): string[] {
  if (from === to) return ids;
  const out = ids.filter((x) => x !== from);
  const i = out.indexOf(to);
  if (i < 0) return ids;
  const fromBefore = ids.indexOf(from) < ids.indexOf(to);
  out.splice(fromBefore ? i + 1 : i, 0, from);
  return out;
}

/** A table cell that is a number reads right-aligned in mono. */
export function numeric(v: unknown): boolean {
  if (typeof v === "number") return true;
  return typeof v === "string" && /^[-+]?[\d.,\s]+\s?(%|[a-zA-Z]{1,3})?$/.test(v.trim()) && /\d/.test(v);
}
