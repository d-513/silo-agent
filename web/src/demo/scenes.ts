import { publish, type Run, start } from "./runs";
import { ago, boards, type Card, cards, chatOf, type Ev, IDEAS, LEDGER, logs, MILL, nextId, SCOUT, stamp, type Subagent, subagents } from "./world";

// What the demo's Bots say and do. Nothing here is a model: each scene is a
// recording written by hand, played through the run engine (runs.ts). A typed
// message picks a scene by its words, and anything else gets an honest answer
// that this is a demo.

const py = (code: string) => ({ code });

// --- what was said before the page opened ---

type Step = [kind: string, body?: string, tool?: string, extra?: Ev];

function past(chatId: string, minAgo: number, steps: Step[]) {
  const runId = nextId("r");
  const log = logs.get(chatId) ?? [];
  logs.set(chatId, log);
  steps.forEach(([kind, body = "", tool = "", extra = {}], i) => {
    log.push({ id: nextId("e"), chatId, runId, kind, body, tool, createdAt: new Date(Date.parse(ago(minAgo)) + i * 4000).toISOString(), ...extra });
  });
  log.push({ id: nextId("e"), chatId, runId, kind: "done", body: "done", createdAt: ago(minAgo - 1) });
}

const PLATE = `from tools import calendar, email
events = calendar.list_events(day="today")
unread = email.list(unread=True, limit=20)
print(len(events), "events,", len(unread), "unread")`;

const SEARCH = `from silo_runtime import web_search_many
found = web_search_many([
    "Kestrel Print large format turnaround",
    "Fold & Co exhibition graphics reviews",
    "Papyrus Lab recycled stock",
])
for q in found: print(q["query"], len(q["hits"]))`;

const PICK = `Pick **Kestrel Print**: cheapest per unit at our volume and the only one with a 5-day turnaround.

| Printer | Per unit at 2,000 | Turnaround | Recycled stock |
| --- | --- | --- | --- |
| Kestrel Print | €0.41 | 5 days | yes |
| Fold & Co | €0.47 | 8 days | yes |
| Papyrus Lab | €0.44 | 10 days | no |

Two things I could not verify: Kestrel's 5 days is from their own site, and none of the three publishes a price for the corner panel. I put the table in \`vendors/comparison.md\`. Want me to ask Kestrel to confirm both by mail?`;

