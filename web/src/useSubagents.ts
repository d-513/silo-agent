import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useCallback } from "react";
import { UI, type TaskItem } from "./gen/silo/v1/ui_pb";
import { patch, reload } from "./query";
import { subagentPollMs } from "./queryPolicy";

// useSubagents loads a chat's subagents and the Taskboard it shares with them.
// chatId may be a lead chat or a subagent's log (the board resolves to the
// lead's; the list is only filled for a lead). Callers refresh on the stream's
// "board"/"subagents" pings; while anything runs it also polls so the tray's
// activity lines move.
export function useSubagents(botId: string | undefined, chatId: string | undefined, opts: { lead?: boolean } = {}) {
  const lead = opts.lead ?? true;
  const input = botId && chatId ? { botId, chatId } : undefined;
  const agentsQ = useQuery(UI.method.listSubagents, input && lead ? input : skipToken, {
    refetchInterval: (q) => subagentPollMs(q.state.data?.subagents ?? [], lead),
  });
  const agents = agentsQ.data?.subagents ?? [];
  const boardQ = useQuery(UI.method.getTaskboard, input ?? skipToken, {
    refetchInterval: subagentPollMs(agents, lead),
  });

  const refresh = useCallback(() => {
    if (!botId || !chatId) return;
    void reload(UI.method.getTaskboard, { botId, chatId });
    if (lead) void reload(UI.method.listSubagents, { botId, chatId });
  }, [botId, chatId, lead]);

  const setBoard = useCallback(
    (items: TaskItem[]) => {
      if (botId && chatId) patch(UI.method.getTaskboard, { botId, chatId }, (r) => ({ ...r, items }));
    },
    [botId, chatId],
  );

  return { agents, board: boardQ.data?.items ?? [], setBoard, refresh };
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
