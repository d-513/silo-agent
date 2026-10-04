import { lastOf, lastRunning, runOk, waitingTool } from "./find.ts";
import type { Handler } from "./text.ts";
import type { Decision } from "./types.ts";

// Handlers for the approval slip: a row pauses on it, and the decision lets it
// go on (or marks it denied) and leaves a receipt.

// body = approval id, tool = connector.action. Pause the running row; a
// connector call from Python pauses its nested call too.
export const approval: Handler = (out, e) => {
  const t = lastRunning(out, (b) => runOk(b, e.runId));
  if (t) {
    t.waiting = true;
    t.approvalId = e.body;
    const c = lastOf(t.calls, (x) => !!x.running);
    if (c) c.waiting = true;
  }
};

export const decision: Handler = (out, e, key) => {
  let d: Record<string, unknown> = {};
  try {
    d = JSON.parse(e.body || "{}") as Record<string, unknown>;
  } catch {
    /* ignore */
  }
  const verdict = String(d.decision || "") as Decision;
  const t = waitingTool(out, e.runId, String(d.approval_id || "")) ?? waitingTool(out, e.runId);
  if (t) {
    t.waiting = false;
    const c = lastOf(t.calls, (x) => !!x.waiting);
    if (c) {
      c.waiting = false;
      if (verdict === "deny") c.outcome = "denied";
    } else if (verdict === "deny") {
      t.outcome = "denied";
    }
  }
  out.push({ key, type: "receipt", decision: verdict, title: String(d.title || e.tool || ""), target: String(d.target || ""), runId: e.runId });
};