export function seed() {
  past("atlas-main", 60 * 26, [
    ["user", "What's on my plate today?"],
    ["tool", JSON.stringify(py(PLATE)), "exec_python"],
    ["call", "List events", "calendar.list_events"],
    ["call_result", '{"events": 4}', "calendar.list_events"],
    ["call", "List messages", "email.list"],
    ["call_result", '{"messages": 12}', "email.list"],
    ["tool_result", "4 events, 12 unread\n", "exec_python"],
    ["assistant", "Four things in the calendar and twelve unread, three of which need you:\n\n- **11:00** call with Kestrel Print. Ask about the 5-day turnaround.\n- **14:30** proofs are due to the client.\n- **Northwind** has not paid the October invoice (nine days late).\n\nThe rest of the inbox is newsletters and two quotes, which I filed under `vendors/`."],
  ]);
  past("atlas-main", 42, [
    ["user", "Compare the three printers we shortlisted for the stand graphics and tell me which to pick."],
    ["thinking", "The quotes are already in vendors/quotes.csv. Per unit at 2,000 is how they want prices compared. I should check turnaround and stock on each printer's own site instead of trusting the quote mails."],
    ["tool", JSON.stringify({ path: "vendors/quotes.csv" }), "read"],
    ["tool_result", "1|printer,quantity,unit_price,turnaround_days\n2|Kestrel Print,2000,0.41,5\n3|Fold & Co,2000,0.47,8\n4|Papyrus Lab,2000,0.44,10\n", "read"],
    ["tool", JSON.stringify(py(SEARCH)), "exec_python"],
    ["call", "Search the web", "web.search"],
    ["call_result", '{"queries": 3}', "web.search"],
    ["tool_result", "Kestrel Print large format turnaround 8\nFold & Co exhibition graphics reviews 8\nPapyrus Lab recycled stock 7\n", "exec_python"],
    ["tool", JSON.stringify({ path: "vendors/comparison.md", content: "# Stand graphics: printers\n…" }), "write"],
    ["tool_result", "wrote vendors/comparison.md", "write"],
    ["assistant", PICK],
  ]);

  past("atlas-fair", 60 * 5 + 6, [
    ["user", "Find me a hotel near the trade fair for two nights. Under €120."],
    ["tool", JSON.stringify(py('from silo_runtime import web_search\nprint(web_search("hotels near the exhibition centre, 2 nights")["hits"][:5])')), "exec_python"],
    ["call", "Search the web", "web.search"],
    ["call_result", '{"hits": 8}', "web.search"],
    ["tool_result", "[The Quay, Old Mill, Station Inn, …]\n", "exec_python"],
    ["assistant", "Two fit: **The Quay** (€118, 6 minutes on foot) and **Old Mill** (€96, 20 minutes by tram). Both are free to cancel until the 3rd."],
    ["tool", JSON.stringify({ title: "Which hotel for the trade fair?" }), "ask_owner"],
    ["tool_result", "Asked. They will answer on the card.", "ask_owner"],
    ["card", "card-hotel", "", { cardId: "card-hotel" }],
  ]);
  past("atlas-fair", 60 * 5, [
    ["user", "I chose **The Quay · €118**."],
    ["tool", JSON.stringify({ action: "look" }), "desktop"],
    ["tool_result", "The Quay's booking page, dates filled in.", "desktop"],
    ["tool", JSON.stringify({ action: "click", x: 912, y: 640 }), "desktop"],
    ["tool_result", "clicked", "desktop"],
    ["assistant", "Booked: The Quay, two nights, €236, free to cancel until the 3rd. The confirmation is in `trade-fair/hotel-confirmation.txt` and I ticked it off the checklist."],
  ]);

  past("atlas-logo", 60 * 30, [
    ["user", "Which of the two logo drafts reads better small?"],
    ["assistant", "The second. At 24 px the first one's thin strokes close up and the mark turns into a blob; the second keeps its counter open down to 16 px."],
  ]);

  past(LEDGER + "-main", 60 * 24, [
    ["user", "How much is outstanding right now?"],
    ["tool", JSON.stringify(py('from tools import invoices\nopen = invoices.list(status="open")\nprint(sum(i["total"] for i in open), len(open))')), "exec_python"],
    ["call", "List invoices", "invoices.list"],
    ["call_result", '{"invoices": 4}', "invoices.list"],
    ["tool_result", "6240.0 4\n", "exec_python"],
    ["assistant", "€6,240 across four invoices. One is late: **Northwind**, €1,980, due nine days ago."],
  ]);
  past(SCOUT + "-main", 60 * 26, [
    ["user", "Anything new this morning?"],
    ["assistant", "Morning brief is in the Feed: two price changes and one new competitor."],
  ]);
  past(MILL + "-main", 60 * 24 * 3, [
    ["user", "Ship the new pricing page."],
    ["tool", JSON.stringify({ command: "pnpm build && ./deploy.sh production" }), "terminal"],
    ["tool_result", "✓ built in 8.2s\n✓ deployed to production\n", "terminal"],
    ["assistant", "Deployed. The new pricing page is live."],
  ]);

  // Ledger opens paused on an approval, so "Needs you" is on screen at once.
  start(LEDGER, LEDGER + "-main", reminder, true);
}

const REMINDER = `from tools import email
email.send(
    to="billing@northwind.example",
    subject="Invoice 2031 is nine days overdue",
    body=open("drafts/northwind-reminder.md").read(),
)`;

