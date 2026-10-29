import { Square } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { t } from "../i18n.ts";
import type { Artifact } from "../Artifact";
import { CallBar } from "../call/CallBar";
import { CallStage } from "../call/CallStage";
import type { Call } from "../call/useCall";
import { useNavigate } from "@tanstack/react-router";
import { chatLink } from "../links";
import { useQuery } from "@connectrpc/connect-query";
import { Composer, type Said } from "../composer/Composer";
import { fail } from "../errors";
import { UI, type Bot, type Chat, type Command, type ModelOption } from "../gen/silo/v1/ui_pb";
import type { AgentLink } from "../links";
import { SubagentTray } from "../SubagentTray";
import { Taskboard } from "../Taskboard";
import { Thread } from "../Thread";
import type { useRunStream } from "../useRunStream";
import type { useSubagents } from "../useSubagents";
import type { ChatPick, RunActions } from "./runActions";
import type { ComposerDraft } from "./useComposerDraft";

const none: Command[] = [];

// The chat's working area: the shared taskboard, the thread, the tray of
// subagents, and the composer.
export function RunPane({
  id,
  bot,
  chatId,
  fresh,
  pick,
  chats,
  models,
  defaultModel,
  voice,
  talk,
  call,
  onTalk,
  run,
  subs,
  draft,
  actions,
  said,
  setSaid,
  waiting,
  waitingRuns,
  agentHref,
  onInspectArtifact,
  onSaveSkill,
  onError,
}: {
  id: string;
  bot: Bot;
  chatId?: string;
  fresh: boolean;
  pick: ChatPick;
  chats: Chat[];
  models: ModelOption[];
  defaultModel: string;
  voice: boolean;
  talk: boolean;
  call: Call;
  onTalk: () => void;
  run: ReturnType<typeof useRunStream>;
  subs: ReturnType<typeof useSubagents>;
  draft: ComposerDraft;
  actions: RunActions;
  said: Said | null;
  setSaid: (s: Said | null) => void;
  // This chat's run is paused on an approval.
  waiting: boolean;
  // Runs of subagents that are paused on an approval.
  waitingRuns: Set<string>;
  agentHref: (name: string) => AgentLink | undefined;
  onInspectArtifact: (a: Artifact) => void;
  onSaveSkill: (a: Artifact) => void;
  onError: (message: string) => void;
}) {
  const chat = chats.find((c) => c.id === chatId);
  const commands = useQuery(UI.method.listCommands, { botId: id }, { staleTime: Infinity }).data?.commands ?? none;
  // The empty chat shows what was picked for it; the first send carries it.
  const picked = !chatId && fresh ? pick : undefined;
  const navigate = useNavigate();
  // The call is drawn here when it is held in this chat (or is making it).
  const callChat = call.snap?.chatId ?? "";
  const callHere = !!call.snap && (callChat === chatId || (callChat === "new" && fresh && !chatId));
  // The call screen takes the column; the composer comes up under it only
  // when asked for (Type).
  const staged = callHere && call.expanded;
  const [typing, setTyping] = useState(false);
  useEffect(() => {
    if (!staged) setTyping(false);
  }, [staged]);
  const composer = (
    <Composer
      text={draft.text}
      setText={draft.setText}
      atts={draft.atts}
      onRemoveAtt={(path) => draft.setAtts((xs) => xs.filter((x) => x.path !== path))}
      quotes={draft.quotes}
      onRemoveQuote={draft.unquote}
      attachErr={draft.attachErr}
      onAttach={draft.attach}
      onSend={() => void actions.send()}
      onStop={() => void actions.stopRun()}
      sending={run.sending}
      chatId={chatId}
      fresh={fresh}
      workerConnected={bot.workerConnected}
      botName={bot.name}
      models={models}
      model={chat?.model || picked?.model || defaultModel}
      onModel={(m) => void actions.pickModel(m)}
      thinking={chat?.thinking ?? picked?.thinking ?? ""}
      onThinking={(l) => void actions.pickThinking(l)}
      usage={run.usage}
      onCompact={() => void actions.compactChat()}
      voice={voice}
      onTranscribe={(audio, mime) => ui.transcribe({ botId: id, audio, mime }).then((r) => r.text)}
      onCollect={() => ui.collectMemories({ botId: id, chatId: chatId! })}
      commands={commands}
      onCommand={actions.sendCommand}
      said={said}
      onDismissSaid={() => setSaid(null)}
      talk={talk && !(callHere && call.live)}
      onTalk={onTalk}
    />
  );
  if (staged) {
    return (
      <>
        <CallStage
          call={call}
          botName={bot.name}
          crest={bot.crest}
          where={chat?.main ? t("Main chat") : chat?.title || t("New chat")}
          waiting={waiting}
          typing={typing}
          onType={() => setTyping((v) => !v)}
          onMinimize={() => call.setExpanded(false)}
        />
        {typing ? composer : null}
      </>
    );
  }
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
        onEditMessage={(eid, t, a, q) => void actions.editMessage(eid, t, a, q)}
        onDeleteMessage={(eid) => void actions.deleteMessage(eid)}
        onDivergeChat={(eid) => void actions.divergeChat(eid)}
        agentHref={agentHref}
        paging={run}
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
      {fresh && !chatId && bot.people.length > 1 ? (
        <p className="mx-auto w-full max-w-[760px] px-4 pb-2 text-center text-[12px] leading-[18px] text-ink-3">
          {t("{{name}}'s managers can read this chat. What you tell {{name}}, it may remember for everyone who uses it.", { name: bot.name })}
        </p>
      ) : null}
      {chat && !chat.mine ? (
        <div className="mx-auto mb-4 flex w-full max-w-[760px] items-center gap-3 rounded-card bg-well px-4 py-3">
          <p className="min-w-0 flex-1 text-[13px] leading-5 text-ink-3">
            {chat.author
              ? t("{{who}}'s chat. You can read it and stop it; only {{who}} writes in it.", { who: chat.author })
              : t("{{name}}'s own chat. You can read it and stop it.", { name: bot.name })}
          </p>
          {run.sending ? (
            <Btn kind="secondary" size="sm" onClick={() => void actions.stopRun()}>
              <Square size={12} />
              {t("Stop")}
            </Btn>
          ) : null}
        </div>
      ) : (
        <>
        <CallBar
          call={call}
          botName={bot.name}
          here={callHere}
          onBack={callChat && callChat !== "new" ? () => void navigate(chatLink(id, callChat)) : undefined}
          onExpand={() => call.setExpanded(true)}
        />
        {composer}
        </>
      )}
    </>
  );
}
