// The pure part of Admin → SiloOS: how the machine's numbers read, and the
// network form to and from what siloosd takes. Node-tested in
// siloosView_test.ts.
import type { NetConnection } from "../../gen/silo/v1/siloos_pb";

// uptime is a duration in the largest two units: "3 d 4 h", "5 h 2 min", "40 s".
export function uptime(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  if (d > 0) return `${d} d ${h} h`;
  if (h > 0) return `${h} h ${m} min`;
  if (m > 0) return `${m} min`;
  return `${s} s`;
}

// used is how full something is, 0 to 100.
export function used(total: number, free: number): number {
  if (total <= 0) return 0;
  return Math.round(((total - free) / total) * 100);
}

// secondsLeft is how long until an RFC 3339 instant, never below 0.
export function secondsLeft(at: string, now: number): number {
  const t = Date.parse(at);
  if (Number.isNaN(t)) return 0;
  return Math.max(0, Math.ceil((t - now) / 1000));
}

// NetForm is the page's form for one wired connection.
export type NetForm = { method: "auto" | "manual"; addresses: string; gateway: string; dns: string; ignoreAutoDns: boolean };

// formOf starts the form from the connection as it is.
export function formOf(c: NetConnection): NetForm {
  return {
    method: c.method === "manual" ? "manual" : "auto",
    addresses: c.addresses.join("\n"),
    gateway: c.gateway,
    dns: c.dns.join(", "),
    ignoreAutoDns: c.ignoreAutoDns,
  };
}

// words splits what was typed on commas, spaces and new lines.
export function words(s: string): string[] {
  return s.split(/[\s,;]+/).filter(Boolean);
}

// requestOf is the ApplyConnection request for a form: DHCP sends no
// addresses or gateway of its own.
export function requestOf(uuid: string, f: NetForm) {
  const manual = f.method === "manual";
  return {
    uuid,
    method: f.method,
    addresses: manual ? words(f.addresses) : [],
    gateway: manual ? f.gateway.trim() : "",
    dns: words(f.dns),
    ignoreAutoDns: !manual && f.ignoreAutoDns,
  };
}

// editable says whether the page may change a connection: wired profiles
// only (siloosd marks others "other:<method>").
export function editable(c: NetConnection | undefined): boolean {
  return !!c && !c.method.startsWith("other:");
}

// priorityTone colours a journal line by its syslog priority.
export function priorityTone(p: number): string {
  if (p <= 3) return "text-vermilion";
  if (p === 4) return "text-ink";
  return "text-ink-2";
}

// clock is a journal line's time as the viewer's hh:mm:ss, with the date
// when it is not today.
export function clock(iso: string, now: Date): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const pad = (n: number) => String(n).padStart(2, "0");
  const time = `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  if (d.toDateString() === now.toDateString()) return time;
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${time}`;
}

// The channels a machine can be moved to: the two moving tags, and the
// version it runs when that is a release (staying put).
export type ChannelChoice = { value: string; label: string };

// channelChoices are the choices for a machine on channel/tag running
// version; current is the one it follows now ("" when none of them).
export function channelChoices(channel: string, tag: string, version: string): { choices: ChannelChoice[]; current: string } {
  const choices: ChannelChoice[] = [
    { value: "latest", label: "latest" },
    { value: "master", label: "master" },
  ];
  const release = /^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$/;
  const pin = channel === "pinned" && release.test(tag) ? tag : release.test(version) ? version : "";
  if (pin) choices.push({ value: pin, label: pin });
  const current = channel === "latest" || channel === "master" ? channel : channel === "pinned" ? tag : "";
  return { choices, current };
}
