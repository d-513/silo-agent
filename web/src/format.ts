// Calendar days from d back to now: 0 is today, 1 is yesterday.
export function daysAgo(d: Date, now = new Date()): number {
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((midnight(now) - midnight(d)) / 86400000);
}

// "Mar 4, 2026" for an ISO timestamp, "" when there is none.
export function day(iso: string) {
  return iso ? new Date(iso).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "";
}

// "Today 09:12", "Yesterday 18:00", "Tue 07:30", "Mar 4 07:30".
export function runWhen(iso?: string) {
  const t = iso ? Date.parse(iso) : NaN;
  if (!Number.isFinite(t)) return "";
  const d = new Date(t);
  const time = d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" });
  const days = daysAgo(d);
  if (days === 0) return `Today ${time}`;
  if (days === 1) return `Yesterday ${time}`;
  if (days < 7) return `${d.toLocaleDateString([], { weekday: "short" })} ${time}`;
  return `${d.toLocaleDateString([], { month: "short", day: "numeric" })} ${time}`;
}
