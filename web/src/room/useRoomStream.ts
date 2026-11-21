import { useEffect, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import type { Room, RoomBot, RoomMessage } from "../gen/silo/v1/ui_pb";
import { STREAM_RETRY_MS, streamRetryMs } from "../queryPolicy";

// useRoomStream follows one room: the room as it stands (its people, and what
// each Bot is doing), and its messages, the newest first page then each one
// as it is said. A dropped stream reconnects from the last message it had.
// `gone` is why the room cannot be shown any more (deleted, left, or a Bot in
// it the caller is no longer on).
export function useRoomStream(roomId: string) {
  const [room, setRoom] = useState<Room | null>(null);
  const [messages, setMessages] = useState<RoomMessage[]>([]);
  const [earlier, setEarlier] = useState(false);
  const [gone, setGone] = useState("");

  useEffect(() => {
    let dead = false;
    let ac = new AbortController();
    let after = 0n;
    const seen = new Set<string>();
    setRoom(null);
    setMessages([]);
    setEarlier(false);
    setGone("");
    (async () => {
      let backoff = STREAM_RETRY_MS;
      while (!dead) {
        ac = new AbortController();
        try {
          for await (const ev of ui.streamRoom({ roomId, afterSeq: after }, { signal: ac.signal })) {
            backoff = STREAM_RETRY_MS;
            if (dead) return;
            if (ev.room) setRoom(ev.room);
            if (ev.earlier) setEarlier(true);
            if (ev.bot) {
              const b: RoomBot = ev.bot;
              setRoom((r) => (r ? { ...r, bots: r.bots.map((x) => (x.botId === b.botId ? b : x)) } : r));
            }
            const m = ev.message;
            if (m && !seen.has(m.id)) {
              seen.add(m.id);
              if (m.seq > after) after = m.seq;
              setMessages((xs) => [...xs, m]);
            }
          }
        } catch (e) {
          if (dead) return;
          const next = streamRetryMs(e, backoff);
          if (next === false) {
            setGone(fail(e));
            return;
          }
          backoff = next;
        }
        if (dead) return;
        await new Promise((r) => setTimeout(r, backoff));
      }
    })();
    return () => {
      dead = true;
      ac.abort();
    };
  }, [roomId]);

  return { room, setRoom, messages, earlier, gone };
}
