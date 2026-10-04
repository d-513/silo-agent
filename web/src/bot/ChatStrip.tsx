import { Brain, Inbox, Library, Pencil, SquarePen, Timer, Trash2 } from "lucide-react";
import { NavLink } from "react-router-dom";
import { Btn } from "../Btn";
import { inputClass } from "../Field";
import type { Bot, Chat } from "../gen/silo/v1/ui_pb";
import { Lamp } from "../Lamp";
import { ChatTitleInput } from "./ChatTitleInput";
import { FadeScroll } from "./FadeScroll";
import { SideChip } from "./SideNav";
import type { Tab } from "./tabs";
import type { ChatRename } from "./useChatList";

// The narrow layout's chats: a sideways strip with the conversation-side pages
// as icon chips at its start.
export function ChatStrip({
  id,
  bot,
  tab,
  chatId,
  chats,
  rename,
  busy,
  onNewChat,
  onDeleteChat,
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
}) {
  const { waiting, sending } = busy;
  return (
    <FadeScroll className="shrink-0 bg-well shadow-[inset_0_-1px_0_var(--color-line)] wide:hidden" innerClass="flex items-center gap-1 px-2 py-1.5" fade="from-well">
      <Btn kind="ghost" size="sm" iconOnly title="New chat" aria-label="New chat" className="h-10 w-10" icon={<SquarePen size={15} />} onClick={onNewChat} />
      <SideChip to={`/bots/${id}/automations`} on={tab === "automations"} icon={Timer} label="Automations" />
      <SideChip to={`/bots/${id}/memories`} on={tab === "memories"} icon={Brain} label="Memories" />
      <SideChip to={`/bots/${id}/knowledge`} on={tab === "knowledge"} icon={Library} label="Knowledge" />
      <SideChip to={`/bots/${id}/feed`} on={tab === "feed"} icon={Inbox} label="Feed" badge={bot.feedUnread} />
      <span aria-hidden className="mx-1 h-5 w-px shrink-0 bg-line-strong" />
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
              <NavLink
                to={`/bots/${id}/run/${c.id}`}
                onDoubleClick={(e) => {
                  e.preventDefault();
                  rename.begin(c);
                }}
                className={`flex h-10 max-w-[11rem] items-center gap-2 px-3 ${on ? "" : "rounded-control hover:bg-pressed hover:text-ink"}`}
              >
                <span className="truncate">{c.title || "New chat"}</span>
                {on && (waiting || sending) ? <Lamp status={waiting ? "needs_you" : "working"} /> : null}
              </NavLink>
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
