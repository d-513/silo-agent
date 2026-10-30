import { ago } from "./format.ts";
import type { Bot, BotActivity } from "./gen/silo/v1/ui_pb";
import { t } from "./i18n.ts";
import { toolMeta } from "./thread/toolInfo.ts";

// How each kind of thing shows its state: the shared lamp, the word beside it
// (a lamp is never alone), and that word's color. Each state vocabulary is the
// server's; the lamp vocabulary is the design system's. Every component reads
// state through here, so a word or a color changes in one place.

// Green is for what is happening now: working breathes, online is a live
// connection. "none" is no lamp: a Bot at rest shows nothing lit.
export type LampStatus = "online" | "working" | "needs_you" | "starting" | "stopped" | "none";

export type StatusLook = { lamp: LampStatus; word: string; tone: string };

const needsYou = (): StatusLook => ({ lamp: "needs_you", word: t("Needs you"), tone: "text-vermilion" });
const onCall = (): StatusLook => ({ lamp: "online", word: t("On a call"), tone: "text-emerald" });

// What a run is about, for the status lines.
export type Doing = Pick<BotActivity, "state" | "waitingOn" | "task" | "step">;

// runLook says what one live run or call is doing, first match wins: waiting
// for you, for someone else, the task in hand, the tool, else Thinking. The
// words are the thread's: a tool row says "Used Python".
export function runLook(a: Doing): StatusLook {
  switch (a.state) {
    case "needs_you":
      return needsYou();
    case "waiting":
      return {
        lamp: "stopped",
        word: a.waitingOn ? t("Waiting on {{who}}", { who: a.waitingOn.split("@")[0] }) : t("Waiting for a manager"),
        tone: "text-ink-3",
      };
    case "on_call":
      return onCall();
  }
  const word = a.task || (a.step ? t("Using {{app}}", { app: toolMeta(a.step).app || a.step }) : t("Thinking"));
  return { lamp: "working", word, tone: "text-emerald" };
}

/** A run with nothing known about it yet: a reply just sent. */
export const started: Doing = { state: "working", waitingOn: "", task: "", step: "" };

/** A run's place on its taskboard, "2/5"; "" with no current task. */
export function progress(a: Pick<BotActivity, "task" | "taskDone" | "taskTotal"> | undefined): string {
  return a?.task && a.taskTotal ? `${a.taskDone}/${a.taskTotal}` : "";
}

// How pressing a row is for the chat it shows on: a chat's own run outranks
// the subagents it leads, and a call is only the backdrop to both.
function rank(a: BotActivity, chatId: string): number {
  if (a.state === "needs_you") return 0;
  if (a.kind === "call") return 3;
  return a.chatId === chatId ? 1 : 2;
}

/** The row that speaks for a chat: its own run, a subagent it leads, its call. */
export function chatDoing(bot: Pick<Bot, "activity">, chatId: string | undefined): BotActivity | undefined {
  if (!chatId) return undefined;
  const rows = bot.activity.filter((a) => a.chatId === chatId || a.leadChatId === chatId);
  return rows.sort((x, y) => rank(x, chatId) - rank(y, chatId))[0];
}

// botLook is the one line for a Bot: what needs you, what it is doing, else
// when it last worked. The box is not part of it at rest: a message or an
// automation starts a stopped Bot, so idle and stopped read the same.
export function botLook(bot: Pick<Bot, "status" | "activity" | "lastActiveAt">): StatusLook {
  const rows = bot.activity.filter((a) => a.state === bot.status);
  switch (bot.status) {
    case "needs_you":
      return needsYou();
    case "working":
      return rows.length > 1 ? { lamp: "working", word: t("Working on {{count}} things", { count: rows.length }), tone: "text-emerald" } : runLook(rows[0] ?? started);
    case "waiting":
      return runLook(rows[0] ?? { ...started, state: "waiting" });
    case "on_call":
      return onCall();
    case "starting":
      return { lamp: "starting", word: t("Starting"), tone: "text-ink-2" };
    default:
      return { lamp: "none", word: bot.lastActiveAt ? t("Last active {{when}}", { when: ago(bot.lastActiveAt) }) : t("Has not worked yet"), tone: "text-ink-3" };
  }
}

