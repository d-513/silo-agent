import { ActivityStrip } from "./ActivityStrip";
import { Bot as BotGlyph, Brain, ChevronDown, ChevronRight, Inbox, LayoutGrid, Library, MessageCircle, MessageCircleQuestionMark, Package, Pencil, Phone, Plus, Radio, Settings, Timer, Trash2 } from "lucide-react";
import { type ReactNode, useEffect, useMemo } from "react";
import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { pressClass } from "../Btn";
import { Clock } from "../call/CallBar";
import type { Call } from "../call/useCall";
import { Crest } from "../Crest";
import { inputClass } from "../Field";
import { Collapse } from "../Collapse";
import { ago, fullTime } from "../format";
import { UI, type Bot, type Chat } from "../gen/silo/v1/ui_pb";
import { Lamp, Status, StatusWord } from "../Lamp";
import { type ChatGroup, groupChats, groupKeyOf, isOpen } from "./chatGroups";
import { setGroupOpen, useGroupsPicked } from "./chatGroupsStore";
import { ChatTitleInput } from "./ChatTitleInput";
import { ActiveBar, SideLink } from "./SideNav";
import { onSettings, type Tab } from "./tabs";
import { toolMeta } from "../thread/toolInfo";
import type { ChatRename } from "./useChatList";
import { t } from "../i18n.ts";
import { Initials } from "../space/People";
import { Tip, TipTitle } from "../Tip";

// What the open chat is doing, for its lamp: waiting on you, or working.
export type ChatLive = { waiting: boolean; sending: boolean; agentsBusy: boolean };

