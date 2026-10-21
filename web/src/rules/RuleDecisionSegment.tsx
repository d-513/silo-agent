import { Bot, Check, CircleHelp, EyeOff, Flag, ScanSearch, X, type LucideIcon } from "lucide-react";
import { ChipRadio } from "../ChipRadio";
import { msg, t } from "../i18n.ts";
import type { Side } from "./model";
import { strictness } from "./model";

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

// modeLabel is the word on a mode's chip.
export function modeLabel(id: string, side: Side): string {
  return t((sideModes[side].find((m) => m.id === id) ?? sideModes[side][0]).label);
}

// The four-way switch for one side of one rule, or of a whole section:
// Allow / Auto / Ask / Deny for calls, Pass / Check / Flag / Withhold for
// results. A floor (the operator's locked value) disables the modes below it:
// the owner may tighten a locked rule, never loosen it.
export function RuleDecisionSegment({
  value,
  onChange,
  disabled,
  size = "md",
  side = "decision",
  floor = "",
}: {
  value: string;
  onChange: (v: string) => void;
  disabled?: boolean;
  size?: "sm" | "md";
  side?: Side;
  floor?: string;
}) {
  const modes = sideModes[side];
  const small = size === "sm";
  return (
    <ChipRadio
      value={value}
      onChange={onChange}
      size={size}
      className=""
      chip={`flex-1 gap-1 ${small ? "px-1.5" : "px-2"}`}
      options={modes.map((m) => {
        const Icon = m.icon;
        const below = floor !== "" && strictness(m.id, side) < strictness(floor, side);
        return {
          value: m.id,
          icon: <Icon size={small ? 11 : 12} />,
          label: <span>{t(m.label)}</span>,
          // Selected segment is a surface chip; only the text carries the tone.
          tone: m.tone,
          disabled: disabled || below,
          title: below ? t("Your operator holds this at {{mode}} or stricter", { mode: modeLabel(floor, side) }) : undefined,
        };
      })}
    />
  );
}
