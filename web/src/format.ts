// Calendar days from d back to now: 0 is today, 1 is yesterday.
export function daysAgo(d: Date, now = new Date()): number {
  const midnight = (x: Date) => new Date(x.getFullYear(), x.getMonth(), x.getDate()).getTime();
  return Math.round((midnight(now) - midnight(d)) / 86400000);
}

// "Mar 4, 2026" for an ISO timestamp, "" when there is none.
export function day(iso: string) {
  return iso ? new Date(iso).toLocaleDateString(undefined, { year: "numeric", month: "short", day: "numeric" }) : "";
}
