import { useEffect, useRef, type ReactNode, type TouchEvent } from "react";

// Sheet is the narrow layout's way to what a wide screen shows beside the
// page: a panel that slides in from one edge over a dimmed page. The left one
// is the sidebar (Workspaces and Bots), the right one a Bot's rail (its pages
// and machine), so each sits on the side it sits on from 960 up.
//
// It follows the finger both ways: a swipe toward its edge puts it away past
// a third of its width, and with `edge` a swipe in from that edge of the
// screen pulls it out. A press outside or Escape puts it away too.

// How close to the screen's edge a pull has to start. Narrow on purpose: the
// page's own gutter is wider, so a press on content never pulls a sheet.
const edgeZone = 16;

// A touch inside something that scrolls sideways is that thing's.
const sideways = (t: EventTarget | null) => t instanceof Element && !!t.closest(".silo-scroll-x, [data-scroll-x]");

export function Sheet({
  side,
  open,
  label,
  edge = false,
  onOpen,
  onClose,
  children,
}: {
  side: "left" | "right";
  open: boolean;
  label: string;
  // Pull it out from the screen's edge.
  edge?: boolean;
  onOpen?: () => void;
  onClose: () => void;
  children: ReactNode;
}) {
  const panel = useRef<HTMLDivElement>(null);
  const shade = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; y: number; dx: number; on: boolean | null } | null>(null);
  const dir = side === "left" ? -1 : 1; // the way out
  const close = useRef(onClose);
  close.current = onClose;
  const pull = useRef(onOpen);
  pull.current = onOpen;

  useEffect(() => {
    if (!open) return;
    // A pull from the edge left the panel where the finger did; now the
    // classes say the same.
    const el = panel.current;
    const bg = shade.current;
    if (el && bg) {
      el.style.transition = el.style.transform = "";
      bg.style.transition = bg.style.opacity = "";
    }
    el?.focus({ preventScroll: true });
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  // Where the panel stands `dx` px from its resting place (open or shut),
  // with the shade following. Cleared, the classes take over again.
  function follow(dx: number | null, fromOpen: boolean) {
    const el = panel.current;
    const bg = shade.current;
    if (!el || !bg) return;
    if (dx === null) {
      el.style.transition = el.style.transform = "";
      bg.style.transition = bg.style.opacity = "";
      return;
    }
    const w = el.offsetWidth || 1;
    const shown = fromOpen ? 1 - Math.abs(dx) / w : Math.abs(dx) / w;
    el.style.transition = bg.style.transition = "none";
    el.style.transform = fromOpen ? `translateX(${dx}px)` : `translateX(calc(${dir * 100}% + ${dx}px))`;
    bg.style.opacity = String(Math.max(0, Math.min(1, shown)));
  }

  // Putting it away: a swipe toward its edge.
  function onTouchStart(e: TouchEvent) {
    if (sideways(e.target)) {
      drag.current = null;
      return;
    }
    const p = e.touches[0];
    drag.current = { x: p.clientX, y: p.clientY, dx: 0, on: null };
  }
  function onTouchMove(e: TouchEvent) {
    const d = drag.current;
    if (!d) return;
    const p = e.touches[0];
    const dx = p.clientX - d.x;
    const dy = p.clientY - d.y;
    if (d.on === null) {
      if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return;
      d.on = Math.abs(dx) > Math.abs(dy) && Math.sign(dx) === dir;
    }
    if (!d.on) return;
    d.dx = dir < 0 ? Math.min(0, dx) : Math.max(0, dx);
    follow(d.dx, true);
  }
  function onTouchEnd() {
    const d = drag.current;
    drag.current = null;
    if (!d?.on) return;
    const w = panel.current?.offsetWidth ?? 1;
    follow(null, true);
    if (Math.abs(d.dx) > w / 3) onClose();
  }

  // Pulling it out: a swipe in from the screen's edge, anywhere on the page.
  useEffect(() => {
    if (!edge || open) return;
    let d: { x: number; y: number; dx: number; on: boolean | null } | null = null;
    const start = (e: globalThis.TouchEvent) => {
      const p = e.touches[0];
      const atEdge = side === "left" ? p.clientX <= edgeZone : p.clientX >= window.innerWidth - edgeZone;
      d = atEdge && e.touches.length === 1 && !sideways(e.target) ? { x: p.clientX, y: p.clientY, dx: 0, on: null } : null;
    };
    const move = (e: globalThis.TouchEvent) => {
      if (!d) return;
      const p = e.touches[0];
      const dx = p.clientX - d.x;
      const dy = p.clientY - d.y;
      if (d.on === null) {
        if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return;
        d.on = Math.abs(dx) > Math.abs(dy) && Math.sign(dx) === -dir;
        if (!d.on) {
          d = null;
          return;
        }
      }
      const w = panel.current?.offsetWidth ?? 1;
      d.dx = dir < 0 ? Math.min(w, Math.max(0, dx)) : Math.max(-w, Math.min(0, dx));
      follow(d.dx, false);
    };
    const end = () => {
      const cur = d;
      d = null;
      if (!cur?.on) return;
      const w = panel.current?.offsetWidth ?? 1;
      if (Math.abs(cur.dx) > w / 3) {
        // Let it finish from where the finger left it.
        const el = panel.current;
        const bg = shade.current;
        if (el && bg) {
          el.style.transition = bg.style.transition = "";
          el.style.transform = "translateX(0)";
          bg.style.opacity = "1";
        }
        // The classes take over once it renders open (the effect below).
        pull.current?.();
      } else {
        follow(null, false);
      }
    };
    document.addEventListener("touchstart", start, { passive: true });
    document.addEventListener("touchmove", move, { passive: true });
    document.addEventListener("touchend", end);
    document.addEventListener("touchcancel", end);
    return () => {
      document.removeEventListener("touchstart", start);
      document.removeEventListener("touchmove", move);
      document.removeEventListener("touchend", end);
      document.removeEventListener("touchcancel", end);
    };
  }, [edge, open, side, dir]);

  const at = side === "left" ? "left-0" : "right-0";
  const away = side === "left" ? "-translate-x-full" : "translate-x-full";
  return (
    <div className={`fixed inset-0 z-40 wide:hidden ${open ? "" : "pointer-events-none"}`} inert={!open}>
      <div
        ref={shade}
        aria-hidden
        onClick={onClose}
        className={`absolute inset-0 bg-ink/30 transition-opacity duration-[280ms] ease-quiet ${open ? "opacity-100" : "opacity-0"}`}
      />
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-label={label}
        tabIndex={-1}
        data-drawer=""
        data-side={side}
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={onTouchEnd}
        onTouchCancel={onTouchEnd}
        className={`absolute inset-y-0 ${at} flex w-[min(21rem,86vw)] flex-col overflow-hidden bg-well pt-[env(safe-area-inset-top)] pb-[env(safe-area-inset-bottom)] outline-none transition-[transform,box-shadow] duration-[320ms] ease-quiet motion-reduce:transition-none ${
          open ? "translate-x-0 shadow-slip" : away
        }`}
      >
        {children}
      </div>
    </div>
  );
}
