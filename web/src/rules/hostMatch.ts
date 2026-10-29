// hostMatch mirrors the gateway's list matching (internal/egress/hosts.go), so
// a page can say whether a host is already on a list: "example.com" covers the
// host and every name below it, "*.example.com" only the names below, an IP
// itself, a CIDR every address in it (IPv4 only here; an IPv6 range is not
// recognised and reads as not covering).

export type Listed = { kind: string; pattern: string; fromOperator: boolean };

export function normHost(h: string): string {
  h = h.trim().toLowerCase();
  if (h.startsWith("[")) h = h.slice(1, h.indexOf("]") < 0 ? undefined : h.indexOf("]"));
  else if (h.split(":").length === 2) h = h.split(":")[0];
  return h.replace(/\.$/, "");
}

function ipv4(s: string): number | null {
  const m = /^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/.exec(s);
  if (!m) return null;
  const parts = m.slice(1).map(Number);
  if (parts.some((p) => p > 255)) return null;
  return ((parts[0] << 24) | (parts[1] << 16) | (parts[2] << 8) | parts[3]) >>> 0;
}

function isAddr(s: string) {
  return ipv4(s) !== null || s.includes(":");
}

/** Whether one pattern covers host. */
export function covers(pattern: string, host: string): boolean {
  host = normHost(host);
  const p = pattern.trim().toLowerCase();
  if (!host || !p) return false;
  const slash = p.indexOf("/");
  if (slash >= 0) {
    const base = ipv4(p.slice(0, slash));
    const bits = Number(p.slice(slash + 1));
    const a = ipv4(host);
    if (base === null || a === null || !Number.isInteger(bits) || bits < 0 || bits > 32) return false;
    const mask = bits === 0 ? 0 : (~0 << (32 - bits)) >>> 0;
    return ((a & mask) >>> 0) === ((base & mask) >>> 0);
  }
  const np = normHost(p);
  if (isAddr(host)) return np === host;
  if (np.startsWith("*.")) return host.endsWith("." + np.slice(2));
  return host === np || host.endsWith("." + np);
}

/** The first entry of `kind` that covers host, the Bot's own exact one first. */
export function listedAs<T extends Listed>(entries: readonly T[], kind: string, host: string): T | undefined {
  const of = entries.filter((e) => e.kind === kind && covers(e.pattern, host));
  const h = normHost(host);
  return of.find((e) => !e.fromOperator && normHost(e.pattern) === h) ?? of[0];
}
