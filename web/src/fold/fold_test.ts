import { foldEvents, type Ev } from "./index.ts";

function ev(kind: string, body = "", tool = ""): Ev {
  return { kind, body, tool };
}

const blocks = foldEvents([
  ev("thinking_chunk", "plan"),
  ev("tool", "", "exec_python"),
  ev("tool_args_chunk", '{"code":"x"}', "exec_python"),
  ev("thinking", "plan"),
  ev("tool_result", "ok", "exec_python"),
]);
const thinks = blocks.filter((b) => b.type === "thinking");
if (thinks.length !== 1) throw new Error(`want 1 thinking, got ${thinks.length}`);
if (thinks[0].streaming) throw new Error("thinking still streaming after tool");
if (thinks[0].text !== "plan") throw new Error(`text ${thinks[0].text}`);
const tools = blocks.filter((b) => b.type === "tool");
if (tools.length !== 1 || tools[0].running) throw new Error("tool not closed");

const nested = foldEvents([
  ev("tool", "", "exec_python"),
  ev("tool_args_chunk", '{"code":"x"}', "exec_python"),
  ev("call", "Twilio Docs · retrieve", "twilio_docs.twilio__retrieve"),
  ev("call_result", "ok", "twilio_docs.twilio__retrieve"),
  ev("tool_result", "done", "exec_python"),
]);
const py = nested.find((b) => b.type === "tool" && b.name === "exec_python");
if (!py || py.type !== "tool" || py.calls?.length !== 1) throw new Error("nested call missing");
if (py.calls[0].title !== "Twilio Docs · retrieve" || py.calls[0].running) throw new Error("call not closed");

const arts = foldEvents([
  ev("tool", '{"path":"bot/demo"}', "artifact"),
  { kind: "artifact", body: JSON.stringify({ type: "skill", artifact_id: "a1", version: 1, name: "demo", title: "demo", path: "bot/demo", status: "pending", pinned: true }), tool: "" },
  { kind: "artifact", body: JSON.stringify({ type: "skill", artifact_id: "a1", version: 1, name: "demo", title: "demo", path: "bot/demo", status: "saved", pinned: true }), tool: "" },
]);
const art = arts.filter((b) => b.type === "artifact");
if (art.length !== 1 || art[0].type !== "artifact" || art[0].artifactType !== "skill" || art[0].saved !== 1 || art[0].versions.length !== 1) {
  throw new Error("artifact fold");
}
if (arts.some((b) => b.type === "artifact_update")) throw new Error("saving a skill is not a new version");

// Emitting the same path again is the next version of the one card, and a row
// marks where it happened.
const versions = foldEvents([
  { kind: "artifact", body: JSON.stringify({ type: "file", artifact_id: "f1", version: 1, name: "q3.pdf", title: "Q3 report", path: "reports/q3.pdf", size: 2048, pinned: true, preview: { image: "data:image/jpeg;base64,AA" } }), tool: "" },
  ev("assistant", "here it is"),
  { kind: "artifact", body: JSON.stringify({ type: "file", artifact_id: "f1", version: 2, name: "q3.pdf", title: "Q3 report v2", path: "reports/q3.pdf", size: 4096, pinned: true }), tool: "" },
]);
const card = versions[0];
if (card.type !== "artifact" || card.versions.length !== 2 || card.versions[1].size !== 4096 || card.title !== "Q3 report v2" || card.versions[0].preview?.image === undefined) {
  throw new Error("file versions fold");
}
const upd = versions[versions.length - 1];
if (upd.type !== "artifact_update" || upd.version !== 2 || upd.artifactId !== "f1") throw new Error("update row");

// A quote rides on the user block.
const quoted = foldEvents([{ kind: "user", body: "why?", tool: "", quotes: [{ source: "message", ref: "reply", label: "", text: "because", locator: "" }] }]);
if (quoted[0].type !== "user" || quoted[0].quotes?.[0].text !== "because") throw new Error("quote fold");

console.log("ok");

// An approval pauses the running tool; the decision leaves a receipt and a
// deny marks the row; a Stop cuts off what is still running.
{
  const r = "run1";
  const evs: Ev[] = [
    { kind: "tool", body: '{"command":"ls"}', tool: "terminal", runId: r },
    { kind: "approval", body: "ap1", tool: "terminal.run", runId: r },
  ];
  let bs = foldEvents(evs);
  const t0 = bs.find((b) => b.type === "tool");
  if (!t0 || t0.type !== "tool" || !t0.waiting) throw new Error("tool not waiting on approval");
  evs.push({ kind: "decision", body: '{"approval_id":"ap1","decision":"deny","title":"Run a command","target":"ls"}', tool: "terminal.run", runId: r });
  evs.push({ kind: "tool_result", body: "error: denied", tool: "terminal", runId: r });
  bs = foldEvents(evs);
  const t1 = bs.find((b) => b.type === "tool");
  if (!t1 || t1.type !== "tool" || t1.waiting || t1.outcome !== "denied") throw new Error("deny not recorded on tool");
  const rc = bs.find((b) => b.type === "receipt");
  if (!rc || rc.type !== "receipt" || rc.decision !== "deny" || rc.title !== "Run a command" || rc.target !== "ls") {
    throw new Error(`receipt ${JSON.stringify(rc)}`);
  }
}
{
  const r = "run2";
  const bs = foldEvents([
    { kind: "tool", body: '{"code":"x"}', tool: "exec_python", runId: r },
    { kind: "done", body: "stopped", tool: "", runId: r },
  ]);
  const t = bs.find((b) => b.type === "tool");
  if (!t || t.type !== "tool" || t.running || t.outcome !== "stopped") throw new Error("stop did not cut the tool");
  const rc = bs[bs.length - 1];
  if (rc.type !== "receipt" || rc.decision !== "stopped") throw new Error("no stop receipt");
}
{
  // Streamed deltas keep their start offsets until the reply settles.
  const bs = foldEvents([ev("chunk", "Hello "), ev("chunk", "there")]);
  const a = bs[0];
  if (a.type !== "assistant" || JSON.stringify(a.bounds) !== "[0,6]") throw new Error(`bounds ${JSON.stringify(a)}`);
  const done = foldEvents([ev("chunk", "Hello "), ev("chunk", "there"), ev("assistant", "Hello there")]);
  if (done[0].type !== "assistant" || done[0].bounds || done[0].streaming) throw new Error("settled reply kept bounds");
}
console.log("fold ok");

