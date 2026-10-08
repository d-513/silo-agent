import { useSyncExternalStore } from "react";

// The `wide:` breakpoint (index.css `--breakpoint-wide`), for the few layouts
// that cannot be said in classes: below it there is no room to dock a pane.
const wide = "(min-width: 960px)";
const subscribe = (cb: () => void) => {
  const m = window.matchMedia(wide);
  m.addEventListener("change", cb);
  return () => m.removeEventListener("change", cb);
};

export function useWide() {
  return useSyncExternalStore(subscribe, () => window.matchMedia(wide).matches, () => true);
}
