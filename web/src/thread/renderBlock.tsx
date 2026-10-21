import type { AgentLink } from "../links";
import type { ReactNode } from "react";
import { ArtifactCard, ArtifactUpdateRow, type Artifact } from "../Artifact";
import type { Attachment, Block, Quote } from "../fold";
import { Boundary, Compaction, InterviewMark, Receipt, Thinking } from "./ActivityRows";
import { LeadNote, Opened, Reply, RunMark, SubagentReport, UserBubble } from "./Bubbles";
import { rowState, rowVerb } from "./FoldRow";
import { isBotScratch, PresentFile, QuietPreview } from "./Present";
import type { ConnectorMarks } from "./toolIcons";
import { asStr, parseToolArgs, toolAction, toolMeta } from "./toolInfo";
import { OfferGroup } from "../setup/OfferCard";
import { ThreadCard } from "./ThreadCard";
import { ToolInput, ToolResult, ToolRow } from "./ToolRow";
import { t } from "../i18n.ts";

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
  agentHref?: (name: string) => AgentLink | undefined;
  onInspectArtifact?: (a: Artifact) => void;
  onSaveSkill?: (a: Artifact) => void;
  onEditMessage?: (eventId: string, text: string, attachments?: Attachment[], quotes?: Quote[]) => void;
  onDeleteMessage?: (eventId: string) => void;
  onDivergeChat?: (eventId: string) => void;
};

type ToolBlock = Extract<Block, { type: "tool" }>;

// One tool call: a Files/Desktop row, a presented file card, or a Python row
// with the connector calls it made stacked above it.
function renderTool(b: ToolBlock, c: BlockCtx): ReactNode {
  const { blocks, sending, botId, marks } = c;
  const state = rowState(b);
  const live = sending && !!b.running;
  const path = asStr(parseToolArgs(b.args).path);
  const failed = !b.running && b.result?.startsWith("error:");
  // An offer that was made is its card, too.
  if (b.name === "offer_setup" && !b.running && b.result && !failed) return null;
  // An artifact call that worked is its card: the row would only repeat it.
  if (b.name === "artifact" && !failed && path && cardFor(blocks, path, b.runId)) return null;
  if (b.name === "look" || (b.name === "present" && path && isBotScratch(path))) {
    const what = b.name === "look" ? t("screen") : path.split("/").filter(Boolean).pop() || path;
    return (
      <ToolRow state={state} icon={b.name} verb={rowVerb(state, b.outcome, t("Looking at"), t("Looked at"))} app={what} live={live}>
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
      <ToolRow state={state} icon={b.name} verb={rowVerb(state, b.outcome, t("Using"), t("Used", { context: "tool" }))} app={app} action={toolAction(b.name, b.args)} live={live} filter={b.filter}>
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
            verb={rowVerb(cs, call.outcome, t("Using"), t("Used", { context: "tool" }))}
            app={call.title}
            action={call.name && call.name !== call.title ? call.name : undefined}
            live={sending && !!call.running}
            filter={call.filter}
          >
            {call.result ? <ToolResult text={call.result} live={sending && !!call.running} /> : undefined}
          </ToolRow>
        );
      })}
      {python}
    </div>
  );
}

const cleanPath = (p: string) => p.replace(/^\/+/, "").replace(/^(workspace\/)+/, "").replace(/\/+$/, "");

// cardFor is the artifact card (or update row) a run made for path.
function cardFor(blocks: Block[], path: string, runId?: string) {
  const want = cleanPath(path);
  return blocks.some(
    (x) =>
      (x.type === "artifact" && cleanPath(x.path) === want && x.versions.some((v) => !runId || !v.runId || v.runId === runId)) ||
      (x.type === "artifact_update" && (!runId || !x.runId || x.runId === runId) && blocks.some((y) => y.type === "artifact" && y.artifactId === x.artifactId && cleanPath(y.path) === want)),
  );
}

// renderBlock turns one folded block into its row, bubble or card.
export function renderBlock(b: Block, c: BlockCtx): ReactNode {
  if (b.type === "user" && c.userAs === "run") {
    return <RunMark prompt={b.text} createdAt={b.createdAt} />;
  }
  if (b.type === "user" && b.from === "onboarding") return <InterviewMark />;
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
        quotes={b.quotes}
        isLast={b.key === c.lastUserKey}
        busy={c.sending}
        fresh={!!id && !!c.fresh?.has(id)}
        onEdit={onEditMessage && id ? (t, a, q) => onEditMessage(id, t, a, q) : undefined}
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
  if (b.type === "quote") return <Opened text={b.text} source={b.source} quoted={b.quoted} createdAt={b.createdAt} />;
  if (b.type === "card") return <ThreadCard botId={c.botId} card={b.card} />;
  if (b.type === "offers") return <OfferGroup botId={c.botId} offers={b.offers} />;
  if (b.type === "boundary") return <Boundary kind={b.kind} count={b.count} />;
  if (b.type === "compaction") return <Compaction text={b.text} reason={b.reason} running={!!b.running && c.sending} />;
  if (b.type === "tool") return renderTool(b, c);
  if (b.type === "artifact") {
    const newestApp = [...c.blocks].reverse().find((x) => x.type === "artifact" && x.artifactType === "app");
    return (
      <ArtifactCard
        botId={c.botId}
        block={b}
        autoRun={newestApp?.key === b.key}
        onOpen={(a) => c.onInspectArtifact?.(a)}
        onSave={c.onSaveSkill ? (a) => c.onSaveSkill?.(a) : undefined}
      />
    );
  }
  if (b.type === "artifact_update") {
    return <ArtifactUpdateRow title={b.title} version={b.version} />;
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
