import {
  cloneElement,
  useCallback,
  useEffect,
  useId,
  useLayoutEffect,
  useRef,
  useState,
  type FocusEvent,
  type PointerEvent,
  type ReactElement,
  type ReactNode,
} from "react";
import { createPortal } from "react-dom";

// Tip is a hover/focus card for a control whose meaning needs more than a
// word. It replaces the native `title` (slow, unstyled, one plain line): it
// opens after a short rest, instantly when moving between tips, sits above the
// control (below if there is no room), stays inside the viewport, and never
// opens from a touch tap.

const OPEN_DELAY = 350;
// A tip opened within this long of another closing skips the delay, so
// scanning a row of controls reads fluidly.
const WARM_MS = 400;
const GAP = 8;
const EDGE = 8;

let lastClosed = 0;

type Props = {
  content: ReactNode;
  children: ReactElement<Record<string, unknown>>;
  // Close the tip when the control is clicked (its state is about to change).
  closeOnClick?: boolean;
};

export function Tip({ content, children, closeOnClick = true }: Props) {
  const id = useId();
  const [open, setOpen] = useState(false);
  const [pos, setPos] = useState<{
    left: number;
    top: number;
    below: boolean;
  } | null>(null);
  const anchor = useRef<HTMLElement | null>(null);
  const card = useRef<HTMLDivElement>(null);
  const timer = useRef<number | undefined>(undefined);

  const hide = useCallback(() => {
    window.clearTimeout(timer.current);
    setOpen((was) => {
      if (was) lastClosed = Date.now();
      return false;
    });
  }, []);

  const show = (el: HTMLElement) => {
    anchor.current = el;
    window.clearTimeout(timer.current);
    const warm = Date.now() - lastClosed < WARM_MS;
    timer.current = window.setTimeout(() => setOpen(true), warm ? 0 : OPEN_DELAY);
  };

  useEffect(() => () => window.clearTimeout(timer.current), []);

  // Place after the card renders so its real size is known.
  useLayoutEffect(() => {
    if (!open) {
      setPos(null);
      return;
    }
    const a = anchor.current?.getBoundingClientRect();
    const c = card.current?.getBoundingClientRect();
    if (!a || !c) return;
    const below = a.top - GAP - c.height < EDGE;
    const top = below ? a.bottom + GAP : a.top - GAP - c.height;
    const center = a.left + a.width / 2 - c.width / 2;
    const left = Math.max(EDGE, Math.min(center, window.innerWidth - EDGE - c.width));
    setPos({ left, top, below });
  }, [open, content]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => e.key === "Escape" && hide();
    window.addEventListener("keydown", onKey);
    window.addEventListener("scroll", hide, true);
    window.addEventListener("resize", hide);
    return () => {
      window.removeEventListener("keydown", onKey);
      window.removeEventListener("scroll", hide, true);
      window.removeEventListener("resize", hide);
    };
  }, [open, hide]);

  const p = children.props as Record<string, ((e: never) => void) | undefined>;
  const chain =
    <E,>(name: string, fn: (e: E) => void) =>
    (e: E) => {
      fn(e);
      (p[name] as ((e: E) => void) | undefined)?.(e);
    };

  const trigger = cloneElement(children, {
    "aria-describedby": open ? id : undefined,
    onPointerEnter: chain<PointerEvent<HTMLElement>>("onPointerEnter", (e) => {
      if (e.pointerType !== "touch") show(e.currentTarget);
    }),
    onPointerLeave: chain("onPointerLeave", hide),
    onFocus: chain<FocusEvent<HTMLElement>>("onFocus", (e) => {
      if (e.currentTarget.matches(":focus-visible")) show(e.currentTarget);
    }),
    onBlur: chain("onBlur", hide),
    onClick: chain("onClick", () => closeOnClick && hide()),
  });

  return (
    <>
      {trigger}
      {open
        ? createPortal(
            <div
              ref={card}
              id={id}
              role="tooltip"
              className="pointer-events-none fixed z-[60] w-max max-w-[260px]"
              style={pos ? { left: pos.left, top: pos.top } : { left: -9999, top: -9999 }}
            >
              <div
                className={`rounded-control bg-surface px-3 py-2.5 text-[12px] leading-[18px] text-ink-2 shadow-slip ${pos ? (pos.below ? "tip-in-below" : "tip-in") : "opacity-0"}`}
              >
                {content}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

// TipTitle is the first line of a tip: what the control is and its state.
export function TipTitle({ children, aside }: { children: ReactNode; aside?: ReactNode }) {
  return (
    <div className="flex items-baseline gap-3 text-[12.5px] font-medium text-ink">
      <span>{children}</span>
      {aside ? <span className="ml-auto font-mono text-[11px] font-normal text-ink-3">{aside}</span> : null}
    </div>
  );
}

// TipAction is the last line: what a click does, set off by a hairline.
export function TipAction({ children }: { children: ReactNode }) {
  return <div className="mt-2 border-t border-line pt-2 text-[11.5px] text-ink-3">{children}</div>;
}
