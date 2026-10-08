import { useSyncExternalStore } from "react";
import { isOpen } from "./chatGroups.ts";

// What the reader folded or opened. A per-viewer convenience, so it lives in
// localStorage, shared by every Bot's list.
const storeKey = "silo.chatGroups";
const subs = new Set<() => void>();

function read(): Record<string, boolean> {
  try {
    const v: unknown = JSON.parse(localStorage.getItem(storeKey) ?? "{}");
    if (!v || typeof v !== "object") return {};
    return Object.fromEntries(Object.entries(v).filter(([, on]) => typeof on === "boolean"));
  } catch {
    return {};
  }
}

let picked = read();

export function setGroupOpen(key: string, open: boolean) {
  if (isOpen(key, picked) === open) return;
  picked = { ...picked, [key]: open };
  try {
    localStorage.setItem(storeKey, JSON.stringify(picked));
  } catch {
    /* private window: still works for this tab */
  }
  subs.forEach((f) => f());
}

function subscribe(f: () => void) {
  subs.add(f);
  return () => subs.delete(f);
}

export function useGroupsPicked(): Readonly<Record<string, boolean>> {
  return useSyncExternalStore(subscribe, () => picked);
}
