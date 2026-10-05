import { skipToken, useQuery } from "@connectrpc/connect-query";
import { useCallback, useEffect } from "react";
import { fail, isGone } from "../errors";
import { UI } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { botPollMs } from "../queryPolicy";

// useBotLive keeps a Bot's row and its pending approvals current: polled fast
// until the box settles (online or stopped), slowly after. Only a Bot that is
// gone (deleted, not ours) ends the page; a blip keeps the last row and the
// next tick recovers.
export function useBotLive(id: string | undefined, chatId: string | undefined) {
  const botQ = useQuery(UI.method.getBot, id ? { id } : skipToken, {
    refetchInterval: (q) => (isGone(q.state.error) ? false : botPollMs(q.state.data)),
  });
  const bot = botQ.data ?? null;
  const approvals = useQuery(UI.method.listApprovals, id ? { botId: id } : skipToken, {
    refetchInterval: botPollMs(botQ.data),
  });

  const reloadApprovals = useCallback(() => {
    if (id) void reload(UI.method.listApprovals, { botId: id });
  }, [id]);

  // Opening a chat looks for approvals right away instead of waiting for a tick.
  useEffect(() => {
    if (chatId) reloadApprovals();
  }, [chatId, reloadApprovals]);

  return {
    bot,
    loadErr: isGone(botQ.error) ? fail(botQ.error) : "",
    pending: approvals.data?.approvals ?? [],
    reloadApprovals,
  };
}
