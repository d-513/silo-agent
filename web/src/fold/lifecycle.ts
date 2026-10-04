import { closeThinking, runOk, runningCompaction } from "./find.ts";
import type { Handler } from "./text.ts";

// Handlers for what happens to the run itself: compaction, its end, artifact
// cards, and errors.

export const compacting: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  out.push({ key, type: "compaction", text: "", reason: e.tool, running: true, runId: e.runId });
};

// The summary: it settles the running compaction, or stands alone.
export const compaction: Handler = (out, e, key) => {
  const c = runningCompaction(out, e.runId);
  if (c) {
    c.text = e.body;
    c.running = false;
  } else {
    out.push({ key, type: "compaction", text: e.body, reason: e.tool, runId: e.runId });
  }
};

// The run ended. Anything still running in it was cut off; a Stop leaves a
// receipt.
export const done: Handler = (out, e, key) => {
  const cut = e.body === "stopped" || e.body === "interrupted" || e.body === "error";
  for (let c = runningCompaction(out, e.runId); c; c = runningCompaction(out, e.runId)) c.running = false;
  for (const b of out) {
    if (b.type !== "tool" || !runOk(b, e.runId)) continue;
    if (b.running) {
      b.running = false;
      if (cut) b.outcome = "stopped";
    }
    b.waiting = false;
    for (const c of b.calls ?? []) {
      if (c.running) {
        c.running = false;
        if (cut) c.outcome = "stopped";
      }
      c.waiting = false;
    }
  }
  closeThinking(out, e.at);
  const last = out[out.length - 1];
  if (last?.type === "assistant" && last.streaming && e.runId) {
    last.streaming = false;
    last.bounds = undefined;
  }
  if (e.body === "stopped") out.push({ key, type: "receipt", decision: "stopped", title: "this run", target: "", runId: e.runId });
};

// An artifact card: a later event for the same approval, or the same file or
// skill, updates the card in place.
export const artifact: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  let body: Record<string, unknown> = {};
  try {
    body = JSON.parse(e.body || "{}") as Record<string, unknown>;
  } catch {
    /* ignore */
  }
  const name = String(body.name || "");
  const approvalId = String(body.approval_id || "");
  const next = {
    key,
    type: "artifact" as const,
    artifactType: String(body.type || "skill"),
    name,
    title: String(body.title || name),
    path: String(body.path || ""),
    scope: String(body.scope || ""),
    approvalId,
    status: String(body.status || ""),
    size: typeof body.size === "number" ? body.size : undefined,
    runId: e.runId,
  };
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type !== "artifact") continue;
    if ((approvalId && b.approvalId === approvalId) || (name && b.name === name && b.path === next.path)) {
      Object.assign(b, next, { key: b.key });
      return;
    }
  }
  out.push(next);
};

export const error: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  const c = runningCompaction(out, e.runId);
  if (c) c.running = false;
  out.push({ key, type: "error", text: e.body });
};
