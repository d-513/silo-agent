import { daysAgo } from "./format.ts";
import { eq } from "./testing.ts";

const now = new Date(2026, 2, 4, 9, 30);
eq(daysAgo(new Date(2026, 2, 4, 0, 5), now), 0, "earlier today");
eq(daysAgo(new Date(2026, 2, 3, 23, 59), now), 1, "just before midnight is yesterday");
eq(daysAgo(new Date(2026, 1, 25, 12), now), 7, "across a month boundary");
eq(daysAgo(new Date(2026, 2, 5, 8), now), -1, "tomorrow is negative");
