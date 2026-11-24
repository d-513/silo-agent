import { type Approval, type Ev, type Live, approvals, botOf, cardOf, chatOf, live, logs, nextId, stamp } from "./world";

// The demo's run engine: a conversation's log, the bus a stream follows, and
// Run, the pen a scene (scenes.ts) writes a run with. It emits the events the
// control plane would, with pauses, so the console draws them as it does live.

const subs = new Map<string, Set<(e: Ev) => void>>();

/** Record an event in a chat's log and send it to whoever follows the chat. */
export function record(chatId: string, e: Ev): Ev {
  const ev = { chatId, id: nextId("e"), createdAt: stamp(), ...e };
  const log = logs.get(chatId) ?? [];
  logs.set(chatId, log);
  log.push(ev);
  subs.get(chatId)?.forEach((f) => f(ev));
  return ev;
}

/** Send an event that is not kept (a ping, a card changing). */
export function publish(chatId: string, e: Ev) {
  subs.get(chatId)?.forEach((f) => f({ chatId, ...e }));
}

const dress = (e: Ev): Ev => {
  const { cardId, ...rest } = e;
  return cardId ? { ...rest, card: cardOf(cardId) } : rest;
};

/** A chat's events after `after`, then every new one until the caller hangs up. */
export async function* follow(chatId: string, after: string, signal: AbortSignal): AsyncGenerator<Ev> {
  const log = logs.get(chatId) ?? [];
  const from = after ? log.findIndex((e) => e.id === after) + 1 : 0;
  const queue = log.slice(from);
  let wake: (() => void) | undefined;
  const on = (e: Ev) => {
    queue.push(e);
    wake?.();
  };
  const set = subs.get(chatId) ?? new Set();
  subs.set(chatId, set);
  set.add(on);
  try {
    while (!signal.aborted) {
      while (queue.length) yield dress(queue.shift()!);
      await new Promise<void>((r) => {
        wake = r;
        signal.addEventListener("abort", () => r(), { once: true });
      });
    }
  } finally {
    set.delete(on);
  }
}

class Stopped extends Error {}

const pending = new Map<string, (decision: string) => void>();

/** The owner's answer to an approval slip. */
export function decide(id: string, decision: string): Approval | undefined {
  const a = approvals.find((x) => x.id === id);
  if (!a || a.status !== "pending") return a;
  a.status = decision === "deny" ? "denied" : "approved";
  pending.get(id)?.(decision);
  pending.delete(id);
  return a;
}

export class Run implements Live {
  id = nextId("r");
  step = "";
  stopped = false;
  inbox: string[] = [];
  // fast: no pauses, for a run the page opens on already under way.
  fast = false;
  private out = 0;
  constructor(
    public botId: string,
    public chatId: string,
  ) {}

  emit(kind: string, body = "", tool = "", extra: Ev = {}) {
    return record(this.chatId, { runId: this.id, kind, body, tool, ...extra });
  }

  async wait(ms: number) {
    if (this.stopped) throw new Stopped();
    if (!this.fast) await new Promise((r) => setTimeout(r, ms));
    if (this.stopped) throw new Stopped();
  }

  async think(text: string) {
    for (const piece of pieces(text, 5)) {
      this.emit("thinking_chunk", piece);
      await this.wait(60);
    }
    this.emit("thinking", text);
  }

  /** The reply, written out a few words at a time. */
  async say(text: string) {
    await this.wait(350);
    for (const piece of pieces(text, 3)) {
      this.emit("chunk", piece);
      await this.wait(34);
    }
    this.emit("assistant", text);
    this.out += Math.round(text.length / 4);
  }

  /** A tool call starting: its arguments stream in, then it runs. */
  async open(name: string, args: Record<string, unknown>) {
    this.step = name;
    const raw = JSON.stringify(args);
    await this.wait(250);
    for (let i = 0; i < raw.length; i += 28) {
      this.emit("tool_args_chunk", raw.slice(i, i + 28), name);
      await this.wait(18);
    }
    this.emit("tool", raw, name);
  }

  /** What the tool prints while it runs. */
  async print(lines: string[], ms = 380) {
    for (const l of lines) {
      await this.wait(ms);
      this.emit("tool_chunk", `${l}\n`);
    }
  }

  /** A call the tool's code makes to a connector or a Silo function. */
  async call(title: string, action: string, result: string, ms = 700) {
    this.emit("call", title, action);
    await this.wait(ms);
    this.emit("call_result", result, action);
  }

  async close(name: string, result: string, ms = 450) {
    await this.wait(ms);
    this.emit("tool_result", result, name);
    this.step = "";
  }

  async tool(name: string, args: Record<string, unknown>, result: string, lines: string[] = []) {
    await this.open(name, args);
    await this.print(lines);
    await this.close(name, result);
  }

  /** Pause on an approval slip until the owner answers; the answer is the decision. */
  async approve(a: Pick<Approval, "connector" | "action" | "title" | "summary" | "fields">): Promise<string> {
    const chat = chatOf(this.chatId);
    const row: Approval = { ...a, id: nextId("ap"), botId: this.botId, runId: this.id, chatId: this.chatId, status: "pending", source: `Chat · ${chat?.title ?? ""}`, person: chat?.author ?? "" };
    approvals.push(row);
    this.emit("approval", row.id, `${a.connector}.${a.action}`);
    const decision = await new Promise<string>((r) => pending.set(row.id, r));
    if (this.stopped) throw new Stopped();
    this.fast = false;
    this.emit("decision", JSON.stringify({ approval_id: row.id, decision, title: a.title, target: a.summary }), `${a.connector}.${a.action}`);
    return decision;
  }

  usage() {
    const input = 9200 + (logs.get(this.chatId)?.length ?? 0) * 140;
    this.emit("usage", JSON.stringify({ input, output: this.out, cache_read: Math.round(input * 0.92), cache_write: Math.round(input * 0.05), window: 200000 }));
  }
}

function pieces(text: string, words: number): string[] {
  const parts = text.match(/\S+\s*|\s+/g) ?? [];
  const out: string[] = [];
  for (let i = 0; i < parts.length; i += words) out.push(parts.slice(i, i + words).join(""));
  return out;
}

/** Start a run in a chat. The scene writes it; however it ends, the run ends once. */
export function start(botId: string, chatId: string, scene: (r: Run) => Promise<void>, fast = false): Run {
  const r = new Run(botId, chatId);
  r.fast = fast;
  live.set(chatId, r);
  const bot = botOf(botId);
  if (bot && bot.machine !== "online") {
    bot.machine = "online";
    bot.workerConnected = true;
  }
  void (async () => {
    let status = "done";
    try {
      await scene(r);
    } catch (e) {
      status = e instanceof Stopped ? "stopped" : "error";
      if (status === "error") r.emit("error", String(e));
    }
    for (const a of approvals) if (a.runId === r.id && a.status === "pending") a.status = "canceled";
    r.usage();
    r.emit("done", status);
    if (live.get(chatId) === r) live.delete(chatId);
    const chat = chatOf(chatId);
    if (chat) chat.updatedAt = stamp();
  })();
  return r;
}

/** Stop a chat's run: it ends at its next pause, or at once if it waits on a slip. */
export function stop(chatId: string) {
  const r = live.get(chatId);
  if (!r) return false;
  r.stopped = true;
  for (const a of approvals) if (a.runId === r.id && a.status === "pending") pending.get(a.id)?.("deny");
  return true;
}
