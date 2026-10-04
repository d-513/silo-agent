import type { ReactNode } from "react";
import { ArtifactCard, downloadArtifact, type Artifact } from "../Artifact";
import type { Attachment, Block } from "../fold";
import { Compaction, Receipt, Thinking } from "./ActivityRows";
import { FeedQuote, LeadNote, Reply, RunMark, SubagentReport, UserBubble } from "./Bubbles";
import { rowState, rowVerb } from "./FoldRow";
import { isBotScratch, PresentFile, QuietPreview } from "./Present";
import type { ConnectorMarks } from "./toolIcons";
import { asStr, parseToolArgs, toolAction, toolMeta } from "./toolInfo";
import { ToolInput, ToolResult, ToolRow } from "./ToolRow";

// Everything a block needs to know about the thread it sits in.
export type BlockCtx = {
  botId: string;
  blocks: Block[];
  sending: boolean;
  fresh?: ReadonlySet<string>;
  userAs: "bubble" | "run";
  marks: ConnectorMarks;
  lastUserKey?: string;
  lastAssistantKey?: string;
  // Re-sends the last user message; offered on the last reply only.
  retry?: () => void;
  agentHref?: (name: string) => string | undefined;
  onInspectArtifact?: (a: Artifact) => void;
  onSaveSkill?: (a: Artifact) => void;
  onEditMessage?: (eventId: string, text: string, attachments?: Attachment[]) => void;
  onDeleteMessage?: (eventId: string) => void;
  onDivergeChat?: (eventId: string) => void;
};

type ToolBlock = Extract<Block, { type: "tool" }>;

// One tool call: a Files/Desktop row, a presented file card, or a Python row
// with the connector calls it made stacked above it.
function renderTool(b: ToolBlock, c: BlockCtx): ReactNode {
  const { blocks, sending, botId, marks } = c;
  if (b.name === "artifact" && blocks.some((x) => x.type === "artifact" && (!x.runId || !b.runId || x.runId === b.runId))) {
    return null;
  }
  const state = rowState(b);
  const live = sending && !!b.running;
  const path = asStr(parseToolArgs(b.args).path);
  const failed = !b.running && b.result?.startsWith("error:");
  if (b.name === "look" || (b.name === "present" && path && isBotScratch(path))) {
    const what = b.name === "look" ? "screen" : path.split("/").filter(Boolean).pop() || path;
    return (
      <ToolRow state={state} icon={b.name} verb={rowVerb(state, b.outcome, "Looking at", "Looked at")} app={what} live={live}>
        {!b.running && b.result && !failed ? (
          <QuietPreview botId={botId} path={b.name === "look" ? "bot/screen.jpg" : path} />
        ) : b.result ? (
          <ToolResult text={b.result} live={live} />
        ) : null}
      </ToolRow>
    );
  }
  if (b.name === "present" && !b.running && b.result && !failed && path) {
    return <PresentFile botId={botId} path={path} />;
  }
  const { app } = toolMeta(b.name);
  const body =
    b.args || b.result ? (
      <>
        <ToolInput name={b.name} args={b.args} running={b.running} />
        {b.result ? <ToolResult text={b.result} live={live} /> : null}
      </>
    ) : undefined;
  const python =
    b.name === "call" ? null : (
      <ToolRow state={state} icon={b.name} verb={rowVerb(state, b.outcome, "Using", "Used")} app={app} action={toolAction(b.name, b.args)} live={live}>
        {body}
      </ToolRow>
    );
  if (!b.calls?.length) return python;
  // Connector calls made from Python sit above that Python row.
  return (
    <div className="space-y-0.5">
      {b.calls.map((call) => {
        const cs = rowState(call);
        return (
          <ToolRow
            key={call.key}
            state={cs}
            icon={call.name || "call"}
            marks={marks}
            verb={rowVerb(cs, call.outcome, "Using", "Used")}
            app={call.title}
            action={call.name && call.name !== call.title ? call.name : undefined}
            live={sending && !!call.running}
          >
            {call.result ? <ToolResult text={call.result} live={sending && !!call.running} /> : undefined}
          </ToolRow>
        );
      })}
      {python}
    </div>
  );
}

// renderBlock turns one folded block into its row, bubble or card.
export function renderBlock(b: Block, c: BlockCtx): ReactNode {
  if (b.type === "user" && c.userAs === "run") {
    return <RunMark prompt={b.text} createdAt={b.createdAt} />;
  }
  if (b.type === "user" && b.from === "lead") {
    return <LeadNote text={b.text} createdAt={b.createdAt} />;
  }
  if (b.type === "user") {
    const id = b.id;
    const { onEditMessage, onDeleteMessage, onDivergeChat } = c;
    return (
      <UserBubble
        text={b.text}
        attachments={b.attachments}
        isLast={b.key === c.lastUserKey}
        busy={c.sending}
        fresh={!!id && !!c.fresh?.has(id)}
        onEdit={onEditMessage && id ? (t, a) => onEditMessage(id, t, a) : undefined}
        onDelete={onDeleteMessage && id ? () => onDeleteMessage(id) : undefined}
        onDiverge={onDivergeChat && id ? () => onDivergeChat(id) : undefined}
      />
    );
  }
  if (b.type === "thinking") {
    return <Thinking text={b.text} streaming={!!b.streaming && c.sending} ms={b.ms} />;
  }
  if (b.type === "receipt") return <Receipt b={b} />;
  if (b.type === "report") return <SubagentReport text={b.text} agents={b.agents} href={c.agentHref} />;
  if (b.type === "quote") return <FeedQuote text={b.text} source={b.source} createdAt={b.createdAt} />;
  if (b.type === "compaction") return <Compaction text={b.text} reason={b.reason} running={!!b.running && c.sending} />;
  if (b.type === "tool") return renderTool(b, c);
  if (b.type === "artifact") {
    const a: Artifact = {
      type: b.artifactType === "file" ? "file" : "skill",
      name: b.name,
      title: b.title,
      path: b.path,
      scope: b.scope,
      approvalId: b.approvalId,
      status: b.status,
      size: b.size,
      runId: b.runId,
    };
    return (
      <ArtifactCard
        artifact={a}
        onOpen={() => c.onInspectArtifact?.(a)}
        onSave={a.status === "pending" ? () => c.onSaveSkill?.(a) : undefined}
        onDownload={() => void downloadArtifact(c.botId, a)}
      />
    );
  }
  if (b.type === "assistant") {
    return <Reply text={b.text} bounds={b.bounds} streaming={b.streaming} onRetry={b.key === c.lastAssistantKey ? c.retry : undefined} />;
  }
  return (
    <div role="alert" className="flex items-start gap-2 rounded-control bg-vermilion-pale px-3 py-2.5 text-[13px] text-vermilion">
      <span className="mt-[7px] h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
      <span className="min-w-0 break-words">{b.text}</span>
    </div>
  );
}
