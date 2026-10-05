import { Radio, X } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { UI, type Channel, type Chat } from "../gen/silo/v1/ui_pb";
import { Thread } from "../Thread";
import { useRunStream } from "../useRunStream";

const none: Chat[] = [];

// ChannelLog is a channel's conversations, read-only, in the shared Thread. It
// follows the open one live, like a chat.
export function ChannelLog({ botId, channel, onClose }: { botId: string; channel: Channel; onClose: () => void }) {
  const chats = useQuery(UI.method.listChats, { botId, channelId: channel.id }).data?.chats ?? none;
  // Until one is picked, the newest conversation is open.
  const [picked, setChatId] = useState("");
  const chatId = picked || (chats[0]?.id ?? "");
  const { events } = useRunStream(botId, chatId || undefined);

  return (
    <div className="fixed inset-0 z-40 flex items-center justify-center bg-ink/30 backdrop-blur-xs p-4" onClick={onClose}>
      <div
        className="flex h-full max-h-[85vh] w-full max-w-4xl flex-col overflow-hidden rounded-card shadow-card bg-surface shadow-slip"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex shrink-0 items-center justify-between border-b border-line px-5 py-3.5">
          <div className="flex items-center gap-2.5">
            <div className="flex h-7 w-7 items-center justify-center rounded-sm bg-well text-ink-2">
              <Radio size={16} />
            </div>
            <div>
              <span className="font-semibold text-sm text-ink">{channel.name} Log</span>
              <span className="ml-2 rounded-full bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">
                {chats.length} {chats.length === 1 ? "conversation" : "conversations"}
              </span>
            </div>
          </div>
          <button
            type="button"
            className="flex h-8 w-8 items-center justify-center rounded-sm text-ink-2 hover:bg-well hover:text-ink transition-colors"
            onClick={onClose}
          >
            <X size={16} />
          </button>
        </div>

        {chats.length > 1 ? (
          <div className="silo-scroll-x flex shrink-0 gap-1.5 border-b border-line bg-well px-4 py-2">
            {chats.map((c) => (
              <button
                key={c.id}
                type="button"
                className={`shrink-0 rounded-sm px-3 py-1.5 text-xs transition-colors ${ c.id === chatId ? "bg-surface font-semibold text-ink shadow-2xs border border-line"
                    : "text-ink-2 hover:text-ink hover:bg-surface/60"
                }`}
                onClick={() => setChatId(c.id)}
              >
                {c.title || c.id}
              </button>
            ))}
          </div>
        ) : null}

        <div className="flex min-h-0 flex-1 flex-col bg-canvas">
          {chatId ? (
            <Thread botId={botId} chatId={chatId} events={events} sending={false} />
          ) : (
            <div className="flex h-full items-center justify-center text-ink-3 text-[13px]">
              No conversations recorded yet.
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
