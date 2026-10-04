import { useEffect, useRef, useState, type WheelEvent } from "react";
import type { Block, Ev } from "../fold";

const reduceMotion = () => typeof window !== "undefined" && window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;

// useStickyScroll keeps a thread glued to its newest item while the reader is at
// the bottom, lets go the moment they scroll up, and says which items arrived
// after the chat opened (those rise in; replayed history does not).
export function useStickyScroll({
  chatId,
  events,
  sending,
  fresh,
  blocks,
}: {
  chatId?: string;
  events: Ev[];
  sending: boolean;
  fresh?: ReadonlySet<string>;
  blocks: Block[];
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const innerRef = useRef<HTMLDivElement>(null);
  const pinned = useRef(true);
  const lastTop = useRef(0);
  const conversation = useRef(chatId);
  const lastKey = useRef<string | undefined>(undefined);
  const openedAt = useRef(Date.now());
  const arrived = useRef(new Map<string, boolean>());
  const [showScrollBottom, setShowScrollBottom] = useState(false);

  if (conversation.current !== chatId) {
    openedAt.current = Date.now();
    arrived.current = new Map();
  }
  // History replays in a burst when a chat opens; only items that show up
  // after that rise in. Each key decides once.
  const rises = (key: string) => {
    let v = arrived.current.get(key);
    if (v === undefined) {
      v = Date.now() - openedAt.current > 900;
      arrived.current.set(key, v);
    }
    return v ? "rise" : "";
  };

  const scrollToBottom = (smooth: boolean) => {
    const el = containerRef.current;
    if (!el) return;
    pinned.current = true;
    el.scrollTo({ top: el.scrollHeight, behavior: smooth && !reduceMotion() ? "smooth" : "auto" });
  };

  useEffect(() => {
    const switched = chatId !== conversation.current;
    const last = blocks[blocks.length - 1];
    const sent = last?.type === "user" && last.key !== lastKey.current && !!last.id && !!fresh?.has(last.id);
    conversation.current = chatId;
    lastKey.current = last?.key;
    if (switched) {
      scrollToBottom(false);
      return;
    }
    if (sent) {
      scrollToBottom(true);
      return;
    }
    // Follow new items only when the reader was already at the bottom. Instant:
    // a smooth scroll restarted on every token judders.
    if (pinned.current) scrollToBottom(false);
  }, [events, sending, chatId]);

  // Rows opening and folding shut change the height without a new event;
  // stay glued to the bottom through those too.
  useEffect(() => {
    const el = containerRef.current;
    const inner = innerRef.current;
    if (!el || !inner) return;
    const ro = new ResizeObserver(() => {
      if (pinned.current) el.scrollTop = el.scrollHeight;
    });
    ro.observe(inner);
    return () => ro.disconnect();
  }, []);

  const onScroll = () => {
    const el = containerRef.current;
    if (!el) return;
    const distanceToBottom = el.scrollHeight - el.scrollTop - el.clientHeight;
    // Scrolling up lets go; reaching the bottom again re-pins. Our own scrolls
    // only ever move down.
    if (distanceToBottom <= 24) pinned.current = true;
    else if (el.scrollTop < lastTop.current - 1) pinned.current = false;
    lastTop.current = el.scrollTop;
    setShowScrollBottom(distanceToBottom > 160);
  };

  const onWheel = (e: WheelEvent<HTMLDivElement>) => {
    if (e.deltaY < 0) pinned.current = false;
  };

  return { containerRef, innerRef, onScroll, onWheel, showScrollBottom, scrollToBottom, rises };
}
