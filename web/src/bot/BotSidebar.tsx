import { ActivityStrip } from "./ActivityStrip";
import { Blocks, Brain, ChevronDown, ChevronRight, Inbox, LayoutGrid, Library, MessageCircle, Package, Pencil, Plus, Settings, Timer, Trash2 } from "lucide-react";
import { type ReactNode, useEffect, useMemo } from "react";
import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { pressClass } from "../Btn";
import { inputClass } from "../Field";
import { Collapse } from "../Collapse";
import { fullTime } from "../format";
import { UI, type Bot, type Chat } from "../gen/silo/v1/ui_pb";
import { Lamp, Status, StatusWord } from "../Lamp";
import { type ChatGroup, groupChats, groupKeyOf, isOpen } from "./chatGroups";
import { setGroupOpen, useGroupsPicked } from "./chatGroupsStore";
import { ChatTitleInput } from "./ChatTitleInput";
import { ActiveBar, SideLink } from "./SideNav";
import { onCustomize, onSettings, type Tab } from "./tabs";
import type { ChatRename } from "./useChatList";
import { t } from "../i18n.ts";

// What the open chat is doing, for its lamp: waiting on you, or working.
export type ChatLive = { waiting: boolean; sending: boolean; agentsBusy: boolean };

// The wide layout's left column, on every page of a Bot: its name and state,
// the Dashboard under them, New chat, the pages beside the chat, the chats
// folded into date groups, and Settings at the foot.
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
}) {
  // The main chat is pinned above the pages; the date groups are the side
  // chats.
  const main = chats.find((c) => c.main);
  const groups = useMemo(() => groupChats(chats.filter((c) => !c.main)), [chats]);
  const picked = useGroupsPicked();
  const dashQ = useQuery(UI.method.listDashboard, { botId: id });
  const freshCount = (dashQ.data?.widgets ?? []).filter((w) => w.fresh).length + (dashQ.data?.posts ?? []).length;

  // Landing on a chat (a new one, a link, a reply that moves it to Today)
  // opens the group it sits in, so the open row is never hidden. Folding that
  // group by hand afterwards sticks: this runs only when the chat or its group
  // changes.
  const openAt = tab === "run" ? chats.find((c) => c.id === chatId)?.updatedAt : undefined;
  const openKey = openAt === undefined ? "" : groupKeyOf(openAt);
  useEffect(() => {
    if (openKey) setGroupOpen(openKey, true);
  }, [chatId, openKey]);

  const busy = waiting || sending || agentsBusy;
  return (
    <aside className="hidden w-[248px] shrink-0 flex-col bg-well wide:flex">
      <BotHome
        botId={id}
        bot={bot}
        on={tab === "dashboard"}
        needs={pendingCount + bot.dashboardAsks}
        fresh={freshCount}
        curating={!!dashQ.data?.curating}
      />
      <ActivityStrip botId={id} bot={bot} />
      <div className="px-2 pb-3">
        <NewChat onClick={onNewChat} />
      </div>
      <nav className="space-y-0.5 px-2" aria-label={t("Pages")}>
        {main ? (
          <ChatRow
            botId={id}
            chat={main}
            on={tab === "run" && main.id === chatId}
            status={tab === "run" && main.id === chatId && busy ? (waiting ? "needs_you" : "working") : ""}
            rename={rename}
            onDelete={() => {}}
          />
        ) : null}
        <SideLink to="/bots/$botId/automations" botId={id} on={tab === "automations"} icon={Timer} label={t("Automations")} />
        <SideLink to="/bots/$botId/artifacts" botId={id} on={tab === "artifacts"} icon={Package} label={t("Artifacts")} />
        <SideLink to="/bots/$botId/memories" botId={id} on={tab === "memories"} icon={Brain} label={t("Memories")} />
        <SideLink to="/bots/$botId/knowledge" botId={id} on={tab === "knowledge"} icon={Library} label={t("Knowledge")} />
        <SideLink to="/bots/$botId/feed" botId={id} on={tab === "feed"} icon={Inbox} label={t("Feed")} badge={tab === "feed" ? 0 : bot.feedUnread} />
        <SideLink to="/bots/$botId/connectors" botId={id} on={onCustomize(tab)} icon={Blocks} label={t("Customize")} />
      </nav>
      <div className="mt-4 min-h-0 flex-1 overflow-auto px-2 pb-3">
        {groups.length === 0 && <p className="px-3 py-2 text-[12.5px] text-ink-3">{t("No other chats")}</p>}
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
                />
              );
            })}
          </ChatGroupSection>
        ))}
      </div>
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

