import { ChevronLeft, ChevronRight, Square } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { chatLink } from "./links";
import { ui } from "./api";
import type { Artifact } from "./Artifact";
import { Btn } from "./Btn";
import { UI, type Bot } from "./gen/silo/v1/ui_pb";
import { Status } from "./Lamp";
import { finishedLook, runLook, started } from "./statusLook";
import { SkeletonRows } from "./Field";
import { Taskboard } from "./Taskboard";
import { Md } from "./Md";
import { Thread } from "./Thread";
import { useRunStream } from "./useRunStream";
import { shortModel, useSubagents } from "./useSubagents";
import { fail } from "./errors";
import { t } from "./i18n.ts";
import { Trans } from "./Trans.tsx";

// SubagentPage shows one subagent's work log with the shared Thread. It is
// read-only: only the lead talks to a subagent. The human can Stop it.
export function SubagentPage({
  bot,
  chatId,
  agentId,
  onError,
  onApprovals,
  onInspectArtifact,
  onSaveSkill,
}: {
  bot: Bot;
  chatId: string;
  agentId: string;
  onError: (s: string) => void;
  onApprovals: () => void;
  onInspectArtifact: (a: Artifact) => void;
  onSaveSkill: (a: Artifact) => void;
}) {
  const { id: botId, name: botName, crest: botCrest } = bot;
  const q = useQuery(UI.method.getSubagent, { botId, id: agentId }, { refetchInterval: 3000, retry: false });
  const sa = q.data ?? null;
  const gone = !sa && !!q.error;
  const [brief, setBrief] = useState(false);
  const { board, refresh } = useSubagents(botId, sa?.chatId, { lead: false });
  const stream = useRunStream(botId, sa?.chatId, { onApproval: onApprovals, onDone: refresh });

  const back = chatLink(botId, chatId);
  if (!sa) {
    return (
      <div className="p-7">
        {gone ? (
          <p className="text-ink-2">
            <Trans i18nKey="This subagent is gone. <link>Back to the chat</link>" components={{ link: <Link {...back} className="text-cobalt" /> }} />
          </p>
        ) : (
          <SkeletonRows rows={3} />
        )}
      </div>
    );
  }
  const busy = sa.running || stream.sending;
  const doing = bot.activity.find((r) => r.kind === "subagent" && r.leadChatId === chatId && r.title === sa.name);
  const stop = () => ui.stopSubagent({ botId, id: sa.id }).catch((e) => onError(fail(e)));
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="shrink-0 px-4 pt-3 wide:px-7">
        <Link {...back} className="mb-1.5 inline-flex items-center gap-1 text-small text-ink-2 hover:text-ink">
          <ChevronLeft size={14} /> {t("Back to the lead")}
        </Link>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h2 className="text-heading leading-7 font-medium tracking-[-0.015em]">{sa.name}</h2>
          <Status look={busy ? runLook(doing ?? started) : finishedLook(sa.status)} />
          <span className="font-mono text-label text-ink-3" title={sa.model}>
            {shortModel(sa.model)}
          </span>
          <span className="flex-1" />
          <button
            type="button"
            aria-expanded={brief}
            onClick={() => setBrief((v) => !v)}
            className="inline-flex h-8 items-center gap-1 rounded-control px-2 text-small text-ink-2 hover:bg-well hover:text-ink"
          >
            <ChevronRight size={13} className={`transition-transform duration-[160ms] ${brief ? "rotate-90" : ""}`} />{t("Brief")}
          </button>
          {busy ? (
            <Btn kind="secondary" size="sm" icon={<Square size={11} />} onClick={() => void stop()}>
              {t("Stop")}
            </Btn>
          ) : null}
        </div>
        {brief ? (
          <div className="rise mt-2 mb-1 max-h-[40vh] overflow-auto rounded-card bg-well px-4 py-3 text-small leading-[20px]">
            <p className="mb-1 text-label-caps text-ink-3 uppercase">{t("Goal")}</p>
            <div className="silo-reply">
              <Md text={sa.goal} />
            </div>
            {sa.context.trim() ? (
              <>
                <p className="mt-3 mb-1 text-label-caps text-ink-3 uppercase">{t("Context")}</p>
                <div className="silo-reply">
                  <Md text={sa.context} />
                </div>
              </>
            ) : null}
          </div>
        ) : null}
      </div>
      <Taskboard items={board} mine={sa.name} />
      <Thread
        botId={botId}
        botName={botName}
        botCrest={botCrest}
        chatId={sa.chatId}
        events={stream.events}
        sending={stream.sending}
        paging={stream}
        onInspectArtifact={onInspectArtifact}
        onSaveSkill={onSaveSkill}
        emptyState={<p className="my-auto py-14 text-center text-small text-ink-3">{t("Starting…")}</p>}
      />
    </div>
  );
}
