import { eq } from "../testing.ts";
import { callCrest, callTone, callWork, stateWords } from "./look.ts";
import type { CallSnapshot } from "./session";

const snap = (o: Partial<CallSnapshot>): CallSnapshot => ({ state: "listening", tool: "", heard: "", hearing: "", saying: "", muted: false, ptt: false, holding: false, level: 0, chatId: "", callId: "", error: "", startedAt: 0, ...o });

// Whose moment it is, the same for the bar and the screen.
eq(callTone(snap({ state: "hearing" })), "you", "you");
eq(callTone(snap({ state: "speaking", muted: true })), "bot", "muted, the Bot's voice is still its voice");
eq(callTone(snap({ state: "working", muted: true })), "muted", "muted outranks what goes on behind the silence");
eq(callTone(snap({ state: "needs_you", muted: true })), "needs_you", "what waits for you outranks being muted");
eq(callTone(snap({ state: "thinking" })), "thinking", "thinking");
eq(callTone(snap({})), "quiet", "listening");

const task = { state: "working", waitingOn: "", task: "Compare flights", step: "" };
eq(callWork({ tool: "exec_python" }), "Using Python", "the call's tool");
eq(callWork({ tool: "exec_python" }, task), "Compare flights", "the task leads");
eq(stateWords(snap({ state: "needs_you" }), "Scout"), "Needs you on screen", "an approval says where to look");

// The crest on the call screen acts the call out.
eq(callCrest(snap({ state: "hearing" })), "listening", "it listens while you speak");
eq(callCrest(snap({ state: "thinking" })), "working", "it works behind the silence");
eq(callCrest(snap({ state: "needs_you" })), "needs_you", "an approval waits");
eq(callCrest(snap({ state: "speaking" })), "idle", "its own voice is its resting face");
eq(callCrest(snap({ state: "ended" })), "asleep", "a call that ended");
