import { ChevronLeft, ChevronRight, Square } from "lucide-react";
import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import { chatLink } from "./links";
import { ui } from "./api";
import type { Artifact } from "./Artifact";
import { Btn } from "./Btn";
import type { Subagent } from "./gen/silo/v1/ui_pb";
import { Lamp } from "./Lamp";
import { SkeletonRows } from "./Field";
import { Taskboard } from "./Taskboard";
import { Md } from "./Md";
import { Thread } from "./Thread";
import { useRunStream } from "./useRunStream";
import { shortModel, useSubagents } from "./useSubagents";
import { fail } from "./errors";

// SubagentPage shows one subagent's work log with the shared Thread. It is
// read-only: only the lead talks to a subagent. The human can Stop it.
export function SubagentPage({
  botId,
  botName,
  botCrest,
  chatId,
  agentId,
  onError,
  onApprovals,
  onInspectArtifact,
  onSaveSkill,
}: {
  botId: string;
  botName: string;
  botCrest: number;
  chatId: string;
  agentId: string;
  onError: (s: string) => void;
  onApprovals: () => void;
  onInspectArtifact: (a: Artifact) => void;
  onSaveSkill: (a: Artifact) => void;
}) {
  const [sa, setSa] = useState<Subagent | null>(null);
  const [gone, setGone] = useState(false);
  const [brief, setBrief] = useState(false);
  const { board, refresh } = useSubagents(botId, sa?.chatId, { lead: false });
  const stream = useRunStream(botId, sa?.chatId, { onApproval: onApprovals, onDone: refresh });

  useEffect(() => {
    let dead = false;
    setSa(null);
    setGone(false);
    const load = () =>
      ui
        .getSubagent({ botId, id: agentId })
        .then((r) => !dead && setSa(r))
        .catch(() => !dead && setGone(true));
    load();
    const t = setInterval(load, 3000);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [botId, agentId]);

  const back = chatLink(botId, chatId);
  if (!sa) {
    return (
      <div className="p-7">
        {gone ? (
          <p className="text-ink-2">
            This subagent is gone. <Link {...back} className="text-cobalt">Back to the chat</Link>
          </p>
        ) : (
          <SkeletonRows rows={3} />
        )}
      </div>
    );
  }
  const busy = sa.running || stream.sending;
  const stop = () => ui.stopSubagent({ botId, id: sa.id }).catch((e) => onError(fail(e)));
  return (
    <div className="flex min-h-0 min-w-0 flex-1 flex-col">
      <div className="shrink-0 px-4 pt-3 wide:px-7">
        <Link {...back} className="mb-1.5 inline-flex items-center gap-1 text-[13px] text-ink-2 hover:text-ink">
          <ChevronLeft size={14} /> Back to the lead
        </Link>
        <div className="flex flex-wrap items-center gap-x-3 gap-y-1">
          <h2 className="text-[20px] leading-7 font-medium tracking-[-0.015em]">{sa.name}</h2>
          <span className="flex items-center gap-1.5 text-[12.5px] text-ink-3">
            <Lamp status={busy ? "working" : "stopped"} />
            {busy ? "Working" : sa.status}
          </span>
          <span className="font-mono text-[12px] text-ink-3" title={sa.model}>
            {shortModel(sa.model)}
          </span>
          <span className="flex-1" />
          <button
            type="button"
            aria-expanded={brief}
            onClick={() => setBrief((v) => !v)}
            className="inline-flex h-8 items-center gap-1 rounded-control px-2 text-[13px] text-ink-2 hover:bg-well hover:text-ink"
          >
            <ChevronRight size={13} className={`transition-transform duration-[160ms] ${brief ? "rotate-90" : ""}`} />
            Brief
          </button>
          {busy ? (
            <Btn kind="secondary" size="sm" icon={<Square size={11} />} onClick={() => void stop()}>
              Stop
            </Btn>
          ) : null}
        </div>
        {brief ? (
          <div className="rise mt-2 mb-1 max-h-[40vh] overflow-auto rounded-card bg-well px-4 py-3 text-[13px] leading-[21px]">
            <p className="mb-1 text-label-caps text-ink-3 uppercase">Goal</p>
            <div className="silo-reply">
              <Md text={sa.goal} />
            </div>
            {sa.context.trim() ? (
              <>
                <p className="mt-3 mb-1 text-label-caps text-ink-3 uppercase">Context</p>
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
        onInspectArtifact={onInspectArtifact}
        onSaveSkill={onSaveSkill}
        emptyState={<p className="my-auto py-14 text-center text-[13px] text-ink-3">Starting…</p>}
      />
    </div>
  );
}
