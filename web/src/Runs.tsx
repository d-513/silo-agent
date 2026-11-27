import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { ArrowUpRight, ChevronRight, ListTree } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Btn } from "./Btn";
import { Count } from "./Count";
import { fail } from "./errors";
import { ErrorWell, SkeletonRows } from "./Field";
import { FilterChip } from "./FilterChip";
import { feedStamp, fullTime } from "./format";
import { UI, type Bot, type ModelCall, type RunCount, type RunRow } from "./gen/silo/v1/ui_pb";
import { t } from "./i18n.ts";
import { Lamp } from "./Lamp";
import { agentLink, chatLink } from "./links";
import { callWord, KINDS, kindsOf, kindWord, spent, tokens, took, triggerWord } from "./runRows";
import { sourceLabel } from "./sources";
import { chatDoing, ranLook, runLook, stillRunning } from "./statusLook";
import { TabPill, TabPills } from "./TabPills";

const noRuns: RunRow[] = [];
const dot = <span aria-hidden>·</span>;

// RunsPane is everything a Bot did, for its managers: every run, live ones
// first, each in numbers and opening into the model calls made beside its
// turns; and, under Background, the model calls made for no run at all, which
// the server keeps in memory only.
export function RunsPane({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [view, setView] = useState<"runs" | "background">("runs");
  useEffect(() => setView("runs"), [bot.id]);
  return (
    <div className="silo-page">
      <div className="mb-1 flex items-center gap-2.5">
        <h2 className="text-title text-ink">{t("Runs")}</h2>
      </div>
      <p className="mb-4 text-ink-2">
        {t("Everything {{name}} did: each time it worked, what started it, what it used and what it cost. Only its managers see this.", { name: bot.name })}
      </p>
      <div className="mb-5 flex">
        <TabPills>
          <TabPill on={view === "runs"} onClick={() => setView("runs")}>
            {t("Runs")}
          </TabPill>
          <TabPill on={view === "background"} onClick={() => setView("background")}>
            {t("Background")}
          </TabPill>
        </TabPills>
      </div>
      {view === "runs" ? <RunList bot={bot} onError={onError} /> : <Background botId={bot.id} onError={onError} />}
    </div>
  );
}

function RunList({ bot, onError }: { bot: Bot; onError: (s: string) => void }) {
  const [kind, setKind] = useState("");
  const [open, setOpen] = useState("");
  useEffect(() => {
    setKind("");
    setOpen("");
  }, [bot.id]);
  const q = useInfiniteQuery(
    UI.method.listRuns,
    { botId: bot.id, kinds: kind ? [kind] : [], before: "" },
    { pageParamKey: "before" as const, getNextPageParam: (last) => last.next || undefined, refetchInterval: 5000 },
  );
  // The chips are the kinds this Bot has runs of, read off the unfiltered list.
  const allQ = useQuery(UI.method.listRuns, { botId: bot.id, kinds: [], before: "" }, { refetchInterval: 30000 });
  const kinds = kindsOf(allQ.data?.runs ?? noRuns);
  const runs = q.data?.pages.flatMap((p) => p.runs) ?? noRuns;
  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);

  if (q.isPending) return <SkeletonRows rows={5} height={64} />;
  return (
    <>
      {kinds.length > 1 ? (
        <div className="mb-4 flex flex-wrap items-center gap-1.5">
          <FilterChip on={kind === ""} onClick={() => setKind("")}>
            {t("All")}
          </FilterChip>
          {KINDS.filter((k) => kinds.includes(k.key)).map((k) => (
            <FilterChip key={k.key} on={kind === k.key} onClick={() => setKind(k.key)}>
              {t(k.label)}
            </FilterChip>
          ))}
        </div>
      ) : null}
      {runs.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-5 py-8 text-center">
          <ListTree size={20} className="text-ink-3" />
          <p className="text-ink-2">{t("No runs yet")}</p>
          <p className="max-w-sm text-meta text-ink-3">{t("A run is listed here each time {{name}} works: a reply, an automation, a subagent, its housekeeping.", { name: bot.name })}</p>
        </div>
      ) : (
        <ul className="overflow-hidden rounded-card bg-surface shadow-card">
          {runs.map((r, i) => (
            <Row key={r.id} bot={bot} r={r} first={i === 0} open={open === r.id} onToggle={() => setOpen((cur) => (cur === r.id ? "" : r.id))} />
          ))}
        </ul>
      )}
      {q.hasNextPage ? (
        <div className="mt-3 flex justify-center">
          <Btn onClick={() => void q.fetchNextPage()} disabled={q.isFetchingNextPage}>
            {q.isFetchingNextPage ? t("Loading…") : t("Load older")}
          </Btn>
        </div>
      ) : null}
    </>
  );
}

