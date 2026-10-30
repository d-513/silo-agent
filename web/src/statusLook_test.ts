import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { eq } from "./testing.ts";
import type { BotActivity } from "./gen/silo/v1/ui_pb";
import { botLook, botMood, channelLook, chatDoing, containerLook, finishedLook, powerWord, progress, runLook } from "./statusLook.ts";

// A QR login waiting to be scanned is not "Connected" and not an error: it
// needs the owner, and says what for.
eq(channelLook("pending_auth").word, "Needs linking", "a pending login is named");
eq(channelLook("pending_auth").lamp, "needs_you", "a pending login lights the needs-you lamp");

eq(channelLook("connected").word, "Connected", "connected");
eq(channelLook("error").lamp, "needs_you", "error");
eq(channelLook("something_new").word, "something new", "an unknown status is shown, not hidden");

// Green is for what is happening: a Bot at rest has no lamp, box up or not.
const row = (o: Partial<BotActivity>): BotActivity => ({ chatId: "", kind: "chat", title: "", step: "", leadChatId: "", state: "working", waitingOn: "", task: "", taskDone: 0, taskTotal: 0, ...o }) as BotActivity;
const bot = (status: string, activity: BotActivity[] = [], lastActiveAt = "") => ({ status, activity, lastActiveAt });
eq(botLook(bot("online")).lamp, "none", "at rest nothing is lit");
eq(botLook(bot("stopped")).lamp, "none", "a stopped Bot rests too: a message starts it");
eq(botLook(bot("online")).word, "Has not worked yet", "a new Bot says so");
eq(botLook(bot("starting")).lamp, "starting", "starting");
eq(botLook(bot("on_call")).lamp, "online", "a call is a live connection");

// What a run says, first match wins.
eq(runLook(row({ task: "Compare flights", step: "exec_python" })).word, "Compare flights", "the task leads");
eq(runLook(row({ step: "exec_python" })).word, "Using Python", "then the tool, in the thread's words");
eq(runLook(row({})).word, "Thinking", "else Thinking");
eq(runLook(row({ state: "needs_you", task: "Compare flights" })).word, "Needs you", "waiting for you beats the task");
eq(runLook(row({ state: "waiting", waitingOn: "ana@test.local" })).word, "Waiting on ana", "someone else's answer names them");
eq(runLook(row({ state: "waiting" })).word, "Waiting for a manager", "a manager's answer");
eq(runLook(row({ state: "waiting" })).lamp, "stopped", "paused on someone else is not working");

eq(progress(row({ task: "x", taskDone: 2, taskTotal: 5 })), "2/5", "progress");
eq(progress(row({ taskDone: 2, taskTotal: 5 })), "", "no task, no progress");
eq(progress(undefined), "", "no run");

// The Bot's line: one run speaks for it, several are counted.
eq(botLook(bot("working", [row({ task: "Compare flights" })])).word, "Compare flights", "one run speaks for the Bot");
eq(botLook(bot("working", [row({}), row({})])).word, "Working on 2 things", "several are counted");
eq(botLook(bot("needs_you", [row({})])).word, "Needs you", "an approval with no live row still needs you");

// A chat's row: what waits for you, its own run, a subagent it leads, its call.
const chat = (activity: BotActivity[]) => chatDoing({ activity }, "c")?.state;
eq(chat([row({ chatId: "x" })]), undefined, "another chat's run");
eq(chat([row({ chatId: "c", kind: "call", state: "on_call" }), row({ chatId: "c", kind: "main" })]), "working", "work beats the call it is on");
eq(chat([row({ chatId: "c", kind: "call", state: "on_call" }), row({ chatId: "s", kind: "subagent", leadChatId: "c", state: "needs_you" })]), "needs_you", "a subagent's approval is its lead chat's");
eq(chatDoing({ activity: [] }, undefined), undefined, "no chat open");

eq(botMood({ status: "needs_you", dashboardAsks: 1 }), "needs_you", "an approval beats a question");
eq(botMood({ status: "stopped", dashboardAsks: 1 }), "asks", "a question waits on a stopped Bot too");
eq(botMood({ status: "waiting", dashboardAsks: 0 }), "working", "paused on someone is not resting");
eq(botMood({ status: "stopped", dashboardAsks: 0 }), "resting", "idle and stopped both rest");
eq(powerWord({ machine: "stopped" }), "Start Bot", "power");
eq(containerLook("exited").word, "Not created", "a container that is neither");
eq(finishedLook("done").word, "Done", "a subagent's end is worded");

// Bot state is read through statusLook, or the copies grow back.
const walk = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return e.name === "gen" ? [] : walk(p);
    return /\.tsx?$/.test(e.name) && !/_test\.ts$/.test(e.name) ? [p] : [];
  });
const src = fileURLToPath(new URL(".", import.meta.url));
for (const f of walk(src)) {
  if (f.endsWith("statusLook.ts")) continue;
  const text = readFileSync(f, "utf8");
  eq(/\b(bot|b)\.status\s*[!=]==/.test(text), false, `${f} compares a Bot's status by hand: use statusLook`);
  eq(/<Lamp status="(needs_you|online|stopped|starting)"\s*\/>\s*\n\s*<StatusWord/.test(text), false, `${f} pairs a lamp and word by hand: use Status`);
}
