import { useEffect, type KeyboardEvent, type RefObject } from "react";

// useDismiss closes a floating menu on a press outside it and its trigger, on a
// resize, and on a scroll that does not happen inside the menu.
export function useDismiss(open: boolean, triggerRef: RefObject<HTMLElement | null>, menuRef: RefObject<HTMLElement | null>, close: () => void) {
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      const t = e.target;
      if (!(t instanceof Node)) return;
      if (triggerRef.current?.contains(t) || menuRef.current?.contains(t)) return;
      close();
    };
    const onScroll = (e: Event) => {
      const t = e.target;
      if (menuRef.current && t instanceof Node && menuRef.current.contains(t)) return;
      close();
    };
    document.addEventListener("mousedown", onDoc);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);
}

// listboxKeys is the keyboard contract of a listbox trigger. Closed, the arrows,
// Enter and Space open it. Open, the arrows and Home/End move the active row,
// Enter and Space pick it, and Escape and Tab close.
export function listboxKeys(
  e: KeyboardEvent,
  m: { open: boolean; count: number; setOpen: (open: boolean) => void; setActive: (next: (i: number) => number) => void; pickActive: () => void },
) {
  if (!m.open) {
    if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      m.setOpen(true);
    }
    return;
  }
  if (e.key === "ArrowDown") {
    e.preventDefault();
    m.setActive((i) => Math.min(i + 1, m.count - 1));
  } else if (e.key === "ArrowUp") {
    e.preventDefault();
    m.setActive((i) => Math.max(i - 1, 0));
  } else if (e.key === "Home") {
    e.preventDefault();
    m.setActive(() => 0);
  } else if (e.key === "End") {
    e.preventDefault();
    m.setActive(() => m.count - 1);
  } else if (e.key === "Enter" || e.key === " ") {
    e.preventDefault();
    m.pickActive();
  } else if (e.key === "Escape" || e.key === "Tab") {
    m.setOpen(false);
  }
}
