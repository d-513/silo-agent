import { useSyncExternalStore } from "react";

// Whether thinking and tool rows open by themselves while they stream. Off by
// default; a per-viewer convenience, so it lives in localStorage.
const key = "silo.autoExpand";
const subs = new Set<() => void>();

function read(): boolean {
  try {
    return localStorage.getItem(key) === "1";
  } catch {
    return false;
  }
}

let value = read();

export function setAutoExpand(on: boolean) {
  value = on;
  try {
    localStorage.setItem(key, on ? "1" : "0");
  } catch {
    /* private window: still works for this tab */
  }
  subs.forEach((f) => f());
}

function subscribe(f: () => void) {
  subs.add(f);
  return () => subs.delete(f);
}

export function useAutoExpand(): boolean {
  return useSyncExternalStore(subscribe, () => value);
}
