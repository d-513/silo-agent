// The Bot page's tabs, as data. Every page of a Bot is a tab key: the
// Dashboard, the main chat, a side chat, the pages beside the conversations,
// the machine's panes, Settings and Runs. A key is either pinned (it heads the
// tab bar on every Bot and leaves the right rail) or not (it waits in the
// rail, and opens as a tab you can close). Any open tab can also be shown in
// the split beside the page. Pure, so node can test it (layout_test.ts); the
// viewer's choices are kept by layoutStore.ts.

import { onSettings, paneKinds, type PaneKind, type Tab } from "./tabs.ts";

export const pageKeys = [
  "dashboard",
  "main",
  "automations",
  "artifacts",
  "memories",
  "knowledge",
  "feed",
  "channels",
  "files",
  "desktop",
  "console",
  "changes",
  "mail",
  "settings",
  "runs",
] as const;
export type PageKey = (typeof pageKeys)[number];
// "new" is the empty chat (/run/new), a tab with no chat behind it yet.
export type TabKey = PageKey | "new" | `chat:${string}`;

export const defaultPinned: readonly TabKey[] = ["dashboard", "main"];

// Where an unpinned key waits in the rail: the pages, then the machine, then
// Settings at the foot. Runs is not in the rail: it opens from the closed
// chats' menu.
export const railPages: readonly PageKey[] = ["dashboard", "main", "automations", "artifacts", "memories", "knowledge", "feed", "channels"];
export const railMachine: readonly PageKey[] = ["files", "desktop", "console", "changes", "mail"];
export const railFoot: readonly PageKey[] = ["settings"];

export const chatKey = (id: string): TabKey => `chat:${id}`;

/** The chat id a key names ("" when it names none; the main chat is "main"). */
export function chatOf(k: TabKey | ""): string {
  return k.startsWith("chat:") ? k.slice(5) : "";
}

export function isPageKey(k: string): k is PageKey {
  return (pageKeys as readonly string[]).includes(k);
}

export function isPane(k: TabKey | ""): k is PaneKind {
  return (paneKinds as readonly string[]).includes(k);
}

/** A conversation: the main chat or a side chat (the empty one too). */
export function isChat(k: TabKey | ""): boolean {
  return k === "main" || k === "new" || chatOf(k) !== "";
}

/** Whether a key may be shown in the split. The empty chat may not: its draft is the page's. */
export function splittable(k: TabKey | ""): boolean {
  return k !== "" && k !== "new";
}

/** Whether a key may be pinned. The empty chat may not: it has no chat to keep. */
export function pinnable(k: TabKey | ""): boolean {
  return k !== "" && k !== "new";
}

/** The tab the route open now is (a subagent's page is its lead chat's; every Settings tab is Settings). */
export function routeKey(tab: Tab, chatId: string | undefined, fresh: boolean, mainId: string | undefined): TabKey | "" {
  if (tab === "run") {
    if (chatId) return chatId === mainId ? "main" : chatKey(chatId);
    return fresh ? "new" : mainId ? "main" : "";
  }
  if (onSettings(tab)) return "settings";
  return isPageKey(tab) ? tab : "";
}

// What this Bot has, for the keys that depend on it.
export type Avail = {
  // Your main chat exists.
  main: boolean;
  // The Bot has a mail address.
  mail: boolean;
  // You manage the Bot (Runs is a manager's).
  runs: boolean;
  // The chats in the list (any, closed ones too).
  chats: ReadonlySet<string>;
};

export function available(k: TabKey, a: Avail): boolean {
  if (k === "main") return a.main;
  if (k === "mail") return a.mail;
  if (k === "runs") return a.runs;
  const cid = chatOf(k);
  if (cid) return a.chats.has(cid);
  return true;
}

function dedupe<T>(xs: readonly T[]): T[] {
  return [...new Set(xs)];
}

/**
 * The tab bar: the pinned keys this Bot has, in the order pinned, then the
 * strip: the keys opened from the rail and the open side chats (`openChats`,
 * tabOrder.ts), in the viewer's order (`order`, new ones at the end), and last
 * the page open now if it is neither.
 */
export function barKeys(a: { pinned: readonly TabKey[]; order: readonly TabKey[]; openChats: readonly string[]; current: TabKey | ""; avail: Avail }): {
  pinned: TabKey[];
  strip: TabKey[];
} {
  const pinned = dedupe(a.pinned).filter((k) => k !== "new" && available(k, a.avail));
  const held = new Set<TabKey>(pinned);
  const open = new Set(a.openChats);
  const strip = dedupe(a.order).filter((k) => {
    if (held.has(k) || k === "new") return false;
    const cid = chatOf(k);
    return cid ? open.has(cid) : available(k, a.avail);
  });
  for (const id of a.openChats) {
    const k = chatKey(id);
    if (!held.has(k) && !strip.includes(k)) strip.push(k);
  }
  if (a.current && !held.has(a.current) && !strip.includes(a.current)) strip.push(a.current);
  return { pinned, strip };
}

/** The rail: every key that is not pinned, in its group. */
export function railKeys(pinned: readonly TabKey[], avail: Avail): { pages: PageKey[]; machine: PageKey[]; foot: PageKey[] } {
  const keep = (xs: readonly PageKey[]) => xs.filter((k) => !pinned.includes(k) && available(k, avail));
  return { pages: keep(railPages), machine: keep(railMachine), foot: keep(railFoot) };
}

/** Where closing a tab leads: the tab after it, else the one before, else "". */
export function neighbour(list: readonly TabKey[], closing: TabKey): TabKey | "" {
  const i = list.indexOf(closing);
  if (i < 0) return "";
  return list[i + 1] ?? list[i - 1] ?? "";
}

/** The list with `k` moved (or put) just before `before`; at the end when `before` is "" or not in it. */
export function moveBefore(list: readonly TabKey[], k: TabKey, before: TabKey | ""): TabKey[] {
  const out = list.filter((x) => x !== k);
  const i = before && before !== k ? out.indexOf(before) : -1;
  if (i < 0) out.push(k);
  else out.splice(i, 0, k);
  return out;
}

/**
 * The split after the page moved: opening the tab shown in the split swaps
 * the two (what was the page goes beside it); any other move keeps it.
 */
export function splitAfterMove(split: TabKey | "", from: TabKey | "", to: TabKey | ""): TabKey | "" {
  if (!split || split !== to) return split;
  return from && from !== to && splittable(from) ? from : "";
}
