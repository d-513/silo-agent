import { Bot, ChevronDown, Square, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { agentLink, type AgentLink } from "./links";
import type { Subagent } from "./gen/silo/v1/ui_pb";
import { Lamp, StatusWord } from "./Lamp";
import { elapsed, shortModel } from "./useSubagents";

// SubagentTray sits between the thread and the composer while a chat has
// subagents working: one card per running subagent (click to open its page),
// plus a chip listing the finished ones. It disappears when none ever ran.
export function SubagentTray({
  botId,
  chatId,
  agents,
  waitingRuns,
  onStop,
  onStopAll,
}: {
  botId: string;
  chatId: string;
  agents: Subagent[];
  // Runs paused on an approval slip.
  waitingRuns: ReadonlySet<string>;
  onStop: (id: string) => void;
  onStopAll: () => void;
}) {
  const [now, setNow] = useState(() => Date.now());
  const running = agents.filter((a) => a.running);
  const finished = agents.filter((a) => !a.running);
  useEffect(() => {
    if (running.length === 0) return;
    const t = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(t);
  }, [running.length]);
  if (agents.length === 0) return null;
  const href = (a: Subagent) => agentLink(botId, chatId, a.id);
  return (
    <div className="shrink-0 px-3 pb-1.5 wide:px-6">
      <div className="mx-auto flex max-w-[760px] items-center gap-1.5">
        <div className="flex min-w-0 flex-1 gap-2 overflow-x-auto px-px pt-1 pb-1 [scrollbar-width:none]">
          {running.map((a) => {
            const waiting = waitingRuns.has(a.runId);
            return (
              <div
                key={a.id}
                className="group relative w-[232px] shrink-0 rounded-card bg-surface shadow-card transition-[transform,box-shadow] duration-[200ms] ease-quiet hover:-translate-y-px hover:shadow-float"
              >
                <Link {...href(a)} className="flex flex-col gap-1 rounded-card px-3 py-2.5">
                  <span className="flex min-w-0 items-baseline gap-2">
                    <span className="min-w-0 flex-1 truncate text-[13.5px] leading-[18px] font-medium text-ink">{a.name}</span>
                    <span className="shrink-0 font-mono text-[11.5px] leading-4 text-ink-3 transition-opacity duration-[160ms] group-focus-within:opacity-0 group-hover:opacity-0">
                      {elapsed(a.createdAt, now)}
                    </span>
                  </span>
                  <span className="flex min-w-0 items-center gap-1.5">
                    <Lamp status={waiting ? "needs_you" : "working"} />
                    <StatusWord status={waiting ? "needs_you" : "working"} />
                    {!waiting ? <span className="min-w-0 truncate font-mono text-[11.5px] leading-4 text-ink-3">{a.activity || shortModel(a.model)}</span> : null}
                  </span>
                </Link>
                <button
                  type="button"
                  title={`Stop ${a.name}`}
                  aria-label={`Stop ${a.name}`}
                  onClick={() => onStop(a.id)}
                  className="absolute top-1.5 right-1.5 flex h-6 w-6 items-center justify-center rounded-sm text-ink-2 opacity-0 transition-[opacity,color,background-color] duration-[160ms] group-focus-within:opacity-100 group-hover:opacity-100 hover:bg-vermilion-pale hover:text-vermilion"
                >
                  <X size={12} />
                </button>
              </div>
            );
          })}
        </div>
        {running.length > 1 ? (
          <button
            type="button"
            onClick={onStopAll}
            title="Stop every running subagent"
            className="flex h-9 shrink-0 items-center gap-1.5 rounded-control px-2.5 text-[12.5px] text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-vermilion"
          >
            <Square size={11} />
            <span className="hidden wide:inline">Stop all</span>
          </button>
        ) : null}
        {finished.length > 0 ? <Finished agents={finished} href={href} /> : null}
      </div>
    </div>
  );
}

const statusTone: Record<string, string> = { done: "text-emerald", error: "text-vermilion" };

function Finished({ agents, href }: { agents: Subagent[]; href: (a: Subagent) => AgentLink }) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const off = (e: MouseEvent) => {
      if (!ref.current?.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener("mousedown", off);
    return () => document.removeEventListener("mousedown", off);
  }, [open]);
  return (
    <div ref={ref} className="relative shrink-0">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="flex h-9 items-center gap-1.5 rounded-control px-2.5 text-[12.5px] text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
      >
        <Bot size={13} />
        <span>{agents.length} finished</span>
        <ChevronDown size={12} className={`transition-transform duration-[160ms] ${open ? "rotate-180" : ""}`} />
      </button>
      {open ? (
        <div className="rise absolute right-0 bottom-full z-30 mb-1 w-64 rounded-card bg-surface p-1 shadow-slip">
          {agents.map((a) => (
            <Link
              key={a.id}
              {...href(a)}
              onClick={() => setOpen(false)}
              className="flex items-center gap-2 rounded-control px-2.5 py-1.5 text-[13px] hover:bg-well"
            >
              <span className="min-w-0 flex-1 truncate font-medium text-ink">{a.name}</span>
              <span className={`shrink-0 text-[12px] ${statusTone[a.status] ?? "text-ink-3"}`}>{a.status}</span>
            </Link>
          ))}
        </div>
      ) : null}
    </div>
  );
}