// A Bot's standing on the Workspace's overview. A question on the Dashboard is
// not Needs you: nothing is paused on it, so it sorts after the approvals.
export type Mood = "needs_you" | "asks" | "working" | "resting";

export function botMood(b: Pick<Bot, "status" | "dashboardAsks">): Mood {
  if (b.status === "needs_you") return "needs_you";
  if (b.dashboardAsks > 0) return "asks";
  return b.status === "online" || b.status === "stopped" ? "resting" : "working";
}

/** The word on Start/Stop, wherever the machine has one. */
export function powerWord(b: Pick<Bot, "machine">): string {
  if (b.machine === "online") return t("Stop Bot");
  return b.machine === "starting" ? t("Starting…") : t("Start Bot");
}

// A Bot's box, for the places that are about the machine.
export function machineLook(machine: string): StatusLook {
  switch (machine) {
    case "online":
      return containerLook("running");
    case "starting":
      return { lamp: "starting", word: t("Starting"), tone: "text-ink-2" };
    default:
      return containerLook("stopped");
  }
}

// One container.
export function containerLook(state: string): StatusLook {
  switch (state) {
    case "running":
      return { lamp: "online", word: t("Running"), tone: "text-emerald" };
    case "stopped":
      return { lamp: "stopped", word: t("Stopped"), tone: "text-ink-3" };
    default:
      return { lamp: "stopped", word: t("Not created"), tone: "text-ink-3" };
  }
}

// A subagent that has finished.
export function finishedLook(status: string): StatusLook {
  switch (status) {
    case "done":
      return { lamp: "none", word: t("Done"), tone: "text-emerald" };
    case "error":
      return { lamp: "none", word: t("Failed"), tone: "text-vermilion" };
    default:
      return { lamp: "stopped", word: t("Stopped"), tone: "text-ink-3" };
  }
}

// A channel adapter's connection.
export function channelLook(status: string): StatusLook {
  switch (status) {
    case "connected":
      return { lamp: "online", word: t("Connected"), tone: "text-ink-3" };
    case "starting":
      return { lamp: "starting", word: t("Starting…"), tone: "text-ink-3" };
    case "pending_auth":
      // A QR login waiting to be scanned (WhatsApp).
      return { lamp: "needs_you", word: t("Needs linking"), tone: "text-vermilion" };
    case "error":
      return { lamp: "needs_you", word: t("Error"), tone: "text-vermilion" };
    case "stopped":
      return { lamp: "stopped", word: t("Stopped"), tone: "text-ink-3" };
    default:
      return { lamp: "stopped", word: status.replace(/_/g, " "), tone: "text-ink-3" };
  }
}

// A connector attached to a Bot; a built-in one signs in with its own settings,
// not an authorization.
export function connectorLook(status: string, builtin?: boolean): StatusLook {
  switch (status) {
    case "authorized":
      return { lamp: "online", word: builtin ? t("Signed in") : t("Authorized"), tone: "text-ink-3" };
    case "needs_auth":
      return { lamp: "needs_you", word: builtin ? t("Needs setup") : t("Needs authorization"), tone: "text-vermilion" };
    case "initializing":
      return { lamp: "starting", word: t("Initializing"), tone: "text-ink-3" };
    case "error":
      return { lamp: "needs_you", word: t("Error"), tone: "text-vermilion" };
    default:
      return { lamp: "stopped", word: t("Ready"), tone: "text-ink-3" };
  }
}

// A mounted drive.
export function driveLook(state: string): StatusLook {
  switch (state) {
    case "mounted":
      return { lamp: "online", word: t("Mounted"), tone: "text-emerald" };
    case "mounting":
      return { lamp: "starting", word: t("Connecting…"), tone: "text-ink-2" };
    case "needs_auth":
      return { lamp: "needs_you", word: t("Reconnect needed"), tone: "text-vermilion" };
    case "needs_setup":
      return { lamp: "needs_you", word: t("Waiting for an admin"), tone: "text-vermilion" };
    case "needs_input":
      return { lamp: "needs_you", word: t("Needs details"), tone: "text-vermilion" };
    case "error":
      return { lamp: "needs_you", word: t("Error"), tone: "text-vermilion" };
    default:
      return { lamp: "stopped", word: t("Stopped"), tone: "text-ink-3" };
  }
}
