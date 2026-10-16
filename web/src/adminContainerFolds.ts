import { useSyncExternalStore } from "react";

// What the reader folded or opened on the Admin Containers tab. A
// per-viewer convenience, so it lives in localStorage, shared with every
// visit to the page.
const storeKey = "silo.adminContainerFolds";
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

export function setFold(key: string, open: boolean) {
  if (picked[key] === open) return;
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

/** The reader's folds so far; anything absent is the fold's own default. */
export function useFolds(): Readonly<Record<string, boolean>> {
  return useSyncExternalStore(subscribe, () => picked);
}
