import { Code, ConnectError, type HandlerContext, type ServiceImpl } from "@connectrpc/connect";
import { type BotSchema, UI } from "../gen/silo/v1/ui_pb";
import { decide, follow, publish, record, stop } from "./runs";
import { answer, answered, seed } from "./scenes";
import * as w from "./world";

// The demo's backend: the UI service, answered in the page. What the demo
// shows is written out below; every other read answers empty (the console
// draws that as "nothing here yet") and every other change is refused in
// words, so nothing looks like it worked when it did not.

const off = () => new ConnectError("This is a demo: that part is switched off here.", Code.FailedPrecondition);
const gone = (what: string) => new ConnectError(`${what} not found`, Code.NotFound);

const reads = /^(list|get|search|plan|check|read|browse|mark|pick|prepare|audit|me$|authOptions$)/;

function fallback(m: (typeof UI.methods)[number]) {
  if (m.methodKind === "server_streaming") {
    return async function* (_req: unknown, ctx: HandlerContext) {
      await new Promise<void>((r) => ctx.signal.addEventListener("abort", () => r(), { once: true }));
    };
  }
  return () => {
    if (reads.test(m.localName)) return {};
    throw off();
  };
}

function bot(id: string) {
  const b = w.botOf(id);
  if (!b) throw gone("Bot");
  return b;
}

function chat(id: string) {
  const c = w.chatOf(id);
  if (!c) throw gone("Chat");
  return c;
}

// A Bot as the viewer sees it now: its state is worked out from what is live,
// as the control plane does, never stored.
function view(b: w.Bot): w.Init<typeof BotSchema> {
  const { busy = [], ...row } = b;
  const waiting = (runId: string) => w.approvals.some((a) => a.runId === runId && a.status === "pending");
  const runs = [...w.live.values()]
    .filter((r) => r.botId === b.id)
    .map((r) => {
      const agent = w.subagents.find((s) => s.chatId === r.chatId);
      const c = w.chatOf(r.chatId);
      const board = w.boards.get(agent?.parentChatId ?? r.chatId) ?? [];
      const task = board.find((t) => !t.done && (agent ? t.assignee === agent.name : !t.assignee));
      return {
        chatId: r.chatId,
        kind: agent ? "subagent" : c?.main ? "main" : "chat",
        title: agent?.name ?? c?.title ?? "",
        leadChatId: agent?.parentChatId ?? "",
        step: r.step,
        state: waiting(r.id) ? "needs_you" : "working",
        task: task?.text ?? "",
        taskDone: board.filter((t) => t.done).length,
        taskTotal: board.length,
      };
    });
  const activity = [...busy, ...runs];
  const needs = activity.some((a) => a.state === "needs_you");
  return {
    ...row,
    activity,
    status: needs ? "needs_you" : activity.length ? "working" : b.machine,
    workerConnected: b.machine === "online",
    feedUnread: w.posts.filter((p) => p.botId === b.id && !p.read).length,
    dashboardAsks: w.cards.filter((c) => c.botId === b.id && c.status === "open").length,
    mailAddress: "",
  };
}

