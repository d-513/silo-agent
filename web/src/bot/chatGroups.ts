import { daysAgo } from "../format.ts";
import { locale, msg, t } from "../i18n.ts";

// The chats column groups a Bot's conversations by when they last moved:
// Today, Yesterday, Previous 7 days, Previous 30 days, then one group per
// month (the year is named only when it is not this one). The list already
// arrives newest first, so a group keeps its members in the order given.

export type ChatGroup<T> = { key: string; label: string; chats: T[] };

const fixed = [
  { key: "today", label: msg("Today") },
  { key: "yesterday", label: msg("Yesterday") },
  { key: "week", label: msg("Previous 7 days") },
  { key: "month", label: msg("Previous 30 days") },
] as const;

function bucket(d: Date, now: Date): { key: string; label: string } {
  const days = daysAgo(d, now);
  // A clock a little ahead of the server's is still today.
  const recent = days <= 0 ? fixed[0] : days === 1 ? fixed[1] : days <= 7 ? fixed[2] : days <= 30 ? fixed[3] : null;
  if (recent) return { key: recent.key, label: t(recent.label) };
  const m = d.toLocaleDateString(locale(), { month: "long" });
  const year = d.getFullYear();
  return {
    key: `m:${year}-${String(d.getMonth() + 1).padStart(2, "0")}`,
    label: year === now.getFullYear() ? m : `${m} ${year}`,
  };
}

/** Split `chats` (newest first) into date groups, newest group first. */
export function groupChats<T extends { updatedAt: string }>(chats: T[], now = new Date()): ChatGroup<T>[] {
  const out: ChatGroup<T>[] = [];
  const at = new Map<string, ChatGroup<T>>();
  const undated: T[] = [];
  for (const c of chats) {
    const ms = Date.parse(c.updatedAt);
    if (!Number.isFinite(ms)) {
      undated.push(c);
      continue;
    }
    const b = bucket(new Date(ms), now);
    let g = at.get(b.key);
    if (!g) {
      g = { ...b, chats: [] };
      at.set(b.key, g);
      out.push(g);
    }
    g.chats.push(c);
  }
  if (undated.length) out.push({ key: "earlier", label: t("Earlier"), chats: undated });
  return out;
}

/** The group a chat sits in, by its last activity ("" when it has no time). */
export function groupKeyOf(updatedAt: string, now = new Date()): string {
  const ms = Date.parse(updatedAt);
  return Number.isFinite(ms) ? bucket(new Date(ms), now).key : "earlier";
}

// The recent groups start open; the month groups start folded, so a long
// history is a short list until the reader goes looking.
export function defaultOpen(key: string): boolean {
  return !key.startsWith("m:") && key !== "earlier";
}

export function isOpen(key: string, picked: Readonly<Record<string, boolean>>): boolean {
  return picked[key] ?? defaultOpen(key);
}
