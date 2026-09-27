import { Check, ChevronRight, ListChecks, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router-dom";
import { useArmed } from "./Feedback";
import type { TaskItem } from "./gen/silo/v1/ui_pb";

function AgentChip({ name, to }: { name: string; to?: string }) {
  const inner = (
    <>
      <span className="truncate font-mono text-[11.5px] leading-4">{name}</span>
    </>
  );
  const cls = "inline-flex h-5 max-w-[9rem] shrink-0 items-center rounded-xs bg-pressed px-1.5 text-ink-2";
  return to ? (
    <Link to={to} onClick={(e) => e.stopPropagation()} className={`${cls} hover:text-ink`}>
      {inner}
    </Link>
  ) : (
    <span className={cls}>{inner}</span>
  );
}

// Taskboard is the chat's shared checklist, a strip at the top of the thread.
// Folded it is a progress line with who owns what; open it lists every task.
// The human only reads it (and may clear it); the agents write it.
export function Taskboard({
  items,
  onClear,
  agentHref,
  mine,
}: {
  items: TaskItem[];
  onClear?: () => void;
  agentHref?: (name: string) => string | undefined;
  // On a subagent's page: its own tasks are lit, the rest dimmed.
  mine?: string;
}) {
  const [open, setOpen] = useState(false);
  const clear = useArmed();
  if (items.length === 0) return null;
  const done = items.filter((t) => t.done).length;
  const owners = [...new Set(items.map((t) => t.assignee).filter(Boolean))];
  const pct = Math.round((done / items.length) * 100);
  return (
    <div className="shrink-0 px-3 pt-2 wide:px-6">
      <div className={`mx-auto max-w-[760px] rounded-card transition-[background-color,box-shadow] duration-[200ms] ease-quiet ${open ? "bg-surface shadow-card" : "bg-well"}`}>
        <button
          type="button"
          aria-expanded={open}
          onClick={() => setOpen((v) => !v)}
          className="relative flex h-9 w-full min-w-0 items-center gap-2 overflow-hidden rounded-card px-3 text-left text-[13px]"
        >
          <ChevronRight size={14} className={`shrink-0 text-ink-3 transition-transform duration-[160ms] ${open ? "rotate-90" : ""}`} />
          <ListChecks size={14} className="shrink-0 text-ink-2" />
          <span className="shrink-0 font-medium text-ink">Tasks</span>
          <span className="shrink-0 font-mono text-[12px] text-ink-3">
            {done}/{items.length}
          </span>
          <span className="flex min-w-0 flex-1 items-center gap-1 overflow-hidden">
            {owners.map((n) => (
              <AgentChip key={n} name={n} to={agentHref?.(n)} />
            ))}
          </span>
          <span aria-hidden className="absolute right-0 bottom-0 left-0 h-[2px] bg-line">
            <span className="block h-full bg-emerald transition-[width] duration-[300ms] ease-quiet" style={{ width: `${pct}%` }} />
          </span>
        </button>
        {open ? (
          <div className="max-h-[40vh] overflow-auto px-2 pt-1 pb-2">
            <ol className="space-y-0.5">
              {items.map((t) => {
                const dim = mine !== undefined && t.assignee.toLowerCase() !== mine.toLowerCase();
                return (
                  <li key={t.n} className={`flex items-start gap-2 rounded-control px-2 py-1.5 text-[13px] leading-5 ${dim ? "opacity-55" : ""}`}>
                    <span
                      aria-label={t.done ? "done" : "open"}
                      className={`mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-[4px] ${t.done ? "bg-emerald text-white" : "shadow-[inset_0_0_0_1.5px_var(--color-line-strong)]"}`}
                    >
                      {t.done ? <Check size={11} strokeWidth={3} /> : null}
                    </span>
                    <span className="w-7 shrink-0 font-mono text-[12px] leading-5 text-ink-3">#{t.n}</span>
                    <span className="min-w-0 flex-1">
                      <span className="flex flex-wrap items-center gap-1.5">
                        {t.assignee ? <AgentChip name={t.assignee} to={agentHref?.(t.assignee)} /> : null}
                        <span className={t.done ? "text-ink-2 line-through decoration-ink-3/60" : "text-ink"}>{t.text}</span>
                      </span>
                      {t.done && (t.doneBy || t.note) ? (
                        <span className="block text-[12px] leading-[18px] text-ink-3">
                          {t.doneBy ? `done by ${t.doneBy}` : "done"}
                          {t.note ? ` · ${t.note}` : ""}
                        </span>
                      ) : null}
                    </span>
                  </li>
                );
              })}
            </ol>
            {onClear ? (
              <div className="flex justify-end px-2 pt-1">
                <button
                  type="button"
                  onClick={() => clear.fire(onClear)}
                  className={`inline-flex h-7 items-center gap-1.5 rounded-control px-2 text-[12.5px] transition-colors duration-[160ms] ${
                    clear.armed ? "bg-vermilion text-white" : "text-ink-2 hover:bg-well hover:text-vermilion"
                  }`}
                >
                  <Trash2 size={12} />
                  {clear.armed ? "Click again to clear" : "Clear board"}
                </button>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
    </div>
  );
}
