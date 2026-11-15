import { useSyncExternalStore } from "react";

// The sidebar's Call sits outside the Bot page that holds the call, so the
// two meet here. `ring` is a press waiting for that Bot's page to pick up (it
// may still be loading, or be another Bot's page until the navigation lands);
// `onAir` is the call the open Bot page holds, as that Bot's row shows it.
// Nothing is stored.

export type OnAir = { botId: string; connecting: boolean; since: number };

// A ring nobody answers (the Bot's page never loaded) is dropped, so opening
// that Bot much later does not start a call.
const ringMs = 15_000;
let ringing = "";
let ringTimer: ReturnType<typeof setTimeout> | undefined;
let air: OnAir | null = null;
const subs = new Set<() => void>();
const tell = () => subs.forEach((f) => f());
const subscribe = (f: () => void) => {
  subs.add(f);
  return () => subs.delete(f);
};

/** Ask the Bot's page to call (or go back to its call). */
export function ring(botId: string) {
  ringing = botId;
  clearTimeout(ringTimer);
  ringTimer = setTimeout(() => hush(botId), ringMs);
  tell();
}

/** The page took the ring, or the Bot it was for is no longer open. */
export function hush(botId: string) {
  if (ringing !== botId) return;
  ringing = "";
  tell();
}

export function useRinging(): string {
  return useSyncExternalStore(subscribe, () => ringing);
}

export function setOnAir(next: OnAir | null) {
  if (air?.botId === next?.botId && air?.connecting === next?.connecting && air?.since === next?.since) return;
  air = next;
  tell();
}

export function useOnAir(): OnAir | null {
  return useSyncExternalStore(subscribe, () => air);
}
