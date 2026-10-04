// A characterization test: one long, varied event log folded into blocks and
// compared with a stored result, so a change to how events fold shows up as a
// diff. Regenerate on purpose with UPDATE=1.
import fs from "node:fs";
import { foldEvents, type Ev } from "./index.ts";

let t = 1000;
const ev = (kind: string, body = "", tool = "", extra: Partial<Ev> = {}): Ev => ({ kind, body, tool, at: (t += 50), ...extra });
const j = (o: unknown) => JSON.stringify(o);
const R1 = "r1";
const R2 = "r2";

const events: Ev[] = [
  // ignored kinds, and an old "no API key" error that is hidden
  ev("chat_title", "A title"),
  ev("usage", j({ input: 1 })),
  ev("error", "Set OpenRouter API key in Admin", "", { runId: R1 }),

  // a user turn with an attachment, and one sent by the lead
  ev("user", "hello", "", { id: "u1", runId: R1, attachments: [{ name: "a.txt", path: "tmp/a.txt", size: 3 }], createdAt: "2026-10-04T10:00:00Z" }),
  ev("user", "do the thing", "lead", { id: "u2", runId: R1 }),

  // thinking streams, then a chunk of reply closes it with a duration
  ev("thinking_chunk", "let me ", "", { runId: R1 }),
  ev("thinking_chunk", "think", "", { runId: R1 }),
  ev("chunk", "Sure", "", { runId: R1 }),
  ev("chunk", ", on it", "", { runId: R1 }),
  ev("assistant", "Sure, on it.", "", { runId: R1 }),

  // a final thinking event without any chunks
  ev("thinking", "quiet thought", "", { runId: R1 }),

  // a tool: partial args stream in, then the call, chunks of output, the result
  ev("tool_args_chunk", '{"co', "exec_python", { runId: R1 }),
  ev("tool_args_chunk", 'de":"print(1)"}', "exec_python", { runId: R1 }),
  ev("tool", '{"code":"print(1)"}', "exec_python", { runId: R1 }),
  ev("tool_chunk", "1\n", "", { runId: R1 }),
  ev("call", "GitHub · list repos", "github.list_repos", { runId: R1 }),
  ev("call_result", "[]", "github.list_repos", { runId: R1 }),
  ev("tool_result", "1\n", "exec_python", { runId: R1 }),

  // a tool with no name on the result, and a tool event that finds a partial row
  ev("tool", "", "read", { runId: R1 }),
  ev("tool", '{"path":"x"}', "", { runId: R1 }),
  ev("tool_result", "contents", "", { runId: R1 }),

  // an approval that is allowed, one that is denied, one denied inside a Python call
  ev("tool", '{"path":"w"}', "write", { runId: R1 }),
  ev("approval", "ap1", "files.write", { runId: R1 }),
  ev("decision", j({ decision: "allow_once", approval_id: "ap1", title: "Write file", target: "w" }), "", { runId: R1 }),
  ev("tool_result", "ok", "write", { runId: R1 }),
  ev("tool", '{"cmd":"rm"}', "terminal", { runId: R1 }),
  ev("approval", "ap2", "terminal.run", { runId: R1 }),
  ev("decision", j({ decision: "deny", approval_id: "ap2", title: "Run command", target: "rm" }), "", { runId: R1 }),
  ev("tool_result", "denied", "terminal", { runId: R1 }),
  ev("tool", '{"code":"x"}', "exec_python", { runId: R1 }),
  ev("call", "Slack · post", "slack.post", { runId: R1 }),
  ev("approval", "ap3", "slack.post", { runId: R1 }),
  ev("decision", j({ decision: "deny", approval_id: "ap3", title: "Post", target: "#general" }), "", { runId: R1 }),
  ev("call_result", "denied", "slack.post", { runId: R1 }),
  ev("tool_result", "failed", "exec_python", { runId: R1 }),
  ev("decision", "not json", "files.write", { runId: R1 }),

  // sections: a live one, a final one, an empty one, and an assistant after streaming
  ev("section_live", "part one", "", { runId: R1 }),
  ev("section", "part two", "", { runId: R1 }),
  ev("section", "   ", "", { runId: R1 }),
  ev("chunk", "tail", "", { runId: R1 }),
  ev("assistant", "", "", { runId: R1 }),

  // compaction: a running one settled by its summary, one without a start, one cut by an error
  ev("compacting", "", "auto", { runId: R1 }),
  ev("compaction", "Summary of earlier turns", "auto", { runId: R1 }),
  ev("compaction", "Manual summary", "manual", { runId: R1 }),
  ev("compacting", "", "manual", { runId: R1 }),
  ev("error", "provider exploded", "", { runId: R1 }),

  // a quoted Feed post and a subagent report
  ev("feed_quote", "A post", "automation “Digest”", { createdAt: "2026-10-03T09:00:00Z" }),
  ev("subagent_report", "### a — done\nok", "a:done,b:error,c", { runId: R2 }),

  // artifacts: a card, then the same card updated by approval id and by name+path
  ev("artifact", j({ type: "skill", name: "pdf", title: "PDF", path: "skills/pdf", scope: "workspace", status: "pending" }), "", { runId: R2 }),
  ev("artifact", j({ type: "skill", name: "pdf", title: "PDF", path: "skills/pdf", scope: "personal", status: "saved" }), "", { runId: R2 }),
  ev("artifact", j({ type: "file", name: "r.pdf", path: "r.pdf", scope: "workspace", status: "ready", size: 12, approval_id: "ap9" }), "", { runId: R2 }),
  ev("artifact", j({ type: "file", name: "r.pdf", path: "r.pdf", scope: "workspace", status: "ready", size: 99, approval_id: "ap9" }), "", { runId: R2 }),
  ev("artifact", "not json", "", { runId: R2 }),

  // an unknown kind is skipped but still uses up a key
  ev("mystery", "?", ""),

  // a second run: a call with no tool open, a cut-off tool, a stop receipt
  ev("call", "Bare call", "python.thing", { runId: R2 }),
  ev("call_result", "r", "python.thing", { runId: R2 }),
  ev("thinking_chunk", "pondering", "", { runId: R2 }),
  ev("tool", '{"code":"slow"}', "exec_python", { runId: R2 }),
  ev("call", "Nested", "a.b", { runId: R2 }),
  ev("chunk", "partial reply", "", { runId: R2 }),
  ev("done", "stopped", "", { runId: R2 }),

  // runs end three ways: clean, interrupted, error; a tool of another run is left alone
  ev("tool", '{"x":1}', "other", { runId: R1 }),
  ev("done", "ok", "", { runId: R2 }),
  ev("done", "interrupted", "", { runId: R1 }),
  ev("tool", '{"y":2}', "after", { runId: R2 }),
  ev("compacting", "", "auto", { runId: R2 }),
  ev("done", "error", "", { runId: R2 }),
];

const got = JSON.stringify(foldEvents(events), null, 1) + "\n";
const path = new URL("./golden.json", import.meta.url);
if (process.env.UPDATE) {
  fs.writeFileSync(path, got);
  console.log("wrote", path.pathname, got.length, "bytes");
} else if (got !== fs.readFileSync(path, "utf8")) {
  fs.writeFileSync(new URL("./golden.actual.json", import.meta.url), got);
  throw new Error("foldEvents output changed; see golden.actual.json");
}
