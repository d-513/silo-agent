import { useInfiniteQuery, useQuery } from "@connectrpc/connect-query";
import { useEffect, useRef, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import { fail } from "./errors";
import { inputClass, SkeletonRows } from "./Field";
import { fmtBytes, runWhen } from "./format";
import { UI, type AuditEvent, type TrafficRow } from "./gen/silo/v1/ui_pb";
import { reload, reloadBot } from "./query";
import { TabPill, TabPills } from "./TabPills";
import { t } from "./i18n.ts";

// Audit is one Bot's timeline, or — for an admin — every Bot's. It answers two
// questions: what happened that a person should look at, and which hosts each
// Bot talked to. The events come from the engine (calls, their decisions, tool
// results the output filter did not pass) and from the network gateway (what it
// blocked or flagged).
export function AuditPane({ botId, embedded = false }: { botId: string; embedded?: boolean }) {
  const [view, setView] = useState<"attention" | "all" | "traffic">("attention");
  const [text, setText] = useState("");
  const [query, setQuery] = useState("");

  const summaryQ = useQuery(UI.method.auditSummary, { botId }, { refetchInterval: 15000 });
  const summary = summaryQ.data;
  const unread = summary?.unread ?? 0;

  // A search is applied a moment after typing stops, so a word in progress
  // does not ask the server for every letter.
  useEffect(() => {
    const id = setTimeout(() => setQuery(text.trim()), 300);
    return () => clearTimeout(id);
  }, [text]);

  // Opening the tab clears the badge: the lines stay, they just stop counting.
  // One request at a time, so a refetch still in flight cannot ask twice; a
  // line that arrives while the tab is open is cleared on the next poll.
  const marking = useRef(false);
  useEffect(() => {
    if (!botId || unread === 0 || marking.current) return;
    marking.current = true;
    void ui
      .markAuditRead({ botId })
      .then(() => {
        reloadBot(botId);
        void reload(UI.method.auditSummary, { botId });
      })
      .finally(() => {
        marking.current = false;
      });
  }, [botId, unread]);

  return (
    <div className={embedded ? "" : "silo-page silo-page-wide"}>
      <div className="mb-3 flex flex-wrap items-center justify-between gap-3">
        <TabPills>
          <TabPill on={view === "attention"} onClick={() => setView("attention")}>
            {t("Needs attention")}
            {summary && summary.attention > 0 ? <span className="ml-1.5 tabular-nums text-vermilion">{summary.attention}</span> : null}
          </TabPill>
          <TabPill on={view === "all"} onClick={() => setView("all")}>
            {t("Everything")}
          </TabPill>
          <TabPill on={view === "traffic"} onClick={() => setView("traffic")}>
            {t("Traffic")}
          </TabPill>
        </TabPills>
        <div className="flex items-center gap-2">
          {botId ? (
            <Link to="/bots/$botId/rules" params={{ botId }} className={inputClass + " flex h-8 items-center px-3 text-[13px] text-ink-2 hover:text-ink"}>
              {t("Network lists")}
            </Link>
          ) : null}
          <input
            value={text}
            onChange={(e) => setText(e.target.value)}
            placeholder={t("Search the timeline")}
            className={`${inputClass} h-8 w-[14rem] px-3 text-[13px]`}
          />
        </div>
      </div>

      {summary ? <Summary summary={summary} /> : null}

      {view === "traffic" ? <Traffic botId={botId} query={query} /> : <Events botId={botId} view={view} query={query} />}
    </div>
  );
}

function Summary({ summary }: { summary: { total: number; attention: number; requests: number; blocked: number; flagged: number; detailDays: number; trafficDays: number } }) {
  const clicks = [{ label: t("Requests"), value: summary.requests }, { label: t("Blocked"), value: summary.blocked }, { label: t("Flagged"), value: summary.flagged }];
  return (
    <p className="mb-4 flex flex-wrap items-center gap-x-4 gap-y-1 text-[12.5px] text-ink-3">
      <span>{t("{{n}} lines kept for {{days}} days", { n: summary.total, days: summary.detailDays })}</span>
      {clicks.map((c) => (
        <span key={c.label} className="tabular-nums">
          {c.label}: <span className="text-ink-2">{c.value}</span>
        </span>
      ))}
      <span>{t("hosts summed over {{days}} days", { days: summary.trafficDays })}</span>
    </p>
  );
}

const noEvents: AuditEvent[] = [];
const noTraffic: TrafficRow[] = [];

function Events({ botId, view, query }: { botId: string; view: "attention" | "all"; query: string }) {
  const q = useInfiniteQuery(
    UI.method.listAuditEvents,
    { botId, view, query, cursor: "" },
    {
      pageParamKey: "cursor" as const,
      getNextPageParam: (last) => (last.more ? last.nextCursor : undefined),
      refetchInterval: 30000,
    },
  );
  const events = q.data?.pages.flatMap((p) => p.events) ?? noEvents;
  const err = q.error ? fail(q.error) : "";

  if (q.isPending) return <SkeletonRows rows={5} height={56} />;
  return (
    <>
      {err ? <p className="mb-3 text-[13px] text-vermilion">{err}</p> : null}
      {events.length === 0 && !err ? (
        <div className="rounded-card border border-dashed border-line bg-surface p-8 text-center text-ink-3">
          {view === "attention" ? t("Nothing needs your attention.") : t("Nothing on the timeline yet.")}
        </div>
      ) : (
        <ul className="overflow-hidden rounded-card bg-surface shadow-card">
          {events.map((e) => (
            <EventRow key={e.id} e={e} showBot={!botId} editable={!!botId} />
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

// What a line reads as: a person glancing down it should know who decided what,
// about which thing, and why — without opening anything.
function EventRow({ e, showBot, editable }: { e: AuditEvent; showBot: boolean; editable: boolean }) {
  const [err, setErr] = useState("");
  const host = e.host ? e.host + (e.port ? `:${e.port}` : "") : "";

  async function list(kind: "block" | "trust") {
    setErr("");
    try {
      await ui.addHostPattern({ botId: e.botId, kind, pattern: e.host });
      void reload(UI.method.getBotNetwork, { botId: e.botId });
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  return (
    <li className="group flex flex-wrap items-baseline gap-x-2.5 gap-y-1 border-b border-line px-3 py-2.5 last:border-b-0">
      <Verdict decision={e.decision} />
      {showBot ? (
        <Link to="/bots/$botId/run" params={{ botId: e.botId }} className="flex items-center gap-1.5 text-[13px] text-ink-2 hover:text-ink">
          <Crest index={e.crest} size={16} />
          {e.botName}
        </Link>
      ) : null}
      <span className="font-mono text-[12.5px] text-ink">{e.action}</span>
      <span className="text-[12.5px] text-ink-2">{who(e.actor)}</span>
      {host ? <span className="font-mono text-[12.5px] text-ink-2">{host}</span> : null}
      {e.chatId ? (
        <Link to="/bots/$botId/run/$chatId" params={{ botId: e.botId, chatId: e.chatId }} className="max-w-[16rem] truncate text-[12.5px] text-cobalt hover:underline">
          {e.chatTitle || t("the conversation")}
        </Link>
      ) : null}
      <span className="ml-auto shrink-0 text-[12px] text-ink-3" title={e.at}>
        {runWhen(e.at)}
      </span>
      <span className="w-full text-[12.5px] leading-[18px] text-ink-2">
        {why(e.reason)}
        {e.detail ? <span className="text-ink-3"> · {e.detail}</span> : null}
        {e.bytesUp || e.bytesDown ? (
          <span className="text-ink-3"> · {t("↑ {{up}} ↓ {{down}}", { up: fmtBytes(e.bytesUp), down: fmtBytes(e.bytesDown) })}</span>
        ) : null}
      </span>
      {editable && e.host && e.attention ? (
        <span className="flex items-center gap-1.5">
          <Btn size="sm" onClick={() => void list("block")}>
            {t("Block this host")}
          </Btn>
          <Btn size="sm" onClick={() => void list("trust")}>
            {t("Trust this host")}
          </Btn>
          {err ? <span className="text-[12.5px] text-vermilion">{err}</span> : null}
        </span>
      ) : null}
    </li>
  );
}

function Verdict({ decision }: { decision: string }) {
  const { label, cls } = verdictOf(decision);
  return <span className={`shrink-0 rounded-sm px-1.5 py-0.5 text-[11.5px] font-medium ${cls}`}>{label}</span>;
}

function verdictOf(d: string): { label: string; cls: string } {
  switch (d) {
    case "deny":
    case "auto_deny":
    case "block":
      return { label: t("Denied"), cls: "bg-vermilion-pale text-vermilion" };
    case "output_withhold":
      return { label: t("Withheld"), cls: "bg-vermilion-pale text-vermilion" };
    case "allow":
    case "auto_allow":
    case "allow_once":
      return { label: t("Allowed"), cls: "bg-emerald/10 text-emerald" };
    case "flag":
    case "output_flag":
      return { label: t("Flagged"), cls: "bg-cobalt-pale text-cobalt" };
    case "output_unchecked":
      return { label: t("Unchecked"), cls: "bg-well text-ink-2" };
    default:
      return { label: d || t("Unknown"), cls: "bg-well text-ink-2" };
  }
}

// who: the actor is a person's email, or one of the engine's own names.
function who(actor: string) {
  if (actor === "egress") return t("Network filter");
  if (actor === "filter") return t("Output filter");
  if (actor === "auto") return t("Auto-approval");
  if (actor === "worker") return t("The Bot");
  return actor;
}

// why: a short code from the engine, in words.
function why(reason: string) {
  const m: Record<string, string> = {
    threat_list: t("On a threat list"),
    private: t("A private address"),
    blocked_host: t("On the blocked list"),
    exfil_sink: t("Upload to a host that swallows data"),
    surrogate_leak: t("A stand-in secret sent in the clear"),
    secret_shape: t("Something shaped like a secret"),
    upload_budget: t("Over the upload budget"),
    beaconing: t("Calling out at a steady beat"),
  };
  return m[reason] ?? reason;
}

function Traffic({ botId, query }: { botId: string; query: string }) {
  const q = useQuery(UI.method.listTraffic, { botId, query }, { refetchInterval: 30000 });
  const rows = q.data?.rows ?? noTraffic;
  const err = q.error ? fail(q.error) : "";
  if (q.isPending) return <SkeletonRows rows={5} height={40} />;
  if (err) return <p className="text-[13px] text-vermilion">{err}</p>;
  if (rows.length === 0)
    return (
      <div className="rounded-card border border-dashed border-line bg-surface p-8 text-center text-ink-3">
        {t("No traffic recorded yet.")}
      </div>
    );
  return (
    <div className="silo-scroll-x overflow-hidden rounded-card bg-surface shadow-card">
      <table className="w-full min-w-[36rem] text-left text-[13px]">
        <thead className="bg-well text-ink-3">
          <tr>
            {!botId ? <th className="p-2 font-medium">{t("Bot")}</th> : null}
            <th className="p-2 font-medium">{t("Host")}</th>
            <th className="p-2 text-right font-medium">{t("Requests")}</th>
            <th className="p-2 text-right font-medium">{t("Blocked")}</th>
            <th className="p-2 text-right font-medium">{t("Flagged")}</th>
            <th className="p-2 text-right font-medium">{t("Sent")}</th>
            <th className="p-2 text-right font-medium">{t("Received")}</th>
            <th className="p-2 font-medium">{t("Last seen")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.botId + "\u0000" + r.host} className="border-t border-line">
              {!botId ? (
                <td className="p-2">
                  <Link to="/bots/$botId/run" params={{ botId: r.botId }} className="text-ink-2 hover:text-ink">
                    {r.botName}
                  </Link>
                </td>
              ) : null}
              <td className="p-2 font-mono">{r.host || t("its own machine")}</td>
              <td className="p-2 text-right tabular-nums">{r.requests}</td>
              <td className="p-2 text-right tabular-nums">{r.blocked ? <span className="text-vermilion">{r.blocked}</span> : 0}</td>
              <td className="p-2 text-right tabular-nums">{r.flagged ? <span className="text-cobalt">{r.flagged}</span> : 0}</td>
              <td className="p-2 text-right tabular-nums">{fmtBytes(r.bytesUp)}</td>
              <td className="p-2 text-right tabular-nums">{fmtBytes(r.bytesDown)}</td>
              <td className="p-2 text-ink-3" title={r.lastAt}>
                {runWhen(r.lastAt)}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
