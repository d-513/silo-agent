export type Attachment = { name: string; path: string; size: number };

// `at` is the client arrival time (ms); replayed history arrives in a burst.
// `createdAt` is the server's record time (RFC 3339) when it sent one.
export type Ev = { id?: string; kind: string; body: string; tool: string; runId?: string; attachments?: Attachment[]; at?: number; createdAt?: string };

// Why a tool stopped without a clean result: the human denied it, or the run
// was stopped/interrupted around it.
export type Outcome = "denied" | "stopped";

export type NestedCall = { key: string; title: string; name: string; result?: string; running?: boolean; waiting?: boolean; outcome?: Outcome };

export type ToolBlock = {
  key: string;
  type: "tool";
  name: string;
  args: string;
  result?: string;
  running?: boolean;
  runId?: string;
  calls?: NestedCall[];
  // Paused on an approval slip.
  waiting?: boolean;
  approvalId?: string;
  outcome?: Outcome;
};

export type Decision = "allow_once" | "always" | "deny" | "stopped";

export type ReceiptBlock = { key: string; type: "receipt"; decision: Decision; title: string; target: string; runId?: string };

export type Block =
  | { key: string; type: "user"; id?: string; runId?: string; text: string; attachments?: Attachment[]; createdAt?: string }
  // `bounds` are source offsets where each streamed delta began.
  | { key: string; type: "assistant"; text: string; streaming?: boolean; bounds?: number[] }
  | { key: string; type: "thinking"; text: string; streaming?: boolean; startAt?: number; ms?: number }
  | ToolBlock
  | ReceiptBlock
  | { key: string; type: "error"; text: string }
  // A Feed post quoted into a new chat: `source` is where it was posted from.
  | { key: string; type: "quote"; text: string; source: string; createdAt?: string }
  | {
      key: string;
      type: "artifact";
      artifactType: string;
      name: string;
      title: string;
      path: string;
      scope: string;
      approvalId: string;
      status: string;
      size?: number;
      runId?: string;
    };

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

function closeThinking(out: Block[], at?: number) {
  const t = lastThinking(out);
  if (t && t.streaming) {
    t.streaming = false;
    if (t.startAt && at) t.ms = at - t.startAt;
  }
}

function lastOf<T>(xs: T[] | undefined, pred: (x: T) => boolean): T | undefined {
  for (let j = (xs?.length ?? 0) - 1; j >= 0; j--) if (pred(xs![j])) return xs![j];
}

function waitingTool(out: Block[], runId?: string, approvalId?: string): ToolBlock | undefined {
  for (let j = out.length - 1; j >= 0; j--) {
    const b = out[j];
    if (b.type !== "tool" || !runOk(b, runId)) continue;
    if (approvalId ? b.approvalId === approvalId : b.waiting) return b;
  }
}

export function foldEvents(events: Ev[]): Block[] {
  const out: Block[] = [];
  let i = 0;
  const push = (b: Block) => {
    out.push(b);
  };
  for (const e of events) {
    if (e.kind === "chat_title" || e.kind === "usage") continue;
    if (e.kind === "error" && staleKey.test(e.body)) continue;
    const key = `${e.kind}-${i++}`;
    if (e.kind === "user") {
      push({ key, type: "user", id: e.id, runId: e.runId, text: e.body, attachments: e.attachments, createdAt: e.createdAt });
      continue;
    }
    if (e.kind === "thinking_chunk") {
      const last = out[out.length - 1];
      if (last?.type === "thinking") {
        last.text += e.body;
        last.streaming = true;
      } else {
        push({ key, type: "thinking", text: e.body, streaming: true, startAt: e.at });
      }
      continue;
    }
    if (e.kind === "thinking") {
      const t = lastThinking(out);
      if (t) {
        t.text = e.body || t.text;
        if (t.streaming && t.startAt && e.at) t.ms = e.at - t.startAt;
        t.streaming = false;
      } else {
        push({ key, type: "thinking", text: e.body, streaming: false });
      }
      continue;
    }
    if (e.kind === "chunk") {
      closeThinking(out, e.at);
      const last = out[out.length - 1];
      if (last?.type === "assistant") {
        (last.bounds ??= [0]).push(last.text.length);
        last.text += e.body;
        last.streaming = true;
      } else {
        push({ key, type: "assistant", text: e.body, streaming: true, bounds: [0] });
      }
      continue;
    }
    if (e.kind === "feed_quote") {
      push({ key, type: "quote", text: e.body, source: e.tool, createdAt: e.createdAt });
      continue;
    }
    if (e.kind === "assistant" || e.kind === "section" || e.kind === "section_live") {
      closeThinking(out, e.at);
      const last = out[out.length - 1];
      if (last?.type === "assistant" && last.streaming) {
        last.text = e.body || last.text;
        last.streaming = false;
        last.bounds = undefined;
      } else if (e.body.trim()) {
        push({ key, type: "assistant", text: e.body });
      }
      continue;
    }
    if (e.kind === "tool_args_chunk") {
      closeThinking(out, e.at);
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
      closeThinking(out, e.at);
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
      closeThinking(out, e.at);
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
      closeThinking(out, e.at);
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
      closeThinking(out, e.at);
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
      continue;
    }
    if (e.kind === "approval") {
      // body = approval id, tool = connector.action. Pause the running row;
      // a connector call from Python pauses its nested call too.
      const t = lastRunning(out, (b) => runOk(b, e.runId));
      if (t) {
        t.waiting = true;
        t.approvalId = e.body;
        const c = lastOf(t.calls, (x) => !!x.running);
        if (c) c.waiting = true;
      }
      continue;
    }
    if (e.kind === "decision") {
      let d: Record<string, unknown> = {};
      try {
        d = JSON.parse(e.body || "{}") as Record<string, unknown>;
      } catch {
        /* ignore */
      }
      const decision = String(d.decision || "") as Decision;
      const t = waitingTool(out, e.runId, String(d.approval_id || "")) ?? waitingTool(out, e.runId);
      if (t) {
        t.waiting = false;
        const c = lastOf(t.calls, (x) => !!x.waiting);
        if (c) {
          c.waiting = false;
          if (decision === "deny") c.outcome = "denied";
        } else if (decision === "deny") {
          t.outcome = "denied";
        }
      }
      push({ key, type: "receipt", decision, title: String(d.title || e.tool || ""), target: String(d.target || ""), runId: e.runId });
      continue;
    }
    if (e.kind === "done") {
      // Anything still running in this run was cut off. A Stop leaves a receipt.
      const cut = e.body === "stopped" || e.body === "interrupted" || e.body === "error";
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
      if (e.body === "stopped") push({ key, type: "receipt", decision: "stopped", title: "this run", target: "", runId: e.runId });
      continue;
    }
    if (e.kind === "artifact") {
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
      let found = false;
      for (let j = out.length - 1; j >= 0; j--) {
        const b = out[j];
        if (b.type !== "artifact") continue;
        if ((approvalId && b.approvalId === approvalId) || (name && b.name === name && b.path === next.path)) {
          Object.assign(b, next, { key: b.key });
          found = true;
          break;
        }
      }
      if (!found) push(next);
      continue;
    }
    if (e.kind === "error") {
      closeThinking(out, e.at);
      push({ key, type: "error", text: e.body });
    }
  }
  return out;
}