async function reminder(r: Run) {
  r.emit("user", "Send Northwind a reminder for the October invoice.");
  await r.tool("write", { path: "drafts/northwind-reminder.md", content: "Hello,\n\ninvoice 2031 (€1,980) was due nine days ago…" }, "wrote drafts/northwind-reminder.md");
  await r.open("exec_python", py(REMINDER));
  r.emit("call", "Send an email", "email.send");
  const decision = await r.approve({
    connector: "email",
    action: "send",
    title: "Send an email",
    summary: "billing@northwind.example",
    fields: [
      { label: "To", value: "billing@northwind.example" },
      { label: "Subject", value: "Invoice 2031 is nine days overdue" },
    ],
  });
  await sent(r, decision, "The reminder is on its way to Northwind. I'll check the account again on Friday.", "Not sent. The draft is in `drafts/northwind-reminder.md` if you want to change it.");
}

async function sent(r: Run, decision: string, yes: string, no: string) {
  const ok = decision !== "deny";
  await r.wait(500);
  r.emit("call_result", ok ? '{"sent": true}' : "denied", "email.send");
  await r.close("exec_python", ok ? "" : "PermissionError: email.send was denied\n");
  await r.say(ok ? yes : no);
}

// --- what a typed message plays ---

async function research(r: Run) {
  await r.think("They want a comparison, so I need each supplier's own numbers, not a listicle. Three searches in one cell, then read the best page for each.");
  await r.open(
    "exec_python",
    py(`from silo_runtime import web_search_many, web_read_many
found = web_search_many([
    "recycled paper supplier FSC certified wholesale",
    "eco paper mill carbon neutral A-grade stock",
    "tree-free paper supplier Europe",
])
pages = web_read_many([q["hits"][0]["url"] for q in found], query="price per tonne, certification")
for p in pages: print(p["title"], "·", len(p["text"]), "chars")`),
  );
  await r.call("Search the web", "web.search", '{"queries": 3, "hits": 24}', 1100);
  await r.call("Read 3 pages", "web.read", '{"pages": 3}', 1400);
  await r.print(["Greenfield Mill · 6120 chars", "Second Leaf Paper · 4388 chars", "Reedworks · 7904 chars"], 260);
  await r.close("exec_python", "");
  await r.say(`Three that hold up, from each supplier's own pages:

| Supplier | Stock | Certification | Price per tonne |
| --- | --- | --- | --- |
| Greenfield Mill | 100% recycled | FSC Recycled, Blue Angel | €1,140 |
| Second Leaf Paper | 80% recycled | FSC Mix | €980 |
| Reedworks | Tree-free (reed, straw) | EU Ecolabel | €1,420 |

**Greenfield** is the safe pick for client work. **Reedworks** is the interesting one, but its minimum order is two tonnes.

*This is a recording: the demo has no model and no network, so these suppliers are made up.*`);
}

async function mail(r: Run) {
  await r.think("Sending mail is set to Ask on this Bot, so the call will wait for them.");
  await r.tool("read", { path: "vendors/comparison.md" }, "1|# Stand graphics: printers\n2|\n3|| Printer | Per unit at 2,000 | Turnaround | Recycled stock |\n…");
  await r.open(
    "exec_python",
    py(`from tools import email
email.send(
    to="sam@harbor.example",
    subject="Stand graphics: printer comparison",
    body=open("vendors/comparison.md").read(),
)`),
  );
  r.emit("call", "Send an email", "email.send");
  const decision = await r.approve({
    connector: "email",
    action: "send",
    title: "Send an email",
    summary: "sam@harbor.example",
    fields: [
      { label: "To", value: "sam@harbor.example" },
      { label: "Subject", value: "Stand graphics: printer comparison" },
    ],
  });
  await sent(r, decision, "Sent to Sam. In a real Silo that slip is the rule **Email → Send an email** set to *Ask*: the control plane holds the call until someone who may answer does.", "Not sent. Nothing left the machine: a denied call never reaches the connector.");
}

