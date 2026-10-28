import { Check, Move, RotateCw } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { type DragEvent, useEffect, useMemo, useState } from "react";
import { Link } from "@tanstack/react-router";
import { ApprovalCard, approveSetAuto } from "../Approval";
import { ui } from "../api";
import { AskSlip } from "../ask/AskCard";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { nextWhen, runWhen } from "../format";
import { UI, type Approval, type Bot, type Chat, type DashboardCard, type DashboardWidget, type FeedPost } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { patch, recheck, reload, reloadBot } from "../query";
import { Tip } from "../Tip";
import { ArrangeTools } from "./ArrangeTools";
import { AskBar } from "./AskBar";
import { StarterIdeas } from "./Ideas";
import { SinceLooked } from "./SinceLooked";
import { suggestions } from "./suggestions";
import { useColumns } from "./useColumns";
import { useWidgetHandlers } from "./useWidgetHandlers";
import { Widget } from "./Widget";
import { reorder } from "./widgets";

// The Dashboard is the owner's board for one Bot, top to bottom: one meta line
// (when the Bot last looked, when it looks next, Look again, Arrange), what
// needs the owner (approvals on the vermilion ribbon, then the Bot's asks),
// the Feed's new posts, then the widgets, and an ask bar pinned to the foot
// for changing any of it in words. Nothing here explains itself: an empty
// board is the ask bar with a few suggestions.
export function DashboardPane({ bot, onError, onQuoted }: { bot: Bot; onError: (s: string) => void; onQuoted: (c: Chat) => void }) {
  const input = { botId: bot.id };
  const q = useQuery(UI.method.listDashboard, input, {
    refetchInterval: (x) => (x.state.data?.curating || x.state.data?.widgets.some((w) => w.busy) ? 4000 : 30000),
  });
  const approvalsQ = useQuery(UI.method.listApprovals, input);
  const data = q.data;
  const approvals = approvalsQ.data?.approvals ?? [];

  // What this visit read: posts and changed widgets stay marked until the
  // owner leaves, though the server counts them read at once. The posts are
  // kept whole here, not as ids over the cache: ListDashboard carries only
  // unread posts, so once marked read they are gone from its answer.
  const [visitPosts, setVisitPosts] = useState<FeedPost[]>([]);
  const [freshIds, setFreshIds] = useState<Set<string>>(new Set());
  const [picked, setPicked] = useState("");
  const [draft, setDraft] = useState("");
  const [looking, setLooking] = useState(false);
  const [order, setOrder] = useState<string[] | null>(null);
  const [places, setPlaces] = useState<Record<string, { size: string; rows: number }>>({});
  const [dragging, setDragging] = useState("");

  const [page, ref] = useState<HTMLDivElement | null>(null);
  const cols = useColumns(page);

  useEffect(() => {
    setVisitPosts([]);
    setFreshIds(new Set());
    setPicked("");
    setOrder(null);
  }, [bot.id]);

  useEffect(() => {
    void recheck(UI.method.listApprovals, input);
  }, [bot.id, bot.status]);

  useEffect(() => {
    if (q.error) onError(fail(q.error));
  }, [q.error]);

  // Read on sight: keep what was new for the visit, then tell the server.
  useEffect(() => {
    if (!data) return;
    const posts = data.posts;
    const fresh = data.widgets.filter((w) => w.fresh).map((w) => w.id);
    const unreadAsks = data.cards.some((c) => !c.read);
    if (!posts.length && !fresh.length && !unreadAsks) return;
    if (posts.length) setVisitPosts((cur) => [...posts.filter((p) => !cur.some((c) => c.id === p.id)), ...cur]);
    if (fresh.length) setFreshIds((cur) => new Set([...cur, ...fresh]));
    ui.markDashboardRead(input)
      .then(() => {
        patch(UI.method.listDashboard, input, (r) => ({
          ...r,
          posts: [],
          cards: r.cards.map((c) => ({ ...c, read: true })),
          widgets: r.widgets.map((w) => ({ ...w, fresh: false })),
        }));
        reloadBot(bot.id);
      })
      .catch(() => {});
  }, [data, bot.id]);

  const widgets = useMemo(() => {
    const ws = data?.widgets ?? [];
    if (!order) return ws;
    const byId = new Map(ws.map((w) => [w.id, w]));
    const placed = order.map((id) => byId.get(id)).filter((w): w is DashboardWidget => !!w);
    return [...placed, ...ws.filter((w) => !order.includes(w.id))].map((w) => (places[w.id] ? { ...w, ...places[w.id] } : w));
  }, [data?.widgets, order, places]);
  const pickedWidget = widgets.find((w) => w.id === picked);

  async function guard<T>(fn: () => Promise<T>): Promise<T | undefined> {
    onError("");
    try {
      return await fn();
    } catch (e) {
      onError(fail(e));
      return undefined;
    }
  }

  const handlersFor = useWidgetHandlers({
    botId: bot.id,
    guard,
    onPick: (id) => setPicked((p) => (p === id ? "" : id)),
    onQuoted,
  });

  // The board says it is being worked on as soon as the server does.
  const markCurating = () => patch(UI.method.listDashboard, input, (cur) => ({ ...cur, curating: true }));

  // An answered or put-away ask leaves the board at once.
  function dropCard(id: string) {
    patch(UI.method.listDashboard, input, (cur) => ({ ...cur, cards: cur.cards.filter((c) => c.id !== id) }));
    reloadBot(bot.id);
  }

  async function decideApproval(ap: Approval, decision: "allow_once" | "always" | "deny") {
    await guard(() => ui.decideApproval({ id: ap.id, decision }));
    void recheck(UI.method.listApprovals, input);
    reloadBot(bot.id);
  }

  async function autoApprove(ap: Approval) {
    await guard(() => approveSetAuto(bot.id, ap));
    void recheck(UI.method.listApprovals, input);
    reloadBot(bot.id);
  }

  async function submitAsk(card: DashboardCard, values: Record<string, unknown>, apart = false) {
    const r = await guard(() => ui.submitDashboardCard({ botId: bot.id, id: card.id, valuesJson: JSON.stringify(values), newChat: apart }));
    if (!r) return;
    dropCard(card.id);
    if (r.chat) onQuoted(r.chat);
  }

  async function dismissAsk(card: DashboardCard, suppressKind: boolean) {
    const r = await guard(() => ui.dismissDashboardCard({ botId: bot.id, id: card.id, suppressKind }));
    if (r) dropCard(card.id);
  }

  async function quotePost(p: FeedPost, apart: boolean) {
    const r = await guard(() => ui.quoteFeedPost({ botId: bot.id, id: p.id, newChat: apart }));
    if (r?.chat) onQuoted(r.chat);
  }

  async function ask(text: string, widgetId: string) {
    const r = await guard(() => ui.askDashboard({ botId: bot.id, text, widgetId }));
    if (!r) return false;
    setPicked("");
    markCurating();
    void reload(UI.method.listDashboard, input);
    return true;
  }

  async function lookAgain() {
    if (!data?.automationId) return;
    setLooking(true);
    const r = await guard(() => ui.runAutomation({ botId: bot.id, id: data.automationId }));
    setLooking(false);
    if (r) {
      markCurating();
      void reload(UI.method.listDashboard, input);
    }
  }

  function startArrange() {
    setOrder(widgets.map((w) => w.id));
    setPlaces({});
    setPicked("");
  }

  async function doneArrange() {
    if (!order) return;
    const ws = widgets;
    const ok = await guard(() =>
      ui.arrangeDashboard({
        botId: bot.id,
        places: ws.map((w, i) => ({ id: w.id, size: w.size, rows: w.rows, position: i })),
      }),
    );
    if (ok) {
      patch(UI.method.listDashboard, input, (cur) => ({ ...cur, widgets: ws.map((w, i) => ({ ...w, position: i })) }));
      setOrder(null);
      setPlaces({});
    }
  }

  async function removeWidget(w: DashboardWidget) {
    const ok = await guard(() => ui.removeWidget({ botId: bot.id, id: w.id }));
    if (ok) {
      patch(UI.method.listDashboard, input, (cur) => ({ ...cur, widgets: cur.widgets.filter((x) => x.id !== w.id) }));
      setOrder((o) => o?.filter((id) => id !== w.id) ?? null);
    }
  }

  if (!data) {
    return (
      <div ref={ref} className={board}>
        <div className="grid grid-cols-2 gap-2.5">
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="skeleton h-36 rounded-card" />
          ))}
        </div>
      </div>
    );
  }

  const arranging = order !== null;
  const empty = !widgets.length && !data.cards.length && !approvals.length && !visitPosts.length;
  const working = data.curating;

  if (empty && !working) {
    return (
      <div ref={ref} className="silo-page flex min-h-full max-w-[680px] flex-col justify-center pb-24">
        <h2 className="text-center text-[40px] leading-[48px] font-medium tracking-[-0.02em] text-ink">{t("Nothing on the board yet")}</h2>
        <p className="mt-2 mb-7 text-center text-ink-2">
          {t("Say what you want to keep an eye on. {{name}} builds it once and keeps it current.", { name: bot.name })}
        </p>
        <AskBar botName={bot.name} big draft={draft} setDraft={setDraft} onAsk={ask} />
        <div className="mt-4 flex flex-wrap justify-center gap-2">
          {suggestions().map((s) => (
            <button
              key={s.label}
              type="button"
              onClick={() => setDraft(s.label)}
              className="inline-flex h-[34px] items-center gap-2 rounded-control bg-surface px-3 text-[13px] font-medium text-ink-2 shadow-card transition-[color,box-shadow] duration-[160ms] hover:text-ink hover:shadow-float"
            >
              <span className="grid h-[18px] w-[18px] place-items-center rounded-[5px] bg-well">
                <s.icon size={12} />
              </span>
              {s.label}
            </button>
          ))}
        </div>
        <div className="mt-10">
          <StarterIdeas botId={bot.id} name={bot.name} />
        </div>
      </div>
    );
  }

  return (
    <div ref={ref} className={`relative flex min-h-full flex-col ${board}`}>
      <div className="mb-2 flex h-8 min-w-0 items-center gap-2.5 text-[12.5px] text-ink-3">
        {working ? (
          <span className="flex min-w-0 items-center gap-2">
            <Lamp status="working" />
            <span className="shimmer-text truncate">{t("{{name}} is working on the board", { name: bot.name })}</span>
            {data.chatId && data.automationId ? (
              <Link
                to="/bots/$botId/automations/$automationId"
                params={{ botId: bot.id, automationId: data.automationId }}
                className="shrink-0 text-ink-2 hover:text-ink hover:underline"
              >
                {t("Watch")}
              </Link>
            ) : null}
          </span>
        ) : (
          <>
            <span className="truncate">{data.curatedAt ? t("Curated {{when}}", { when: runWhen(data.curatedAt) }) : t("Not curated yet")}</span>
            {data.automationId ? (
              <>
                <span aria-hidden>·</span>
                <Tip content={t("Change when the Bot looks again")}>
                  <Link
                    to="/bots/$botId/automations/$automationId"
                    params={{ botId: bot.id, automationId: data.automationId }}
                    className="shrink-0 border-b border-dotted border-line-strong text-ink-2 hover:text-ink"
                  >
                    {data.nextAt ? t("next {{when}}", { when: nextWhen(data.nextAt) }) : t("no schedule")}
                  </Link>
                </Tip>
                <button
                  type="button"
                  disabled={looking}
                  onClick={() => void lookAgain()}
                  aria-label={t("Look again now")}
                  title={t("Look again now")}
                  className="grid h-[26px] w-[26px] shrink-0 place-items-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink disabled:opacity-50"
                >
                  <RotateCw size={13.5} />
                </button>
              </>
            ) : null}
          </>
        )}
        <span className="flex-1" />
        {widgets.length > 1 ? (
          arranging ? (
            <Btn kind="primary" size="sm" icon={<Check size={13} />} onClick={() => void doneArrange()}>
              {t("Done")}
            </Btn>
          ) : (
            <Btn kind="ghost" size="sm" icon={<Move size={13} />} onClick={startArrange}>
              {t("Arrange")}
            </Btn>
          )
        ) : null}
      </div>

      {approvals.length || data.cards.length ? (
        <section aria-label={t("Needs you")} className={`mb-2.5 grid gap-2.5 ${cols > 1 ? "grid-cols-2" : "grid-cols-1"}`}>
          {approvals.map((ap) => (
            <ApprovalCard key={ap.id} approval={ap} onDecide={(d) => void decideApproval(ap, d)} onAutoApprove={() => void autoApprove(ap)} />
          ))}
          {data.cards.map((c) => (
            <AskSlip key={c.id} botId={bot.id} card={c} onSubmit={submitAsk} onDismiss={dismissAsk} />
          ))}
        </section>
      ) : null}

      {visitPosts.length ? (
        <div className="mb-2.5">
          <SinceLooked botId={bot.id} posts={visitPosts} onQuote={quotePost} />
        </div>
      ) : null}

      {!widgets.some((w) => w.kind === "ideas") && widgets.length < 3 ? (
        <div className="mb-2.5">
          <StarterIdeas botId={bot.id} name={bot.name} />
        </div>
      ) : null}

      {widgets.length ? (
        <div className="grid grid-flow-row-dense gap-2.5" style={{ gridTemplateColumns: `repeat(${cols}, minmax(0, 1fr))` }}>
          {widgets.map((w) => {
            const drag = arranging
              ? {
                  draggable: true,
                  onDragStart: (e: DragEvent<HTMLElement>) => {
                    setDragging(w.id);
                    e.dataTransfer.effectAllowed = "move";
                  },
                  onDragOver: (e: DragEvent<HTMLElement>) => {
                    e.preventDefault();
                    if (dragging && dragging !== w.id) setOrder((o) => (o ? reorder(o, dragging, w.id) : o));
                  },
                  onDragEnd: () => setDragging(""),
                }
              : undefined;
            return (
              <Widget
                key={w.id}
                drag={drag}
                botId={bot.id}
                w={w}
                cols={cols}
                fresh={freshIds.has(w.id)}
                selected={picked === w.id || dragging === w.id}
                arranging={arranging}
                arrange={
                  <ArrangeTools
                    w={w}
                    cols={cols}
                    onSize={(size, rows) => setPlaces((p) => ({ ...p, [w.id]: { size, rows } }))}
                    onRemove={() => void removeWidget(w)}
                  />
                }
                handlers={handlersFor(w)}
              />
            );
          })}
        </div>
      ) : null}

      {/* Pinned to the foot of the page however little is on the board. */}
      <div className="sticky bottom-0 z-10 -mx-1 mt-auto bg-gradient-to-b from-transparent to-canvas to-30% px-1 pt-6 pb-3">
        {/* A line to the Bot, not a page-wide field. */}
        <div className="mx-auto w-full min-[960px]:w-[70%]">
          <AskBar
            botName={bot.name}
            picked={pickedWidget ? { id: pickedWidget.id, title: pickedWidget.title } : undefined}
            onUnpick={() => setPicked("")}
            draft={draft}
            setDraft={setDraft}
            onAsk={ask}
          />
        </div>
      </div>
    </div>
  );
}

// The board takes the whole page: widgets are the content, so it keeps only a
// thin edge instead of the reading column the other pages use.
const board = "w-full px-3 pt-2 min-[960px]:px-4 min-[960px]:pt-3";
