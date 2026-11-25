// What the Admin panel says about a provider's program (ProviderProgram.tsx),
// kept pure so it is tested without a browser.

type Program = { name: string; installed: boolean; version: string; path: string; managed: boolean; canInstall: boolean; installing: boolean; doneBytes: bigint; totalBytes: bigint };
type T = (key: string, vars?: Record<string, unknown>) => string;

/** The program's state as one line. */
export function programLine(p: Program, t: T): string {
  if (!p.installed) return t("{{name}} is not installed on this server", { name: p.name });
  const what = p.version ? `${p.name} ${p.version}` : p.name;
  if (p.managed) return t("{{what}} · installed by Silo", { what });
  if (!p.canInstall) return t("{{what}} · your own, at {{path}}", { what, path: p.path });
  return t("{{what}} · found on this server at {{path}}", { what, path: p.path });
}

/** How far a download is, 0 to 100; null when the size is not known. */
export function installPercent(p: Pick<Program, "installing" | "doneBytes" | "totalBytes">): number | null {
  if (!p.installing || p.totalBytes <= 0n) return null;
  const pct = Number((p.doneBytes * 100n) / p.totalBytes);
  return Math.max(0, Math.min(100, pct));
}
