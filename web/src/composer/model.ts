// The composer's small decisions as plain functions: how a number or a level is
// shown, so they can be checked without a screen.
import type { Usage } from "../useRunStream";

// "850", "1.5k", "12k": token counts for the context meter.
export function fmtTokens(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(n >= 10000 ? 0 : 1)}k`;
  return String(n);
}

// How full the model's context window was on the last turn, and the tones the
// ring and the bar take as it fills.
export function contextFill(usage: Pick<Usage, "input" | "output" | "window">) {
  const used = usage.input + usage.output;
  const frac = Math.min(1, used / usage.window);
  return {
    used,
    frac,
    pct: Math.round(frac * 100),
    tone: frac >= 0.9 ? "text-vermilion" : frac >= 0.7 ? "text-ink" : "text-ink-3",
    barTone: frac >= 0.9 ? "bg-vermilion" : frac >= 0.7 ? "bg-ink" : "bg-ink-3",
  };
}

// What the mic's tooltip says: why it is off, or what the click will do.
export function micTitle(supported: boolean, state: string): string {
  if (!supported) return "Dictation needs https or localhost and a browser that can record";
  if (state === "recording") return "Stop and transcribe";
  if (state === "transcribing") return "Transcribing…";
  return "Dictate";
}

// Heights (px) of the five level bars while a take is live; the middle bar
// moves most.
export function levelBars(level: number): number[] {
  return [0.35, 0.7, 1, 0.7, 0.35].map((w) => Math.max(3, Math.round(14 * Math.min(1, level * w * 1.6))));
}

// The textarea's height (px) for its content: grows up to 168, never under 26.
export function growHeight(scrollHeight: number): number {
  return Math.min(Math.max(scrollHeight, 26), 168);
}

export function placeholderFor(chatId?: string, botName?: string): string {
  if (!chatId) return "Select or create a chat to begin…";
  return botName ? `Ask ${botName}…` : "Ask this Bot…";
}
