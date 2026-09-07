export type Ev = { id?: string; kind: string; body: string; tool: string; runId?: string };

export type NestedCall = { key: string; title: string; name: string; result?: string; running?: boolean };

export type ToolBlock = { key: string; type: "tool"; name: string; args: string; result?: string; running?: boolean; runId?: string; calls?: NestedCall[] };

export type Block =
  | { key: string; type: "user"; text: string }
  | { key: string; type: "assistant"; text: string; streaming?: boolean }
  | { key: string; type: "thinking"; text: string; streaming?: boolean }
  | ToolBlock
  | { key: string; type: "error"; text: string };

const staleKey = /openrouter api key|set openrouter|silo_openrouter|api key in admin/i;

function runOk(b: ToolBlock, runId?: string) {
  return !runId || !b.runId || b.runId === runId;
}

function isPartialArgs(args: string) {
  if (!args.trim()) return true;
  try {
    JSON.parse(args);
    return false;
  } catch {
    return true;
  }
}

function lastRunning(out: Block[], pred: (b: ToolBlock) => boolean): ToolBlock | undefined {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "tool" && b.running && pred(b)) return b;
  }
}

function lastThinking(out: Block[]) {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type === "thinking") return b;
  }
}

function closeThinking(out: Block[]) {
  const t = lastThinking(out);
  if (t) t.streaming = false;
}

export function foldEvents(events: Ev[]): Block[] {
  const out: Block[] = [];
  let i = 0;
  const push = (b: Block) => {
    out.push(b);
  };
  for (const e of events) {
    if (e.kind === "done" || e.kind === "chat_title") continue;
    if (e.kind === "error" && staleKey.test(e.body)) continue;
    const key = `${e.kind}-${i++}`;
    if (e.kind === "user") {
      push({ key, type: "user", text: e.body });
      continue;
    }
    if (e.kind === "thinking_chunk") {
      const last = out[out.length - 1];
      if (last?.type === "thinking") {
        last.text += e.body;
        last.streaming = true;
      } else {
        push({ key, type: "thinking", text: e.body, streaming: true });
      }
      continue;
    }
    if (e.kind === "thinking") {
      const t = lastThinking(out);
      if (t) {
        t.text = e.body || t.text;
        t.streaming = false;
      } else {
        push({ key, type: "thinking", text: e.body, streaming: false });
      }
      continue;
    }
    if (e.kind === "chunk") {
      closeThinking(out);
      const last = out[out.length - 1];
      if (last?.type === "assistant") {
        last.text += e.body;
        last.streaming = true;
      } else {
        push({ key, type: "assistant", text: e.body, streaming: true });
      }
      continue;
    }
    if (e.kind === "assistant") {
      closeThinking(out);
      const last = out[out.length - 1];
      if (last?.type === "assistant" && last.streaming) {
        last.text = e.body || last.text;
        last.streaming = false;
      } else if (e.body.trim()) {
        push({ key, type: "assistant", text: e.body });
      }
      continue;
    }
    if (e.kind === "tool_args_chunk") {
      closeThinking(out);
      const named = e.tool ? lastRunning(out, (b) => b.name === e.tool && runOk(b, e.runId)) : undefined;
      const t = named || lastRunning(out, (b) => runOk(b, e.runId));
      if (t) {
        t.args += e.body;
        if (e.tool) t.name = e.tool;
        if (e.runId) t.runId = e.runId;
      } else {
        push({ key, type: "tool", name: e.tool || "tool", args: e.body, running: true, runId: e.runId });
      }
      continue;
    }
    if (e.kind === "tool") {
      closeThinking(out);
      const named = e.tool ? lastRunning(out, (b) => b.name === e.tool && runOk(b, e.runId)) : undefined;
      const t = named || lastRunning(out, (b) => isPartialArgs(b.args) && runOk(b, e.runId));
      if (t) {
        if (e.body) t.args = e.body;
        if (e.tool) t.name = e.tool;
        if (e.runId) t.runId = e.runId;
      } else {
        push({ key, type: "tool", name: e.tool || "tool", args: e.body, running: true, runId: e.runId });
      }
      continue;
    }
    if (e.kind === "tool_chunk") {
      closeThinking(out);
      for (let j = out.length - 1; j >= 0; j--) {
        const b = out[j];
        if (b.type === "tool" && b.running && (!e.runId || b.runId === e.runId)) {
          b.result = (b.result || "") + e.body;
          break;
        }
      }
      continue;
    }
    if (e.kind === "tool_result") {
      closeThinking(out);
      for (let j = out.length - 1; j >= 0; j--) {
        const b = out[j];
        if (b.type === "tool" && b.running && (b.name === e.tool || !e.tool) && (!e.runId || b.runId === e.runId)) {
          b.result = e.body;
          b.running = false;
          break;
        }
      }
      continue;
    }
    if (e.kind === "call") {
      closeThinking(out);
      const item: NestedCall = { key, title: e.body || e.tool || "call", name: e.tool, running: true };
      const t = lastRunning(out, (b) => runOk(b, e.runId));
      if (t) {
        t.calls = t.calls || [];
        t.calls.push(item);
      } else {
        push({ key, type: "tool", name: "call", args: "", running: true, runId: e.runId, calls: [item] });
      }
      continue;
    }
    if (e.kind === "call_result") {
      closeThinking(out);
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
      continue;
    }
    if (e.kind === "error") {
      closeThinking(out);
      push({ key, type: "error", text: e.body });
    }
  }
  return out;
}
