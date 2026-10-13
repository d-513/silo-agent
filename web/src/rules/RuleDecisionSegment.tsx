import { Bot, Check, CircleHelp, EyeOff, Flag, ScanSearch, X, type LucideIcon } from "lucide-react";
import { msg, t } from "../i18n.ts";
import type { Side } from "./model";

// One choice of a rule. Only the selected chip's text carries the tone.
type Mode = { id: string; label: string; icon: LucideIcon; tone: string };

// May the action run.
const callModes: Mode[] = [
  { id: "allow", label: msg("Allow"), icon: Check, tone: "text-emerald" },
  { id: "auto", label: msg("Auto"), icon: Bot, tone: "text-ink-2" },
  { id: "ask", label: msg("Ask"), icon: CircleHelp, tone: "text-ink" },
  { id: "deny", label: msg("Deny"), icon: X, tone: "text-vermilion" },
];

// What happens to its result before the Bot reads it. The order mirrors the
// call side: through, a model decides, a middle way, never.
const outputModes: Mode[] = [
  { id: "pass", label: msg("Pass"), icon: Check, tone: "text-emerald" },
  { id: "check", label: msg("Check"), icon: ScanSearch, tone: "text-ink-2" },
  { id: "flag", label: msg("Flag"), icon: Flag, tone: "text-ink" },
  { id: "withhold", label: msg("Withhold"), icon: EyeOff, tone: "text-vermilion" },
];

export const sideModes: Record<Side, Mode[]> = { decision: callModes, output: outputModes };

// The four-way switch for one side of one rule, or of a whole section:
// Allow / Auto / Ask / Deny for calls, Pass / Check / Flag / Withhold for
// results.
export function RuleDecisionSegment({
  value,
  onChange,
  disabled,
  size = "md",
  side = "decision",
}: {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  size?: "sm" | "md";
  side?: Side;
}) {
  const modes = sideModes[side];
  return (
    <div role="radiogroup" className="flex select-none items-center gap-0.5 rounded-control bg-well p-[3px]">
      {modes.map((m) => {
        const active = value === m.id;
        const Icon = m.icon;
        // Selected segment is a surface chip; only the text carries the tone.
        const tone = m.tone;
        return (
          <button
            key={m.id}
            type="button"
            role="radio"
            aria-checked={active}
            disabled={disabled}
            className={`flex flex-1 items-center justify-center gap-1 rounded-sm font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:scale-[.97] active:duration-[70ms] ${
              size === "sm" ? "h-7 px-1.5 text-[11.5px]" : "h-8 px-2 text-[12.5px]"
            } ${active ? `bg-surface shadow-card ${tone}` : "text-ink-3 hover:bg-pressed hover:text-ink"} ${
              disabled ? "cursor-not-allowed opacity-50" : "cursor-pointer"
            }`}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              onChange(m.id);
            }}
          >
            <Icon size={size === "sm" ? 11 : 12} />
            <span>{t(m.label)}</span>
          </button>
        );
      })}
    </div>
  );
}
