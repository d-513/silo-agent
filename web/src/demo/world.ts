import type { DescMessage, MessageInitShape } from "@bufbuild/protobuf";
import type {
  ApprovalSchema,
  AutomationSchema,
  BotActivitySchema,
  BotSchema,
  ChatSchema,
  DashboardCardSchema,
  DashboardWidgetSchema,
  FeedPostSchema,
  FileEntrySchema,
  MemorySchema,
  ModelOptionSchema,
  RuleSectionSchema,
  RunEventSchema,
  SpaceSchema,
  SubagentSchema,
  TaskItemSchema,
  UserSchema,
} from "../gen/silo/v1/ui_pb";

// The demo's world: everything the fake backend (backend.ts) answers from. It
// lives in memory, so a reload starts it over. The rows are typed by the
// generated API, so a change to the proto fails the typecheck here.

// The plain-object half of a message's init shape (never a made message), so
// rows can be spread and patched.
export type Init<S extends DescMessage> = Extract<MessageInitShape<S>, { readonly $typeName?: undefined }>;

export type Bot = Init<typeof BotSchema> & { id: string; name: string; machine: string; busy?: Init<typeof BotActivitySchema>[] };
export type Chat = Init<typeof ChatSchema> & { id: string; botId: string };
// cardId: a card event carries the card as it stands when it is read.
export type Ev = Init<typeof RunEventSchema> & { cardId?: string };
export type Card = Init<typeof DashboardCardSchema> & { id: string; botId: string; status: string };
export type Widget = Init<typeof DashboardWidgetSchema> & { id: string; botId: string };
export type Post = Init<typeof FeedPostSchema> & { id: string; botId: string };
export type Memory = Init<typeof MemorySchema> & { id: string; botId: string };
export type Automation = Init<typeof AutomationSchema> & { id: string; botId: string };
export type Approval = Init<typeof ApprovalSchema> & { id: string; botId: string; runId: string; status: string };
export type Subagent = Init<typeof SubagentSchema> & { id: string; botId: string; parentChatId: string; chatId: string; name: string };
export type Task = Init<typeof TaskItemSchema>;
export type File = Init<typeof FileEntrySchema> & { path: string; text?: string };

// A run the demo is playing. `step` is the tool it is in, for the Bot's row.
export type Live = { id: string; botId: string; chatId: string; step: string; stopped: boolean; inbox: string[] };

const now = Date.now();
/** An RFC 3339 time `min` minutes before the page opened. */
export const ago = (min: number) => new Date(now - min * 60_000).toISOString();
export const stamp = () => new Date().toISOString();

let seq = 0;
export const nextId = (p: string) => `${p}${(++seq).toString(36).padStart(4, "0")}`;

export const SPACE = "harbor";
export const ATLAS = "atlas";
export const LEDGER = "ledger";
export const SCOUT = "scout";
export const MILL = "mill";

export const me: Init<typeof UserSchema> & { language: string } = {
  id: "you",
  email: "you@harbor.example",
  admin: false,
  hasPassword: true,
  createdAt: ago(60 * 24 * 40),
  lastSignInAt: ago(3),
  language: "en",
};

export const spaces: Init<typeof SpaceSchema>[] = [{ id: SPACE, name: "Harbor Studio", mark: "HS", role: "owner", bots: 4, people: 1, createdAt: ago(60 * 24 * 40) }];

const base = { spaceId: SPACE, role: "manage", timeZone: "Europe/Warsaw", language: "en", proactivity: "quiet", acceptBotMessages: true, model: "" };

