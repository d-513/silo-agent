import { ChatCircle } from "@phosphor-icons/react";
import { useEffect, useRef } from "react";

export type Ev = { id?: string; kind: string; body: string; tool: string; runId?: string };

type Block =
  | { key: string; type: "user"; text: string }
  | { key: string; type: "assistant"; text: string; streaming?: boolean }
  | { key: string; type: "thinking"; text: string }
  | { key: string; type: "tool"; name: string; args: string; result?: string; running?: boolean; runId?: string }
  | { key: string; type: "error"; text: string };

const staleKey = /openrouter api key|set openrouter|silo_openrouter|api key in admin/i;

export function foldEvents(events: Ev[]): Block[] {
  const out: Block[] = [];
  let i = 0;
  const push = (b: Block) => {
    out.push(b);
  };
  for (const ev of events) {
    if (ev.kind === "done") continue;
    if (ev.kind === "error" && staleKey.test(ev.body)) continue;
    const key = `${ev.kind}-${i++}`;
    if (ev.kind === "user") {
      push({ key, type: "user", text: ev.body });
      continue;
    }
    if (ev.kind === "thinking_chunk") {
      const last = out[out.length - 1];
      if (last?.type === "thinking") {
        last.text += ev.body;
      } else {
        push({ key, type: "thinking", text: ev.body });
      }
      continue;
    }
    if (ev.kind === "thinking") {
      const last = out[out.length - 1];
      if (last?.type === "thinking") {
        last.text = ev.body || last.text;
      } else {
        push({ key, type: "thinking", text: ev.body });
      }
      continue;
    }
    if (ev.kind === "chunk") {
      const last = out[out.length - 1];
      if (last?.type === "assistant") {
        last.text += ev.body;
        last.streaming = true;
      } else {
        push({ key, type: "assistant", text: ev.body, streaming: true });
      }
      continue;
    }
    if (ev.kind === "assistant") {
      const last = out[out.length - 1];
      if (last?.type === "assistant" && last.streaming) {
        last.text = ev.body || last.text;
        last.streaming = false;
      } else if (ev.body.trim()) {
        push({ key, type: "assistant", text: ev.body });
      }
      continue;
    }
    if (ev.kind === "tool") {
      push({ key, type: "tool", name: ev.tool || "tool", args: ev.body, running: true, runId: ev.runId });
      continue;
    }
    if (ev.kind === "tool_chunk") {
      for (let j = out.length - 1; j >= 0; j--) {
        const b = out[j];
        if (b.type === "tool" && b.running && (!ev.runId || b.runId === ev.runId)) {
          b.result = (b.result || "") + ev.body;
          break;
        }
      }
      continue;
    }
    if (ev.kind === "tool_result") {
      for (let j = out.length - 1; j >= 0; j--) {
        const b = out[j];
        if (b.type === "tool" && b.running && (b.name === ev.tool || !ev.tool) && (!ev.runId || b.runId === ev.runId)) {
          b.result = ev.body;
          b.running = false;
          break;
        }
      }
      continue;
    }
    if (ev.kind === "error") {
      push({ key, type: "error", text: ev.body });
    }
  }
  return out;
}

function prettyArgs(raw: string) {
  try {
    return JSON.stringify(JSON.parse(raw), null, 2);
  } catch {
    return raw;
  }
}

function toolLabel(name: string) {
  switch (name) {
    case "exec_python":
      return "Python";
    case "terminal":
      return "terminal";
    default:
      return name;
  }
}

export function Thread({ events, sending }: { events: Ev[]; sending: boolean }) {
  const end = useRef<HTMLDivElement>(null);
  const blocks = foldEvents(events);
  useEffect(() => {
    end.current?.scrollIntoView({ block: "end" });
  }, [events, sending]);
  return (
    <div className="flex-1 space-y-4 overflow-auto p-4">
      {blocks.length === 0 && !sending && (
        <p className="flex items-center gap-2 text-stone">
          <ChatCircle size={16} />
          Ask this Bot…
        </p>
      )}
      {blocks.map((b) => {
        if (b.type === "user") {
          return (
            <div key={b.key} className="border-l-4 border-bindery pl-3">
              {b.text}
            </div>
          );
        }
        if (b.type === "thinking") {
          return (
            <details key={b.key} className="text-stone">
              <summary className="cursor-pointer text-[12px] font-medium tracking-wide">Thinking</summary>
              <div className="mt-2 whitespace-pre-wrap font-mono text-[13px] text-stone">{b.text}</div>
            </details>
          );
        }
        if (b.type === "tool") {
          return (
            <details key={b.key} className="group">
              <summary className="flex cursor-pointer items-center gap-3 text-[12px] font-medium tracking-wide text-stone">
                <span className="h-px flex-1 bg-thread-2" />
                <span className="font-mono">
                  {b.running ? "Using" : "Used"} {toolLabel(b.name)}
                </span>
                <span className="h-px flex-1 bg-thread-2" />
              </summary>
              <div className="mt-2 space-y-2 rounded bg-cloth p-3 font-mono text-[13px]">
                {b.args && <pre className="whitespace-pre-wrap">{prettyArgs(b.args)}</pre>}
                {b.result && (
                  <pre className="whitespace-pre-wrap border-t border-thread-2 pt-2 text-stone">{b.result.slice(0, 4000)}</pre>
                )}
              </div>
            </details>
          );
        }
        if (b.type === "assistant") {
          return (
            <div key={b.key} className="whitespace-pre-wrap">
              {b.text}
              {b.streaming && <span className="ml-0.5 inline-block h-4 w-px translate-y-0.5 bg-iron align-middle" />}
            </div>
          );
        }
        return (
          <div key={b.key} className="text-carmine">
            {b.text}
          </div>
        );
      })}
      {sending && blocks.every((b) => b.type !== "assistant" || !b.streaming) && (
        <div className="text-stone">Working…</div>
      )}
      <div ref={end} />
    </div>
  );
}
