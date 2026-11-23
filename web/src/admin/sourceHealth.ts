import type { SearchSource, SearchTest } from "../gen/silo/v1/ui_pb";
import { ago } from "../format.ts";
import { t } from "../i18n.ts";
import type { StatusLook } from "../statusLook.ts";

// How one source of the Built-in Scraper reads on Admin → Settings → Search:
// a lamp, a word, and a line of detail under its name.
export type SourceLook = StatusLook & { detail: string };

type Source = Pick<SearchSource, "on" | "offReason" | "state" | "reason" | "restingUntil" | "lastOk">;

export function sourceLook(s: Source, now = new Date()): SourceLook {
  if (!s.on) return { lamp: "stopped", word: t("Off"), tone: "text-ink-3", detail: offWhy(s.offReason) };
  switch (s.state) {
    case "ok":
      return { lamp: "online", word: t("Answering"), tone: "text-emerald", detail: t("Last answered {{when}}", { when: ago(s.lastOk) }) };
    case "resting":
      return {
        lamp: "needs_you",
        word: t("Resting"),
        tone: "text-vermilion",
        detail: t("{{reason}}; left alone for {{wait}}", { reason: s.reason || t("it failed"), wait: waitWord(s.restingUntil, now) }),
      };
    case "failing":
      return { lamp: "needs_you", word: t("Failed last time"), tone: "text-vermilion", detail: t("{{reason}}; the next search tries it again", { reason: s.reason || t("it failed") }) };
    default:
      return { lamp: "stopped", word: t("Not asked yet"), tone: "text-ink-3", detail: t("No search has needed it since the server started") };
  }
}

// The server words why a source is off in English; these are its three cases.
function offWhy(reason: string): string {
  if (reason.includes("key")) return t("Add its API key above to use it");
  if (reason.includes("challenge")) return t("Answers most addresses with a script challenge; name it under Sources to try it");
  if (reason.includes("sources list")) return t("Not named under Sources");
  return reason;
}

// How long until a time, in the unit that fits.
export function waitWord(iso: string, now = new Date()): string {
  const s = Math.max(0, Math.round((new Date(iso).getTime() - now.getTime()) / 1000));
  if (!iso || Number.isNaN(s)) return t("a moment");
  if (s < 90) return `${Math.max(1, s)} s`;
  return t("{{n}} min", { n: Math.round(s / 60) });
}

// The one line that sums up a test search.
export function testLine(r: Pick<SearchTest, "tookMs" | "hits" | "error">): { ok: boolean; text: string } {
  if (r.error) return { ok: false, text: r.error };
  const secs = (r.tookMs / 1000).toFixed(1);
  if (r.hits.length === 0) return { ok: false, text: t("No results in {{secs}} s", { secs }) };
  return { ok: true, text: t("{{count}} results in {{secs}} s", { count: r.hits.length, secs }) };
}
