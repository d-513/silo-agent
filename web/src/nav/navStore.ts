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
