import type { Rule } from "../gen/silo/v1/ui_pb";
import type { Side } from "./model";
import { RuleDecisionSegment } from "./RuleDecisionSegment";
import { t } from "../i18n.ts";

// One action and the switch for the side of its rule the page is showing.
export function RuleRow({ rule: r, side, onChange }: { rule: Rule; side: Side; onChange: (decision: string) => void }) {
  return (
    <div className="group/row -mx-2.5 flex flex-col gap-2 rounded-sm border-t border-line px-2.5 py-2.5 transition-colors hover:bg-well wide:flex-row wide:items-center wide:gap-4">
      <div className="flex min-w-0 flex-1 items-center gap-2.5">
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className={r.connector === "secrets" ? "font-mono text-[13px] font-medium text-ink" : "text-[14px] font-medium text-ink"}>
              {r.title || r.action}
            </span>
            {r.connector !== "secrets" && r.title && r.action && r.title !== r.action && r.action !== "*" && (
              <span className="rounded-sm bg-well px-1.5 py-0.5 font-mono text-[11px] text-ink-3">{r.action}</span>
            )}
            {r.connector === "secrets" && <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] text-ink-3">{t("Secret")}</span>}
          </div>
        </div>
      </div>
      <div className="w-full wide:w-[260px] wide:shrink-0">
        <RuleDecisionSegment side={side} value={r[side]} onChange={onChange} />
      </div>
    </div>
  );
}