const own: Partial<ServiceImpl<typeof UI>> = {
  me: () => ({ user: w.me }),
  signIn: () => ({ user: w.me }),
  signOut: () => ({}),
  setLanguage: (req) => {
    w.me.language = req.language;
    return w.me;
  },

  listSpaces: () => ({ spaces: w.spaces }),
  getSpace: () => w.spaces[0],

  listBots: () => ({ bots: w.bots.map(view) }),
  getBot: (req) => view(bot(req.id)),
  startBot: (req) => {
    const b = bot(req.id);
    if (b.machine === "stopped") {
      b.machine = "starting";
      setTimeout(() => (b.machine = "online"), 2500);
    }
    return view(b);
  },
  stopBot: (req) => {
    const b = bot(req.id);
    for (const r of w.live.values()) if (r.botId === b.id) stop(r.chatId);
    b.machine = "stopped";
    return view(b);
  },
  updateBot: (req) => {
    const b = bot(req.id);
    const { id: _id, ...change } = req;
    Object.assign(b, Object.fromEntries(Object.entries(change).filter(([k, v]) => v !== undefined && !k.startsWith("$"))));
    return view(b);
  },

  listChats: (req) => ({ chats: w.chats.filter((c) => c.botId === req.botId) }),
  createChat: (req) => newChat(req.botId, "New chat"),
  renameChat: (req) => Object.assign(chat(req.id), { title: req.title }),
  deleteChat: (req) => {
    const c = chat(req.id);
    if (c.main) throw new ConnectError("The main chat stays.", Code.FailedPrecondition);
    stop(c.id);
    w.chats.splice(w.chats.indexOf(c), 1);
    return {};
  },
  setChatHidden: (req) => Object.assign(chat(req.chatId), { hidden: req.hidden }),
  setChatModel: (req) => Object.assign(chat(req.chatId), { model: req.model }),
  setChatThinking: (req) => Object.assign(chat(req.chatId), { thinking: req.thinking }),
  listModels: () => ({ models: w.models, defaultModel: w.models[0].id, titleModel: w.models[0].id }),

  send: (req) => {
    const b = bot(req.botId);
    const text = req.text.trim();
    if (text.startsWith("/") && !text.startsWith("//")) return { command: "demo", notice: "Slash commands are switched off in this demo." };
    const c = req.chatId ? chat(req.chatId) : req.newChat ? newChat(b.id, text.split(/\s+/).slice(0, 5).join(" "), req.model, req.thinking) : w.chats.find((x) => x.botId === b.id && x.main)!;
    Object.assign(b, { lastMessage: text, lastMessageAt: w.stamp(), lastMessageMine: true });
    const attachments = req.attachments.map((a) => ({ name: a.name, path: a.path, size: a.size }));
    const running = w.live.get(c.id);
    if (running) {
      record(c.id, { runId: running.id, kind: "user", body: text, attachments });
      running.inbox.push(text);
      return { runId: running.id, chatId: c.id };
    }
    return { runId: answer(b.id, c.id, text, { attachments }).id, chatId: c.id };
  },
  stopRun: (req) => {
    stop(req.chatId);
    return {};
  },
  streamRun: (req, ctx) => follow(req.chatId, req.afterEventId, ctx.signal),

  listApprovals: (req) => ({ approvals: w.approvals.filter((a) => a.botId === req.botId && a.status === "pending") }),
  decideApproval: (req) => {
    const a = decide(req.id, req.decision);
    if (!a) throw gone("Approval");
    return a;
  },

  listSubagents: (req) => ({ subagents: w.subagents.filter((s) => s.parentChatId === req.chatId) }),
  getSubagent: (req) => {
    const s = w.byId(w.subagents, req.id);
    if (!s) throw gone("Subagent");
    return s;
  },
  stopSubagent: (req) => {
    const s = w.byId(w.subagents, req.id);
    if (!s) throw gone("Subagent");
    stop(s.chatId);
    return {};
  },
  getTaskboard: (req) => ({ chatId: req.chatId, items: w.boards.get(req.chatId) ?? [] }),

  listDashboard: (req) => ({
    cards: w.cards.filter((c) => c.botId === req.botId && c.status === "open"),
    posts: w.posts.filter((p) => p.botId === req.botId && !p.read),
    widgets: w.widgets.filter((x) => x.botId === req.botId).map((x, i) => ({ ...x, position: i })),
    automationId: w.automations.find((a) => a.botId === req.botId && a.kind === "dashboard")?.id ?? "",
    curatedAt: w.ago(400),
    schedule: "0 7 * * *",
  }),
  markDashboardRead: (req) => {
    for (const p of w.posts) if (p.botId === req.botId) p.read = true;
    return {};
  },
  submitDashboardCard: (req) => {
    const c = card(req.id);
    c.status = "submitted";
    if (c.chatId) publish(c.chatId, { kind: "card_state", card: c });
    let values: Record<string, unknown> = {};
    try {
      values = JSON.parse(req.valuesJson || "{}");
    } catch {
      /* no values */
    }
    const run = answered(c, values);
    return { chat: w.chatOf(run.chatId), runId: run.id };
  },
  dismissDashboardCard: (req) => {
    const c = card(req.id);
    c.status = "dismissed";
    if (c.chatId) publish(c.chatId, { kind: "card_state", card: c });
    return {};
  },
  setWidgetState: (req) => Object.assign(widget(req.id), { stateJson: req.stateJson }),
  refreshWidget: (req) => Object.assign(widget(req.id), { dataAt: w.stamp() }),

  listFeed: (req) => ({ posts: w.posts.filter((p) => p.botId === req.botId) }),
  markFeedRead: (req) => {
    for (const p of w.posts) if (p.botId === req.botId) p.read = true;
    return {};
  },
  deleteFeedPost: (req) => {
    drop(w.posts, req.id);
    return {};
  },

  listMemories: (req) => ({ memories: w.memories.filter((m) => m.botId === req.botId) }),
  searchMemories: (req) => {
    const q = req.query.toLowerCase();
    return { memories: w.memories.filter((m) => m.botId === req.botId && m.content?.toLowerCase().includes(q)).map((m) => ({ ...m, distance: 0.2 })) };
  },
  keepMemory: (req) => {
    const m = w.byId(w.memories, req.id);
    if (!m) throw gone("Memory");
    return Object.assign(m, { status: "active", inferred: false });
  },
  deleteMemory: (req) => {
    drop(w.memories, req.id);
    return {};
  },

  listAutomations: (req) => ({ automations: w.automations.filter((a) => a.botId === req.botId) }),
  updateAutomation: (req) => {
    const a = w.byId(w.automations, req.id);
    if (!a) throw gone("Automation");
    return Object.assign(a, { name: a.kind ? a.name : req.name, prompt: req.prompt, schedule: req.schedule, enabled: req.enabled });
  },

  listRules: () => ({ sections: w.rules, rules: w.rules.flatMap((s) => s.rules ?? []), filterEnabled: true }),
  setRule: (req) => {
    const r = w.rules.flatMap((s) => s.rules ?? []).find((x) => x.connector === req.connector && x.action === req.action);
    if (!r) throw gone("Rule");
    if (req.decision) r.decision = req.decision;
    if (req.output !== undefined) r.output = req.output;
    return r;
  },

  listFiles: (req) => {
    const at = req.path.replace(/^\/+|\/+$/g, "");
    if (req.botId !== w.ATLAS) return { entries: [] };
    return { entries: w.files.filter((f) => (f.path.includes("/") ? f.path.slice(0, f.path.lastIndexOf("/")) : "") === at).map(({ text: _t, ...e }) => e) };
  },
  readFile: (req) => {
    const f = w.files.find((x) => x.path === req.path.replace(/^\/+/, "") && !x.dir);
    if (!f) throw gone("File");
    return { name: f.name, content: f.text ?? "", size: f.size };
  },
};

function newChat(botId: string, title: string, model = "", thinking = ""): w.Chat {
  const c: w.Chat = { id: w.nextId("chat"), botId, title, model, thinking, updatedAt: w.stamp(), createdAt: w.stamp(), author: w.me.email, mine: true };
  w.chats.push(c);
  return c;
}

function card(id: string) {
  const c = w.cardOf(id);
  if (!c) throw gone("Card");
  return c;
}

function widget(id: string) {
  const x = w.byId(w.widgets, id);
  if (!x) throw gone("Widget");
  return x;
}

function drop<T extends { id: string }>(xs: T[], id: string) {
  const i = xs.findIndex((x) => x.id === id);
  if (i >= 0) xs.splice(i, 1);
}

let seeded = false;

/** The whole UI service: what is written above, and the fallback for the rest. */
export function serve(): Partial<ServiceImpl<typeof UI>> {
  if (!seeded) {
    seeded = true;
    seed();
  }
  const rest = Object.fromEntries(UI.methods.map((m) => [m.localName, fallback(m)]));
  return { ...rest, ...own } as Partial<ServiceImpl<typeof UI>>;
}

