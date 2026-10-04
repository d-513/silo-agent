import { Radio, X } from "lucide-react";
import { useEffect, useState } from "react";
import { ui } from "../api";
import type { Ev } from "../fold";
import type { Channel, Chat } from "../gen/silo/v1/ui_pb";
import { Thread } from "../Thread";

export function ChannelLog({ botId, channel, onClose }: { botId: string; channel: Channel; onClose: () => void }) {
  const [chats, setChats] = useState<Chat[]>([]);
  const [chatId, setChatId] = useState("");
  const [events, setEvents] = useState<Ev[]>([]);

  useEffect(() => {
    let dead = false;
    ui.listChats({ botId, channelId: channel.id })
      .then((r) => {
        if (dead) return;
        setChats(r.chats);
        setChatId(r.chats[0]?.id ?? "");
      })
      .catch(() => {});
    return () => {
      dead = true;
    };
  }, [botId, channel.id]);

  useEffect(() => {
    if (!chatId) {
      setEvents([]);
      return;
    }
    let dead = false;
    const ac = new AbortController();
    setEvents([]);
    (async () => {
      try {
        for await (const ev of ui.streamRun({ botId, chatId, afterEventId: "" }, { signal: ac.signal })) {
          if (dead) return;
          setEvents((xs) => [
            ...xs,
            {
              id: ev.id,
              kind: ev.kind,
              body: ev.body,
              tool: ev.tool,
              runId: ev.runId,
              attachments: ev.attachments.map((a) => ({ name: a.name, path: a.path, size: Number(a.size) })),
            },
          ]);
        }
      } catch {
        /* closed */
      }
    })();
    return () => {
      dead = true;
      ac.abort();
    };
  }, [botId, chatId]);

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
