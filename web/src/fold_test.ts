import { foldEvents, type Ev } from "./fold.ts";

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
  { kind: "artifact", body: JSON.stringify({ type: "skill", name: "demo", title: "demo", path: "bot/demo", scope: "workspace", approval_id: "a1", status: "pending" }), tool: "" },
  { kind: "artifact", body: JSON.stringify({ type: "skill", name: "demo", title: "demo", path: "bot/demo", scope: "personal", approval_id: "a1", status: "saved" }), tool: "" },
]);
const art = arts.filter((b) => b.type === "artifact");
if (art.length !== 1 || art[0].type !== "artifact" || art[0].artifactType !== "skill" || art[0].status !== "saved" || art[0].approvalId !== "a1") {
  throw new Error("artifact fold");
}

const files = foldEvents([
  { kind: "artifact", body: JSON.stringify({ type: "file", name: "q3.pdf", title: "Q3 report", path: "reports/q3.pdf", scope: "workspace", status: "ready", size: 2048 }), tool: "" },
]);
const file = files.filter((b) => b.type === "artifact");
if (file.length !== 1 || file[0].type !== "artifact" || file[0].artifactType !== "file" || file[0].size !== 2048) {
  throw new Error("file artifact fold");
}

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
