// How the UI writes sizes and times. Every page that shows one comes here, so a
// byte count or a "Tue 07:30" reads the same wherever it appears.

// "512 B", "1.5 KB", "12.0 MB", "2.0 GB": whole bytes, then one decimal.
export function fmtBytes(n: bigint | number) {
  const v = typeof n === "bigint" ? Number(n) : n;
  if (v <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let x = v;
  let i = 0;
  while (x >= 1024 && i < units.length - 1) {
    x /= 1024;
    i++;
  }
  return i === 0 ? `${Math.round(x)} B` : `${x.toFixed(1)} ${units[i]}`;
}

const clockTime = (d: Date) => d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
const weekdayShort = (d: Date) => d.toLocaleDateString([], { weekday: "short" });
const monthDay = (d: Date) => d.toLocaleDateString([], { month: "short", day: "numeric" });

// Calendar days from d back to now: 0 is today, 1 is yesterday.
export function daysAgo(d: Date, now = new Date()): number {
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((midnight(now) - midnight(d)) / 86400000);
}

// "Mar 4, 2026" for an ISO timestamp, "" when there is none.
export function day(iso: string) {
  return iso ? new Date(iso).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "";
}

// "just now", "12 min ago", "3 h ago", then the date; "never" for none.
export function ago(iso: string) {
  if (!iso) return "never";
  const s = Math.max(0, (Date.now() - new Date(iso).getTime()) / 1000);
  if (s < 90) return "just now";
  if (s < 5400) return `${Math.round(s / 60)} min ago`;
  if (s < 129600) return `${Math.round(s / 3600)} h ago`;
  return day(iso);
}

// Chat-row meta: "Just now", "12 min ago", "09:12", "Yesterday", "Tue", "Mar 4".
export function chatWhen(iso: string) {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "";
  const d = new Date(t);
  const now = new Date();
  const mins = Math.floor((now.getTime() - t) / 60000);
  if (mins < 1) return "Just now";
  if (mins < 60) return `${mins} min ago`;
  const days = daysAgo(d, now);
  if (days === 0) return clockTime(d);
  if (days === 1) return "Yesterday";
  if (days < 7) return weekdayShort(d);
  return monthDay(d);
}

// A run's divider: "Today 09:12", "Yesterday 18:00", "Tue 07:30", "Mar 4 07:30".
export function runWhen(iso?: string) {
  const t = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(t)) return "";
  const d = new Date(t);
  const time = clockTime(d);
  const days = daysAgo(d);
  if (days === 0) return `Today ${time}`;
  if (days === 1) return `Yesterday ${time}`;
  if (days < 7) return `${weekdayShort(d)} ${time}`;
  return `${monthDay(d)} ${time}`;
}

// A Feed post's time: just the clock today, the date and clock before.
export function feedStamp(iso: string) {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return daysAgo(d) === 0 ? clockTime(d) : d.toLocaleString(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" });
}

// When an automation fires next: "Tue, Oct 4 09:12".
export function nextWhen(iso: string) {
  if (!iso) return "";
  const d = new Date(iso);
  return `${d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })} ${clockTime(d)}`;
}

// "Oct 4, 2026, 9:12 AM": a time the reader may want in full.
export function fullTime(d: Date) {
  return d.toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}
