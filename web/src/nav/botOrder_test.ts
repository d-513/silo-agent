import { byLastMessage, shortWhen } from "./botOrder.ts";
import { eq } from "../testing.ts";

const bot = (id: string, lastMessageAt = "") => ({ id, lastMessageAt });
const ids = (xs: { id: string }[]) => xs.map((x) => x.id);

// Newest conversation first; the silent ones after, in the order they came.
eq(
  ids(byLastMessage([bot("new"), bot("old", "2026-11-01T10:00:00Z"), bot("fresh", "2026-11-14T10:00:00Z"), bot("newer")])),
  ["fresh", "old", "new", "newer"],
  "by last message",
);
eq(ids(byLastMessage([bot("a", "nonsense"), bot("b")])), ["a", "b"], "a bad time is no time");
eq(ids(byLastMessage([])), [], "none");

// A row's time gets shorter the nearer it is.
const now = new Date(2026, 10, 15, 16, 0);
eq(shortWhen(new Date(2026, 10, 15, 9, 5).toISOString(), now, "en-GB"), "09:05", "today");
eq(shortWhen(new Date(2026, 10, 12, 9, 5).toISOString(), now, "en-GB"), "Thu", "this week");
eq(shortWhen(new Date(2026, 10, 3, 9, 5).toISOString(), now, "en-GB"), "3 Nov", "this year");
eq(shortWhen(new Date(2024, 10, 3, 9, 5).toISOString(), now, "en-GB"), "3 Nov 2024", "another year");
eq(shortWhen("", now), "", "no time");

console.log("ok");
