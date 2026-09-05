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
console.log("ok");
