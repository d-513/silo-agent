import type { LucideIcon } from "lucide-react";
import { useEffect, useLayoutEffect, useRef, useState, useSyncExternalStore, type DragEvent, type MouseEvent } from "react";
import { createPortal } from "react-dom";
import type { TabKey } from "./layout";

// The tabs' context menu (a right-click on a tab, a rail item or the split's
// head; a long press on a finger) and the tab being dragged. One of each is
// open at a time across the page, so both are tiny stores, and TabMenuHost
// draws the menu once, in BotPage.

export type MenuItem = { label: string; icon?: LucideIcon; onSelect: () => void; danger?: boolean } | "sep";

type Open = { x: number; y: number; title: string; items: MenuItem[] };

let menu: Open | null = null;
const menuSubs = new Set<() => void>();
const emitMenu = () => menuSubs.forEach((f) => f());

/** Open the menu at a point (a right-click, or where a finger held). */
export function openMenu(at: { x: number; y: number }, title: string, items: MenuItem[]) {
  const shown = items.filter((x, i, xs) => x !== "sep" || (i > 0 && i < xs.length - 1 && xs[i - 1] !== "sep"));
  if (!shown.some((x) => x !== "sep")) return;
  menu = { x: at.x, y: at.y, title, items: shown };
  emitMenu();
}

/** A right-click handler that opens the menu where the pointer is. */
export function onMenu(title: string, items: () => MenuItem[]) {
  return (e: MouseEvent) => {
    e.preventDefault();
    openMenu({ x: e.clientX, y: e.clientY }, title, items());
  };
}

function closeMenu() {
  if (!menu) return;
  menu = null;
  emitMenu();
}

export function TabMenuHost() {
  const open = useSyncExternalStore(
    (f) => {
      menuSubs.add(f);
      return () => menuSubs.delete(f);
    },
    () => menu,
  );
  const box = useRef<HTMLDivElement>(null);
  const [at, setAt] = useState<{ left: number; top: number } | null>(null);
  const [active, setActive] = useState(-1);

  // Placed once its size is known, kept inside the viewport.
  useLayoutEffect(() => {
    setActive(-1);
    if (!open) {
      setAt(null);
      return;
    }
    const r = box.current?.getBoundingClientRect();
    const w = r?.width ?? 220;
    const h = r?.height ?? 200;
    setAt({ left: Math.max(8, Math.min(open.x, window.innerWidth - 8 - w)), top: Math.max(8, Math.min(open.y, window.innerHeight - 8 - h)) });
    box.current?.focus();
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const away = (e: Event) => {
      if (e.target instanceof Node && box.current?.contains(e.target)) return;
      closeMenu();
    };
    document.addEventListener("pointerdown", away, true);
    window.addEventListener("scroll", away, true);
    window.addEventListener("resize", closeMenu);
    window.addEventListener("blur", closeMenu);
    return () => {
      document.removeEventListener("pointerdown", away, true);
      window.removeEventListener("scroll", away, true);
      window.removeEventListener("resize", closeMenu);
      window.removeEventListener("blur", closeMenu);
    };
  }, [open]);

  if (!open) return null;
  const picks = open.items.flatMap((x, i) => (x === "sep" ? [] : [i]));
  const pick = (i: number) => {
    const it = open.items[i];
    closeMenu();
    if (it && it !== "sep") it.onSelect();
  };
  return createPortal(
    <div
      ref={box}
      role="menu"
      tabIndex={-1}
      aria-label={open.title}
      onContextMenu={(e) => e.preventDefault()}
      onKeyDown={(e) => {
        const at = picks.indexOf(active);
        if (e.key === "Escape" || e.key === "Tab") {
          e.preventDefault();
          closeMenu();
        } else if (e.key === "ArrowDown") {
          e.preventDefault();
          setActive(picks[(at + 1) % picks.length]);
        } else if (e.key === "ArrowUp") {
          e.preventDefault();
          setActive(picks[(at - 1 + picks.length) % picks.length]);
        } else if ((e.key === "Enter" || e.key === " ") && active >= 0) {
          e.preventDefault();
          pick(active);
        }
      }}
      className="fixed z-[70] w-max max-w-[min(300px,calc(100vw-16px))] min-w-[200px] rounded-control bg-surface p-1 shadow-slip outline-none"
      style={at ?? { left: -9999, top: -9999 }}
    >
      <div className={at ? "rise" : "opacity-0"}>
        <div className="truncate px-2 pt-1.5 pb-1 text-section text-ink-3">{open.title}</div>
        {open.items.map((it, i) =>
          it === "sep" ? (
            <span key={`sep${i}`} aria-hidden className="mx-2 my-1 block h-px bg-line" />
          ) : (
            <button
              key={it.label}
              type="button"
              role="menuitem"
              data-active={i === active || undefined}
              onPointerEnter={() => setActive(i)}
              onClick={() => pick(i)}
              className={`flex h-8 w-full items-center gap-2.5 rounded-sm px-2 text-left text-row outline-none transition-colors duration-[120ms] data-active:bg-well ${
                it.danger ? "text-vermilion" : "text-ink"
              }`}
            >
              {it.icon ? <it.icon size={14} className={`shrink-0 ${it.danger ? "" : "text-ink-3"}`} /> : <span className="w-3.5 shrink-0" />}
              <span className="min-w-0 flex-1 truncate">{it.label}</span>
            </button>
          ),
        )}
      </div>
    </div>,
    document.body,
  );
}

// The tab being dragged, so every drop target knows what it would take
// (dataTransfer's value is unreadable until the drop).
let dragging: TabKey | null = null;
const dragSubs = new Set<() => void>();
const emitDrag = () => dragSubs.forEach((f) => f());
const mime = "application/x-silo-tab";

/** Start dragging a tab (a tab in the bar, an item of the rail). */
export function dragStart(e: DragEvent, k: TabKey) {
  e.dataTransfer.setData(mime, k);
  e.dataTransfer.effectAllowed = "move";
  closeMenu();
  // Set after the browser has taken its picture of the tab, so the drop zones
  // that appear do not get in it.
  window.setTimeout(() => {
    dragging = k;
    emitDrag();
  });
}

export function dragEnd() {
  if (dragging === null) return;
  dragging = null;
  emitDrag();
}

/** The key a drop carries, if it is a tab. */
export function dropped(e: DragEvent): TabKey | null {
  const k = e.dataTransfer.getData(mime);
  dragEnd();
  return k ? (k as TabKey) : null;
}

/** Whether a drag over this element carries a tab. */
export function carriesTab(e: DragEvent): boolean {
  return e.dataTransfer.types.includes(mime);
}

export function useDragging(): TabKey | null {
  return useSyncExternalStore(
    (f) => {
      dragSubs.add(f);
      return () => dragSubs.delete(f);
    },
    () => dragging,
  );
}
