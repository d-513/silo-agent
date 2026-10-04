import { closeThinking, isPartialArgs, lastRunning, runOk } from "./find.ts";
import type { Handler } from "./text.ts";
import type { Block, Ev, NestedCall, ToolBlock } from "./types.ts";

// Handlers for tool rows and the connector calls a Python tool makes inside one.

// upsertTool is how a streaming tool event finds its row: the running call with
// the event's own name, else the latest running call `fallback` accepts, else a
// new running row. `setArgs` applies the event's body to a call that exists.
function upsertTool(out: Block[], e: Ev, key: string, fallback: (b: ToolBlock) => boolean, setArgs: (t: ToolBlock) => void) {
  const named = e.tool ? lastRunning(out, (b) => b.name === e.tool && runOk(b, e.runId)) : undefined;
  const t = named || lastRunning(out, (b) => fallback(b) && runOk(b, e.runId));
  if (!t) {
    out.push({ key, type: "tool", name: e.tool || "tool", args: e.body, running: true, runId: e.runId });
    return;
  }
  setArgs(t);
  if (e.tool) t.name = e.tool;
  if (e.runId) t.runId = e.runId;
}

export const toolArgsChunk: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  upsertTool(out, e, key, () => true, (t) => {
    t.args += e.body;
  });
};

export const tool: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  upsertTool(out, e, key, (b) => isPartialArgs(b.args), (t) => {
    if (e.body) t.args = e.body;
  });
};

export const toolChunk: Handler = (out, e) => {
  closeThinking(out, e.at);
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "tool" && b.running && (!e.runId || b.runId === e.runId)) {
      b.result = (b.result || "") + e.body;
      break;
    }
  }
};

export const toolResult: Handler = (out, e) => {
  closeThinking(out, e.at);
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "tool" && b.running && (b.name === e.tool || !e.tool) && (!e.runId || b.runId === e.runId)) {
      b.result = e.body;
      b.running = false;
      break;
    }
  }
};

export const call: Handler = (out, e, key) => {
  closeThinking(out, e.at);
  const item: NestedCall = { key, title: e.body || e.tool || "call", name: e.tool, running: true };
  const t = lastRunning(out, (b) => runOk(b, e.runId));
  if (t) {
    t.calls = t.calls || [];
    t.calls.push(item);
  } else {
    out.push({ key, type: "tool", name: "call", args: "", running: true, runId: e.runId, calls: [item] });
  }
};

export const callResult: Handler = (out, e) => {
  closeThinking(out, e.at);
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type !== "tool" || (e.runId && b.runId && b.runId !== e.runId)) continue;
    const cs = b.calls;
    if (!cs?.length) continue;
    for (let k = cs.length - 1; k >= 0; k--) {
      if (cs[k].running && (cs[k].name === e.tool || !e.tool)) {
        cs[k].result = e.body;
        cs[k].running = false;
        break;
      }
    }
    break;
  }
};
