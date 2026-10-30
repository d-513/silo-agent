import { t } from "../i18n.ts";
import { type Doing, runLook, started } from "../statusLook.ts";
import type { CallSnapshot, EndReason } from "./session";

// How a call shows what is going on, whichever way it is drawn: the bar's orb
// and the screen's rings read one tone, and both say the same words.

// tone is whose moment it is: you, the Bot's voice, what waits for you, then
// what goes on behind the silence.
export type CallTone = "ended" | "you" | "bot" | "needs_you" | "muted" | "working" | "thinking" | "quiet";

export function callTone(s: Pick<CallSnapshot, "state" | "muted">): CallTone {
  switch (s.state) {
    case "ended":
      return "ended";
    case "hearing":
      return "you";
    case "speaking":
      return "bot";
    case "needs_you":
      return "needs_you";
  }
  if (s.muted) return "muted";
  if (s.state === "working") return "working";
  return s.state === "thinking" ? "thinking" : "quiet";
}

// What the Bot is on while a call waits, in the status lines' words: its task
// if it has one, else the tool the call last heard of.
export function callWork(s: Pick<CallSnapshot, "tool">, doing?: Doing): string {
  return runLook({ ...started, task: doing?.task ?? "", step: s.tool }).word;
}

export function stateWords(s: CallSnapshot, botName: string, doing?: Doing): string {
  switch (s.state) {
    case "connecting":
      return t("Connecting…");
    case "hearing":
      return s.holding ? t("Talking…") : t("Hearing you…");
    case "thinking":
      return t("{{name}} is thinking…", { name: botName });
    case "working":
      return callWork(s, doing);
    case "needs_you":
      return t("Needs you on screen");
    case "speaking":
      return t("{{name}} is speaking", { name: botName });
    case "ended":
      return s.error || endWords(s.ended);
    default:
      if (s.muted) return t("Muted");
      return s.ptt ? t("Hold to talk") : t("Listening");
  }
}

export function endWords(r?: EndReason): string {
  switch (r) {
    case "idle":
      return t("Call ended after a quiet while");
    case "limit":
      return t("Call ended: it reached the longest call allowed");
    case "access":
      return t("Call ended: you no longer have this Bot");
    case "replaced":
      return t("The call moved to another tab");
    case "lost":
      return t("The call's connection dropped");
    case "error":
      return t("The call could not go on");
    default:
      return t("Call ended");
  }
}
