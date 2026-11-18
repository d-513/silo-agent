import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { Bot as BotGlyph, ChevronDown, Columns2, ListTree, Plus, Trash2, X } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type DragEvent, type MouseEvent, type PointerEvent, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { ArmedButton } from "../Feedback";
import { inputClass } from "../Field";
import { UI, type Bot, type BotActivity, type Chat } from "../gen/silo/v1/ui_pb";
import { locale, t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { shortWhen } from "../nav/botOrder";
import { Initials } from "../space/People";
import { chatDoing, type Doing, runLook, started } from "../statusLook";
import { Tip } from "../Tip";
import { useDismiss } from "../useMenu";
import { useWide } from "../useWide";
import { chatOf, pinnable, type TabKey } from "./layout";
import { ChatTitleInput } from "./ChatTitleInput";
import { FadeScroll } from "./FadeScroll";
import { UnreadBadge } from "./SideNav";
import { tabMenu, type TabControl } from "./tabControl";
import { keyIcon, keyLabel, keyLink } from "./tabMeta";
import { dragEnd, dragStart, dropped, carriesTab, openMenu, onMenu, useDragging } from "./TabMenu";
import type { ChatRename } from "./useChatList";

// TabGround is what a tab stands on. The open one is the page's own color,
// joined to the page under it, with the cobalt bar on its top edge; the one in
// the split is lifted, with the bar faint; any other only lifts a little under
// the pointer.
function TabGround({ on, beside = false }: { on: boolean; beside?: boolean }) {
  return (
    <>
      <span
        aria-hidden
        className={`pointer-events-none absolute transition-[background-color] duration-[160ms] ease-quiet ${
          on
            ? "inset-0 rounded-t-control bg-canvas shadow-[inset_0_1px_0_var(--color-line),inset_1px_0_0_var(--color-line),inset_-1px_0_0_var(--color-line)]"
            : beside
              ? "inset-x-0 top-1 bottom-1 rounded-control bg-surface/70 shadow-[inset_0_0_0_1px_var(--color-line)]"
              : "inset-x-0 top-1 bottom-1 rounded-control group-hover/tab:bg-pressed"
        }`}
      />
      <span
        aria-hidden
        className={`pointer-events-none absolute inset-x-3 top-0 h-0.5 rounded-full bg-cobalt transition-[opacity,transform] duration-200 ease-quiet motion-reduce:transition-opacity ${
          on ? "scale-x-100 opacity-100" : "scale-x-50 opacity-0"
        }`}
      />
    </>
  );
}

const tabBox = "group/tab relative flex h-9 shrink-0 items-center self-end transition-colors duration-[160ms] ease-quiet";
const tone = (on: boolean) => (on ? "text-ink" : "text-ink-2 hover:text-ink");

// What a chat is doing, as its tab has room for it: the lamp, the words in
// its tooltip.
function TabLamp({ doing }: { doing: Doing }) {
  const look = runLook(doing);
  return (
    <span className="relative flex shrink-0 items-center" title={look.word}>
      <Lamp status={look.lamp} />
    </span>
  );
}

// What waits on the Dashboard: approvals first (a run is paused on them),
// then what is new there, then that the Bot is working on the board; null
// with nothing to say.
export function dashMark(needs: number, fresh: number, curating: boolean): ReactNode {
  if (needs > 0) {
    return (
      <span aria-label={t("{{count}} need you", { count: needs })} className="rise min-w-[18px] rounded-full bg-vermilion px-1.5 text-center font-mono text-micro leading-[18px] text-on-accent">
        {needs > 99 ? "99+" : needs}
      </span>
    );
  }
  if (fresh > 0) return <UnreadBadge n={fresh} />;
  return curating ? <Lamp status="working" /> : null;
}

/** What waits on the Dashboard, counted the way its tab counts it. */
export function useDashFresh(bot: Bot) {
  const dashQ = useQuery(UI.method.listDashboard, { botId: bot.id });
  const fresh = bot.dashboardAsks + (dashQ.data?.widgets ?? []).filter((w) => w.fresh).length + (dashQ.data?.posts ?? []).length;
  return { fresh, curating: !!dashQ.data?.curating };
}

// Where a dragged tab would land: before `before` in `group` ("" at its end).
type Over = { group: "pinned" | "strip"; before: TabKey | "" };

// The thin cobalt mark where a dragged tab would go. It takes no room, so the
// tabs never jump under the pointer.
function DropMark() {
  return <span aria-hidden className="pointer-events-none relative z-10 -mx-[2px] mb-1.5 h-6 w-[3px] shrink-0 self-end rounded-full bg-cobalt" />;
}

// ChatTabs heads every page of a Bot (below 960 the strip under the Bot's
// top bar). Every page of the Bot can be a tab here: first the pinned ones
// (the Dashboard and the main chat unless you unpinned them, and whatever you
// pinned), which stay; then the strip: what you opened from the rail and your
// side chats, each closed with its ×; + opens a new chat; the arrow lists the
// closed chats (and, for a manager, everyone else's) to reopen or delete.
// A right-click (a long press on a finger) is a tab's menu: pin, unpin, split,
// rename, close. A tab drags: among the pinned to pin it, into the strip to
// unpin or reorder it, onto the page to split it (BotPage), onto the rail to
// put it away.
export function ChatTabs({
  bot,
  ctl,
  keys,
  rename,
  pendingCount,
  sending,
  callChat,
  onNewChat,
  onDeleteChat,
  onHideChat,
}: {
  bot: Bot;
  ctl: TabControl;
  keys: { pinned: TabKey[]; strip: TabKey[] };
  rename: ChatRename;
  pendingCount: number;
  // The open chat has a reply on its way, before the Bot's row says so.
  sending: boolean;
  // The chat a live call is held in, "" with none.
  callChat: string;
  onNewChat: () => void;
  onDeleteChat: (cid: string) => void;
  onHideChat: (cid: string, hidden: boolean) => void;
}) {
  const id = bot.id;
  const { chats, current } = ctl;
  // Below 960 the bar is the strip under the Bot's top bar: pinned pages are
  // their glyphs, and the tabs are a little narrower.
  const wide = useWide();
  const openChatId = current === "main" ? ctl.mainId : chatOf(current);
  // A conversation answering another Bot is its own group: it is read-only,
  // and nobody here deletes it.
  const closed = chats.filter((c) => c.mine && !c.main && c.hidden && !c.fromBot && !ctl.pinned.includes(`chat:${c.id}`));
  const team = chats.filter((c) => !c.mine && !c.fromBot);
  const fromBots = chats.filter((c) => c.fromBot);
  const dash = useDashFresh(bot);
  const doing = (cid: string | undefined): Doing | undefined =>
    chatDoing(bot, cid) ?? (sending && cid && cid === openChatId ? started : cid && cid === callChat ? { ...started, state: "on_call" } : undefined);

  // The open tab is never scrolled out of sight.
  const strip = useRef<HTMLDivElement>(null);
  useEffect(() => {
    strip.current?.querySelector("[data-tab-on]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [current, keys.strip.length]);

  const dragging = useDragging();
  const [over, setOver] = useState<Over | null>(null);
  useEffect(() => {
    if (!dragging) setOver(null);
  }, [dragging]);

  function overTab(e: DragEvent, group: Over["group"], list: TabKey[], k: TabKey) {
    if (!carriesTab(e) || (group === "pinned" && dragging && !pinnable(dragging))) return;
    e.preventDefault();
    e.stopPropagation();
    e.dataTransfer.dropEffect = "move";
    const r = e.currentTarget.getBoundingClientRect();
    const after = e.clientX > r.left + r.width / 2;
    const before = after ? (list[list.indexOf(k) + 1] ?? "") : k;
    if (over?.group !== group || over.before !== before) setOver({ group, before });
  }
  function overEnd(e: DragEvent, group: Over["group"]) {
    if (!carriesTab(e) || (group === "pinned" && dragging && !pinnable(dragging))) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    if (over?.group !== group || over.before !== "") setOver({ group, before: "" });
  }
  function drop(e: DragEvent) {
    e.preventDefault();
    const k = dropped(e);
    const at = over;
    setOver(null);
    if (k && at) ctl.drop(k, at.group, at.before);
  }

  const extras = (k: TabKey, on: boolean): ReactNode => {
    if (k === "dashboard") return dashMark(pendingCount, on ? 0 : dash.fresh, dash.curating);
    if (k === "feed" && !on && bot.feedUnread > 0) return <UnreadBadge n={bot.feedUnread} />;
    if (k === "settings" && !on && bot.auditUnseen > 0) return <UnreadBadge n={bot.auditUnseen} />;
    const cid = k === "main" ? ctl.mainId : chatOf(k);
    const d = cid ? doing(cid) : undefined;
    return d ? <TabLamp doing={d as BotActivity} /> : null;
  };

  const render = (group: Over["group"], list: TabKey[]) =>
    list.map((k) => (
      <span key={k} className="contents">
        {over?.group === group && over.before === k ? <DropMark /> : null}
        <BarTab
          k={k}
          ctl={ctl}
          pinned={group === "pinned"}
          bare={!wide && group === "pinned" && k !== "main" && !chatOf(k)}
          on={k === current}
          beside={k === ctl.split}
          chat={chats.find((c) => c.id === (k === "main" ? ctl.mainId : chatOf(k)))}
          rename={rename}
          end={extras(k, k === current)}
          onDragOver={(e) => overTab(e, group, list, k)}
        />
      </span>
    ));

  const pinDrop = !!dragging && pinnable(dragging);
  return (
    <nav
      aria-label={t("Conversations")}
      onDragLeave={(e) => {
        if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) setOver(null);
      }}
      onDrop={drop}
      className="relative flex h-11 shrink-0 items-end gap-1 bg-well pr-2 pl-2 shadow-[inset_0_-1px_0_var(--color-line)] max-wide:h-10 max-wide:pr-1 max-wide:pl-1.5"
    >
      <div role="group" aria-label={t("Pinned")} className="flex h-full shrink-0 items-end gap-1" onDragOver={(e) => overEnd(e, "pinned")}>
        {render("pinned", keys.pinned)}
        {over?.group === "pinned" && over.before === "" ? <DropMark /> : null}
        {keys.pinned.length === 0 && pinDrop ? (
          <span className="mb-1 flex h-7 shrink-0 items-center self-end rounded-sm px-2.5 text-label text-ink-3 shadow-[inset_0_0_0_1px_var(--color-line-strong)]">{t("Pin here")}</span>
        ) : null}
      </div>
      {keys.pinned.length && keys.strip.length ? <span aria-hidden className="mb-2.5 h-4 w-px shrink-0 self-end bg-line" /> : null}
      <FadeScroll fade="from-well" className="flex-initial self-stretch">
        <div ref={strip} role="group" aria-label={t("Open tabs")} className="flex h-full min-w-6 items-end gap-1" onDragOver={(e) => overEnd(e, "strip")}>
          {render("strip", keys.strip)}
          {over?.group === "strip" && over.before === "" ? <DropMark /> : null}
        </div>
      </FadeScroll>
      <button
        type="button"
        title={t("New chat")}
        aria-label={t("New chat")}
        onClick={onNewChat}
        className="group/new mb-1 flex h-7 w-7 shrink-0 items-center justify-center self-end rounded-sm text-ink-2 transition-[background-color,color] duration-[160ms] ease-quiet hover:bg-pressed hover:text-ink"
      >
        <Plus size={16} className="transition-transform duration-[280ms] ease-quiet group-hover/new:rotate-90 motion-reduce:transition-none" />
      </button>
      <span className="min-w-2 flex-1 self-stretch" onDragOver={(e) => overEnd(e, "strip")} />
      <ClosedMenu
        botId={id}
        closed={closed}
        team={team}
        fromBots={fromBots}
        runs={bot.role === "manage"}
        onOpen={(c) => {
          if (c.mine && c.hidden) onHideChat(c.id, false);
          ctl.open(`chat:${c.id}`);
        }}
        onDelete={onDeleteChat}
      />
      {bot.people.length > 1 && wide ? (
        <span className="mb-1.5 ml-1 flex self-end">
          <People people={bot.people} />
        </span>
      ) : null}
    </nav>
  );
}

function CloseX({ on, onClick }: { on: boolean; onClick: () => void }) {
  return (
    <button
      type="button"
      title={t("Close tab")}
      aria-label={t("Close tab")}
      onClick={onClick}
      className={`absolute top-1/2 right-1.5 flex h-6 w-6 -translate-y-1/2 items-center justify-center rounded-sm text-ink-3 transition-[opacity,background-color,color] duration-[160ms] ease-quiet hover:bg-pressed hover:text-ink focus-visible:opacity-100 ${
        on ? "opacity-100" : "opacity-0 group-hover/tab:opacity-100 [@media(hover:none)]:pointer-events-none"
      }`}
    >
      <X size={13} />
    </button>
  );
}

// One tab: any page of the Bot, or a chat. A pinned one has no × and, below
// 960, shows its glyph alone (the main chat and pinned chats keep their
// words). A side chat's name renames in place on a double-click.
function BarTab({
  k,
  ctl,
  pinned,
  bare,
  on,
  beside,
  chat,
  rename,
  end,
  onDragOver,
}: {
  k: TabKey;
  ctl: TabControl;
  pinned: boolean;
  bare: boolean;
  on: boolean;
  // Shown in the split beside the page.
  beside: boolean;
  chat?: Chat;
  rename: ChatRename;
  end: ReactNode;
  onDragOver: (e: DragEvent) => void;
}) {
  const label = keyLabel(k, ctl.chats);
  const side = !!chatOf(k);
  const editing = !!chat && rename.editing === chat.id;
  const menu = () => tabMenu(k, ctl, "bar");
  const press = useHold((at) => openMenu(at, label, menu()));
  const closable = !pinned;
  const Icon = keyIcon(k);
  const glyph = side ? (
    chat?.fromBot ? (
      <BotGlyph size={14} className="shrink-0 text-ink-3" aria-label={t("From the Bot {{name}}", { name: chat.fromBot })} />
    ) : chat && !chat.mine ? (
      chat.author ? (
        <Initials email={chat.author} size={18} />
      ) : (
        <BotGlyph size={14} className="shrink-0 text-ink-3" aria-label={t("The Bot's own")} />
      )
    ) : null
  ) : (
    <Icon size={15} className="relative shrink-0" />
  );
  const width = side || k === "new" ? "max-w-[220px] min-w-[104px] max-wide:max-w-[176px] max-wide:min-w-[92px]" : "";

  if (k === "new") {
    return (
      <div data-tab-on={on || undefined} className={`${tabBox} ${tone(on)} ${width} pr-8 pl-3`} onDragOver={onDragOver}>
        <TabGround on={on} />
        <span className="relative min-w-0 flex-1 truncate text-small font-medium">{label}</span>
        <CloseX on={on} onClick={() => ctl.close(k)} />
      </div>
    );
  }
  return (
    <div className={`${tabBox} ${tone(on)} ${width}`} onDragOver={onDragOver} onContextMenu={onMenu(label, menu)}>
      <TabGround on={on} beside={beside} />
      {editing && chat ? (
        <ChatTitleInput rename={rename} cid={chat.id} className={`${inputClass} relative mx-1.5 h-7 w-44 px-2 text-small`} />
      ) : (
        <>
          <Link
            {...keyLink(k, ctl.botId, ctl.mainId)}
            // Opening the tab shown in the split swaps the two: BotPage does
            // that as the route moves, so a plain link is enough.
            onClick={press.onClick}
            data-tab-on={on || undefined}
            title={bare ? label : chat?.mine && !chat.main && !chat.fromBot ? `${label} · ${t("Double-click to rename")}` : label}
            aria-label={label}
            draggable
            onDragStart={(e) => dragStart(e, k)}
            onDragEnd={dragEnd}
            onDoubleClick={(e) => {
              if (!chat?.mine || chat.main || chat.fromBot) return;
              e.preventDefault();
              rename.begin(chat);
            }}
            onPointerDown={press.onPointerDown}
            onPointerMove={press.onPointerMove}
            onPointerUp={press.onPointerUp}
            onPointerCancel={press.onPointerCancel}
            className={`relative flex h-full min-w-0 flex-1 items-center gap-2 rounded-t-control outline-offset-[-2px] select-none [-webkit-touch-callout:none] ${
              closable ? "pr-8" : bare ? "pr-2.5" : "pr-3"
            } ${bare ? "pl-2.5" : "pl-3"}`}
          >
            {glyph}
            <span className={bare ? "sr-only" : `relative min-w-0 ${side ? "flex-1 truncate" : "whitespace-nowrap"} text-small font-medium`}>{label}</span>
            {beside ? <Columns2 size={12} aria-label={t("In the split view")} className="relative shrink-0 text-cobalt" /> : null}
            {end ? <span className="relative flex shrink-0 items-center">{end}</span> : null}
          </Link>
          {closable ? <CloseX on={on} onClick={() => ctl.close(k)} /> : null}
        </>
      )}
    </div>
  );
}

// useHold is a long press, for a finger, which has no right-click: it opens
// the tab's menu where the finger is. The press that started it opens nothing.
function useHold(fire: (at: { x: number; y: number }) => void) {
  const st = useRef<{ timer: number; at: { x: number; y: number } | null; fired: boolean }>({ timer: 0, at: null, fired: false });
  const go = useRef(fire);
  go.current = fire;
  useEffect(() => () => window.clearTimeout(st.current.timer), []);
  const cancel = () => {
    window.clearTimeout(st.current.timer);
    st.current.at = null;
  };
  return {
    onPointerDown: (e: PointerEvent) => {
      st.current.fired = false;
      if (e.pointerType !== "touch") return;
      const at = { x: e.clientX, y: e.clientY };
      st.current.at = at;
      window.clearTimeout(st.current.timer);
      st.current.timer = window.setTimeout(() => {
        st.current.fired = true;
        st.current.at = null;
        navigator.vibrate?.(8);
        go.current(at);
      }, 480);
    },
    onPointerMove: (e: PointerEvent) => {
      const at = st.current.at;
      if (at && Math.hypot(e.clientX - at.x, e.clientY - at.y) > 8) cancel();
    },
    onPointerUp: cancel,
    onPointerCancel: cancel,
    onClick: (e: MouseEvent) => {
      if (st.current.fired) e.preventDefault();
      st.current.fired = false;
    },
  };
}

// ClosedMenu is the arrow at the end of the tabs: the chats you closed,
// newest first, each a press from being a tab again or (armed) deleted; and
// for a manager the Team's chats, which only open, and at its foot the way to
// everything the Bot did (Runs), which is theirs alone to review.
function ClosedMenu({
  botId,
  closed,
  team,
  fromBots,
  runs,
  onOpen,
  onDelete,
}: {
  botId: string;
  closed: Chat[];
  team: Chat[];
  fromBots: Chat[];
  runs: boolean;
  onOpen: (c: Chat) => void;
  onDelete: (cid: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [find, setFind] = useState("");
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<CSSProperties>({});
  useDismiss(open, trigger, menu, () => setOpen(false));
  useEffect(() => {
    if (!open) return;
    setFind("");
    const r = trigger.current?.getBoundingClientRect();
    if (r) setBox({ right: Math.max(8, window.innerWidth - r.right), top: r.bottom + 6 });
    menu.current?.focus();
  }, [open]);
  // Another Bot's chats are not this menu's.
  useEffect(() => setOpen(false), [botId]);

  const q = find.trim().toLowerCase();
  const match = (c: Chat) => !q || (c.title || "").toLowerCase().includes(q) || c.author.toLowerCase().includes(q);
  const mine = closed.filter(match);
  const theirs = team.filter(match);
  const bots = fromBots.filter((c) => !q || c.fromBot.toLowerCase().includes(q) || match(c));
  const total = closed.length + team.length + fromBots.length;
  const label = t("Closed chats");
  const row = "group/row relative flex h-9 items-center rounded-sm transition-colors duration-[160ms] hover:bg-well";
  const pick = (c: Chat) => {
    setOpen(false);
    onOpen(c);
  };
  return (
    <>
      <button
        ref={trigger}
        type="button"
        title={label}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="mb-1 flex h-7 shrink-0 items-center gap-1 self-end rounded-sm px-1.5 text-ink-2 transition-[background-color,color] duration-[160ms] ease-quiet hover:bg-pressed hover:text-ink aria-expanded:bg-pressed aria-expanded:text-ink"
      >
        {closed.length ? <span className="font-mono text-label leading-4 text-ink-3 tabular-nums">{closed.length}</span> : null}
        <ChevronDown size={16} className={`transition-transform duration-200 ease-quiet motion-reduce:transition-none ${open ? "rotate-180" : ""}`} />
      </button>
      {open
        ? createPortal(
            <div
              ref={menu}
              role="menu"
              tabIndex={-1}
              aria-label={label}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  setOpen(false);
                  trigger.current?.focus();
                }
              }}
              className="fixed z-50 flex max-h-[min(70vh,560px)] w-[min(340px,calc(100vw-16px))] flex-col rounded-control bg-surface p-1 shadow-slip outline-none"
              style={box}
            >
              <div className="rise flex min-h-0 flex-col">
                {total > 8 ? <input className={`${inputClass} mb-1 h-8 shrink-0 px-2.5 text-small`} placeholder={t("Find a chat")} aria-label={t("Find a chat")} value={find} onChange={(e) => setFind(e.target.value)} /> : null}
                <div className="min-h-0 flex-1 overflow-auto overscroll-contain">
                  <div className="px-2 pt-1.5 pb-1 text-section text-ink-3">{label}</div>
                  {mine.length === 0 ? (
                    <p className="px-2 pt-0.5 pb-2 text-meta leading-[18px] text-ink-3">{closed.length ? t("No chat by that name.") : t("Nothing here yet. A tab you close waits here.")}</p>
                  ) : null}
                  {mine.map((c) => (
                    <div key={c.id} className={row}>
                      <button type="button" role="menuitem" onClick={() => pick(c)} title={t("Open as a tab again")} className="flex h-full min-w-0 flex-1 items-center gap-2 rounded-sm pr-1 pl-2 text-left outline-offset-[-2px]">
                        <span className="min-w-0 flex-1 truncate text-row text-ink">{c.title || t("New chat")}</span>
                        <span className="shrink-0 text-micro text-ink-3 tabular-nums">{shortWhen(c.updatedAt, new Date(), locale())}</span>
                      </button>
                      <ArmedButton kind="ghost" size="sm" iconOnly className="mr-0.5" title={t("Delete chat")} armedLabel={t("Click again to delete")} icon={<Trash2 size={13} />} onConfirm={() => onDelete(c.id)} />
                    </div>
                  ))}
                  {team.length ? (
                    <>
                      <span aria-hidden className="mx-2 my-1 block h-px bg-line" />
                      <div className="px-2 pt-1.5 pb-1 text-section text-ink-3">{t("Team")}</div>
                      {theirs.map((c) => (
                        <div key={c.id} className={row}>
                          <button type="button" role="menuitem" onClick={() => pick(c)} className="flex h-full min-w-0 flex-1 items-center gap-2 rounded-sm px-2 text-left outline-offset-[-2px]">
                            {c.author ? <Initials email={c.author} size={18} /> : <BotGlyph size={14} className="shrink-0 text-ink-3" aria-label={t("The Bot's own")} />}
                            <span className="min-w-0 flex-1 truncate text-row text-ink">{c.main ? t("Main chat") : c.title || t("New chat")}</span>
                            <span className="shrink-0 text-micro text-ink-3 tabular-nums">{shortWhen(c.updatedAt, new Date(), locale())}</span>
                          </button>
                        </div>
                      ))}
                    </>
                  ) : null}
                  {bots.length ? (
                    <>
                      <span aria-hidden className="mx-2 my-1 block h-px bg-line" />
                      <div className="px-2 pt-1.5 pb-1 text-section text-ink-3">{t("From other Bots")}</div>
                      {bots.map((c) => (
                        <div key={c.id} className={row}>
                          <button type="button" role="menuitem" onClick={() => pick(c)} className="flex h-full min-w-0 flex-1 items-center gap-2 rounded-sm px-2 text-left outline-offset-[-2px]">
                            <BotGlyph size={14} className="shrink-0 text-ink-3" aria-hidden />
                            <span className="min-w-0 flex-1 truncate text-row text-ink">{c.fromBot}</span>
                            {!c.mine && c.author ? <span className="max-w-[40%] shrink-0 truncate text-micro text-ink-3">{c.author}</span> : null}
                            <span className="shrink-0 text-micro text-ink-3 tabular-nums">{shortWhen(c.updatedAt, new Date(), locale())}</span>
                          </button>
                        </div>
                      ))}
                    </>
                  ) : null}
                </div>
                {runs ? (
                  <>
                    <span aria-hidden className="mx-2 my-1 block h-px shrink-0 bg-line" />
                    <Link
                      to="/bots/$botId/runs"
                      params={{ botId }}
                      role="menuitem"
                      onClick={() => setOpen(false)}
                      className="flex h-9 shrink-0 items-center gap-2 rounded-sm px-2 text-row text-ink outline-offset-[-2px] transition-colors duration-[160ms] hover:bg-well"
                    >
                      <ListTree size={14} className="shrink-0 text-ink-3" />
                      <span className="min-w-0 flex-1 truncate">{t("View all runs")}</span>
                    </Link>
                  </>
                ) : null}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

// People is the faces of a Bot several people use: up to three, then +N,
// with everyone and their role in a Tip.
export function People({ people }: { people: Bot["people"] }) {
  return (
    <Tip
      content={
        <ul className="space-y-0.5 text-meta">
          {people.map((p) => (
            <li key={p.userId}>
              {p.email} · {p.role === "manage" ? t("Can manage") : t("Can use")}
            </li>
          ))}
        </ul>
      }
    >
      <span tabIndex={0} className="flex shrink-0 items-center -space-x-1.5 outline-none">
        {people.slice(0, 3).map((p) => (
          <Initials key={p.userId} email={p.email} size={20} />
        ))}
        {people.length > 3 ? <span className="pl-2 text-micro text-ink-3">{`+${people.length - 3}`}</span> : null}
      </span>
    </Tip>
  );
}
