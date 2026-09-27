import { useCallback, useEffect, useRef, useState } from "react";
import { ui } from "./api";
import type { Subagent, TaskItem } from "./gen/silo/v1/ui_pb";

// useSubagents loads a chat's subagents and the Taskboard it shares with them.
// chatId may be a lead chat or a subagent's log (the board resolves to the
// lead's; the list is only filled for a lead). Callers refresh on the stream's
// "board"/"subagents" pings; while anything runs it also polls so the tray's
// activity lines move.
export function useSubagents(botId: string | undefined, chatId: string | undefined, opts: { lead?: boolean } = {}) {
  const lead = opts.lead ?? true;
  const [agents, setAgents] = useState<Subagent[]>([]);
  const [board, setBoard] = useState<TaskItem[]>([]);
  const gen = useRef(0);

  const refresh = useCallback(() => {
    if (!botId || !chatId) return;
    const mine = gen.current;
    ui.getTaskboard({ botId, chatId })
      .then((r) => mine === gen.current && setBoard(r.items))
      .catch(() => {});
    if (lead) {
      ui.listSubagents({ botId, chatId })
        .then((r) => mine === gen.current && setAgents(r.subagents))
        .catch(() => {});
    }
  }, [botId, chatId, lead]);

  useEffect(() => {
    gen.current++;
    setAgents([]);
    setBoard([]);
    refresh();
  }, [refresh]);

  const live = agents.some((a) => a.running);
  useEffect(() => {
    if (!live && lead) return;
    const t = setInterval(refresh, live ? 2500 : 5000);
    return () => clearInterval(t);
  }, [live, lead, refresh]);

  return { agents, board, setBoard, refresh };
}

// elapsed renders "42s", "3m", "1h05m" since an RFC 3339 time.
export function elapsed(iso: string, now = Date.now()): string {
  const t = Date.parse(iso);
  if (!Number.isFinite(t)) return "";
  const s = Math.max(0, Math.round((now - t) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  return `${Math.floor(s / 3600)}h${String(Math.floor((s % 3600) / 60)).padStart(2, "0")}m`;
}

// shortModel is the last path segment of a provider/model id.
export function shortModel(id: string): string {
  const parts = id.split("/");
  return parts[parts.length - 1] || id;
}
