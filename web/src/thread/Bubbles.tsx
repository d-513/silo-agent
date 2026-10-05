import { Bot, GitBranch, Inbox, Paperclip, Pencil, RotateCcw, Timer, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import type { AgentLink } from "../links";
import { Btn } from "../Btn";
import { CopyButton, useArmed } from "../Feedback";
import type { Attachment } from "../fold";
import { fmtBytes, fullTime, runWhen } from "../format";
import { Md } from "../Md";
import { FoldRow } from "./FoldRow";

const miniBtn =
  "flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:opacity-40";

export function UserBubble({
  text,
  attachments,
  isLast,
  busy,
  fresh,
  onEdit,
  onDelete,
  onDiverge,
}: {
  text: string;
  attachments?: Attachment[];
  isLast?: boolean;
  busy?: boolean;
  fresh?: boolean;
  onEdit?: (text: string, attachments?: Attachment[]) => void;
  onDelete?: () => void;
  onDiverge?: () => void;
}) {
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(text);
  const del = useArmed();

  const startEdit = () => {
    setDraft(text);
    setEditing(true);
  };

  const saveEdit = () => {
    const next = draft.trim();
    if ((!next && !attachments?.length) || next === text) {
      setEditing(false);
      return;
    }
    setEditing(false);
    onEdit?.(next, attachments);
  };

  if (editing) {
    return (
      <div className="flex w-full justify-end">
        <div className="w-full max-w-[88%] sm:max-w-[80%]">
          <textarea
            autoFocus
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && !e.shiftKey) {
                e.preventDefault();
                saveEdit();
              }
              if (e.key === "Escape") setEditing(false);
            }}
            rows={Math.min(12, Math.max(2, draft.split("\n").length))}
            className="w-full resize-none rounded-bubble rounded-br-[6px] bg-surface px-4 py-2.5 text-[15px] leading-6 text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)] outline-none"
          />
          <div className="mt-1.5 flex items-center justify-end gap-1.5">
            <Btn kind="secondary" size="sm" onClick={() => setEditing(false)}>
              Cancel
            </Btn>
            <Btn kind="primary" size="sm" onClick={saveEdit}>
              Save & resend
            </Btn>
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className={`group flex w-full flex-col items-end gap-1 ${fresh ? "glide" : ""}`}>
      <div className="min-w-0 max-w-[88%] rounded-bubble rounded-br-[6px] bg-well px-4 py-2.5 text-ink sm:max-w-[80%]">
        <div className="whitespace-pre-wrap break-words text-[15px] leading-6 select-text">{text}</div>
        {attachments?.length ? (
          <div className="mt-2 flex flex-wrap gap-1.5">
            {attachments.map((a) => (
              <span
                key={a.path}
                title={a.path}
                className="inline-flex max-w-full items-center gap-1.5 rounded-sm bg-surface px-2.5 py-1 text-[12px] text-ink shadow-card"
              >
                <Paperclip size={12} className="shrink-0 text-ink-2" />
                <span className="max-w-[180px] truncate font-medium">{a.name}</span>
                <span className="shrink-0 font-mono text-[11px] text-ink-3">{fmtBytes(a.size)}</span>
              </span>
            ))}
          </div>
        ) : null}
      </div>
      <div className="flex translate-y-0.5 items-center gap-0.5 opacity-0 transition-[opacity,transform] duration-[180ms] ease-quiet group-focus-within:translate-y-0 group-focus-within:opacity-100 group-hover:translate-y-0 group-hover:opacity-100">
        <CopyButton text={text} title="Copy prompt" size={13} />
        {onEdit && isLast ? (
          <button type="button" onClick={startEdit} disabled={busy} title={busy ? "Stop the Bot to edit" : "Edit and resend"} className={miniBtn}>
            <Pencil size={13} />
          </button>
        ) : null}
        {onDelete && isLast ? (
          <button
            type="button"
            onClick={() => del.fire(() => onDelete())}
            disabled={busy}
            title={busy ? "Stop the Bot to delete" : del.armed ? "Click again to delete" : "Delete"}
            className={`${miniBtn} relative overflow-hidden ${del.armed ? "!bg-vermilion !text-white" : "hover:!text-vermilion"}`}
          >
            <Trash2 size={13} />
            {del.armed ? <span aria-hidden className="drain absolute inset-x-0 bottom-0 h-[2px] bg-white" /> : null}
          </button>
        ) : null}
        {onDiverge ? (
          <button type="button" onClick={onDiverge} disabled={busy} title="Diverge into a new chat" className={miniBtn}>
            <GitBranch size={13} />
          </button>
        ) : null}
      </div>
    </div>
  );
}

