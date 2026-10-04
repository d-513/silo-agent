import type { DriveTemplate, DriveVar } from "../gen/silo/v1/ui_pb";

export function slug(s: string) {
  return s
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40);
}

export function uniqueName(base: string, taken: Set<string>) {
  const b = slug(base) || "drive";
  if (!taken.has(b)) return b;
  for (let i = 2; ; i++) {
    const n = `${b.slice(0, 36)}-${i}`;
    if (!taken.has(n)) return n;
  }
}

export const NAME_RE = /^[a-z0-9][a-z0-9-]{0,39}$/;

export function visible(v: DriveVar, values: Record<string, string>, t: DriveTemplate) {
  for (const [k, want] of Object.entries(v.visibleIf)) {
    const def = t.vars.find((x) => x.kind === "user" && x.key === k)?.defaultValue ?? "";
    if ((values[k] || def) !== want) return false;
  }
  return true;
}
