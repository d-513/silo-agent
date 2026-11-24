import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { eq } from "./testing.ts";
import type { BotActivity } from "./gen/silo/v1/ui_pb";
import { askPhase, botLook, botNow, crestState, channelLook, chatDoing, containerLook, containerMade, containerUp, driveSettling, finishedFailed, finishedLook, folderPhase, machineStarting, needsSignIn, offerPhase, pausedOnYou, powerWord, progress, ranLook, reportMark, runLook, stillRunning } from "./statusLook.ts";

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

// A Bot at rest has nothing to say for itself; one at work does.
eq(botNow(bot("online")), undefined, "at rest there is no now");
eq(botNow(bot("needs_you"))?.word, "Needs you", "paused on you is now");

// What a run says, first match wins.
eq(runLook(row({ task: "Compare flights", step: "exec_python" })).word, "Compare flights", "the task leads");
eq(runLook(row({ step: "exec_python" })).word, "Using Python", "then the tool, in the thread's words");
eq(runLook(row({})).word, "Thinking", "else Thinking");
eq(runLook(row({ state: "needs_you", task: "Compare flights" })).word, "Needs you", "waiting for you beats the task");
eq(runLook(row({ state: "waiting", waitingOn: "ana@test.local" })).word, "Waiting on ana", "someone else's answer names them");
eq(runLook(row({ state: "waiting" })).word, "Waiting for a manager", "a manager's answer");
eq(runLook(row({ state: "waiting" })).lamp, "stopped", "paused on someone else is not working");

// A listed run: at work, or how it ended.
eq([ranLook("running").word, ranLook("done").word, ranLook("error").word, ranLook("stopped").word, ranLook("interrupted").word], ["Working", "Done", "Failed", "Stopped", "Interrupted"], "a listed run");
eq([stillRunning("running"), stillRunning("done")], [true, false], "still running");

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

eq(powerWord({ machine: "stopped" }), "Start Bot", "power");
eq(containerLook("exited").word, "Not created", "a container that is neither");
eq(finishedLook("done").word, "Done", "a subagent's end is worded");

// The predicates and phases the pages read instead of the server's words.
eq(machineStarting({ machine: "starting" }), true, "a box on its way up");
eq(machineStarting({ machine: "online" }), false, "an online box is not starting");
eq(pausedOnYou({ state: "needs_you" }), true, "a run paused on you");
eq(pausedOnYou({ state: "waiting" }), false, "waiting on someone else is not you");
eq([containerUp("running"), containerUp("stopped")], [true, false], "a container is up only while running");
eq([containerMade("stopped"), containerMade("absent"), containerMade("")], [true, false, false], "a stopped container exists, an absent one does not");
eq([needsSignIn("needs_auth"), needsSignIn("authorized")], [true, false], "sign-in needed");
eq([driveSettling("mounting"), driveSettling("mounted")], [true, false], "a drive mounting");
eq([finishedFailed("error"), finishedFailed("done")], [true, false], "a failed run");
eq(reportMark("done"), { mark: "✓", tone: "text-emerald" }, "a done subagent is a tick");
eq(reportMark("error"), { mark: "Failed", tone: "text-vermilion" }, "a failed subagent is worded, not its raw status");
eq(reportMark("interrupted").mark, "Stopped", "an interrupted subagent reads as stopped");
eq([folderPhase("syncing"), folderPhase("error"), folderPhase("idle"), folderPhase("")], ["syncing", "error", "idle", "idle"], "folder phases");
eq([offerPhase("open"), offerPhase("done"), offerPhase("skipped")], ["open", "done", "skipped"], "offer phases");
eq([askPhase("open"), askPhase("submitted"), askPhase("dismissed")], ["open", "answered", "put_away"], "ask phases");

// Bot state is read through statusLook, or the copies grow back. The demo
// folder is left out: it stands in for the server, which decides that state.
const walk = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return e.name === "gen" || e.name === "demo" ? [] : walk(p);
    return /\.tsx?$/.test(e.name) && !/_test\.ts$/.test(e.name) ? [p] : [];
  });
const src = fileURLToPath(new URL(".", import.meta.url));
for (const f of walk(src)) {
  if (f.endsWith("statusLook.ts")) continue;
  const text = readFileSync(f, "utf8");
  eq(/\b(bot|b)\.status\s*[!=]==/.test(text), false, `${f} compares a Bot's status by hand: use statusLook`);
  eq(/\.machine\s*[!=]==/.test(text), false, `${f} compares a Bot's machine by hand: use machineStarting or machineLook`);
  eq(/\.state\s*[!=]==\s*"(running|absent|mounting|needs_auth)"/.test(text), false, `${f} compares a container's or drive's state by hand: use statusLook`);
  eq(/\b(a|activity)\.state\s*[!=]==\s*"needs_you"/.test(text), false, `${f} compares a run's state by hand: use pausedOnYou`);
  eq(/authStatus\s*[!=]==/.test(text), false, `${f} compares a connector's status by hand: use needsSignIn or connectorLook`);
  eq(/<Lamp status="(needs_you|online|stopped|starting)"\s*\/>\s*\n\s*<StatusWord/.test(text), false, `${f} pairs a lamp and word by hand: use Status`);
}

// The crest acts out one state, most pressing first.
const face = (status: string, machine = "online", feedUnread = 0) => ({ status, machine, feedUnread });
eq(crestState(face("needs_you", "online", 3)), "needs_you", "paused on you beats everything");
eq(crestState(face("working"), true), "working", "work beats being written to");
eq(crestState(face("online"), true), "listening", "words in the composer are heard");
eq(crestState(face("on_call")), "listening", "a call is heard");
eq(crestState(face("stopped", "stopped", 2)), "done", "an unread report beats a box that is off");
eq(crestState(face("stopped", "stopped")), "asleep", "a stopped box sleeps");
eq(crestState(face("waiting")), "asleep", "paused on someone else");
eq(crestState(face("online")), "idle", "at rest");
eq(crestState(face("starting", "starting")), "idle", "a box on its way up is not asleep");

// SiloOS: Caddy, a network device, an update job.
import { netDeviceLook, proxyLook, updateJobLook } from "./statusLook.ts";
eq([proxyLook("ok").lamp, proxyLook("error").lamp, proxyLook("off").word, proxyLook("").word], ["online", "needs_you", "Serving nothing", "Not set up"], "Caddy");
eq([netDeviceLook("connected").word, netDeviceLook("connecting (getting IP configuration)").lamp, netDeviceLook("unavailable").word], ["Connected", "starting", "No cable"], "a device");
eq([updateJobLook({ running: true, error: "" }).word, updateJobLook({ running: false, error: "x" }).word, updateJobLook({ running: false, error: "" }).word], ["Working", "Failed", "Done"], "an update job");
