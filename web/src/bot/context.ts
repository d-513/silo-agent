import type { Said } from "../composer/Composer";
import { createContext, useContext } from "react";
import type { Artifact } from "../Artifact";
import type { Call } from "../call/useCall";
import type { Bot, Chat, ModelOption } from "../gen/silo/v1/ui_pb";
import type { AgentLink } from "../links";
import type { useRunStream } from "../useRunStream";
import type { useSubagents } from "../useSubagents";
import type { ChatPick, RunActions } from "./runActions";
import type { Tab } from "./tabs";
import type { ChatRename } from "./useChatList";
import type { ComposerDraft } from "./useComposerDraft";
import type { SecretsState } from "./useSecrets";

// What BotPage (the /bots/$botId layout) holds for the pages under it:
// the live Bot row and everything that must survive moving between pages.
export type BotPageState = {
  id: string;
  bot: Bot;
  tab: Tab;
  chatId?: string;
  // The empty chat (/run/new) and what is picked for it.
  fresh: boolean;
  pick: ChatPick;
  agentId?: string;
  start: () => void;
  stop: () => void;
  // The banner across the top of the page; "" clears it.
  onError: (message: string) => void;
  reloadApprovals: () => void;
  inspectArtifact: (a: Artifact) => void;
  saveSkill: (a: Artifact) => void;
  secrets: SecretsState;
  // The chat side.
  chats: Chat[];
  models: ModelOption[];
  defaultModel: string;
  voice: boolean;
  // talk offers a call (voice on, speech both ways); call is the live one.
  talk: boolean;
  call: Call;
  startCall: () => void;
  rename: ChatRename;
  newChat: () => void;
  deleteChat: (cid: string) => void;
  run: ReturnType<typeof useRunStream>;
  subs: ReturnType<typeof useSubagents>;
  draft: ComposerDraft;
  actions: RunActions;
  // What the last slash command said back in the composer.
  said: Said | null;
  setSaid: (s: Said | null) => void;
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