async function ask(r: Run) {
  await r.say("Two candidates, and they pull in different directions, so this one is yours.");
  const card: Card = {
    id: nextId("card"),
    botId: r.botId,
    key: "subject-line",
    title: "Which subject line for the spring newsletter?",
    summary: "The first is plainer; the second tested better last year but is longer.",
    blocksJson: JSON.stringify([
      { type: "segmented", name: "subject", label: "Subject", options: [{ value: "a", label: "New papers for spring" }, { value: "b", label: "Six new recycled stocks, and what we printed on them" }], default: "a" },
      { type: "switch", name: "emoji", label: "Add a leaf emoji", default: false },
    ]),
    submitLabel: "Use it",
    status: "open",
    sourceKind: "chat",
    sourceName: chatOf(r.chatId)?.title ?? "",
    chatId: r.chatId,
    createdAt: stamp(),
    updatedAt: stamp(),
  };
  cards.push(card);
  await r.tool("ask_owner", { title: card.title }, "Asked. They will answer on the card.");
  r.emit("card", card.id, "", { cardId: card.id });
}

/** The owner answered a card: their choice is their turn, and the Bot goes on. */
export function answered(card: Card, values: Record<string, unknown>) {
  const chatId = card.chatId || `${card.botId}-main`;
  const picked = Object.entries(values)
    .map(([k, v]) => `${k}: ${String(v)}`)
    .join(", ");
  return start(card.botId, chatId, async (r) => {
    r.emit("user", `I chose **${card.submitLabel || "Submit"}**${picked ? ` with: ${picked}` : ""}.`);
    if (card.key === "domain-renewal") {
      await r.tool("exec_python", py('from tools import registrar\nprint(registrar.renew("harborstudio.example", years=2))'), "renewed until 2028-12-06\n");
      await r.say("Done as you chose. The receipt is in `bot/receipts/` and the Dashboard card is closed.");
      return;
    }
    await r.say("Noted, and scheduled with that choice. An answer to a card arrives as your turn in the chat it was asked in, so the conversation reads in order.");
  });
}

async function team(r: Run) {
  await r.think("Three independent pieces: pricing, the mailing list, and the print schedule. Two can run in parallel as subagents; I keep the schedule.");
  const tasks = ["[PRICING] Price the six new stocks at 500, 1,000 and 2,000", "[LIST] Clean the mailing list and count who opened last spring", "Draft the print schedule back from the launch date", "Put it together as one plan"];
  await r.tool("task_add", { tasks }, "added 4 tasks");
  boards.set(
    r.chatId,
    tasks.map((t, i) => {
      const m = t.match(/^\[(\w+)\]\s*(.*)$/);
      return { n: i + 1, text: m ? m[2] : t, assignee: m ? m[1] : "", done: false, createdBy: "lead" };
    }),
  );
  publish(r.chatId, { kind: "board" });
  const a = await spawn(r, "PRICING", "Price the six new stocks at three quantities.", [
    ["read", { path: "vendors/quotes.csv" }, "1|printer,quantity,unit_price,turnaround_days\n…"],
    ["exec_python", py("import csv\nrows = list(csv.DictReader(open('vendors/quotes.csv')))\nprint(len(rows), 'quotes')"), "3 quotes\n"],
  ], "Priced all six at 500 / 1,000 / 2,000. The table is in `launch/pricing.md`. The two heaviest stocks only break even from 1,000 up.");
  const b = await spawn(r, "LIST", "Clean the mailing list and report last spring's opens.", [
    ["exec_python", py("from tools import email\nprint(email.campaign_stats('spring-2026'))"), "{'sent': 1840, 'opened': 702}\n"],
  ], "1,840 addresses, 61 bounced and removed. 702 opened last spring's mail (38%).");
  await r.say("Two subagents are on it. I'll put the plan together when they report back.");
  void report(r.botId, r.chatId, [a, b]);
}

type Did = [tool: string, args: Record<string, unknown>, result: string];

