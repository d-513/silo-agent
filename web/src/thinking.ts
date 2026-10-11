import { msg, t } from "./i18n.ts";

// Thinking levels mirror internal/llm/thinking.go: the CP lists what each
// model accepts (ModelOption.thinkingLevels) and fits a chat's level to the
// model on every turn. The composer shows the level that will actually be
// sent, so it applies the same fit.

export const THINKING_ORDER = ["off", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

const LABELS: Record<string, string> = {
  off: msg("Off"),
  minimal: msg("Minimal"),
  low: msg("Low"),
  medium: msg("Medium"),
  high: msg("High"),
  xhigh: msg("Extra high"),
  max: msg("Max"),
};

export function thinkingLabel(level: string): string {
  return level ? (LABELS[level] ? t(LABELS[level]) : level) : t("Default");
}

function rank(level: string): number {
  return (THINKING_ORDER as readonly string[]).indexOf(level);
}

// fitThinking is llm.NearestThinking: the level itself when the model takes
// it, else the closest one (ties go to the cheaper), else "" (the default).
export function fitThinking(level: string, levels: readonly string[]): string {
  const want = rank(level);
  if (want < 0 || levels.length === 0) return "";
  let best = "";
  let bestDist = Infinity;
  for (const l of levels) {
    const r = rank(l);
    if (r < 0) continue;
    const d = Math.abs(r - want);
    if (d < bestDist || (d === bestDist && r < rank(best))) {
      best = l;
      bestDist = d;
    }
  }
  return best;
}
