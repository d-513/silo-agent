import { useSyncExternalStore } from "react";
import { palette } from "./tokens.ts";

// Light or dark. What the reader picks is a per-viewer convenience, so it
// lives in localStorage; until they pick, the app follows the system. The
// palettes themselves are index.css: `data-theme` on <html> selects one, and
// the inline script in index.html sets it before the first paint.
export type Theme = "light" | "dark";

export const themeKey = "silo.theme";

export function storedTheme(raw: string | null): Theme | null {
  return raw === "light" || raw === "dark" ? raw : null;
}

export function resolveTheme(picked: Theme | null, systemDark: boolean): Theme {
  return picked ?? (systemDark ? "dark" : "light");
}

export function flipTheme(t: Theme): Theme {
  return t === "dark" ? "light" : "dark";
}

const subs = new Set<() => void>();
const system = typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : null;

function read(): Theme | null {
  try {
    return storedTheme(localStorage.getItem(themeKey));
  } catch {
    return null;
  }
}

let picked = read();
let theme = resolveTheme(picked, system?.matches ?? false);

// apply paints the theme. Controls ease their colors over 160ms and plain text
// does not, so a switch would arrive in two beats: transitions are held while
// the new palette is computed (the read forces that), then let go.
function apply() {
  if (typeof document === "undefined") return;
  const root = document.documentElement;
  if (root.dataset.theme !== theme) {
    root.classList.add("theme-switch");
    root.dataset.theme = theme;
    void root.offsetHeight;
    root.classList.remove("theme-switch");
  }
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", theme === "dark" ? palette.canvasDark : palette.canvas);
}

function settle() {
  const next = resolveTheme(picked, system?.matches ?? false);
  if (next === theme) return;
  theme = next;
  apply();
  subs.forEach((f) => f());
}

export function setTheme(t: Theme) {
  picked = t;
  try {
    localStorage.setItem(themeKey, t);
  } catch {
    /* private window: still works for this tab */
  }
  settle();
}

apply();
system?.addEventListener("change", settle);
if (typeof window !== "undefined") {
  // A pick made in another tab lands here too.
  window.addEventListener("storage", (e) => {
    if (e.key !== themeKey) return;
    picked = storedTheme(e.newValue);
    settle();
  });
}

function subscribe(f: () => void) {
  subs.add(f);
  return () => subs.delete(f);
}

export function useTheme(): Theme {
  return useSyncExternalStore(subscribe, () => theme);
}
