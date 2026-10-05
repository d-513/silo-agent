import { Brain, Inbox, Library, Pencil, SquarePen, Timer, Trash2 } from "lucide-react";
import { Link } from "@tanstack/react-router";
import { Btn } from "../Btn";
import { inputClass } from "../Field";
import { chatWhen } from "../format";
import type { Bot, Chat } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { ChatTitleInput } from "./ChatTitleInput";
import { SideLink } from "./SideNav";
import type { Tab } from "./tabs";
import type { ChatRename } from "./useChatList";

// What the open chat is doing, for its lamp: waiting on you, or working.
export type ChatLive = { waiting: boolean; sending: boolean; agentsBusy: boolean };

// The wide layout's left column: the conversation-side pages, then the chats.
export function ChatSidebar({
  id,
  bot,
  tab,
  chatId,
  chats,
  rename,
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
  live: ChatLive;
  onNewChat: () => void;
  onDeleteChat: (cid: string) => void;
}) {
  return (
    <aside className="hidden w-[248px] shrink-0 flex-col bg-well wide:flex">
      <nav className="space-y-0.5 px-2 pt-2" aria-label="Conversation">
        <SideLink to="/bots/$botId/automations" botId={id} on={tab === "automations"} icon={Timer} label="Automations" />
        <SideLink to="/bots/$botId/memories" botId={id} on={tab === "memories"} icon={Brain} label="Memories" />
        <SideLink to="/bots/$botId/knowledge" botId={id} on={tab === "knowledge"} icon={Library} label="Knowledge" />
        <SideLink to="/bots/$botId/feed" botId={id} on={tab === "feed"} icon={Inbox} label="Feed" badge={tab === "feed" ? 0 : bot.feedUnread} />
      </nav>
      <div className="flex h-12 items-center justify-between pr-2 pl-4">
        <span className="text-label-caps leading-4 text-ink-3 uppercase">Chats</span>
        <Btn kind="ghost" size="sm" iconOnly title="New chat" aria-label="New chat" icon={<SquarePen size={15} />} onClick={onNewChat} />
      </div>
      <div className="min-h-0 flex-1 space-y-0.5 overflow-auto px-2 pb-3">
        {chats.length === 0 && <p className="px-2 py-2 text-[12.5px] text-ink-3">No chats</p>}
        {chats.map((c) => {
          const on = tab === "run" && c.id === chatId;
          const lit = on && (waiting || sending || agentsBusy);
          return (
            <div
              key={c.id}
              className={`group relative rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${ on ? "bg-surface shadow-card" : "hover:bg-pressed"
              }`}
            >
              {rename.editing === c.id ? (
                <div className="px-1.5 py-1.5">
                  <ChatTitleInput rename={rename} cid={c.id} className={`${inputClass} h-8 px-2 text-[13.5px]`} />
                </div>
              ) : (
                <Link
                  to="/bots/$botId/run/$chatId"
                  params={{ botId: id, chatId: c.id }}
                  onDoubleClick={(e) => {
                    e.preventDefault();
                    rename.begin(c);
                  }}
                  className="block min-w-0 rounded-control px-3 py-2 pr-14"
                >
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="truncate text-[13.5px] leading-5 font-medium text-ink">{c.title || "New chat"}</span>
                    {lit ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
                  </span>
                  <span className={`block truncate text-[12.5px] leading-[18px] ${waiting && on ? "text-vermilion" : "text-ink-3"}`}>
                    {on && waiting ? "Waiting for you" : on && sending ? "Working…" : chatWhen(c.updatedAt)}
                  </span>
                </Link>
              )}
              {rename.editing !== c.id && (
                <span className="absolute top-1.5 right-1.5 hidden items-center group-focus-within:flex group-hover:flex">
                  <button
                    type="button"
                    title="Rename chat"
                    className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
                    onClick={(e) => {
                      e.preventDefault();
                      rename.begin(c);
                    }}
                  >
                    <Pencil size={13} />
                  </button>
                  <button
                    type="button"
                    title="Delete chat"
                    className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-colors duration-[160ms] hover:bg-vermilion-pale hover:text-vermilion"
                    onClick={() => onDeleteChat(c.id)}
                  >
                    <Trash2 size={13} />
                  </button>
                </span>
              )}
            </div>
          );
        })}
      </div>
    </aside>
  );
}
