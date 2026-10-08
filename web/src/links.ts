import { linkOptions } from "@tanstack/react-router";

// Links that more than one place builds. Everything else writes its `to` and
// `params` inline; the route tree in router.tsx type-checks them.

/** A chat, or the chat page (which opens the newest chat) when there is none. */
export function chatLink(botId: string, chatId?: string) {
  return chatId
    ? linkOptions({ to: "/bots/$botId/run/$chatId", params: { botId, chatId } })
    : linkOptions({ to: "/bots/$botId/run", params: { botId } });
}

/** One of a chat's subagents. */
export function agentLink(botId: string, chatId: string, agentId: string) {
  return linkOptions({ to: "/bots/$botId/run/$chatId/agent/$agentId", params: { botId, chatId, agentId } });
}
export type AgentLink = ReturnType<typeof agentLink>;
