import { Pencil, RefreshCw, Trash2 } from "lucide-react";
import { Btn } from "../Btn";
import { CategoryChip, ConnectorMark, CustomChip, McpChip } from "../ConnectorForm";
import { ArmedButton } from "../Feedback";
import type { BotConnector } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { connectorLook } from "../statusLook";
import { isCustom } from "./model";

// One connector attached to the Bot: what it is, its state, and what you can do
// about it.
export function AttachedCard({
  row,
  busy,
  onEdit,
  onAuthorize,
  onRefresh,
  onDetach,
  onViewError,
}: {
  row: BotConnector;
  busy: boolean;
  onEdit: () => void;
  onAuthorize: () => void;
  onRefresh: () => void;
  onDetach: () => void;
  onViewError: () => void;
}) {
  const c = row.connector;
  if (!c) return null;
  const isBusy = busy || row.authStatus === "initializing";
  const look = connectorLook(row.authStatus, !!c.builtin);
  return (
    <div className="flex flex-col justify-between rounded-card shadow-card bg-surface p-5 transition-[background-color,color,box-shadow] hover:shadow-float hover:shadow-xs min-h-[140px]">
      <div>
        {/* Card Top: Mark + Title/Badges + Status */}
        <div className="flex items-start gap-4">
          <div className="shrink-0 pt-0.5">
            <ConnectorMark id={c.id} hasImage={c.hasImage} size={48} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-semibold text-[15px] text-ink tracking-tight">{c.name}</span>
              <McpChip transport={c.transport} />
              {isCustom(c) && <CustomChip />}
              {c.category && <CategoryChip label={c.category} />}
            </div>

            <p className="mt-1.5 line-clamp-2 text-xs text-ink-2 leading-relaxed">{c.description || (c.transport === "http" ? c.httpUrl : c.stdioCommand)}</p>

            {/* Status line */}
            <div className="mt-2.5 flex items-center gap-1.5 text-[11px] font-medium text-ink-3">
              {row.authStatus === "initializing" ? (
                <span className="inline-block h-2.5 w-2.5 spin rounded-full border border-line border-t-cobalt" />
              ) : (
                <Lamp status={look.lamp} />
              )}
              <span className={look.tone}>{look.word}</span>
              {row.statusDetail && <span className="truncate text-ink-3">· {row.statusDetail}</span>}
              {row.lastError && <span className="truncate text-vermilion">· {row.lastError}</span>}
              {(row.lastError || (row.authStatus === "error" && row.statusDetail)) && (
                <button type="button" className="shrink-0 font-medium text-cobalt hover:underline" onClick={onViewError}>
                  View error
                </button>
              )}
            </div>
          </div>
        </div>
      </div>

      {/* Card Bottom: Actions */}
      <div className="mt-4 flex items-center justify-between border-t border-line/80 pt-3.5">
        <div className="text-[11px] font-mono text-ink-3 uppercase tracking-wider">{c.builtin ? "built-in" : `${c.transport} · ${c.auth}`}</div>

        <div className="flex items-center gap-1.5">
          {row.authStatus === "needs_auth" &&
            (c.builtin ? (
              <Btn kind="primary" onClick={onEdit}>
                Set up
              </Btn>
            ) : (
              <Btn kind="primary" onClick={onAuthorize}>
                Authorize
              </Btn>
            ))}
          <Btn kind="ghost" onClick={onEdit}>
            <Pencil size={13} />
            <span>Edit</span>
          </Btn>
          <Btn kind="ghost" disabled={isBusy} onClick={onRefresh} title="Refresh status">
            <RefreshCw size={13} className={isBusy ? "spin" : ""} />
            <span>Refresh</span>
          </Btn>
          <ArmedButton kind="ghost" armedLabel="Click again to remove" icon={<Trash2 size={13} />} onConfirm={onDetach}>
            Remove
          </ArmedButton>
        </div>
      </div>
    </div>
  );
}