export const bots: Bot[] = [
  {
    ...base,
    id: ATLAS,
    name: "Atlas",
    crest: 25,
    description: "Research and the everyday running of the studio.",
    soul: "You are Atlas. You work for a small print and design studio. Be brief, check before you claim, and say what you did not verify.",
    machine: "online",
    proactivity: "active",
    lastMessage: "Pick Kestrel Print: cheapest per unit at our volume and the only one with a 5-day turnaround.",
    lastMessageAt: ago(42),
  },
  {
    ...base,
    id: LEDGER,
    name: "Ledger",
    crest: 4,
    description: "Invoices, receipts and the monthly close.",
    machine: "online",
    lastMessage: "Send Northwind a reminder for the October invoice.",
    lastMessageAt: ago(2),
    lastMessageMine: true,
  },
  {
    ...base,
    id: SCOUT,
    name: "Scout",
    crest: 22,
    description: "Watches competitors, prices and the news.",
    machine: "online",
    lastMessage: "Morning brief is in the Feed: two price changes and one new competitor.",
    lastMessageAt: ago(60 * 26),
    feedUnread: 1,
    busy: [{ chatId: "scout-brief", kind: "automation", title: "Morning brief", step: "exec_python", state: "working", task: "Read the three launch posts", taskDone: 2, taskTotal: 5 }],
  },
  {
    ...base,
    id: MILL,
    name: "Mill",
    crest: 42,
    description: "Builds and deploys the website.",
    machine: "stopped",
    proactivity: "off",
    lastMessage: "Deployed. The new pricing page is live.",
    lastMessageAt: ago(60 * 24 * 3),
  },
];

const chat = (botId: string, id: string, title: string, minAgo: number, more: Partial<Chat> = {}): Chat => ({
  id,
  botId,
  title,
  updatedAt: ago(minAgo),
  createdAt: ago(minAgo + 600),
  author: me.email,
  mine: true,
  ...more,
});

export const chats: Chat[] = [
  chat(ATLAS, "atlas-main", "Main", 42, { main: true }),
  chat(ATLAS, "atlas-fair", "Trade fair travel", 60 * 5),
  chat(ATLAS, "atlas-logo", "Logo feedback", 60 * 30, { hidden: true }),
  chat(LEDGER, "ledger-main", "Main", 2, { main: true }),
  chat(SCOUT, "scout-main", "Main", 60 * 26, { main: true }),
  chat(MILL, "mill-main", "Main", 60 * 24 * 3, { main: true }),
];

export const models: Init<typeof ModelOptionSchema>[] = [
  { id: "claude/claude-opus-5-5", provider: "claude", label: "Claude Opus 5.5", kind: "text", thinkingLevels: ["off", "low", "medium", "high", "xhigh", "max"] },
  { id: "openai/gpt-6.2-astra", provider: "openai", label: "GPT-6.2 Astra", kind: "text", thinkingLevels: ["minimal", "low", "medium", "high", "xhigh"] },
  { id: "anthropic/claude-sonnet-5-5", provider: "anthropic", label: "Claude Sonnet 5.5", kind: "text", thinkingLevels: ["off", "low", "medium", "high", "max"] },
  { id: "openrouter/deepseek/deepseek-v4.1-flash", provider: "openrouter", label: "DeepSeek V4.1 Flash", kind: "text", thinkingLevels: ["off", "high"] },
  { id: "local/inkling-2-mini", provider: "local", label: "Inkling 2 Mini", kind: "text", thinkingLevels: [] },
];

export const logs = new Map<string, Ev[]>();
export const live = new Map<string, Live>();
export const approvals: Approval[] = [];
export const subagents: Subagent[] = [];
export const boards = new Map<string, Task[]>();

