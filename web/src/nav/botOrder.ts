// How the sidebar lists a Workspace's Bots. Pure, so node can test it
// (botOrder_test.ts).

type Listed = { lastMessageAt: string };

/**
 * Bots by their newest message, newest first. A Bot nobody has written to
 * keeps the order it came in (newest made first), after the ones with a
 * conversation.
 */
export function byLastMessage<T extends Listed>(bots: readonly T[]): T[] {
  const at = (b: T) => Date.parse(b.lastMessageAt) || 0;
  return bots
    .map((b, i) => ({ b, i }))
    .sort((x, y) => at(y.b) - at(x.b) || x.i - y.i)
    .map((x) => x.b);
}

/** A time as a list row has room for it: 14:05 today, Mon this week, 3 Nov, or 3 Nov 2024. */
export function shortWhen(iso: string, now = new Date(), lang?: string): string {
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return "";
  const d = new Date(ms);
  const day = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  const days = Math.round((day(now) - day(d)) / 86_400_000);
  if (days <= 0) return d.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" });
  if (days < 7) return d.toLocaleDateString(lang, { weekday: "short" });
  return d.toLocaleDateString(lang, d.getFullYear() === now.getFullYear() ? { day: "numeric", month: "short" } : { day: "numeric", month: "short", year: "numeric" });
}