// A quoted Feed post folds into one quote block ahead of the reply to it.
{
  const q = foldEvents([
    { kind: "feed_quote", body: "**Prices** up", tool: "automation “Nightly”", createdAt: "2026-09-26T10:00:00Z" },
    ev("user", "why?"),
    ev("assistant", "because"),
  ]);
  const quote = q[0];
  if (quote?.type !== "quote" || quote.text !== "**Prices** up" || quote.source !== "automation “Nightly”") {
    throw new Error(`quote block ${JSON.stringify(quote)}`);
  }
  if (q.length !== 3 || q[1].type !== "user" || q[2].type !== "assistant") throw new Error(`blocks ${q.map((b) => b.type)}`);
}

// A quoted Dashboard widget keeps what it was, for the thread to draw it.
{
  const quoted = { from: "widget" as const, title: "Disk", widget: { kind: "stat", dataJson: '{"value":3}' } as never };
  const q = foldEvents([{ kind: "feed_quote", body: "```json\n{}\n```", tool: "", quoted }]);
  if (q[0]?.type !== "quote" || q[0].quoted?.from !== "widget" || q[0].quoted.title !== "Disk") throw new Error(`widget quote ${JSON.stringify(q[0])}`);
}

// A compaction opens as a running row and settles with its summary; a run that
// ends without one leaves a settled, empty row (it failed or was stopped).
{
  const r = { runId: "r1" };
  const ok = foldEvents([
    { ...ev("compacting", "", "manual"), ...r },
    { ...ev("usage", "{}"), ...r },
    { ...ev("compaction", "## Request\nship it", "manual"), ...r },
    { ...ev("done", "done"), ...r },
  ]);
  const c = ok[0];
  if (ok.length !== 1 || c?.type !== "compaction" || c.running || c.text !== "## Request\nship it" || c.reason !== "manual") {
    throw new Error(`compaction block ${JSON.stringify(ok)}`);
  }
  const live = foldEvents([{ ...ev("compacting", "", "auto"), ...r }]);
  if (live[0]?.type !== "compaction" || !live[0].running) throw new Error("compacting not running");
  const failed = foldEvents([
    { ...ev("compacting", "", "auto"), ...r },
    { ...ev("error", "compaction failed: boom"), ...r },
  ]);
  if (failed[0]?.type !== "compaction" || failed[0].running || failed[0].text || failed[1]?.type !== "error") {
    throw new Error(`failed compaction ${JSON.stringify(failed)}`);
  }
}

// A subagent report opens the lead's wake run as one report block; the brief a
// lead sends a subagent keeps its sender.
{
  const r = foldEvents([
    { kind: "subagent_report", body: "### scout — done\nok", tool: "scout:done,writer-2:error", runId: "w1" },
    ev("assistant", "noted"),
    { ...ev("user", "## Goal\nlook"), tool: "lead" },
  ]);
  const rep = r[0];
  if (rep?.type !== "report" || rep.agents.length !== 2 || rep.agents[1].name !== "writer-2" || rep.agents[1].status !== "error") {
    throw new Error(`report block ${JSON.stringify(rep)}`);
  }
  if (r[2]?.type !== "user" || r[2].from !== "lead") throw new Error(`lead brief ${JSON.stringify(r[2])}`);
}

// An output rule's verdict lands on the call it is about: a chat tool's own
// row, or the connector call a Python cell made.
const marked = foldEvents([
  ev("tool", '{"q":"x"}', "echo__echo"),
  ev("filtered", '{"verdict":"flagged","mode":"check"}', "echo.echo"),
  ev("tool_result", "[Silo output filter: …]\n\nx", "echo__echo"),
  ev("tool", '{"code":"y"}', "exec_python"),
  ev("call", "Web search", "web.search"),
  ev("filtered", '{"verdict":"withheld","mode":"check","original":"bad page"}', "web.search"),
  ev("call_result", "[Silo output filter: withheld]", "web.search"),
  ev("call", "Echo · echo", "echo.echo"),
  ev("call_result", "fine", "echo.echo"),
  ev("tool_result", "done", "exec_python"),
  ev("filtered", "not json", "echo.echo"),
  ev("filtered", '{"verdict":"clean"}', "echo.echo"),
]);
const direct = marked.find((b) => b.type === "tool" && b.name === "echo__echo");
if (!direct || direct.type !== "tool" || direct.filter?.verdict !== "flagged" || direct.filter.original !== undefined) throw new Error("chat tool row not marked flagged");
const cell = marked.find((b) => b.type === "tool" && b.name === "exec_python");
if (!cell || cell.type !== "tool" || cell.filter) throw new Error("the Python row took a verdict meant for its call");
if (cell.calls?.[0].filter?.verdict !== "withheld" || cell.calls[0].filter.original !== "bad page") throw new Error("nested call not marked withheld");
if (cell.calls?.[1].filter) throw new Error("a clean call was marked");
if (marked.length !== 2) throw new Error(`a filtered event made a block of its own: ${marked.length}`);
