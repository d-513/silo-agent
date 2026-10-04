import { ui } from "../api";
import type { Artifact } from "../Artifact";
import { Composer } from "../Composer";
import { fail } from "../errors";
import type { Bot, Chat, ModelOption } from "../gen/silo/v1/ui_pb";
import { SubagentTray } from "../SubagentTray";
import { Taskboard } from "../Taskboard";
import { Thread } from "../Thread";
import type { useRunStream } from "../useRunStream";
import type { useSubagents } from "../useSubagents";
import type { RunActions } from "./runActions";
import type { ComposerDraft } from "./useComposerDraft";

// The Chat tab's working area: the shared taskboard, the thread, the tray of
// subagents, and the composer.
export function RunPane({
  id,
  bot,
  chatId,
  chats,
  models,
  defaultModel,
  voice,
  run,
  subs,
  draft,
  actions,
  waitingRuns,
  agentHref,
  onInspectArtifact,
  onSaveSkill,
  onError,
}: {
  id: string;
  bot: Bot;
  chatId?: string;
  chats: Chat[];
  models: ModelOption[];
  defaultModel: string;
  voice: boolean;
  run: ReturnType<typeof useRunStream>;
  subs: ReturnType<typeof useSubagents>;
  draft: ComposerDraft;
  actions: RunActions;
  // Runs of subagents that are paused on an approval.
  waitingRuns: Set<string>;
  agentHref: (name: string) => string | undefined;
  onInspectArtifact: (a: Artifact) => void;
  onSaveSkill: (a: Artifact) => void;
  onError: (message: string) => void;
}) {
  const chat = chats.find((c) => c.id === chatId);
  return (
    <>
      <Taskboard
        items={subs.board}
        agentHref={agentHref}
        onClear={() => {
          if (!chatId) return;
          ui.clearTaskboard({ botId: id, chatId })
            .then((b) => subs.setBoard(b.items))
            .catch((e) => onError(fail(e)));
        }}
      />
      <Thread
        botId={id}
        botName={bot.name}
        botCrest={bot.crest}
        chatId={chatId}
        events={run.events}
        sending={run.sending}
        fresh={run.fresh}
        onInspectArtifact={onInspectArtifact}
        onSaveSkill={onSaveSkill}
        onSelectPrompt={(p) => draft.setText(p)}
        onEditMessage={(eid, t, a) => void actions.editMessage(eid, t, a)}
        onDeleteMessage={(eid) => void actions.deleteMessage(eid)}
        onDivergeChat={(eid) => void actions.divergeChat(eid)}
        agentHref={agentHref}
      />
      {chatId ? (
        <SubagentTray
          botId={id}
          chatId={chatId}
          agents={subs.agents}
          waitingRuns={waitingRuns}
          onStop={(sid) => {
            ui.stopSubagent({ botId: id, id: sid }).then(subs.refresh).catch((e) => onError(fail(e)));
          }}
          onStopAll={() => {
            for (const a of subs.agents.filter((x) => x.running)) {
              ui.stopSubagent({ botId: id, id: a.id }).catch((e) => onError(fail(e)));
            }
            setTimeout(subs.refresh, 300);
          }}
        />
      ) : null}
      <Composer
        text={draft.text}
        setText={draft.setText}
        atts={draft.atts}
        onRemoveAtt={(path) => draft.setAtts((xs) => xs.filter((x) => x.path !== path))}
        attachErr={draft.attachErr}
        onAttach={draft.attach}
        onSend={() => void actions.send()}
        onStop={() => void actions.stopRun()}
        sending={run.sending}
        chatId={chatId}
        workerConnected={bot.workerConnected}
        botName={bot.name}
        models={models}
        model={chat?.model || defaultModel}
        onModel={(m) => void actions.pickModel(m)}
        thinking={chat?.thinking ?? ""}
        onThinking={(l) => void actions.pickThinking(l)}
        usage={run.usage}
        onCompact={() => void actions.compactChat()}
        voice={voice}
        onTranscribe={(audio, mime) => ui.transcribe({ botId: id, audio, mime }).then((r) => r.text)}
        onCollect={() => ui.collectMemories({ botId: id, chatId: chatId! })}
      />
    </>
  );
}