export const cards: Card[] = [
  {
    id: "card-domain",
    botId: ATLAS,
    key: "domain-renewal",
    title: "Renew harborstudio.example?",
    summary: "It expires in 12 days. The registrar's price went up 8% this year.",
    blocksJson: JSON.stringify([
      { type: "kv", items: [{ label: "Expires", value: "in 12 days" }, { label: "1 year", value: "€14.90" }, { label: "2 years", value: "€27.80" }] },
      { type: "segmented", name: "term", label: "Term", options: [{ value: "1", label: "1 year" }, { value: "2", label: "2 years" }, { value: "no", label: "Let it lapse" }], default: "2" },
      { type: "switch", name: "auto", label: "Turn on auto-renew", default: true },
    ]),
    submitLabel: "Renew",
    status: "open",
    sourceKind: "automation",
    sourceName: "Heartbeat",
    createdAt: ago(190),
    updatedAt: ago(190),
  },
  {
    id: "card-hotel",
    botId: ATLAS,
    key: "fair-hotel",
    title: "Which hotel for the trade fair?",
    summary: "Both are free to cancel until the 3rd.",
    blocksJson: JSON.stringify([
      { type: "segmented", name: "hotel", label: "Hotel", options: [{ value: "quay", label: "The Quay · €118" }, { value: "mill", label: "Old Mill · €96" }], default: "quay" },
    ]),
    submitLabel: "Book it",
    status: "submitted",
    sourceKind: "chat",
    sourceName: "Trade fair travel",
    chatId: "atlas-fair",
    createdAt: ago(60 * 5 + 4),
    updatedAt: ago(60 * 5 + 2),
    read: true,
  },
];

const today = (h: number, m = 0) => {
  const d = new Date();
  d.setHours(h, m, 0, 0);
  return d.toISOString();
};

const widget = (id: string, kind: string, title: string, size: string, data: unknown, more: Partial<Widget> = {}): Widget => ({
  id,
  botId: ATLAS,
  key: id,
  kind,
  title,
  size,
  rows: 1,
  specJson: "{}",
  dataJson: JSON.stringify(data),
  stateJson: "{}",
  dataAt: ago(6),
  updatedAt: ago(6),
  source: "Dashboard",
  ...more,
});

// The prompts the demo answers with a recording (scenes.ts reads them by key
// word, so these stay in step with it).
export const IDEAS = [
  { title: "Research something on the web", prompt: "Research three eco-friendly paper suppliers and compare them." },
  { title: "Do something that needs my OK", prompt: "Email the vendor comparison to sam@harbor.example." },
  { title: "Ask me before deciding", prompt: "Ask me which newsletter subject line to use." },
  { title: "Split a job across subagents", prompt: "Plan the spring catalogue launch with a team of subagents." },
];

export const widgets: Widget[] = [
  widget("w-ideas", "ideas", "Try asking", "m", { items: IDEAS }),
  widget("w-note", "note", "Today", "m", { text: "Two quotes came in overnight and the **Northwind** invoice is nine days late. The trade fair is in 16 days: the stand graphics still need a printer." }),
  widget("w-invoices", "stat", "Open invoices", "s", { value: 4, caption: "€6,240 outstanding", series: [7, 6, 6, 5, 6, 4, 4] }, { hasProbe: true, every: 3600 }),
  widget("w-inbox", "stat", "Inbox", "s", { value: 12, caption: "3 need a reply", series: [18, 22, 15, 19, 14, 16, 12] }, { hasProbe: true, every: 900 }),
  widget("w-orders", "chart", "Orders this week", "m", { points: [{ label: "Mon", value: 9 }, { label: "Tue", value: 14 }, { label: "Wed", value: 11 }, { label: "Thu", value: 17 }, { label: "Fri", value: 6 }], caption: "57 so far, 12 more than last week" }, { specJson: JSON.stringify({ type: "bar" }), hasProbe: true, every: 3600 }),
  widget("w-day", "timeline", "Today", "m", { items: [{ time: today(9, 30), title: "Stand-up" }, { time: today(11), title: "Call with Kestrel Print", detail: "Ask about the 5-day turnaround" }, { time: today(14, 30), title: "Proofs due to the client" }, { time: today(17), title: "Courier pickup" }] }, { rows: 2 }),
  widget("w-fair", "checklist", "Before the trade fair", "m", { items: [{ id: "stand", text: "Confirm the stand size" }, { id: "print", text: "Pick a printer for the graphics" }, { id: "hotel", text: "Book the hotel" }, { id: "samples", text: "Pack the paper samples" }] }, { stateJson: JSON.stringify({ checked: ["stand", "hotel"] }), rows: 2 }),
  widget("w-services", "status", "Services", "m", { items: [{ label: "Shop", state: "ok", meta: "182 ms" }, { label: "Order API", state: "ok", meta: "96 ms" }, { label: "Proofing tool", state: "warn", word: "Slow", meta: "2.4 s" }] }, { hasProbe: true, every: 300 }),
];

