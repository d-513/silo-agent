import { msg, t } from "../i18n.ts";
import type { Side } from "./model";

const entries: Record<Side, { dot: string; title: string; text: string }[]> = {
  decision: [
    { dot: "bg-lamp", title: msg("Allow"), text: msg("Runs immediately without asking") },
    { dot: "bg-ink-2", title: msg("Auto"), text: msg("The approval model decides from your policy") },
    { dot: "bg-line", title: msg("Ask"), text: msg("Pauses run and opens an approval slip") },
    { dot: "bg-vermilion", title: msg("Deny"), text: msg("Refuses execution automatically") },
  ],
  output: [
    { dot: "bg-lamp", title: msg("Pass"), text: msg("The Bot reads the result as it is") },
    { dot: "bg-ink-2", title: msg("Check"), text: msg("The filter model passes, flags or withholds it by your policy") },
    { dot: "bg-line", title: msg("Flag"), text: msg("Always handed over marked as untrusted, with no model asked") },
    { dot: "bg-vermilion", title: msg("Withhold"), text: msg("The Bot never reads the result") },
  ],
};

// What each of the four choices does, above the rules.
export function DecisionLegend({ side = "decision" }: { side?: Side }) {
  return (
    <div className="mb-5 rounded-card bg-well p-3.5">
      <div className="grid grid-cols-1 gap-2.5 sm:grid-cols-2 lg:grid-cols-4">
        {entries[side].map((e) => (
          <div key={e.title} className="flex items-start gap-2.5">
            <span className={`mt-1 flex h-2 w-2 shrink-0 rounded-full ${e.dot}`} />
            <div>
              <div className="text-[13px] font-medium text-ink">{t(e.title)}</div>
              <div className="text-[12px] leading-snug text-ink-3">{t(e.text)}</div>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
