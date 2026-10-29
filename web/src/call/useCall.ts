import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from "react";
import { CallSession, type CallSnapshot } from "./session";

const noSubscribe = () => () => {};
const noSnap = () => null;

// How long an ended call stays on screen, so its last word can be read.
const ENDED_MS = 2500;
const FAILED_MS = 9000;

export type Call = ReturnType<typeof useCall>;

// useCall is a Bot page's one call: started from a press, kept while the
// person moves between the Bot's pages, ended with the page.
export function useCall(botId: string) {
  const [session, setSession] = useState<CallSession | null>(null);
  // expanded is the call screen; otherwise the call is the bar over the
  // composer. The big Call button opens the screen, Talk the bar.
  const [expanded, setExpanded] = useState(false);
  const snap: CallSnapshot | null = useSyncExternalStore(session?.subscribe ?? noSubscribe, session?.get ?? noSnap);
  const live = useRef<CallSession | null>(null);
  live.current = session;

  // An ended call lingers a moment, then goes.
  const ended = snap?.state === "ended";
  const failed = !!snap?.error;
  useEffect(() => {
    if (!ended || !session) return;
    const id = setTimeout(() => setSession((cur) => (cur === session ? null : cur)), failed ? FAILED_MS : ENDED_MS);
    return () => clearTimeout(id);
  }, [ended, failed, session]);

  // Leaving the Bot ends the call.
  useEffect(() => () => live.current?.hangup(), [botId]);

  const start = useCallback(
    (chat: string, screen = false) => {
      live.current?.hangup();
      const s = new CallSession(botId, chat);
      setSession(s);
      setExpanded(screen);
      void s.start();
    },
    [botId],
  );

  return {
    snap,
    // live is a call that has not ended.
    live: !!snap && snap.state !== "ended",
    start,
    expanded,
    setExpanded,
    end: () => session?.hangup(),
    dismiss: () => setSession(null),
    session,
  };
}