// The wide layout's left column, on every page of a Bot: its name and state,
// the home card (the main chat with its Call, and the Dashboard), the pages
// beside the chat, the side chats folded into date groups under their own
// New side chat, and Settings at the foot. In the narrow layout it
// is the body of the Bot's drawer (`drawer`): one scroll for all of it, rows
// tall enough for a thumb, and the machine's panes (`machine`) under the pages.
export function BotSidebar({
  id,
  bot,
  tab,
  chatId,
  chats,
  rename,
  pendingCount = 0,
  live: { waiting, sending, agentsBusy },
  onNewChat,
  onDeleteChat,
  call,
  onCall,
  drawer = false,
  machine,
}: {
  id: string;
  bot: Bot;
  tab: Tab;
  chatId?: string;
  chats: Chat[];
  rename: ChatRename;
  pendingCount?: number;
  live: ChatLive;
  onNewChat: () => void;
  onDeleteChat: (cid: string) => void;
  // The Bot's call while calls are on; undefined hides Call.
  call?: Call;
  onCall: () => void;
  drawer?: boolean;
  machine?: ReactNode;
}) {
  // The main chat is pinned above the pages; the date groups are the side
  // chats.
  const main = chats.find((c) => c.main && c.mine);
  // Your own side chats by date; on a Bot several people use, its managers
  // also get the Team: everyone else's chats and the Bot's own, read-only.
  const groups = useMemo(() => {
    const own = groupChats(chats.filter((c) => c.mine && !c.main));
    const others = chats.filter((c) => !c.mine);
    return others.length ? [...own, { key: "team", label: t("Team"), chats: others }] : own;
  }, [chats]);
  const picked = useGroupsPicked();
  const dashQ = useQuery(UI.method.listDashboard, { botId: id });
  const freshCount = (dashQ.data?.widgets ?? []).filter((w) => w.fresh).length + (dashQ.data?.posts ?? []).length;

  // Landing on a chat (a new one, a link, a reply that moves it to Today)
  // opens the group it sits in, so the open row is never hidden. Folding that
  // group by hand afterwards sticks: this runs only when the chat or its group
  // changes.
  // The main chat sits in no group.
  const openAt = tab === "run" && chatId !== main?.id ? chats.find((c) => c.id === chatId)?.updatedAt : undefined;
  const openKey = openAt === undefined ? "" : groupKeyOf(openAt);
  useEffect(() => {
    if (openKey) setGroupOpen(openKey, true);
  }, [chatId, openKey]);

  const busy = waiting || sending || agentsBusy;
  // The main chat's lamp: waiting on the owner while open, else working
  // whenever a run is live there, open or not.
  const mainOpen = tab === "run" && !!main && main.id === chatId;
  const mainStatus = mainOpen && waiting ? "needs_you" : (mainOpen && busy) || bot.activity.some((a) => a.kind === "main") ? "working" : "";
  return (
    <aside className={drawer ? "flex min-h-0 flex-1 flex-col bg-well" : "hidden w-[248px] shrink-0 flex-col bg-well wide:flex"}>
      <Scroll drawer={drawer}>
        <BotHead bot={bot} />
        <div className="px-2 pb-3">
          <HomeCard
            botId={id}
            bot={bot}
            main={main}
            mainOn={mainOpen}
            mainStatus={mainStatus}
            dashOn={tab === "dashboard"}
            needs={pendingCount}
            asks={bot.dashboardAsks}
            fresh={freshCount}
            curating={!!dashQ.data?.curating}
            call={call}
            onCall={onCall}
          />
        </div>
        <ActivityStrip bot={bot} />
        <nav className="space-y-0.5 px-2" aria-label={t("Pages")}>
          <SideLink to="/bots/$botId/automations" botId={id} on={tab === "automations"} icon={Timer} label={t("Automations")} />
          <SideLink to="/bots/$botId/artifacts" botId={id} on={tab === "artifacts"} icon={Package} label={t("Artifacts")} />
          <SideLink to="/bots/$botId/memories" botId={id} on={tab === "memories"} icon={Brain} label={t("Memories")} />
          <SideLink to="/bots/$botId/knowledge" botId={id} on={tab === "knowledge"} icon={Library} label={t("Knowledge")} />
          <SideLink to="/bots/$botId/feed" botId={id} on={tab === "feed"} icon={Inbox} label={t("Feed")} badge={tab === "feed" ? 0 : bot.feedUnread} />
          <SideLink to="/bots/$botId/channels" botId={id} on={tab === "channels"} icon={Radio} label={t("Channels")} />
        </nav>
        {machine}
        <SideChatsHead onNewChat={onNewChat} />
        <div className={drawer ? "px-2 pb-3" : "min-h-0 flex-1 overflow-auto px-2 pb-3"}>
          {groups.map((g) => (
            <ChatGroupSection key={g.key} group={g} open={isOpen(g.key, picked)}>
              {g.chats.map((c) => {
                const on = tab === "run" && c.id === chatId;
                return (
                  <ChatRow
                    key={c.id}
                    botId={id}
                    chat={c}
                    on={on}
                    status={on && busy ? (waiting ? "needs_you" : "working") : ""}
                    rename={rename}
                    onDelete={() => onDeleteChat(c.id)}
                    team={g.key === "team"}
                    touch={drawer}
                  />
                );
              })}
            </ChatGroupSection>
          ))}
        </div>
      </Scroll>
      <nav className="shrink-0 p-2 shadow-[inset_0_1px_0_var(--color-line)]" aria-label={t("Bot")}>
        <SideLink
          to="/bots/$botId/settings"
          botId={id}
          on={onSettings(tab)}
          icon={Settings}
          label={t("Settings")}
          badge={onSettings(tab) ? 0 : bot.auditUnseen}
        />
      </nav>
    </aside>
  );
}

// Scroll is the drawer's one scroll; the wide column scrolls only its chats.
function Scroll({ drawer, children }: { drawer: boolean; children: ReactNode }) {
  if (!drawer) return <>{children}</>;
  return <div className="min-h-0 flex-1 overflow-auto overscroll-contain">{children}</div>;
}

// BotHead heads the sidebar: the Bot's face, name and state. It is not a
// link; the home card under it holds where the Bot lives.
function BotHead({ bot }: { bot: Bot }) {
  return (
    <div className="flex items-center gap-2.5 px-4 pt-3.5 pb-2.5">
      <Crest index={bot.crest} size={28} />
      <span className="min-w-0 flex-1">
        <h1 className="truncate text-card-title leading-5">{bot.name}</h1>
        <span className="flex items-center gap-1.5">
          <Lamp status={bot.status} />
          <StatusWord status={bot.status} />
        </span>
      </span>
      {bot.people.length > 1 ? <People people={bot.people} /> : null}
    </div>
  );
}

