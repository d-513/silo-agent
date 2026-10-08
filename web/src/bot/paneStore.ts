import { useSyncExternalStore } from "react";
import { paneKinds, type PaneKind } from "./tabs.ts";

// The pane docked beside the chat (Files, Desktop or Console) and how much of
// the row it takes. A per-viewer convenience, so it lives in localStorage,
// shared by every Bot.
export type Docked = { kind: PaneKind | ""; share: number };

const storeKey = "silo.pane";
const subs = new Set<() => void>();

export const minShare = 0.25;
export const maxShare = 0.75;

export function clampShare(n: number) {
  return Number.isFinite(n) ? Math.min(maxShare, Math.max(minShare, n)) : 0.5;
}

function read(): Docked {
  try {
    const v = JSON.parse(localStorage.getItem(storeKey) ?? "{}") as Partial<Docked> | null;
    const kind = paneKinds.find((k) => k === v?.kind) ?? "";
    return { kind, share: clampShare(typeof v?.share === "number" ? v.share : 0.5) };
  } catch {
    return { kind: "", share: 0.5 };
  }
}

let docked = read();

function write(next: Docked) {
  if (next.kind === docked.kind && next.share === docked.share) return;
  docked = next;
  try {
    localStorage.setItem(storeKey, JSON.stringify(docked));
  } catch {
    /* private window: still works for this tab */
  }
  subs.forEach((f) => f());
}

/** Dock a pane beside the chat; "" closes it. */
export function setDocked(kind: PaneKind | "") {
  write({ ...docked, kind });
}

/** The docked pane's share of the row the chat and the pane split. */
export function setShare(share: number) {
  write({ ...docked, share: clampShare(share) });
}

function subscribe(f: () => void) {
  subs.add(f);
  return () => subs.delete(f);
}

export function useDocked(): Readonly<Docked> {
  return useSyncExternalStore(subscribe, () => docked);
}
