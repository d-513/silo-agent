import { ArrowDown } from "lucide-react";
import type { ReactNode } from "react";
import type { Artifact } from "./Artifact";
import type { AgentLink } from "./links";
import { foldEvents, type Attachment, type Block, type Ev } from "./fold";
import { EmptyThread } from "./thread/EmptyThread";
import { isBotScratch } from "./thread/Present";
import { renderBlock, type BlockCtx } from "./thread/renderBlock";
import { useConnectorMarks } from "./thread/toolIcons";
import { asStr, parseToolArgs } from "./thread/toolInfo";
import { useStickyScroll } from "./thread/useStickyScroll";

// One-line rows (as opposed to bubbles, replies and cards).
function isActivity(b: Block) {
  if (b.type === "thinking" || b.type === "receipt") return true;
  if (b.type !== "tool") return false;
  const path = asStr(parseToolArgs(b.args).path);
  const card = b.name === "present" && !b.running && !!b.result && !b.result.startsWith("error:") && !!path && !isBotScratch(path);
  return !card;
}

function gapAfter(prev: boolean | undefined, row: boolean) {
  if (prev === undefined) return "";
  return prev && row ? "mt-0.5" : "mt-4";
}

export function Thread({
  botId,
  botName,
  botCrest,
  chatId,
  events,
  sending,
  fresh,
  onInspectArtifact,
  onSaveSkill,
  onSelectPrompt,
  onEditMessage,
  onDeleteMessage,
  onDivergeChat,
  emptyState,
  userAs = "bubble",
  agentHref,
}: {
  botId: string;
  botName?: string;
  botCrest?: number;
  chatId?: string;
  events: Ev[];
  sending: boolean;
  // User message ids sent from this tab this session; they glide in.
  fresh?: ReadonlySet<string>;
  onInspectArtifact?: (a: Artifact) => void;
  onSaveSkill?: (a: Artifact) => void;
  onSelectPrompt?: (prompt: string) => void;
  onEditMessage?: (eventId: string, text: string, attachments?: Attachment[]) => void;
  onDeleteMessage?: (eventId: string) => void;
  onDivergeChat?: (eventId: string) => void;
  // Replaces the "Ready for your prompt" empty state.
  emptyState?: ReactNode;
  // "run" draws each user message as a run divider (automation logs, where the
  // message is the automation's own prompt, not something a human typed).
  userAs?: "bubble" | "run";
  // Links a subagent named in a wake report to its page.
  agentHref?: (name: string) => AgentLink | undefined;
}) {
  const marks = useConnectorMarks(botId);
  const blocks = foldEvents(events);
  const scroll = useStickyScroll({ chatId, events, sending, fresh, blocks });
  let lastUser: Extract<Block, { type: "user" }> | undefined;
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i];
    if (b.type === "user") {
      lastUser = b;
      break;
    }
  }
  let lastAssistantKey: string | undefined;
  for (let i = blocks.length - 1; i >= 0 && blocks[i].type !== "user"; i--) {
    if (blocks[i].type === "assistant") {
      lastAssistantKey = blocks[i].key;
      break;
    }
  }

  const working =
    sending &&
    !blocks.some(
      (b) => (b.type === "assistant" && b.streaming) || (b.type === "thinking" && b.streaming) || (b.type === "tool" && b.running),
    );

  const retry =
    onEditMessage && lastUser?.id && !sending
      ? () => onEditMessage(lastUser!.id!, lastUser!.text, lastUser!.attachments)
      : undefined;

  const ctx: BlockCtx = {
    botId,
    blocks,
    sending,
    fresh,
    userAs,
    marks,
    lastUserKey: lastUser?.key,
    lastAssistantKey,
    retry,
    agentHref,
    onInspectArtifact,
    onSaveSkill,
    onEditMessage,
    onDeleteMessage,
    onDivergeChat,
  };

  // Consecutive thinking / tool / receipt rows stack tight; everything else
  // keeps the thread's breathing room.
  let prevRow: boolean | undefined;
  const rows = blocks.map((b) => {
    const node = renderBlock(b, ctx);
    if (!node) return null;
    const row = isActivity(b);
    const gap = gapAfter(prevRow, row);
    prevRow = row;
    return (
      <div key={b.key} className={`min-w-0 ${gap} ${b.type === "user" ? "" : scroll.rises(b.key)}`}>
        {node}
      </div>
    );
  });

  return (
    <div
      ref={scroll.containerRef}
      onScroll={scroll.onScroll}
      onWheel={scroll.onWheel}
      className="relative min-w-0 flex-1 overflow-x-hidden overflow-y-auto px-4 py-6"
    >
      <div ref={scroll.innerRef} className="mx-auto flex max-w-[720px] flex-col">
        {blocks.length === 0 && !sending && emptyState}
        {blocks.length === 0 && !sending && !emptyState && <EmptyThread botName={botName} botCrest={botCrest} onSelectPrompt={onSelectPrompt} />}
        {rows}
        {working ? (
          <div className={`rise flex h-8 items-center px-3 text-[13px] font-medium ${gapAfter(prevRow, true)}`}>
            <span className="shimmer-text">Working…</span>
          </div>
        ) : null}
      </div>

      {scroll.showScrollBottom && (
        <div className="pointer-events-none sticky bottom-0 z-20 mx-auto flex max-w-[720px] justify-end">
          <button
            type="button"
            onClick={() => scroll.scrollToBottom(true)}
            className="rise pointer-events-auto flex h-8 items-center gap-1.5 rounded-control bg-surface px-3 text-[12.5px] font-medium text-ink shadow-float transition-[background-color,transform] duration-[160ms] ease-quiet hover:bg-well active:scale-[.97] active:duration-[70ms]"
          >
            <ArrowDown size={13} />
            <span>Latest</span>
          </button>
        </div>
      )}
    </div>
  );
}