export const posts: Post[] = [
  { id: "p1", botId: ATLAS, title: "Two quotes came in overnight", body: "**Kestrel Print** quoted €0.41 a unit at 2,000 and **Fold & Co** €0.47. Both are in `vendors/quotes.csv`; the comparison is updated.", sourceKind: "automation", sourceName: "Heartbeat", createdAt: ago(200), read: false },
  { id: "p2", botId: ATLAS, title: "Proofing tool is slow again", body: "Response time has been over 2 s since 07:40. Nothing is down. I'll post again if it clears or gets worse.", sourceKind: "automation", sourceName: "Heartbeat", createdAt: ago(380), read: true },
  { id: "p3", botId: ATLAS, title: "", body: "Booked **The Quay** for the trade fair, two nights, free to cancel until the 3rd. The confirmation is in `trade-fair/`.", sourceKind: "chat", sourceName: "Trade fair travel", chatId: "atlas-fair", createdAt: ago(60 * 5), read: true },
  { id: "p4", botId: SCOUT, title: "Morning brief", body: "Two price changes and one new competitor.\n\n- **Papyrus Lab** dropped A5 flyers by 6%.\n- **Inkwell** now charges for proofs.\n- New: **Grainline**, recycled stock only, launching next month.", sourceKind: "automation", sourceName: "Morning brief", createdAt: ago(60 * 26), read: false },
];

const mem = (id: string, kind: string, content: string, days: number, more: Partial<Memory> = {}): Memory => ({ id, botId: ATLAS, kind, content, status: "active", createdAt: ago(days * 1440), lastUsedAt: ago(days * 300), useCount: 3, ...more });

export const memories: Memory[] = [
  mem("m1", "preference", "Wants prices compared per unit at 2,000 copies, VAT excluded.", 2, { useCount: 6 }),
  mem("m2", "preference", "Replies to clients go out as drafts first; never send without asking.", 9, { useCount: 11 }),
  mem("m3", "lesson", "The proofing tool's export only works in Chromium with the sidebar collapsed.", 5),
  mem("m4", "lesson", "Kestrel Print's quote form rejects files over 20 MB: send a link instead.", 3),
  mem("m5", "fact", "The studio's fiscal year ends on 30 June.", 30),
  mem("m6", "fact", "Sam handles accounts; Ines handles production.", 21, { useCount: 8 }),
  mem("m7", "fact", "The trade fair stand is 3 × 2 m, corner position.", 6),
  mem("m8", "work", "Vendor comparison for the stand graphics: three printers shortlisted, table in vendors/comparison.md, waiting on a decision.", 1),
  mem("m9", "fact", "Prefers trains over flights for trips under five hours.", 4, { status: "tentative", inferred: true, evidence: 1, useCount: 0 }),
];

const auto = (id: string, botId: string, name: string, kind: string, schedule: string, more: Partial<Automation> = {}): Automation => ({ id, botId, name, kind, schedule, enabled: true, prompt: "", chatId: `${id}-log`, lastStatus: "done", createdAt: ago(60 * 24 * 30), createdBy: me.email, ...more });

export const automations: Automation[] = [
  auto("a-heart", ATLAS, "Heartbeat", "heartbeat", "0 9-21/3 * * *", { prompt: "Look over what is open and report what changed.", lastRunAt: ago(200), nextRunAt: ago(-40) }),
  auto("a-dash", ATLAS, "Dashboard", "dashboard", "0 7 * * *", { prompt: "Keep the Dashboard current.", lastRunAt: ago(400), nextRunAt: ago(-60 * 14) }),
  auto("a-mem", ATLAS, "Memory", "memory", "", { onItsOwn: true, lastRunAt: ago(55) }),
  auto("a-inv", ATLAS, "Friday invoice check", "", "0 15 * * 5", { prompt: "List every invoice more than 7 days overdue and draft a reminder for each. Do not send anything.", lastRunAt: ago(60 * 24 * 4), nextRunAt: ago(-60 * 24 * 3) }),
  auto("a-brief", SCOUT, "Morning brief", "", "0 8 * * 1-5", { prompt: "Check the competitor list for price and product changes and post a brief to the Feed.", running: true, lastRunAt: ago(4) }),
];

