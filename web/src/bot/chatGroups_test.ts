import { defaultOpen, groupChats, groupKeyOf, isOpen } from "./chatGroups.ts";
import { eq } from "../testing.ts";

// Thursday 8 Oct 2026, mid-morning: the week before it spans the month edge.
const now = new Date(2026, 9, 8, 10, 30);
const at = (y: number, m: number, d: number, h = 12) => new Date(y, m - 1, d, h).toISOString();
const chat = (id: string, updatedAt: string) => ({ id, updatedAt });

const groups = groupChats(
  [
    chat("a", at(2026, 10, 8, 9)),
    chat("b", at(2026, 10, 7, 23)),
    chat("c", at(2026, 10, 1)), // 7 days back
    chat("d", at(2026, 9, 30)), // 8 days back
    chat("e", at(2026, 9, 8)), // 30 days back
    chat("f", at(2026, 9, 7)), // 31 days back: September
    chat("g", at(2026, 9, 2)),
    chat("h", at(2025, 12, 24)),
    chat("i", at(2025, 12, 1)),
    chat("j", "not a time"),
  ],
  now,
);
eq(
  groups.map((g) => [g.key, g.label, g.chats.map((c) => c.id).join("")]),
  [
    ["today", "Today", "a"],
    ["yesterday", "Yesterday", "b"],
    ["week", "Previous 7 days", "c"],
    ["month", "Previous 30 days", "de"],
    ["m:2026-09", "September", "fg"],
    ["m:2025-12", "December 2025", "hi"],
    ["earlier", "Earlier", "j"],
  ],
  "buckets, in the order the list arrives",
);

eq(groupChats([], now), [], "no chats, no groups");
eq(groupChats([chat("x", at(2026, 10, 9, 8))], now).map((g) => g.key), ["today"], "a clock ahead of the server is still today");
eq(groupChats([chat("x", at(2026, 10, 8, 0))], now)[0].key, "today", "just after midnight is today");
eq(groupChats([chat("x", at(2026, 10, 7, 23))], new Date(2026, 9, 8, 0, 1))[0].key, "yesterday", "a minute past midnight is a new day");

// A chat that moves (a reply bumps updatedAt) changes group: the sidebar uses
// the key to open the group the open chat lands in.
eq(groupKeyOf(at(2026, 10, 1), now), "week", "key of a week-old chat");
eq(groupKeyOf(at(2026, 10, 8, 9), now), "today", "key after a reply");
eq(groupKeyOf("", now), "earlier", "no time");

// Recent groups start open, months start folded, and a pick wins either way.
eq(["today", "yesterday", "week", "month"].map(defaultOpen), [true, true, true, true], "recent groups are open");
eq(["m:2026-09", "m:2025-12", "earlier"].map(defaultOpen), [false, false, false], "older groups are folded");
eq(isOpen("today", {}), true, "default open");
eq(isOpen("today", { today: false }), false, "folded by the reader");
eq(isOpen("m:2026-09", { "m:2026-09": true }), true, "opened by the reader");
