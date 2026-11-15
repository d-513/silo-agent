import { useSyncExternalStore } from "react";

// Which Bot the person is writing to right now: its composer holds words not
// yet sent. Its crest listens for as long as it does. Nothing is stored.
let heard = "";
const subs = new Set<() => void>();

export function setHeard(botId: string, on: boolean) {
  const next = on ? botId : heard === botId ? "" : heard;
  if (next === heard) return;
  heard = next;
  subs.forEach((f) => f());
}

export function useHeard(botId: string): boolean {
  return useSyncExternalStore(
    (f) => {
      subs.add(f);
      return () => subs.delete(f);
    },
    () => heard === botId,
  );
}
