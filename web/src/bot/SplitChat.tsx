import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import type { Said } from "../composer/Composer";
import { UI } from "../gen/silo/v1/ui_pb";
import { agentLink, chatLink, newChatLink } from "../links";
import { reload } from "../query";
import { useRunStream } from "../useRunStream";
import { useSubagents } from "../useSubagents";
import { useBotPage } from "./context";
import { RunPane } from "./RunPane";
import { runActions, type ChatPick } from "./runActions";
import { patchChats } from "./useChatList";
import { useComposerDraft } from "./useComposerDraft";

// A chat in the split beside the page: a conversation of its own, with its
// own stream, subagents and unsent draft, beside whatever the page is (even
// another chat). What the page holds for every chat (the list, the models,
// the call) it shares.
export function SplitChat({ chatId }: { chatId: string }) {
  const p = useBotPage();
  const navigate = useNavigate();
  const draft = useComposerDraft(p.id, p.bot.workerConnected);
  const [pick, setPick] = useState<ChatPick>({ model: "", thinking: "" });
  const [said, setSaid] = useState<Said | null>(null);
  const subs = useSubagents(p.id, chatId);
  const run = useRunStream(p.id, chatId, {
    onApproval: p.reloadApprovals,
    onTitle: (title) => patchChats(p.id, (xs) => xs.map((c) => (c.id === chatId ? { ...c, title } : c))),
    onDone: () => {
      void reload(UI.method.listChats, { botId: p.id });
      void reload(UI.method.listArtifacts, { botId: p.id });
      subs.refresh();
    },
    onPing: () => subs.refresh(),
  });
  const agentHref = (name: string) => {
    const a = subs.agents.find((x) => x.name.toLowerCase() === name.toLowerCase());
    return a ? agentLink(p.id, chatId, a.id) : undefined;
  };
  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <RunPane
        id={p.id}
        bot={p.bot}
        chatId={chatId}
        fresh={false}
        pick={pick}
        chats={p.chats}
        models={p.models}
        defaultModel={p.defaultModel}
        voice={p.voice}
        talk={p.talk}
        call={p.call}
        onTalk={() => p.call.start(chatId)}
        run={run}
        subs={subs}
        draft={draft}
        actions={runActions({
          id: p.id,
          chatId,
          fresh: false,
          pick,
          setPick,
          draft,
          run,
          onError: p.onError,
          openChat: (cid) => void navigate(chatLink(p.id, cid)),
          openNew: () => void navigate(newChatLink(p.id)),
          onSaid: setSaid,
          showCommands: () => draft.setText("/"),
        })}
        said={said}
        setSaid={setSaid}
        agentHref={agentHref}
        onInspectArtifact={p.inspectArtifact}
        onSaveSkill={p.saveSkill}
        onError={p.onError}
      />
    </section>
  );
}
