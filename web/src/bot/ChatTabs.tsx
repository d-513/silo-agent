import { useQuery } from "@connectrpc/connect-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Bot as BotGlyph, ChevronDown, LayoutGrid, MessageCircle, Plus, Trash2, X, type LucideIcon } from "lucide-react";
import { useEffect, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { ArmedButton } from "../Feedback";
import { inputClass } from "../Field";
import { UI, type Bot, type BotActivity, type Chat } from "../gen/silo/v1/ui_pb";
import { locale, t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { chatLink } from "../links";
import { shortWhen } from "../nav/botOrder";
import { Initials } from "../space/People";
import { chatDoing, type Doing, runLook, started } from "../statusLook";
import { useDismiss } from "../useMenu";
import { People } from "./BotSidebar";
import { afterClose, chatTabs, closedChats } from "./tabOrder";
import { ChatTitleInput } from "./ChatTitleInput";
import { FadeScroll } from "./FadeScroll";
import { UnreadBadge } from "./SideNav";
import type { Tab } from "./tabs";
import type { ChatRename } from "./useChatList";

// TabGround is what a tab stands on. The open one is the page's own color,
// joined to the page under it, with the cobalt bar on its top edge; any other
// only lifts a little under the pointer.
function TabGround({ on }: { on: boolean }) {
  return (
    <>
      <span
        aria-hidden
        className={`pointer-events-none absolute transition-[background-color] duration-[160ms] ease-quiet ${
          on
            ? "inset-0 rounded-t-control bg-canvas shadow-[inset_0_1px_0_var(--color-line),inset_1px_0_0_var(--color-line),inset_-1px_0_0_var(--color-line)]"
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

// A standing tab: Dashboard, or the main chat. It cannot be closed.
function PageTab({ on, icon: Icon, label, end, ...link }: { on: boolean; icon: LucideIcon; label: string; end?: ReactNode } & ({ to: "/bots/$botId/dashboard"; params: { botId: string } } | ReturnType<typeof chatLink>)) {
  return (
    <Link {...link} data-tab-on={on || undefined} className={`${tabBox} ${tone(on)} gap-2 rounded-t-control px-3 outline-offset-[-2px]`}>
      <TabGround on={on} />
      <Icon size={15} className="relative shrink-0" />
      <span className="relative text-small font-medium whitespace-nowrap">{label}</span>
      {end ? <span className="relative flex shrink-0 items-center">{end}</span> : null}
    </Link>
  );
}

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
function dashMark(needs: number, fresh: number, curating: boolean): ReactNode {
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

// ChatTabs heads every page of a Bot in the wide layout: its conversations as
// tabs. Dashboard and the main chat stand first and stay; then your side
// chats, each closed with its ×, which only puts it away; + opens a new one;
// the arrow lists the closed ones (and, for a manager, everyone else's) to
// reopen or delete.
export function ChatTabs({
  id,
  bot,
  tab,
  chatId,
  fresh,
  chats,
  rename,
  pendingCount,
  sending,
  callChat,
  onNewChat,
  onDeleteChat,
  onHideChat,
}: {
  id: string;
  bot: Bot;
  tab: Tab;
  chatId?: string;
  // The empty chat (/run/new) is open: a tab with no chat behind it yet.
  fresh: boolean;
  chats: Chat[];
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
  const navigate = useNavigate();
  const onRun = tab === "run";
  const main = chats.find((c) => c.main && c.mine);
  const tabs = useMemo(() => chatTabs(chats, onRun ? chatId : undefined), [chats, onRun, chatId]);
  const closed = useMemo(() => closedChats(chats), [chats]);
  const team = useMemo(() => chats.filter((c) => !c.mine), [chats]);
  const dashQ = useQuery(UI.method.listDashboard, { botId: id });
  const freshCount = bot.dashboardAsks + (dashQ.data?.widgets ?? []).filter((w) => w.fresh).length + (dashQ.data?.posts ?? []).length;
  const doing = (cid: string | undefined): Doing | undefined =>
    chatDoing(bot, cid) ?? (sending && cid === chatId ? started : cid && cid === callChat ? { ...started, state: "on_call" } : undefined);

  // The open tab is never scrolled out of sight.
  const strip = useRef<HTMLDivElement>(null);
  useEffect(() => {
    strip.current?.querySelector("[data-tab-on]")?.scrollIntoView({ block: "nearest", inline: "nearest" });
  }, [chatId, fresh, onRun, tabs.length]);

  const home = () => void navigate(main ? chatLink(id, main.id) : { to: "/bots/$botId/dashboard", params: { botId: id } });

  // Closing a tab puts the chat away; if it was the open one, its neighbour
  // opens (the main chat after the last).
  function close(c: Chat) {
    const next = afterClose(tabs, c.id);
    if (c.mine && !c.hidden) onHideChat(c.id, true);
    if (onRun && c.id === chatId) {
      if (next) void navigate(chatLink(id, next));
      else home();
    }
  }

  return (
    <nav aria-label={t("Conversations")} className="relative flex h-11 shrink-0 items-end gap-1 bg-well pr-2 pl-2 shadow-[inset_0_-1px_0_var(--color-line)]">
      <PageTab
        to="/bots/$botId/dashboard"
        params={{ botId: id }}
        on={tab === "dashboard"}
        icon={LayoutGrid}
        label={t("Dashboard")}
        end={dashMark(pendingCount, tab === "dashboard" ? 0 : freshCount, !!dashQ.data?.curating)}
      />
      {main ? (
        <PageTab {...chatLink(id, main.id)} on={onRun && main.id === chatId} icon={MessageCircle} label={t("Main chat")} end={doing(main.id) ? <TabLamp doing={doing(main.id)!} /> : null} />
      ) : null}
      {tabs.length || fresh ? <span aria-hidden className="mb-2.5 h-4 w-px shrink-0 self-end bg-line" /> : null}
      <FadeScroll fade="from-well" className="flex-initial self-stretch">
        <div ref={strip} className="flex h-full items-end gap-1">
          {tabs.map((c) => (
            <ChatTab key={c.id} botId={id} chat={c} on={onRun && c.id === chatId} doing={doing(c.id)} rename={rename} onClose={() => close(c)} />
          ))}
          {fresh && onRun && !chatId ? (
            <div data-tab-on className={`${tabBox} ${tone(true)} pr-8 pl-3`}>
              <TabGround on />
              <span className="relative text-small font-medium whitespace-nowrap">{t("New chat")}</span>
              <CloseX on onClick={home} />
            </div>
          ) : null}
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
      <span className="min-w-2 flex-1" />
      <ClosedMenu
        botId={id}
        closed={closed}
        team={team}
        onOpen={(c) => {
          if (c.mine && c.hidden) onHideChat(c.id, false);
          void navigate(chatLink(id, c.id));
        }}
        onDelete={onDeleteChat}
      />
      {bot.people.length > 1 ? (
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
        on ? "opacity-100" : "opacity-0 group-hover/tab:opacity-100"
      }`}
    >
      <X size={13} />
    </button>
  );
}

// One side chat as a tab. A double-click on its name renames it in place.
function ChatTab({ botId, chat, on, doing, rename, onClose }: { botId: string; chat: Chat; on: boolean; doing?: Doing; rename: ChatRename; onClose: () => void }) {
  const editing = rename.editing === chat.id;
  const title = chat.main ? t("Main chat") : chat.title || t("New chat");
  return (
    <div className={`${tabBox} ${tone(on)} max-w-[220px] min-w-[104px]`}>
      <TabGround on={on} />
      {editing ? (
        <ChatTitleInput rename={rename} cid={chat.id} className={`${inputClass} relative mx-1.5 h-7 w-44 px-2 text-small`} />
      ) : (
        <>
          <Link
            {...chatLink(botId, chat.id)}
            data-tab-on={on || undefined}
            title={chat.mine ? `${title} · ${t("Double-click to rename")}` : title}
            onDoubleClick={(e) => {
              e.preventDefault();
              if (chat.mine) rename.begin(chat);
            }}
            className="relative flex h-full min-w-0 flex-1 items-center gap-2 rounded-t-control pr-8 pl-3 outline-offset-[-2px]"
          >
            {chat.mine ? null : chat.author ? <Initials email={chat.author} size={18} /> : <BotGlyph size={14} className="shrink-0 text-ink-3" aria-label={t("The Bot's own")} />}
            <span className="min-w-0 flex-1 truncate text-small font-medium">{title}</span>
            {doing ? <TabLamp doing={doing as BotActivity} /> : null}
          </Link>
          <CloseX on={on} onClick={onClose} />
        </>
      )}
    </div>
  );
}

// ClosedMenu is the arrow at the end of the tabs: the chats you closed,
// newest first, each a press from being a tab again or (armed) deleted; and
// for a manager the Team's chats, which only open.
function ClosedMenu({ botId, closed, team, onOpen, onDelete }: { botId: string; closed: Chat[]; team: Chat[]; onOpen: (c: Chat) => void; onDelete: (cid: string) => void }) {
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
  const total = closed.length + team.length;
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
                </div>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
