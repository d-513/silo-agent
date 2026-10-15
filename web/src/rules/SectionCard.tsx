import { ChevronRight } from "lucide-react";
import type { Rule, RuleSection } from "../gen/silo/v1/ui_pb";
import { sectionDecision, floorOf, type Side } from "./model";
import { RuleDecisionSegment } from "./RuleDecisionSegment";
import { RuleRow } from "./RuleRow";
import { t } from "../i18n.ts";

// One folio of rules (the Bot, its secrets, one connector): a header with a
// switch for the whole section, and a row per action underneath.
export function SectionCard({
  section: s,
  side,
  open,
  locked,
  onToggle,
  onSection,
  onRule,
}: {
  section: RuleSection;
  side: Side;
  open: boolean;
  // locked keeps the card open (a search is showing its matches).
  locked: boolean;
  onToggle: (open: boolean) => void;
  onSection: (decision: string) => void;
  onRule: (rule: Rule, decision: string) => void;
}) {
  const shared = sectionDecision(s, side);
  // The strictest operator lock in the section: a section-wide value looser
  // than it would be refused row by row anyway.
  const floor = floorOf(s, side);
  return (
    <details
      className="group mb-5 rounded-card shadow-card bg-surface"
      open={open}
      onToggle={(e) => {
        if (locked) return;
        onToggle(e.currentTarget.open);
      }}
    >
      <summary className="flex cursor-pointer list-none flex-wrap items-start gap-3 p-4 transition-colors hover:bg-well [&::-webkit-details-marker]:hidden">
        <ChevronRight size={14} className="mt-1.5 shrink-0 text-ink-3 transition-transform group-open:rotate-90" />
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <h3 className="text-[16px] font-medium text-ink">{s.title}</h3>
            {s.rules.length > 0 && (
              <span className="rounded-sm bg-well px-1.5 py-0.5 font-mono text-[11px] text-ink-3">
                {t("{{count}} actions", { count: s.rules.length })}
              </span>
            )}
          </div>
          <p className="mt-1 text-[13px] text-ink-2">{side === "output" ? s.outputSummary : s.summary}</p>
        </div>
        {s.rules.length > 0 && (
          <div
            className="flex w-full items-center justify-between gap-2 pt-2 wide:w-auto wide:justify-end wide:pt-0"
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
            }}
          >
            <span className="text-[11px] font-medium uppercase tracking-wider text-ink-3 wide:hidden">{t("All")}</span>
            {shared === "" && <span className="hidden text-[11px] font-medium text-ink-3 wide:inline">{t("Mixed")}</span>}
            <div
              className="w-full wide:w-[260px] wide:shrink-0"
              title={shared === "" ? t("Mixed. Pick one to apply to every action below.") : t("Apply to every action below.")}
            >
              <RuleDecisionSegment size="sm" side={side} value={shared} floor={floor} onChange={onSection} />
            </div>
          </div>
        )}
      </summary>
      <div className="px-4 pb-4">
        {s.rules.length === 0 ? (
          <p className="py-2 text-ink-2">{t("Nothing to set here yet.")}</p>
        ) : (
          s.rules.map((r) => <RuleRow key={`${r.connector}.${r.action}`} rule={r} side={side} onChange={(d) => onRule(r, d)} />)
        )}
      </div>
    </details>
  );
}