// HomeCard is the Bot's two standing places, lifted as one card: the main
// chat (one per person, where it opens and where replies land) with Call
// beside it, and the Dashboard. Each row says what is there now.
function HomeCard({
  botId,
  bot,
  main,
  mainOn,
  mainStatus,
  dashOn,
  needs,
  asks,
  fresh,
  curating,
  call,
  onCall,
}: {
  botId: string;
  bot: Bot;
  main?: Chat;
  mainOn: boolean;
  mainStatus: "needs_you" | "working" | "";
  dashOn: boolean;
  needs: number;
  asks: number;
  fresh: number;
  curating: boolean;
  call?: Call;
  onCall: () => void;
}) {
  const row = "relative flex items-center gap-2.5 outline-offset-[-2px] transition-[background-color] duration-[160ms] ease-quiet";
  const onCallHere = !!call?.live && !!main && call.snap?.chatId === main.id;
  return (
    <div className="overflow-hidden rounded-card bg-surface shadow-float">
      <div className={`${row} pr-2 ${mainOn ? "" : "hover:bg-well/60"}`}>
        <ActiveBar on={mainOn} />
        {main ? (
          <Link
            to="/bots/$botId/run/$chatId"
            params={{ botId, chatId: main.id }}
            data-tab-on={mainOn || undefined}
            className="flex min-w-0 flex-1 items-center gap-2.5 py-2 pl-2.5 outline-offset-[-2px] in-data-drawer:py-2.5"
          >
            <span className="flex h-7 w-7 shrink-0 items-center justify-center rounded-xs bg-ink text-on-ink">
              <MessageCircle size={14} strokeWidth={2.25} />
            </span>
            <span className="min-w-0 flex-1">
              <span className="block truncate text-[13.5px] leading-5 font-medium text-ink">{t("Main chat")}</span>
              <MainLine bot={bot} at={main.updatedAt} status={mainStatus} onCall={onCallHere} />
            </span>
          </Link>
        ) : (
          <span className="h-[52px] flex-1" />
        )}
        {call && main ? <CallButton call={call} onCall={onCall} /> : null}
      </div>
      <div aria-hidden className="mx-2.5 h-px bg-line" />
      <Link
        to="/bots/$botId/dashboard"
        params={{ botId }}
        data-tab-on={dashOn || undefined}
        className={`group/home ${row} py-2 pr-2.5 pl-2.5 in-data-drawer:py-2.5 ${dashOn ? "" : "hover:bg-well/60"}`}
      >
        <ActiveBar on={dashOn} />
        <span className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-xs bg-well shadow-[inset_0_0_0_1px_var(--color-line)] ${dashOn ? "text-cobalt" : "text-ink-2"}`}>
          <LayoutGrid size={14} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[13.5px] leading-5 font-medium text-ink">{t("Dashboard")}</span>
          <span className="flex h-[18px] items-center text-[12.5px] leading-[18px] text-ink-3">
            <Digest needs={needs} asks={asks} fresh={fresh} curating={curating} />
          </span>
        </span>
        <ChevronRight size={14} className="shrink-0 text-ink-3 transition-transform duration-[160ms] ease-quiet group-hover/home:translate-x-0.5 motion-reduce:transition-none" />
      </Link>
    </div>
  );
}

// MainLine is what the main chat is doing now: a call, a question waiting,
// work (with the tool it is in), else when it last moved.
function MainLine({ bot, at, status, onCall }: { bot: Bot; at: string; status: "needs_you" | "working" | ""; onCall: boolean }) {
  const cls = "block truncate text-[12.5px] leading-[18px]";
  if (onCall) return <span className={`${cls} text-emerald`}>{t("On a call")}</span>;
  if (status === "needs_you") return <span className={`${cls} font-medium text-vermilion`}>{t("Needs you")}</span>;
  if (status === "working") {
    const step = bot.activity.find((a) => a.kind === "main")?.step;
    return <span className={`${cls} text-emerald`}>{step ? t("Working: {{what}}", { what: toolMeta(step).app || step }) : t("Working…")}</span>;
  }
  return <span className={`${cls} text-ink-3`}>{at ? t("Last active {{when}}", { when: ago(at) }) : "\u00a0"}</span>;
}

// CallButton rings the Bot in the main chat. It carries the call: Calling…
// while it connects, then the call's clock, which leads back to it.
function CallButton({ call, onCall }: { call: Call; onCall: () => void }) {
  const s = call.live ? call.snap : null;
  const base = `flex h-8 shrink-0 items-center gap-1.5 rounded-control px-2.5 text-[12.5px] font-medium ${pressClass}`;
  if (s?.state === "connecting") {
    return (
      <button type="button" onClick={onCall} className={`${base} bg-well text-ink-2`}>
        <span aria-hidden className="breathe h-1.5 w-1.5 rounded-full bg-ink-3" />
        {t("Calling…")}
      </button>
    );
  }
  if (s) {
    return (
      <Tip content={t("Back to the call")}>
        <button type="button" onClick={onCall} aria-label={t("Back to the call")} className={`${base} bg-well text-emerald`}>
          <Lamp status="working" />
          <Clock since={s.startedAt} />
        </button>
      </Tip>
    );
  }
  return (
    <Tip
      content={
        <>
          <TipTitle>{t("Call")}</TipTitle>
          <p className="mt-1">{t("A voice call in your main chat. What you both say is written there.")}</p>
        </>
      }
    >
      <button type="button" onClick={onCall} className={`${base} bg-ink text-on-ink hover:bg-ink-deep`}>
        <Phone size={13} strokeWidth={2.25} />
        {t("Call")}
      </button>
    </Tip>
  );
}

// SideChatsHead names the side chats and holds the one way to start one.
function SideChatsHead({ onNewChat }: { onNewChat: () => void }) {
  return (
    <div className="mt-4 flex h-8 items-center gap-2 pr-2 pl-5 in-data-drawer:h-10">
      <h2 className="min-w-0 flex-1 truncate text-[12.5px] font-medium text-ink-2">{t("Side chats")}</h2>
      <Tip content={t("New side chat")}>
        <button
          type="button"
          onClick={onNewChat}
          aria-label={t("New side chat")}
          className={`group/new flex h-7 w-7 shrink-0 items-center justify-center rounded-sm bg-surface text-ink-2 shadow-card hover:text-ink ${pressClass}`}
        >
          <Plus size={14} strokeWidth={2.25} className="transition-transform duration-[280ms] ease-quiet group-hover/new:rotate-90 motion-reduce:transition-none" />
        </button>
      </Tip>
    </div>
  );
}

// People is the faces of a Bot several people use: up to three, then +N,
// with everyone and their role in a Tip.
function People({ people }: { people: Bot["people"] }) {
  return (
    <Tip
      content={
        <ul className="space-y-0.5 text-[12.5px]">
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
        {people.length > 3 ? <span className="pl-2 text-[11px] text-ink-3">{`+${people.length - 3}`}</span> : null}
      </span>
    </Tip>
  );
}

// Digest is the one thing worth saying about the board right now: what needs
// the owner first, then what changed, then that the Bot is curating it, else
// that nothing is new (so the row keeps the main chat's two lines).
function Digest({ needs, asks, fresh, curating }: { needs: number; asks: number; fresh: number; curating: boolean }) {
  // Approvals are Needs you (a run is paused on them); the Bot's questions
  // only wait, so they are cobalt like anything else that is new there.
  if (needs > 0) {
    return (
      <span className="rise flex items-center gap-1.5 font-medium text-vermilion">
        <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
        <span className="truncate">{t("{{count}} need you", { count: needs })}</span>
      </span>
    );
  }
  if (asks > 0) {
    return (
      <span className="rise flex items-center gap-1.5 font-medium text-cobalt">
        <MessageCircleQuestionMark size={13} className="shrink-0" />
        <span className="truncate">{t("{{count}} questions", { count: asks })}</span>
      </span>
    );
  }
  if (fresh > 0) {
    return (
      <span className="rise flex items-center gap-1.5 text-cobalt">
        <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" />
        <span className="truncate">{t("{{count}} new", { count: fresh })}</span>
      </span>
    );
  }
  if (curating) {
    return (
      <span className="flex items-center gap-1.5 text-ink-3">
        <Lamp status="working" />
        <span className="truncate">{t("Curating")}</span>
      </span>
    );
  }
  return <span className="truncate text-ink-3">{t("Nothing new")}</span>;
}

// One date group: a header that folds it, and its rows. The header is a
// section label (small caps, not a row), so it never reads as one more chat;
// it sticks to the top while its rows scroll under it.
function ChatGroupSection({ group, open, children }: { group: ChatGroup<Chat>; open: boolean; children: ReactNode }) {
  return (
    <section aria-label={group.label} className="mt-3 first:mt-0">
      <button
        type="button"
        aria-expanded={open}
        onClick={() => setGroupOpen(group.key, !open)}
        className="group/head sticky top-0 z-10 flex h-7 in-data-drawer:h-9 w-full items-center gap-1.5 rounded-sm bg-well px-3 text-left text-label-caps leading-4 text-ink-3 uppercase transition-colors duration-[160ms] ease-quiet outline-offset-[-2px] hover:text-ink"
      >
        <span className="min-w-0 flex-1 truncate">{group.label}</span>
        <span
          className={`tracking-normal tabular-nums transition-opacity duration-[160ms] ease-quiet ${open ? "opacity-0 group-hover/head:opacity-100" : "opacity-100"}`}
        >
          {group.chats.length}
        </span>
        <ChevronDown size={13} className={`transition-transform duration-200 ease-quiet motion-reduce:transition-none ${open ? "" : "-rotate-90"}`} />
      </button>
      <Collapse open={open}>
        <div className="space-y-px pb-1.5">{children}</div>
      </Collapse>
    </section>
  );
}

// One side chat: a single quiet line (the main chat is in HomeCard, above). The open one is lifted by tone and marked
// with the cobalt bar; rename and delete fade in over the end of the title.
function ChatRow({
  botId,
  chat,
  on,
  status,
  rename,
  onDelete,
  team = false,
  touch = false,
}: {
  botId: string;
  chat: Chat;
  on: boolean;
  // The lamp state of the open chat while it works or waits; "" otherwise.
  status: "needs_you" | "working" | "";
  rename: ChatRename;
  onDelete: () => void;
  // A row of the Team group: someone else's chat, or the Bot's own. It
  // reads only: no rename, no delete.
  team?: boolean;
  // In the drawer: there is no hover, so the open row shows its actions.
  touch?: boolean;
}) {
  const editing = rename.editing === chat.id;
  const ms = Date.parse(chat.updatedAt);
  return (
    <div
      className={`group relative rounded-control bg-(--row) transition-[background-color] duration-[160ms] ease-quiet ${
        on ? "[--row:var(--color-surface)]" : "[--row:var(--color-well)] hover:[--row:var(--color-pressed)]"
      }`}
    >
      <ActiveBar on={on} />
      {editing ? (
        <div className="px-1">
          <ChatTitleInput rename={rename} cid={chat.id} className={`${inputClass} h-8 px-2 text-[13.5px]`} />
        </div>
      ) : (
        <Link
          to="/bots/$botId/run/$chatId"
          params={{ botId, chatId: chat.id }}
          title={Number.isFinite(ms) ? fullTime(new Date(ms)) : undefined}
          onDoubleClick={(e) => {
            e.preventDefault();
            if (!team) rename.begin(chat);
          }}
          className={`flex min-w-0 items-center gap-2 rounded-control pl-3 outline-offset-[-2px] ${touch ? "h-10" : "h-8"} ${touch && on && !team ? "pr-[76px]" : "pr-2"}`}
        >
          {team ? chat.author ? <Initials email={chat.author} size={18} /> : <BotGlyph size={14} className="shrink-0 text-ink-3" aria-label={t("The Bot's own")} /> : null}
          <span
            className={`min-w-0 flex-1 truncate text-[13.5px] leading-5 transition-colors duration-[160ms] ease-quiet ${
              on ? "text-ink" : "text-ink-2 group-hover:text-ink"
            }`}
          >
            {chat.title || t("New chat")}
          </span>
          {status ? <Status status={status} className="shrink-0" /> : null}
        </Link>
      )}
      {!editing && !team && (
        <span
          className={`absolute inset-y-0 right-0 flex items-center gap-0.5 rounded-r-control bg-linear-to-l from-(--row) from-70% to-transparent pr-1 pl-6 transition-opacity duration-[160ms] ease-quiet ${
            touch
              ? on
                ? ""
                : "hidden"
              : `pointer-events-none opacity-0 group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100 ${
                  on ? "[@media(hover:none)]:pointer-events-auto [@media(hover:none)]:opacity-100" : ""
                }`
          }`}
        >
          <button
            type="button"
            title={t("Rename chat")}
            aria-label={t("Rename chat")}
            className={`flex items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink ${touch ? "h-9 w-8" : "h-7 w-7"}`}
            onClick={(e) => {
              e.preventDefault();
              rename.begin(chat);
            }}
          >
            <Pencil size={13} />
          </button>
          <button
            type="button"
            title={t("Delete chat")}
            aria-label={t("Delete chat")}
            className={`flex items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-vermilion-pale hover:text-vermilion ${touch ? "h-9 w-8" : "h-7 w-7"}`}
            onClick={onDelete}
          >
            <Trash2 size={13} />
          </button>
        </span>
      )}
    </div>
  );
}