// where is the conversation a run ran in, as a link to it.
function where(botId: string, r: RunRow) {
  if (r.subagentId && r.leadChatId) return agentLink(botId, r.leadChatId, r.subagentId);
  if (r.automationId) return { to: "/bots/$botId/automations/$automationId", params: { botId, automationId: r.automationId } } as const;
  return r.chatId ? chatLink(botId, r.chatId) : null;
}

function Row({ bot, r, first, open, onToggle }: { bot: Bot; r: RunRow; first: boolean; open: boolean; onToggle: () => void }) {
  const live = stillRunning(r.status);
  // A live run says what it is on, in the words the rest of the page uses.
  const doing = live ? chatDoing(bot, r.chatId) : undefined;
  const look = doing ? runLook(doing) : ranLook(r.status);
  const { read, wrote } = spent(r.summary);
  const trigger = triggerWord(r.trigger);
  const place = r.sourceKind === "subagent" ? r.sourceName : sourceLabel(r.sourceKind, r.sourceName);
  // A run from before titles were kept is headed by where it ran.
  const kind = t(kindWord(r.kind, r.job));
  const heading = r.title || place || kind;
  const started = new Date(r.createdAt);
  const tools = (r.summary?.tools ?? []).reduce((n, c) => n + c.count, 0) + (r.summary?.calls ?? []).reduce((n, c) => n + c.count, 0);
  return (
    <li className={first ? "" : "shadow-[inset_0_1px_0_var(--color-line)]"}>
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className="flex w-full items-start gap-3 px-4 py-3 text-left transition-[background-color] duration-[160ms] ease-quiet hover:bg-well outline-offset-[-2px]"
      >
        <ChevronRight size={14} className={`mt-[3px] shrink-0 text-ink-3 transition-transform duration-200 ease-quiet ${open ? "rotate-90" : ""}`} />
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-center gap-2">
            <span className="truncate text-row font-medium text-ink">{heading}</span>
            <span className="ml-auto shrink-0 font-mono text-label text-ink-3" title={Number.isNaN(started.getTime()) ? undefined : fullTime(started)}>
              {feedStamp(r.createdAt)}
            </span>
          </span>
          <span className="mt-0.5 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-meta text-ink-3">
            <span className={`flex items-center gap-1.5 ${look.tone}`}>
              <Lamp status={look.lamp} />
              <span className="truncate">{look.word}</span>
            </span>
            {heading !== kind ? (
              <>
                {dot}
                <span className="text-ink-2">{kind}</span>
              </>
            ) : null}
            {place && place !== heading ? (
              <>
                {dot}
                <span className="truncate">{place}</span>
              </>
            ) : null}
            {trigger ? (
              <>
                {dot}
                <span>{t(trigger)}</span>
              </>
            ) : null}
            {r.person && bot.people.length > 1 ? (
              <>
                {dot}
                <span>{r.person}</span>
              </>
            ) : null}
            {r.durationMs > 0n ? (
              <>
                {dot}
                <span className="tabular-nums">{took(Number(r.durationMs))}</span>
              </>
            ) : null}
            {read + wrote > 0 ? (
              <>
                {dot}
                <span className="tabular-nums" title={t("Tokens read and written, calls beside the turns included")}>
                  {t("{{read}} in, {{wrote}} out", { read: tokens(read), wrote: tokens(wrote) })}
                </span>
              </>
            ) : null}
            {tools > 0 ? (
              <>
                {dot}
                <span className="tabular-nums">{t("{{count}} tool calls", { count: tools })}</span>
              </>
            ) : null}
          </span>
        </span>
      </button>
      {open ? <Detail botId={bot.id} r={r} /> : null}
    </li>
  );
}