// BotHome heads the sidebar: the Bot's name and state over its Dashboard, so
// the board reads as the Bot's front page rather than one more button. The
// whole block opens it; its second line says what is waiting there.
function BotHome({
  botId,
  bot,
  on,
  needs,
  fresh,
  curating,
}: {
  botId: string;
  bot: Bot;
  on: boolean;
  needs: number;
  fresh: number;
  curating: boolean;
}) {
  return (
    <div className="px-2 pt-2 pb-2">
      <Link
        to="/bots/$botId/dashboard"
        params={{ botId }}
        data-tab-on={on || undefined}
        className={`group/home relative block rounded-card px-3 pt-2.5 pb-2 transition-[background-color,box-shadow] duration-[160ms] ease-quiet outline-offset-[-2px] ${
          on ? "bg-surface shadow-card" : "hover:bg-pressed"
        }`}
      >
        <ActiveBar on={on} />
        <div className="flex items-center gap-3">
          <h1 className="min-w-0 flex-1 truncate text-card-title leading-6">{bot.name}</h1>
          <span className="flex shrink-0 items-center gap-2">
            <Lamp status={bot.status} />
            <StatusWord status={bot.status} />
          </span>
        </div>
        <div
          className={`mt-1.5 flex h-6 items-center gap-2 text-[12.5px] leading-4 transition-colors duration-[160ms] ease-quiet ${
            on ? "text-ink" : "text-ink-2 group-hover/home:text-ink"
          }`}
        >
          <LayoutGrid size={13} className={on ? "text-cobalt" : "text-ink-3 group-hover/home:text-cobalt"} />
          <span className="font-medium">{t("Dashboard")}</span>
          <span className="flex min-w-0 flex-1 items-center justify-end gap-1.5 truncate">
            <Digest needs={needs} fresh={fresh} curating={curating} />
          </span>
          <ChevronRight
            size={13}
            className="-mr-1 shrink-0 text-ink-3 opacity-0 transition-[opacity,transform] duration-[160ms] ease-quiet group-hover/home:translate-x-0.5 group-hover/home:opacity-100 motion-reduce:transition-opacity"
          />
        </div>
      </Link>
    </div>
  );
}

// Digest is the one thing worth saying about the board right now: what needs
// the owner first, then what changed, then that the Bot is curating it.
function Digest({ needs, fresh, curating }: { needs: number; fresh: number; curating: boolean }) {
  if (needs > 0) {
    return (
      <span className="rise flex items-center gap-1.5 font-medium text-vermilion">
        <span aria-hidden className="h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
        <span className="truncate">{t("{{count}} need you", { count: needs })}</span>
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
  return null;
}

// New chat is the sidebar's lifted control to start a conversation: a full-width
// surface button whose plus sits in a glyph well and turns a quarter on hover.
function NewChat({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`group/new flex h-10 w-full items-center gap-2.5 rounded-control bg-surface pr-3 pl-2 text-[13.5px] font-medium text-ink shadow-card hover:shadow-float ${pressClass}`}
    >
      <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-xs bg-ink text-on-ink">
        <Plus size={15} strokeWidth={2.25} className="transition-transform duration-[280ms] ease-quiet group-hover/new:rotate-90 motion-reduce:transition-none" />
      </span>
      {t("New chat")}
    </button>
  );
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
        className="group/head sticky top-0 z-10 flex h-7 w-full items-center gap-1.5 rounded-sm bg-well px-3 text-left text-label-caps leading-4 text-ink-3 uppercase transition-colors duration-[160ms] ease-quiet outline-offset-[-2px] hover:text-ink"
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

// One chat: a single quiet line. The open one is lifted by tone and marked
// with the cobalt bar; rename and delete fade in over the end of the title.
function ChatRow({
  botId,
  chat,
  on,
  status,
  rename,
  onDelete,
}: {
  botId: string;
  chat: Chat;
  on: boolean;
  // The lamp state of the open chat while it works or waits; "" otherwise.
  status: "needs_you" | "working" | "";
  rename: ChatRename;
  onDelete: () => void;
}) {
  const editing = rename.editing === chat.id;
  const ms = Date.parse(chat.updatedAt);
  // The main chat keeps its name and cannot be deleted.
  const fixed = chat.main;
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
            if (fixed) return;
            e.preventDefault();
            rename.begin(chat);
          }}
          className="flex h-8 min-w-0 items-center gap-2 rounded-control pr-2 pl-3 outline-offset-[-2px]"
        >
          {fixed ? <MessageCircle size={15} className={`shrink-0 ${on ? "text-ink" : "text-ink-2"}`} aria-hidden /> : null}
          <span
            className={`min-w-0 flex-1 truncate text-[13.5px] leading-5 transition-colors duration-[160ms] ease-quiet ${
              on ? "text-ink" : "text-ink-2 group-hover:text-ink"
            }`}
          >
            {fixed ? t("Main chat") : chat.title || t("New chat")}
          </span>
          {status ? <Status status={status} className="shrink-0" /> : null}
        </Link>
      )}
      {!editing && !fixed && (
        <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center gap-0.5 rounded-r-control bg-linear-to-l from-(--row) from-70% to-transparent pr-1 pl-6 opacity-0 transition-opacity duration-[160ms] ease-quiet group-hover:pointer-events-auto group-hover:opacity-100 group-has-[:focus-visible]:pointer-events-auto group-has-[:focus-visible]:opacity-100">
          <button
            type="button"
            title={t("Rename chat")}
            aria-label={t("Rename chat")}
            className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
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
            className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-vermilion-pale hover:text-vermilion"
            onClick={onDelete}
          >
            <Trash2 size={13} />
          </button>
        </span>
      )}
    </div>
  );
}
