import { daysAgo } from "./format.ts";
import { msg } from "./i18n.ts";

// What the Feed page shows of a list of posts: which source a chip narrows to,
// the one line a post reads as elsewhere, the days the list is cut into and
// which long posts are cut. Pure, so it is tested without a page.

export type Post = { id: string; title: string; body: string; sourceKind: string; sourceName: string; createdAt: string };

export type Filter = "all" | "automation" | "chat" | "channel";

export const FILTERS: { key: Filter; label: string }[] = [
  { key: "all", label: msg("All") },
  { key: "automation", label: msg("Automations") },
  { key: "chat", label: msg("Chats") },
  { key: "channel", label: msg("Channels") },
];

/** Whether a post is listed under a chip. The main chat is a chat; a source
 *  with no chip of its own (a room, another Bot) is under All alone. */
export function shows(f: Filter, p: Pick<Post, "sourceKind">): boolean {
  if (f === "all") return true;
  const kind = p.sourceKind === "main" ? "chat" : p.sourceKind;
  return kind === f;
}

/** The chips worth offering: All, and each source that has a post, only once
 *  there are two sources to tell apart. */
export function chips(posts: Pick<Post, "sourceKind">[]): { key: Filter; label: string; n: number }[] {
  const rows = FILTERS.map((f) => ({ ...f, n: posts.filter((p) => shows(f.key, p)).length }));
  const kinds = rows.filter((r) => r.key !== "all" && r.n > 0);
  return kinds.length < 2 ? [] : [rows[0], ...kinds];
}

/** Markdown as the plain words it reads as, on one line. */
export function plain(md: string): string {
  return md
    .replace(/```[^\n]*\n?/g, " ")
    .replace(/!\[([^\]]*)\]\([^)]*\)/g, "$1")
    .replace(/\[([^\]]+)\]\([^)]*\)/g, "$1")
    .replace(/^\s{0,3}(#{1,6}|>+|[-*+]|\d+[.)])\s+/gm, "")
    .replace(/^\s*\|?[\s:|-]{3,}\|?\s*$/gm, " ")
    .replace(/\|/g, " ")
    .replace(/(\*\*|__|~~|`)/g, "")
    .replace(/(^|\s)[*_](\S[^*_\n]*\S|\S)[*_](?=\s|[.,;:!?)]|$)/g, "$1$2")
    .replace(/\s+/g, " ")
    .trim();
}

const clip = (s: string, n: number) => (s.length > n ? s.slice(0, n - 1).trimEnd() + "…" : s);

/** A closed row: the post's title, or its first line when the Bot gave none,
 *  and under it the words that follow. */
export function summary(p: Pick<Post, "title" | "body">): { head: string; rest: string } {
  const lines = p.body.split("\n").map((l) => l.trim());
  let head = p.title.trim();
  let from = 0;
  if (!head) {
    from = lines.findIndex((l) => plain(l) !== "");
    head = from < 0 ? "" : plain(lines[from]);
    from = from < 0 ? lines.length : from + 1;
  }
  return { head: clip(head, 110), rest: clip(plain(lines.slice(from).join("\n")), 220) };
}

/** Whether a post is too long to stand whole in the list: it is then cut
 *  until the reader asks for the rest. */
export function isLong(body: string): boolean {
  return body.length > 900 || body.split("\n").length > 12;
}

/** Whether every word of a search is somewhere in the post. */
export function matches(p: Post, query: string): boolean {
  const words = query.toLowerCase().split(/\s+/).filter(Boolean);
  if (!words.length) return true;
  const hay = `${p.title}\n${p.body}\n${p.sourceName}`.toLowerCase();
  return words.every((w) => hay.includes(w));
}

export type Day<T> = { key: string; days: number; date: Date | null; posts: T[] };

/** Posts (newest first) cut into calendar days of the reader's own zone,
 *  newest day first. `days` is how many days back: 0 today, 1 yesterday. */
export function byDay<T extends Pick<Post, "createdAt">>(posts: T[], now = new Date()): Day<T>[] {
  const out: Day<T>[] = [];
  const at = new Map<string, Day<T>>();
  for (const p of posts) {
    const ms = Date.parse(p.createdAt);
    const date = Number.isFinite(ms) ? new Date(ms) : null;
    // A clock a little ahead of the server's is still today.
    const days = date ? Math.max(0, daysAgo(date, now)) : -1;
    const key = date ? `d${days}` : "undated";
    let g = at.get(key);
    if (!g) {
      g = { key, days, date, posts: [] };
      at.set(key, g);
      out.push(g);
    }
    g.posts.push(p);
  }
  return out;
}

/** The long posts shown whole when the page opens: what is new, while that
 *  is a few posts; a longer backlog stays cut, its newest whole. */
export function opensWith(unread: string[]): string[] {
  return unread.length <= 3 ? unread : unread.slice(0, 1);
}