function Fact({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex min-w-0 gap-3 py-1">
      <dt className="w-32 shrink-0 text-meta text-ink-3">{label}</dt>
      <dd className="min-w-0 flex-1 text-meta break-words text-ink">{children}</dd>
    </div>
  );
}

// Named counts as a line: `exec_python ×4, read ×2`.
function Counts({ of }: { of: RunCount[] }) {
  return (
    <span className="font-mono text-label">
      {of.map((c, i) => (
        <span key={c.name}>
          {i ? ", " : ""}
          {c.name}
          {c.count > 1 ? <span className="text-ink-3"> ×{c.count}</span> : null}
        </span>
      ))}
    </span>
  );
}

// Detail is a run opened: its numbers, the model calls made for it beside its
// turns, the approvals it raised, and the way to its conversation.
function Detail({ botId, r }: { botId: string; r: RunRow }) {
  const q = useQuery(UI.method.getRunCalls, { botId, runId: r.id }, { refetchInterval: stillRunning(r.status) ? 5000 : false });
  const s = r.summary;
  const to = where(botId, r);
  const calls = q.data?.calls ?? [];
  const approvals = q.data?.approvals ?? [];
  return (
    <div className="rise px-4 pb-4 pl-[38px]">
      <dl className="rounded-control bg-well px-3 py-2">
        {r.model ? (
          <Fact label={t("Model")}>
            <span className="font-mono text-label">{r.model}</span>
          </Fact>
        ) : null}
        {s && s.turns > 0 ? <Fact label={t("Model turns")}>{s.turns}</Fact> : null}
        {s && s.input + s.output > 0n ? (
          <Fact label={t("Tokens")}>
            <span className="tabular-nums">
              {t("{{read}} in, {{wrote}} out", { read: tokens(Number(s.input)), wrote: tokens(Number(s.output)) })}
              {s.cacheRead > 0n ? <span className="text-ink-3"> · {t("{{n}} read from cache", { n: tokens(Number(s.cacheRead)) })}</span> : null}
            </span>
          </Fact>
        ) : null}
        {s && s.tools.length ? (
          <Fact label={t("Tools")}>
            <Counts of={s.tools} />
          </Fact>
        ) : null}
        {s && s.calls.length ? (
          <Fact label={t("Called from code")}>
            <Counts of={s.calls} />
          </Fact>
        ) : null}
        {s && s.toolErrors > 0 ? <Fact label={t("Tool errors")}>{s.toolErrors}</Fact> : null}
        {s && s.filtered.length ? (
          <Fact label={t("Results filtered")}>
            <Counts of={s.filtered} />
          </Fact>
        ) : null}
        {s && s.compactions + s.cleared > 0 ? (
          <Fact label={t("Context")}>
            {[s.compactions ? t("compacted {{count}} times", { count: s.compactions }) : "", s.cleared ? t("old results cleared {{count}} times", { count: s.cleared }) : ""].filter(Boolean).join(" · ")}
          </Fact>
        ) : null}
        {s && s.lastError ? (
          <Fact label={s.errors > 1 ? t("Last of {{count}} errors", { count: s.errors }) : t("Error")}>
            <span className="text-vermilion">{s.lastError}</span>
          </Fact>
        ) : null}
        {!s && !r.model ? <p className="py-1 text-meta text-ink-3">{t("Still at work: its numbers are added up when it ends.")}</p> : null}
      </dl>

      {q.error ? <ErrorWell className="mt-2">{fail(q.error)}</ErrorWell> : null}
      {approvals.length ? (
        <>
          <h3 className="mt-3 mb-1 text-section text-ink-3">{t("Approvals")}</h3>
          <ul>
            {approvals.map((a, i) => (
              <li key={i} className="flex min-w-0 items-center gap-2 py-0.5 text-meta">
                <span className="min-w-0 flex-1 truncate text-ink">{a.title || a.action}</span>
                {a.auto ? <span className="shrink-0 text-ink-3">{t("the gate did not decide")}</span> : null}
                <span className="shrink-0 font-mono text-label text-ink-2">{a.status}</span>
              </li>
            ))}
          </ul>
        </>
      ) : null}
      {calls.length ? (
        <>
          <h3 className="mt-3 mb-1 flex items-center gap-2 text-section text-ink-3">
            {t("Model calls beside its turns")}
            <Count n={calls.length} />
          </h3>
          <ul>
            {calls.map((c, i) => (
              <CallLine key={i} c={c} />
            ))}
          </ul>
        </>
      ) : null}
      {to ? (
        <Link {...to} className="mt-3 inline-flex items-center gap-1 text-meta font-medium text-cobalt hover:underline">
          {r.automationId ? t("Open the automation's log") : r.subagentId ? t("Open the subagent's log") : t("Open the conversation")}
          <ArrowUpRight size={13} />
        </Link>
      ) : null}
    </div>
  );
}

