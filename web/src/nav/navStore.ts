import { useSyncExternalStore } from "react";

// Whether the narrow layout's sidebar sheet (NavSheet) is out: its own store,
// because the top bars that open it and the rows that close it are far apart.

let open = false;
const subs = new Set<() => void>();
function set(v: boolean) {
  if (open === v) return;
  open = v;
  subs.forEach((f) => f());
}
export const openNav = () => set(true);
export const closeNav = () => set(false);
const subscribe = (f: () => void) => {
  subs.add(f);
  return () => subs.delete(f);
};
export function useNavOpen() {
  return useSyncExternalStore(subscribe, () => open, () => false);
}

// Whether the wide layout's sidebar is folded to its rail of crests: the
// reader's own choice, kept in the browser like the theme.

export const foldKey = "silo.sidebar";

/** What a stored value means: only "folded" folds, anything else is the full sidebar. */
export function storedFold(raw: string | null): boolean {
  return raw === "folded";
}

function readFold(): boolean {
  try {
    return storedFold(localStorage.getItem(foldKey));
  } catch {
    return false;
  }
}

let folded = readFold();
const foldSubs = new Set<() => void>();
export function setFolded(v: boolean) {
  if (folded === v) return;
  folded = v;
  try {
    localStorage.setItem(foldKey, v ? "folded" : "open");
  } catch {
    // A browser that keeps nothing still folds for this visit.
  }
  foldSubs.forEach((f) => f());
}
export const toggleFolded = () => setFolded(!folded);
const subscribeFold = (f: () => void) => {
  foldSubs.add(f);
  return () => foldSubs.delete(f);
};
export function useFolded() {
  return useSyncExternalStore(subscribeFold, () => folded, () => false);
}

/** Ctrl/⌘+B folds the sidebar, unless the keys are being typed into a field. */
export function isFoldKey(e: { key: string; ctrlKey: boolean; metaKey: boolean; altKey: boolean; shiftKey: boolean }, typing: boolean): boolean {
  return !typing && (e.ctrlKey || e.metaKey) && !e.altKey && !e.shiftKey && e.key.toLowerCase() === "b";
}

/** The shortcut as the reader's keyboard names it. */
export function foldHint(): string {
  return typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform) ? "⌘B" : "Ctrl+B";
}
