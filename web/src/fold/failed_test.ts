import { lastReplyFailed } from "./failed.ts";
import type { Ev } from "./types.ts";

const ev = (kind: string, body = "", runId = "r1", tool = ""): Ev => ({ kind, body, tool, runId });

function eq(got: boolean, want: boolean, what: string) {
  if (got !== want) throw new Error(`${what}: got ${got}, want ${want}`);
}

eq(lastReplyFailed([]), false, "empty");
eq(lastReplyFailed([ev("user", "hi"), ev("assistant", "yo"), ev("done", "done")]), false, "answered");
eq(lastReplyFailed([ev("user", "hi"), ev("error", "boom"), ev("done", "error")]), true, "failed");
eq(lastReplyFailed([ev("user", "hi"), ev("done", "interrupted")]), true, "interrupted");
eq(lastReplyFailed([ev("user", "hi"), ev("done", "stopped")]), false, "stopped");
// A command's row after the failure (/model) ends no run.
eq(lastReplyFailed([ev("user", "hi"), ev("error", "boom"), ev("done", "error"), ev("compaction", "", "r2")]), true, "after a command");
eq(lastReplyFailed([ev("user", "hi"), ev("error", "boom"), ev("done", "error"), ev("context_cleared", "", "r2")]), false, "cleared");
// A failed manual compaction is not the reply: the one before it counts.
const compactFail = [ev("compacting", "", "r2", "manual"), ev("error", "x", "r2"), ev("done", "error", "r2")];
eq(lastReplyFailed([ev("user", "hi"), ev("assistant", "yo"), ev("done", "done"), ...compactFail]), false, "compaction after a reply");
eq(lastReplyFailed([ev("user", "hi"), ev("done", "error"), ...compactFail]), true, "compaction after a failure");
// A retry that worked.
eq(lastReplyFailed([ev("user", "hi"), ev("done", "error"), ev("assistant", "ok", "r2"), ev("done", "done", "r2")]), false, "retried");
