import { useCallback, useEffect, useRef, useState } from "react";
import { ui } from "./api";
import type { RunEvent } from "./gen/silo/v1/ui_pb";
import { isTransient } from "./errors";
import { type Ev, quotedFrom } from "./fold";
import { quoteFromProto } from "./quote/model";

// window is the model's context size in tokens (0 when the server did not say).
export type Usage = { input: number; output: number; cacheRead: number; cacheWrite: number; window: number };

// chatBusy: a conversation is busy while any run that opened with a user
// message (or a manual compaction, or a subagent report) has not reached done
// or error.
export function chatBusy(events: Ev[]): boolean {
  const open = new Set<string>();
  for (const ev of events) {
    if (ev.runId && (ev.kind === "user" || ev.kind === "compacting" || ev.kind === "subagent_report")) open.add(ev.runId);
    if (ev.runId && (ev.kind === "done" || ev.kind === "error")) open.delete(ev.runId);
  }
  return open.size > 0;
}

type Handlers = {
  onApproval?: () => void;
  onTitle?: (title: string) => void;
  onDone?: () => void;
  // Transient pings ("board", "subagents") that say the chat's taskboard or
  // subagents moved; they are never part of the thread.
  onPing?: (kind: string) => void;
};

const pings = new Set(["board", "subagents"]);

function toEv(ev: RunEvent): Ev {
  return {
    id: ev.id,
    kind: ev.kind,
    body: ev.body,
    tool: ev.tool,
    runId: ev.runId,
    at: Date.now(),
    createdAt: ev.createdAt || undefined,
    attachments: ev.attachments.map((a) => ({ name: a.name, path: a.path, size: Number(a.size) })),
    quotes: ev.quotes.length ? ev.quotes.map(quoteFromProto) : undefined,
    quoted: ev.quoted ? { from: quotedFrom(ev.quoted.from), title: ev.quoted.title, widget: ev.quoted.widget } : undefined,
  };
}

// useRunStream is the one reader of a conversation's run log: it replays the
// persisted events, follows the live bus, reconnects with after_event_id (no
// duplicates), and resets on a history truncation. Chats and automation logs
// both render through it. A long thread replays its newest turns only:
// `earlier` says there is more and loadEarlier puts the page before in front
// (`prepended` counts those events, so the fold keeps its keys).
export function useRunStream(botId: string | undefined, chatId: string | undefined, handlers: Handlers = {}) {
  const [events, setEvents] = useState<Ev[]>([]);
  const [sending, setSending] = useState(false);
  const [usage, setUsage] = useState<Usage | null>(null);
  const [nonce, setNonce] = useState(0);
  // User messages sent from this tab glide in; history does not animate.
  const sentAt = useRef(0);
  const [fresh, setFresh] = useState<ReadonlySet<string>>(() => new Set());
  const [earlier, setEarlier] = useState(false);
  const [prepended, setPrepended] = useState(0);
  const [loadingEarlier, setLoadingEarlier] = useState(false);
  const first = useRef<string | undefined>(undefined);
  first.current = events[0]?.id;
  const on = useRef(handlers);
  on.current = handlers;

  useEffect(() => {
    setUsage(null);
  }, [chatId]);

  useEffect(() => {
    if (!botId || !chatId) {
      setEvents([]);
      setSending(false);
      return;
    }
    let dead = false;
    let ac = new AbortController();
    let after = "";
    const seen = new Set<string>();
    setEvents([]);
    setSending(false);
    setEarlier(false);
    setPrepended(0);
    const push = (ev: Ev) => {
      setEvents((xs) => {
        const next = [...xs, ev];
        setSending(chatBusy(next));
        return next;
      });
    };
    (async () => {
      // Reconnect quickly after a clean end, back off while the CP is down.
      let backoff = 800;
      while (!dead) {
        ac = new AbortController();
        try {
          for await (const ev of ui.streamRun({ botId, chatId, afterEventId: after }, { signal: ac.signal })) {
            backoff = 800;
            if (ev.id) {
              if (seen.has(ev.id)) continue;
              seen.add(ev.id);
              after = ev.id;
            }
            if (ev.kind === "earlier") {
              if (!dead) setEarlier(true);
              continue;
            }
            if (pings.has(ev.kind)) {
              on.current.onPing?.(ev.kind);
              continue;
            }
            if (ev.kind === "approval") on.current.onApproval?.();
            if (ev.kind === "chat_title" && ev.body) on.current.onTitle?.(ev.body);
            if (ev.kind === "usage" && ev.body) {
              try {
                const u = JSON.parse(ev.body) as Record<string, number>;
                if (!dead) {
                  setUsage({ input: u.input ?? 0, output: u.output ?? 0, cacheRead: u.cache_read ?? 0, cacheWrite: u.cache_write ?? 0, window: u.window ?? 0 });
                }
              } catch {
                /* ignore malformed usage */
              }
            }
            if (ev.kind === "done") on.current.onDone?.();
            if (ev.kind === "reset") {
              // History was truncated by an edit/delete elsewhere; drop the
              // cached events and replay from scratch.
              setEvents([]);
              setSending(false);
              setEarlier(false);
              setPrepended(0);
              setNonce((n) => n + 1);
              return;
            }
            if (ev.kind === "user" && ev.id && sentAt.current && Date.now() - sentAt.current < 15000) {
              sentAt.current = 0;
              const sid = ev.id;
              setFresh((xs) => new Set(xs).add(sid));
            }
            if (!dead) push(toEv(ev));
          }
        } catch (e) {
          if (!dead) setSending(false);
          if (isTransient(e)) backoff = Math.min(backoff * 2, 8000);
        }
        if (dead) return;
        await new Promise((r) => setTimeout(r, backoff));
      }
    })();
    return () => {
      dead = true;
      ac.abort();
    };
  }, [botId, chatId, nonce]);

  // markSent flags the next user event as this tab's own (it glides in) and
  // shows the run as busy before the first event arrives.
  const markSent = useCallback(() => {
    sentAt.current = Date.now();
    setSending(true);
  }, []);

  const resync = useCallback(() => {
    setEvents([]);
    setSending(false);
    setNonce((n) => n + 1);
  }, []);

  // loadEarlier puts the page before the oldest loaded event in front.
  const loadEarlier = useCallback(async () => {
    const before = first.current;
    if (!botId || !chatId || !before || loadingEarlier) return;
    setLoadingEarlier(true);
    try {
      const page = await ui.listChatEvents({ botId, chatId, beforeEventId: before });
      const older = page.events.map(toEv);
      setEvents((xs) => (xs[0]?.id === before ? [...older, ...xs] : xs));
      setPrepended((n) => n + older.length);
      setEarlier(page.more);
    } catch (e) {
      // The anchor went (a wipe or an edit elsewhere): start over.
      if (!isTransient(e)) resync();
    } finally {
      setLoadingEarlier(false);
    }
  }, [botId, chatId, loadingEarlier, resync]);

  return { events, sending, setSending, usage, fresh, markSent, resync, earlier, prepended, loadEarlier, loadingEarlier };
}