// One model call: what it was for, on which model, what came of it.
function CallLine({ c, stamp = false }: { c: ModelCall; stamp?: boolean }) {
  const cost = Number(c.input + c.output);
  return (
    <li className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5 py-0.5 text-meta text-ink-3">
      <span className="text-ink">{t(callWord(c.label))}</span>
      {c.count > 1 ? <span className="tabular-nums">×{c.count}</span> : null}
      {c.verdict ? <span className="font-mono text-label text-ink-2">{c.verdict}</span> : null}
      {c.error ? <span className="min-w-0 truncate text-vermilion">{c.error}</span> : null}
      <span className="ml-auto flex shrink-0 items-center gap-2 font-mono text-label tabular-nums">
        <span className="max-w-[220px] truncate">{c.model}</span>
        {cost > 0 ? <span>{tokens(cost)}</span> : null}
        {c.durationMs > 0n ? <span>{took(Number(c.durationMs))}</span> : null}
        {stamp ? <span>{feedStamp(c.at)}</span> : null}
      </span>
    </li>
  );
}

// Background is the model calls made for no run: a chat's title, the memory
// digest, the talker on a call, embeddings, speech. The server keeps them in
// memory, so the list starts over when it does.
function Background({ botId, onError }: { botId: string; onError: (s: string) => void }) {
  const q = useQuery(UI.method.listBackground, { botId }, { refetchInterval: 5000 });
  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);
  if (q.isPending) return <SkeletonRows rows={4} height={32} />;
  const calls = q.data?.calls ?? [];
  const since = q.data?.since ? new Date(q.data.since) : null;
  return (
    <>
      <p className="mb-3 text-meta leading-[18px] text-ink-3">
        {t("Model calls made outside any run: embeddings, speech, a call's talker, the memory digest. They change nothing a run would have to explain, so they are kept in memory only.")}
        {since && !Number.isNaN(since.getTime()) ? ` ${t("Listed since the server started, {{when}}.", { when: fullTime(since) })}` : ""}
      </p>
      {calls.length === 0 ? (
        <div className="rounded-card bg-well px-5 py-8 text-center text-ink-2">{t("Nothing in the background yet.")}</div>
      ) : (
        <ul className="rounded-card bg-surface px-4 py-2 shadow-card">
          {calls.map((c, i) => (
            <CallLine key={i} c={c} stamp />
          ))}
        </ul>
      )}
    </>
  );
}
