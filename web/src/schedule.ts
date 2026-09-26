// Friendly schedules over 5-field cron (minute hour day-of-month month
// day-of-week). The Control Plane only stores cron; these shapes are what the
// Automations editor offers, and anything else stays "custom".

export type Mode = "none" | "minutes" | "hours" | "daily" | "weekly" | "monthly" | "custom";

export type Sched = {
  mode: Mode;
  every: number; // minutes or hours
  minute: number;
  hour: number;
  days: number[]; // 0 = Sunday … 6 = Saturday
  dom: number;
  cron: string; // custom
};

export const DAY_SHORT = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"];
export const MINUTE_STEPS = [5, 10, 15, 20, 30];
export const HOUR_STEPS = [1, 2, 3, 4, 6, 8, 12];

export function defaults(): Sched {
  return { mode: "none", every: 30, minute: 0, hour: 9, days: [1, 2, 3, 4, 5], dom: 1, cron: "" };
}

const num = (s: string, lo: number, hi: number) => {
  if (!/^\d+$/.test(s)) return null;
  const n = Number(s);
  return n >= lo && n <= hi ? n : null;
};

function parseDays(s: string): number[] | null {
  const out = new Set<number>();
  for (const part of s.split(",")) {
    const r = part.split("-");
    if (r.length === 2) {
      const a = num(r[0], 0, 7);
      const b = num(r[1], 0, 7);
      if (a === null || b === null || a > b) return null;
      for (let d = a; d <= b; d++) out.add(d % 7);
    } else {
      const d = num(part, 0, 7);
      if (d === null) return null;
      out.add(d % 7);
    }
  }
  return [...out].sort((x, y) => x - y);
}

// parse reads cron into the friendliest shape that round-trips exactly.
export function parse(cron: string): Sched {
  const base = defaults();
  const c = cron.trim().replace(/\s+/g, " ");
  if (!c) return base;
  const f = c.split(" ");
  const custom = { ...base, mode: "custom" as Mode, cron: c };
  if (f.length !== 5) return custom;
  const [mi, ho, dom, mon, dow] = f;
  if (mon !== "*") return custom;
  let m = /^\*\/(\d+)$/.exec(mi);
  if (m && ho === "*" && dom === "*" && dow === "*" && MINUTE_STEPS.includes(Number(m[1]))) {
    return { ...base, mode: "minutes", every: Number(m[1]) };
  }
  const minute = num(mi, 0, 59);
  if (minute === null) return custom;
  if (dom === "*" && dow === "*") {
    if (ho === "*") return { ...base, mode: "hours", every: 1, minute };
    m = /^\*\/(\d+)$/.exec(ho);
    if (m && HOUR_STEPS.includes(Number(m[1]))) return { ...base, mode: "hours", every: Number(m[1]), minute };
  }
  const hour = num(ho, 0, 23);
  if (hour === null) return custom;
  if (dom === "*" && dow === "*") return { ...base, mode: "daily", minute, hour };
  if (dom === "*") {
    const days = parseDays(dow);
    if (days && days.length) return { ...base, mode: days.length === 7 ? "daily" : "weekly", minute, hour, days: days.length === 7 ? base.days : days };
    return custom;
  }
  const d = num(dom, 1, 31);
  if (d !== null && dow === "*") return { ...base, mode: "monthly", minute, hour, dom: d };
  return custom;
}

function dowField(days: number[]): string {
  const ds = [...new Set(days)].sort((a, b) => a - b);
  if (ds.length === 5 && ds.join() === "1,2,3,4,5") return "1-5";
  if (ds.length === 2 && ds.join() === "0,6") return "0,6";
  return ds.join(",");
}

// toCron writes a shape back as cron. "none" is the empty schedule.
export function toCron(s: Sched): string {
  switch (s.mode) {
    case "none":
      return "";
    case "minutes":
      return `*/${s.every} * * * *`;
    case "hours":
      return s.every === 1 ? `${s.minute} * * * *` : `${s.minute} */${s.every} * * *`;
    case "daily":
      return `${s.minute} ${s.hour} * * *`;
    case "weekly":
      return s.days.length ? `${s.minute} ${s.hour} * * ${dowField(s.days)}` : `${s.minute} ${s.hour} * * *`;
    case "monthly":
      return `${s.minute} ${s.hour} ${s.dom} * *`;
    case "custom":
      return s.cron.trim().replace(/\s+/g, " ");
  }
}

const pad = (n: number) => String(n).padStart(2, "0");
export const clock = (h: number, m: number) => `${pad(h)}:${pad(m)}`;

function ordinal(n: number) {
  const s = n % 100 >= 11 && n % 100 <= 13 ? "th" : ["th", "st", "nd", "rd"][n % 10] ?? "th";
  return `${n}${s}`;
}

// describe is the one-line human reading of a cron schedule.
export function describe(cron: string): string {
  const s = parse(cron);
  switch (s.mode) {
    case "none":
      return "No schedule";
    case "minutes":
      return `Every ${s.every} minutes`;
    case "hours":
      return s.every === 1 ? `Every hour at :${pad(s.minute)}` : `Every ${s.every} hours at :${pad(s.minute)}`;
    case "daily":
      return `Every day at ${clock(s.hour, s.minute)}`;
    case "weekly": {
      const f = dowField(s.days);
      const when = f === "1-5" ? "Weekdays" : f === "0,6" ? "Weekends" : s.days.map((d) => DAY_SHORT[d]).join(", ");
      return `${when} at ${clock(s.hour, s.minute)}`;
    }
    case "monthly":
      return `Monthly on the ${ordinal(s.dom)} at ${clock(s.hour, s.minute)}`;
    case "custom":
      return `Custom: ${s.cron}`;
  }
}