// FeedQuote is the Feed post a quoted chat starts from: the Bot's words, set
// off by a cobalt rule, with where and when they were posted.
export function FeedQuote({ text, source, createdAt }: { text: string; source: string; createdAt?: string }) {
  const at = createdAt ? new Date(createdAt) : null;
  return (
    <figure className="min-w-0 rounded-card bg-well px-4 py-3 shadow-[inset_3px_0_0_var(--color-cobalt)]">
      <figcaption className="mb-1.5 flex items-center gap-1.5 text-[12px] leading-4 text-ink-3">
        <Inbox size={12} className="shrink-0" />
        <span className="truncate">
          Quoted from the Feed{source ? ` · ${source}` : ""}
          {at && !Number.isNaN(at.getTime()) ? ` · ${fullTime(at)}` : ""}
        </span>
      </figcaption>
      <div className="silo-reply">
        <Md text={text} />
      </div>
    </figure>
  );
}

// LeadNote is a message a subagent got from its lead: the brief it started
// from, or a later interjection. Set off like a quote, labeled with the sender.
export function LeadNote({ text, createdAt }: { text: string; createdAt?: string }) {
  const when = runWhen(createdAt);
  return (
    <figure className="min-w-0 rounded-card bg-well px-4 py-3 shadow-[inset_3px_0_0_var(--color-ink-3)]">
      <figcaption className="mb-1.5 flex items-center gap-1.5 text-[12px] leading-4 text-ink-3">
        <Bot size={12} className="shrink-0" />
        <span className="truncate">From the lead{when ? ` · ${when}` : ""}</span>
      </figcaption>
      <div className="silo-reply">
        <Md text={text} />
      </div>
    </figure>
  );
}

const reportTone: Record<string, string> = {
  done: "text-emerald",
  error: "text-vermilion",
  stopped: "text-ink-3",
  interrupted: "text-ink-3",
};

// SubagentReport opens a lead's wake run: which subagents finished, each a
// link to its page, with the report the lead was handed folded underneath.
export function SubagentReport({ text, agents, href }: { text: string; agents: { name: string; status: string }[]; href?: (name: string) => AgentLink | undefined }) {
  return (
    <FoldRow
      lead={
        <>
          <span className="h-4 w-4 shrink-0" aria-hidden />
          <span className="flex h-4 w-4 shrink-0 items-center justify-center text-ink-2" aria-hidden>
            <Bot size={14} strokeWidth={1.75} />
          </span>
        </>
      }
      title={<span className="shrink-0 font-medium text-ink-2">{agents.length === 1 ? "Subagent finished" : "Subagents finished"}</span>}
      tail={
        <span className="flex min-w-0 items-center gap-1.5 truncate">
          {agents.map((ag, i) => {
            const to = href?.(ag.name);
            const label = (
              <>
                <span className="font-mono text-[12.5px] text-ink">{ag.name}</span>
                <span className={`text-[12px] ${reportTone[ag.status] ?? "text-ink-3"}`}>{ag.status === "done" ? "✓" : ag.status}</span>
              </>
            );
            return (
              <span key={ag.name} className="inline-flex shrink-0 items-center gap-1">
                {i > 0 ? <span className="text-ink-3">·</span> : null}
                {to ? (
                  <Link {...to} onClick={(e) => e.stopPropagation()} className="inline-flex items-center gap-1 rounded-xs hover:underline">
                    {label}
                  </Link>
                ) : (
                  label
                )}
              </span>
            );
          })}
        </span>
      }
    >
      <div className="silo-reply text-[13px] leading-[21px] text-ink-2">
        <Md text={text} />
      </div>
    </FoldRow>
  );
}

export function Reply({ text, bounds, streaming, onRetry }: { text: string; bounds?: number[]; streaming?: boolean; onRetry?: () => void }) {
  return (
    <div className="group/reply min-w-0">
      <div className={`silo-reply ${streaming ? "silo-live" : ""}`}>
        <Md text={text} bounds={streaming ? bounds : undefined} />
        {streaming && !text ? <span className="breathe inline-block h-[7px] w-[7px] rounded-full bg-ink" /> : null}
      </div>
      {!streaming ? (
        <div className="mt-1 -ml-1.5 flex translate-y-0.5 items-center gap-0.5 opacity-0 transition-[opacity,transform] duration-[180ms] ease-quiet group-focus-within/reply:translate-y-0 group-focus-within/reply:opacity-100 group-hover/reply:translate-y-0 group-hover/reply:opacity-100">
          <CopyButton text={text} title="Copy reply" />
          {onRetry ? (
            <button type="button" onClick={onRetry} title="Retry" aria-label="Retry" className={miniBtn}>
              <RotateCcw size={14} />
            </button>
          ) : null}
        </div>
      ) : null}
    </div>
  );
}

// RunMark opens one automation run in its log: a hairline divider with when it
// ran. The prompt is the automation's own, so it is a tooltip, not a bubble.
export function RunMark({ prompt, createdAt }: { prompt: string; createdAt?: string }) {
  const when = runWhen(createdAt);
  return (
    <div className="flex items-center gap-3 pt-4 pb-1" title={prompt}>
      <span aria-hidden className="h-px flex-1 bg-line" />
      <span className="flex items-center gap-1.5 text-[12px] leading-4 font-medium text-ink-3">
        <Timer size={12} />
        {when ? `Ran ${when}` : "Ran"}
      </span>
      <span aria-hidden className="h-px flex-1 bg-line" />
    </div>
  );
}