async function spawn(lead: Run, name: string, goal: string, did: Did[], result: string): Promise<Subagent> {
  const row: Subagent = { id: nextId("sa"), botId: lead.botId, parentChatId: lead.chatId, chatId: nextId("sa-chat"), name, goal, status: "running", running: true, createdAt: stamp(), activity: "Starting" };
  await lead.tool("spawn_agent", { name, goal }, `started ${name}`);
  subagents.push(row);
  publish(lead.chatId, { kind: "subagents" });
  const run = start(lead.botId, row.chatId, async (r) => {
    r.emit("user", goal, "lead");
    await r.wait(1500);
    for (const [tool, args, out] of did) {
      row.activity = tool;
      await r.wait(1800);
      await r.tool(tool, args, out);
    }
    await r.wait(1200);
    await r.say(result);
  });
  row.runId = run.id;
  row.result = result;
  return row;
}

// The lead is woken once every subagent it started has ended.
async function report(botId: string, chatId: string, agents: Subagent[]) {
  const board = boards.get(chatId) ?? [];
  for (const a of agents) {
    while (logs.get(a.chatId)?.at(-1)?.kind !== "done") await new Promise((r) => setTimeout(r, 400));
    const ended = logs.get(a.chatId)?.at(-1)?.body ?? "done";
    a.status = ended;
    a.running = false;
    a.finishedAt = stamp();
    a.activity = "";
    if (ended === "done") for (const t of board) if (t.assignee === a.name) Object.assign(t, { done: true, doneBy: a.name });
    publish(chatId, { kind: "subagents" });
    publish(chatId, { kind: "board" });
  }
  if (agents.some((a) => a.status !== "done")) return;
  while (logs.get(chatId)?.at(-1)?.kind !== "done") await new Promise((r) => setTimeout(r, 300));
  start(botId, chatId, async (r) => {
    r.emit("subagent_report", agents.map((a) => `${a.name}: ${a.result}`).join("\n\n"), agents.map((a) => `${a.name}:${a.status}`).join(","));
    await r.tool("task_done", { ids: [3, 4] }, "done #3 #4");
    for (const t of board) Object.assign(t, { done: true, doneBy: t.doneBy || "lead" });
    publish(chatId, { kind: "board" });
    await r.say(`Both are back. The plan:

1. **Pricing** is set for all six stocks; the two heaviest are offered from 1,000 up only.
2. **The list** is clean: 1,779 addresses, and last spring 38% opened.
3. **Print** has to start 12 days before launch to leave room for one round of proofs.

Each subagent ran with its own context and reported only its result, so this conversation did not pay for their tool output. Open one from the tray under the thread to read what it did.`);
  });
}

async function other(r: Run, text: string) {
  const quoted = text.length > 80 ? `${text.slice(0, 80)}…` : text;
  await r.think("There is no model behind this page, so the honest thing is to say so and point at what the demo can show.");
  await r.say(`This is the real Silo console running in your browser, but nothing is behind it: no model, no machine, no network. So I can't answer “${quoted}”.

These four play a recording of what a Bot would do:

${IDEAS.map((i) => `- ${i.prompt}`).join("\n")}

Or look around: the Dashboard, the other Bots in the sidebar, and the pages in the rail on the right all work.`);
}

function pick(text: string): (r: Run) => Promise<void> {
  const t = text.toLowerCase();
  if (/\b(subagents?|team|plan|launch|parallel)\b/.test(t)) return team;
  if (/\b(e-?mail|send|approv\w*|permission)\b/.test(t)) return mail;
  if (/\b(ask me|which|choose|decide)\b/.test(t)) return ask;
  if (/\b(research|search|compare|find|look up|web)\b/.test(t)) return research;
  return (r) => other(r, text);
}

/** The run a message starts; messages sent while it runs are answered after it. */
export function answer(botId: string, chatId: string, text: string, extra: Ev = {}) {
  return start(botId, chatId, async (r) => {
    r.emit("user", text, "", extra);
    await pick(text)(r);
    while (r.inbox.length) await other(r, r.inbox.shift()!);
  });
}

