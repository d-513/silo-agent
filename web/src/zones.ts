// Time zones as the UI handles them: the device's, the names a Bot can be set
// to, and the offset to show beside one. A Bot's zone is an IANA name; the
// server checks it again. (zones.ts, because timezone.ts would clash with
// TimeZone.tsx on a case-insensitive disk.)

// The IANA zone of this device, "" when the browser will not say.
export function deviceZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone ?? "";
  } catch {
    return "";
  }
}

// Every zone the picker offers, sorted, with UTC and the Bot's current one
// (the server may know a name this browser does not list).
export function zoneNames(current = ""): string[] {
  let names: string[] = [];
  try {
    names = Intl.supportedValuesOf("timeZone");
  } catch {
    names = [];
  }
  const all = new Set([...names, "UTC"]);
  if (current) all.add(current);
  return [...all].sort((a, b) => a.localeCompare(b));
}

// "+02:00" for a zone at a moment, "" for a name the browser does not know.
export function zoneOffset(zone: string, at = new Date()): string {
  if (!zone) return "";
  try {
    const parts = new Intl.DateTimeFormat("en-US", { timeZone: zone, timeZoneName: "longOffset" }).formatToParts(at);
    const m = /^GMT([+-]\d{2}:\d{2})?$/.exec(parts.find((p) => p.type === "timeZoneName")?.value ?? "");
    return m ? (m[1] ?? "+00:00") : "";
  } catch {
    return "";
  }
}

// "Europe/Warsaw · +02:00", or the bare name when the offset is unknown.
export function zoneLabel(zone: string, at = new Date()): string {
  const off = zoneOffset(zone, at);
  return off ? `${zone} · ${off}` : zone;
}
