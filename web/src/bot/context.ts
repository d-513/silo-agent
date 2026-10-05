import { createContext, useContext } from "react";
import type { Artifact } from "../Artifact";
import type { Bot, BotConnector, Chat, ModelOption } from "../gen/silo/v1/ui_pb";
import type { AgentLink } from "../links";
import type { useRunStream } from "../useRunStream";
import type { useSubagents } from "../useSubagents";
import type { RunActions } from "./runActions";
import type { Tab } from "./tabs";
import type { ChatRename } from "./useChatList";
import type { ComposerDraft } from "./useComposerDraft";
import type { SecretsState } from "./useSecrets";

// What BotPage (the /bots/$botId layout) holds for the tab pages under it:
// the live Bot row and everything that must survive moving between tabs.
export type BotPageState = {
  id: string;
  bot: Bot;
  admin: boolean;
  tab: Tab;
  chatId?: string;
  agentId?: string;
  start: () => void;
  stop: () => void;
  // The banner under the header; "" clears it.
  onError: (message: string) => void;
  reloadApprovals: () => void;
  inspectArtifact: (a: Artifact) => void;
  saveSkill: (a: Artifact) => void;
  // Ask for a connector sign-in on the right-hand slip.
  needAuth: (c: BotConnector | null) => void;
  secrets: SecretsState;
  // The chat side.
  chats: Chat[];
  models: ModelOption[];
  defaultModel: string;
  voice: boolean;
  rename: ChatRename;
  newChat: () => void;
  deleteChat: (cid: string) => void;
  run: ReturnType<typeof useRunStream>;
  subs: ReturnType<typeof useSubagents>;
  draft: ComposerDraft;
  actions: RunActions;
  // The open chat (or one of its subagents) is paused on an approval.
  waiting: boolean;
  waitingRuns: Set<string>;
  agentsBusy: boolean;
  agentHref: (name: string) => AgentLink | undefined;
};

export const BotPageCtx = createContext<BotPageState | null>(null);

export function useBotPage() {
  const c = useContext(BotPageCtx);
  if (!c) throw new Error("useBotPage outside a Bot page");
  return c;
}

// A pane with sub-pages (Channels, Drives, Automations) stays mounted across
// them and is told which one the URL names: `view` from the route's
// staticData, `id` from its path param. No view is the list.
export type SubPage = { view?: "new" | "add" | "edit" | "setup"; id?: string };
