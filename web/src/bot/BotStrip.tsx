import { Blocks, Brain, Folder, Inbox, Library, Mail, Monitor, Pencil, Plus, Power, Settings, SquareTerminal, Timer, Trash2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Btn } from "../Btn";
import { inputClass } from "../Field";
import type { Bot, Chat } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { ChatTitleInput } from "./ChatTitleInput";
import { FadeScroll } from "./FadeScroll";
import { SideChip } from "./SideNav";
import { onCustomize, onSettings, type Tab } from "./tabs";
import type { ChatRename } from "./useChatList";

// The narrow layout's way around a Bot: one sideways strip with New chat, every
// page as an icon chip (the machine panes too, which have no room to dock
// here), Start/Stop, and then the chats.
export function BotStrip({
  id,
  bot,
  tab,
  chatId,
  chats,
  rename,
  busy,
  onNewChat,
  onDeleteChat,
  onStart,
  onStop,
}: {
  id: string;
  bot: Bot;
  tab: Tab;
  chatId?: string;
  chats: Chat[];
  rename: ChatRename;
  // The open chat is waiting on you or working.
  busy: { waiting: boolean; sending: boolean };
  onNewChat: () => void;
  onDeleteChat: (cid: string) => void;
  onStart: () => void;
  onStop: () => void;
}) {
  const { waiting, sending } = busy;
  const starting = bot.status === "starting";
  const power = bot.workerConnected ? "Stop Bot" : starting ? "Starting…" : "Start Bot";
  const rule = <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-line-strong" />;
  return (
    <FadeScroll className="shrink-0 bg-well shadow-[inset_0_-1px_0_var(--color-line)] wide:hidden" innerClass="flex items-center gap-1 px-2 py-1.5" fade="from-well">
      <Btn kind="primary" size="sm" iconOnly title="New chat" aria-label="New chat" className="h-10 w-10" icon={<Plus size={17} />} onClick={onNewChat} />
      <SideChip to="/bots/$botId/automations" botId={id} on={tab === "automations"} icon={Timer} label="Automations" />
      <SideChip to="/bots/$botId/memories" botId={id} on={tab === "memories"} icon={Brain} label="Memories" />
      <SideChip to="/bots/$botId/knowledge" botId={id} on={tab === "knowledge"} icon={Library} label="Knowledge" />
      <SideChip to="/bots/$botId/feed" botId={id} on={tab === "feed"} icon={Inbox} label="Feed" badge={bot.feedUnread} />
      <SideChip to="/bots/$botId/connectors" botId={id} on={onCustomize(tab)} icon={Blocks} label="Customize" />
      <SideChip to="/bots/$botId/settings" botId={id} on={onSettings(tab)} icon={Settings} label="Settings" />
      {rule}
      <SideChip to="/bots/$botId/files" botId={id} on={tab === "files"} icon={Folder} label="Files" />
      <SideChip to="/bots/$botId/desktop" botId={id} on={tab === "desktop"} icon={Monitor} label="Desktop" />
      <SideChip to="/bots/$botId/console" botId={id} on={tab === "console"} icon={SquareTerminal} label="Console" />
      {bot.mailAddress || tab === "mail" ? <SideChip to="/bots/$botId/mail" botId={id} on={tab === "mail"} icon={Mail} label="Mail" /> : null}
      <Btn kind="ghost" size="sm" iconOnly title={power} aria-label={power} className="h-10 w-10" disabled={!bot.workerConnected && starting} icon={<Power size={15} />} onClick={bot.workerConnected ? onStop : onStart} />
      {rule}
      {chats.map((c) => {
        const on = tab === "run" && c.id === chatId;
        return (
          <div
            key={c.id}
            className={`flex h-10 shrink-0 items-center rounded-control text-[13px] font-medium ${ on ? "bg-surface text-ink shadow-card" : "text-ink-2"
            }`}
          >
            {rename.editing === c.id ? (
              <ChatTitleInput rename={rename} cid={c.id} className={`${inputClass} h-8 w-40 px-2`} />
            ) : (
              <Link
                to="/bots/$botId/run/$chatId"
                params={{ botId: id, chatId: c.id }}
                onDoubleClick={(e) => {
                  e.preventDefault();
                  rename.begin(c);
                }}
                className={`flex h-10 max-w-[11rem] items-center gap-2 px-3 ${on ? "" : "rounded-control hover:bg-pressed hover:text-ink"}`}
              >
                <span className="truncate">{c.title || "New chat"}</span>
                {on && (waiting || sending) ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
              </Link>
            )}
            {on && rename.editing !== c.id && (
              <button
                type="button"
                title="Rename chat"
                className="flex h-10 w-8 items-center justify-center text-ink-2 hover:text-ink"
                onClick={(e) => {
                  e.preventDefault();
                  rename.begin(c);
                }}
              >
                <Pencil size={13} />
              </button>
            )}
            {on && (
              <button type="button" title="Delete chat" className="flex h-10 w-8 items-center justify-center text-ink-2 hover:text-vermilion" onClick={() => onDeleteChat(c.id)}>
                <Trash2 size={13} />
              </button>
            )}
          </div>
        );
      })}
    </FadeScroll>
  );
}
