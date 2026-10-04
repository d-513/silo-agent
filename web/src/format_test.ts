import { ago, day, daysAgo, fmtBytes } from "./format.ts";
import { eq } from "./testing.ts";

const now = new Date(2026, 2, 4, 9, 30);
eq(daysAgo(new Date(2026, 2, 4, 0, 5), now), 0, "earlier today");
eq(daysAgo(new Date(2026, 2, 3, 23, 59), now), 1, "just before midnight is yesterday");
eq(daysAgo(new Date(2026, 1, 25, 12), now), 7, "across a month boundary");
eq(daysAgo(new Date(2026, 2, 5, 8), now), -1, "tomorrow is negative");

// Sizes: whole bytes, then one decimal, up to terabytes.
eq(fmtBytes(0), "0 B", "zero");
eq(fmtBytes(-5), "0 B", "negative");
eq(fmtBytes(512), "512 B", "bytes");
eq(fmtBytes(1536), "1.5 KB", "kilobytes");
eq(fmtBytes(20480), "20.0 KB", "ten kilobytes and up keep the decimal");
eq(fmtBytes(12 * 1024 * 1024), "12.0 MB", "megabytes");
eq(fmtBytes(2 * 1024 ** 3), "2.0 GB", "gigabytes (container RAM)");
eq(fmtBytes(3n * 1024n ** 4n), "3.0 TB", "bigint, terabytes");
eq(fmtBytes(1024 ** 5), "1024.0 TB", "stops at terabytes");

// "ago" steps from just now to minutes to hours, and says never for none.
const iso = (msAgo: number) => new Date(Date.now() - msAgo).toISOString();
eq(ago(""), "never", "no time");
eq(ago(iso(30_000)), "just now", "seconds");
eq(ago(iso(10 * 60_000)), "10 min ago", "minutes");
eq(ago(iso(3 * 3600_000)), "3 h ago", "hours");
eq(ago(iso(5 * 86400_000)), day(iso(5 * 86400_000)), "older than a day and a half is the date");
