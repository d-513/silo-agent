import { useSyncExternalStore } from "react";
import { defaultPinned, moveBefore, type TabKey } from "./layout.ts";

// The viewer's tab layout (layout.ts), kept in localStorage: a per-viewer
// convenience. What is pinned is shared by every Bot (pin Desktop once and
// every Bot has it in the bar; a pinned chat shows only on its own Bot); the
// open tabs, their order and the split are each Bot's; the split's share of
// the row is one for all.

type BotLayout = { order: TabKey[]; split: TabKey | "" };
type State = { pinned: TabKey[]; bots: Record<string, BotLayout>; share: number };

const storeKey = "silo.layout";
// The old docked pane's share (paneStore), read once so the split opens as wide.
const oldKey = "silo.pane";
const subs = new Set<() => void>();
const empty: BotLayout = { order: [], split: "" };
// So many Bots' layouts are kept; the oldest go first.
const keepBots = 40;

export const minShare = 0.25;
export const maxShare = 0.75;

export function clampShare(n: number) {
  return Number.isFinite(n) ? Math.min(maxShare, Math.max(minShare, n)) : 0.5;
}

const keys = (v: unknown): TabKey[] => (Array.isArray(v) ? v.filter((x): x is TabKey => typeof x === "string" && x !== "" && x !== "new") : []);

function read(): State {
  try {
    const v = JSON.parse(localStorage.getItem(storeKey) ?? "null") as Partial<State> | null;
    if (v) {
      const bots: Record<string, BotLayout> = {};
      for (const [id, b] of Object.entries(v.bots ?? {})) {
        bots[id] = { order: keys(b?.order), split: typeof b?.split === "string" && b.split !== "new" ? (b.split as TabKey) : "" };
      }
      return { pinned: Array.isArray(v.pinned) ? keys(v.pinned) : [...defaultPinned], bots, share: clampShare(typeof v.share === "number" ? v.share : 0.5) };
    }
    const old = JSON.parse(localStorage.getItem(oldKey) ?? "{}") as { share?: number } | null;
    return { pinned: [...defaultPinned], bots: {}, share: clampShare(typeof old?.share === "number" ? old.share : 0.5) };
  } catch {
    return { pinned: [...defaultPinned], bots: {}, share: 0.5 };
  }
}

let state = read();

function write(next: State) {
  state = next;
  try {
    localStorage.setItem(storeKey, JSON.stringify(state));
  } catch {
    /* private window: still works for this tab */
  }
  subs.forEach((f) => f());
}

function bot(id: string): BotLayout {
  return state.bots[id] ?? empty;
}

function putBot(id: string, b: BotLayout) {
  const cur = bot(id);
  if (cur.split === b.split && cur.order.length === b.order.length && cur.order.every((k, i) => k === b.order[i])) return;
  // The Bot written last moves to the end, so the oldest are the first dropped.
  const rest = Object.entries(state.bots).filter(([k]) => k !== id);
  write({ ...state, bots: Object.fromEntries([...rest.slice(-(keepBots - 1)), [id, b]]) });
}

/** Pin a key to the head of the bar (before `before`, else last). */
export function pin(k: TabKey, before: TabKey | "" = "") {
  if (k === "new") return;
  write({ ...state, pinned: moveBefore(state.pinned, k, before) });
}

/** Unpin a key: it goes back to the rail (a chat back among the open tabs). */
export function unpin(k: TabKey) {
  if (!state.pinned.includes(k)) return;
  write({ ...state, pinned: state.pinned.filter((x) => x !== k) });
}

/** A key opened as a tab on a Bot: kept where it is, else put last. */
export function openTab(botId: string, k: TabKey) {
  if (k === "new") return;
  const b = bot(botId);
  if (b.order.includes(k)) return;
  putBot(botId, { ...b, order: [...b.order, k] });
}

/** The tab bar's strip in a new order (a drag). */
export function setOrder(botId: string, order: TabKey[]) {
  putBot(botId, { ...bot(botId), order: order.filter((k) => k !== "new") });
}

/** A tab closed: out of the strip, and out of the split. */
export function closeTab(botId: string, k: TabKey) {
  const b = bot(botId);
  putBot(botId, { order: b.order.filter((x) => x !== k), split: b.split === k ? "" : b.split });
}

/** Show a tab in the split beside the page ("" closes the split). */
export function setSplit(botId: string, k: TabKey | "") {
  const b = bot(botId);
  if (k === "new") return;
  putBot(botId, { order: k && !b.order.includes(k) ? [...b.order, k] : b.order, split: k });
}

/** The split's share of the row it divides with the page. */
export function setShare(share: number) {
  const s = clampShare(share);
  if (s !== state.share) write({ ...state, share: s });
}

function subscribe(f: () => void) {
  subs.add(f);
  return () => subs.delete(f);
}

export type Layout = { pinned: readonly TabKey[]; order: readonly TabKey[]; split: TabKey | ""; share: number };

export function useLayout(botId: string): Layout {
  const s = useSyncExternalStore(subscribe, () => state);
  const b = s.bots[botId] ?? empty;
  return { pinned: s.pinned, order: b.order, split: b.split, share: s.share };
}