const rule = (connector: string, action: string, title: string, decision: string, output = "pass") => ({ id: `${connector}.${action}`, botId: ATLAS, connector, action, title, decision, output });

export const rules: Init<typeof RuleSectionSchema>[] = [
  {
    id: "bot",
    title: "Bot",
    summary: "The machine, files and the Bot's own tools",
    rules: [
      rule("python", "run", "Run Python", "allow"),
      rule("terminal", "run", "Run a shell command", "allow"),
      rule("files", "*", "Read and write files", "allow"),
      rule("desktop", "*", "Use the desktop", "allow"),
      rule("web", "search", "Search the web", "allow", "check"),
      rule("web", "read", "Read a web page", "allow", "check"),
      rule("automations", "create", "Create an automation", "ask"),
      rule("tunnels", "public", "Expose a port to everyone", "ask", ""),
    ],
  },
  {
    id: "email",
    title: "Email",
    summary: "The studio's mailbox",
    rules: [rule("email", "list", "List messages", "allow", "check"), rule("email", "read", "Read a message", "allow", "check"), rule("email", "send", "Send an email", "ask"), rule("email", "delete", "Delete a message", "deny")],
  },
  {
    id: "secrets",
    title: "Secrets",
    summary: "Each secret is its own rule",
    rules: [rule("secrets", "SHOP_API_KEY", "SHOP_API_KEY", "allow", ""), rule("secrets", "BANK_LOGIN", "BANK_LOGIN", "ask", "")],
  },
];

const text = (path: string, body: string, days: number): File => ({ path, name: path.split("/").pop()!, size: BigInt(body.length), modified: ago(days * 1440), text: body });
const dir = (path: string, days: number): File => ({ path, name: path.split("/").pop()!, dir: true, modified: ago(days * 1440) });

export const files: File[] = [
  dir("vendors", 1),
  text(
    "vendors/comparison.md",
    "# Stand graphics: printers\n\n| Printer | Per unit at 2,000 | Turnaround | Recycled stock |\n| --- | --- | --- | --- |\n| Kestrel Print | €0.41 | 5 days | yes |\n| Fold & Co | €0.47 | 8 days | yes |\n| Papyrus Lab | €0.44 | 10 days | no |\n\n**Pick:** Kestrel Print.\n",
    0,
  ),
  text("vendors/quotes.csv", "printer,quantity,unit_price,turnaround_days\nKestrel Print,2000,0.41,5\nFold & Co,2000,0.47,8\nPapyrus Lab,2000,0.44,10\n", 0),
  dir("trade-fair", 5),
  text("trade-fair/checklist.md", "# Trade fair\n\n- [x] Confirm the stand size (3 × 2 m, corner)\n- [x] Book the hotel (The Quay)\n- [ ] Pick a printer for the graphics\n- [ ] Pack the paper samples\n", 5),
  text("trade-fair/hotel-confirmation.txt", "The Quay · 2 nights · €236 · free cancellation until the 3rd\nConfirmation: HQ-48213\n", 5),
  text("notes.md", "# Notes\n\nSam handles accounts. Ines handles production.\n", 12),
  dir("bot", 0),
];

export const byId = <T extends { id: string }>(xs: T[], id: string) => xs.find((x) => x.id === id);
export const chatOf = (id: string) => byId(chats, id);
export const botOf = (id: string) => byId(bots, id);
export const cardOf = (id: string) => byId(cards, id);
