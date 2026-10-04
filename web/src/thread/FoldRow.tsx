import { Check, ChevronRight, X } from "lucide-react";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useAutoExpand } from "../autoExpand";
import { Spinner } from "../Feedback";

export type RowState = "running" | "done" | "waiting" | "stopped";

export function rowState(t: { running?: boolean; waiting?: boolean; outcome?: string; result?: string }): RowState {
  if (t.waiting) return "waiting";
  if (t.outcome) return "stopped";
  if (t.running) return "running";
  return "done";
}

export function rowVerb(state: RowState, outcome: string | undefined, live: string, past: string) {
  if (outcome === "denied") return "Skipped";
  if (outcome === "stopped") return "Stopped";
  return state === "running" || state === "waiting" ? live : past;
}

// 16px slot that crossfades spinner → check / paused dot / ✕.
export function StateSlot({ state }: { state: RowState }) {
  const cell = (on: boolean) =>
    `col-start-1 row-start-1 flex items-center justify-center transition-[opacity,transform] duration-300 ease-settle motion-reduce:transition-opacity ${
      on ? "opacity-100" : "scale-[.6] opacity-0"
    }`;
  return (
    <span className="grid h-4 w-4 shrink-0 place-items-center" aria-hidden>
      <span className={cell(state === "running")}>{state === "running" ? <Spinner size={13} /> : null}</span>
      <span className={cell(state === "done")}>
        <Check size={15} strokeWidth={2.25} className="text-emerald" />
      </span>
      <span className={cell(state === "waiting")}>
        <span className="breathe h-[7px] w-[7px] rounded-full bg-vermilion" />
      </span>
      <span className={cell(state === "stopped")}>
        <X size={14} className="text-ink-3" />
      </span>
    </span>
  );
}

// grid-template-rows 0fr → 1fr fold; contents fade in 60ms behind the height.
// Children mount on first open so closed rows cost nothing to render.
function Fold({ open, children }: { open: boolean; children: ReactNode }) {
  const [mounted, setMounted] = useState(open);
  if (open && !mounted) setMounted(true);
  return (
    <div
      className={`grid transition-[grid-template-rows] duration-[320ms] ease-quiet motion-reduce:transition-none ${
        open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"
      }`}
    >
      <div
        className={`min-h-0 overflow-hidden transition-opacity ease-quiet ${
          open ? "opacity-100 delay-[60ms] duration-[260ms]" : "opacity-0 duration-[120ms]"
        }`}
        inert={!open}
      >
        {mounted ? children : null}
      </div>
    </div>
  );
}

// While streaming, keep the newest lines in view: the body is capped and pinned
// to its bottom, with the top fading out once it overflows.
function LiveTail({ live, children }: { live: boolean; children: ReactNode }) {
  const ref = useRef<HTMLDivElement>(null);
  const [clipped, setClipped] = useState(false);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    if (!live) {
      if (clipped) setClipped(false);
      return;
    }
    el.scrollTop = el.scrollHeight;
    const c = el.scrollHeight > el.clientHeight + 1;
    if (c !== clipped) setClipped(c);
  });
  return (
    <div
      ref={ref}
      className={live ? "max-h-[220px] overflow-hidden" : ""}
      style={clipped ? { maskImage: "linear-gradient(to bottom, transparent, #000 40px)", WebkitMaskImage: "linear-gradient(to bottom, transparent, #000 40px)" } : undefined}
    >
      <div className="space-y-2">{children}</div>
    </div>
  );
}

// With auto-expand on, a `live` row is open while it streams and folds shut
// when it finishes; off, rows stay shut. The reader's own toggle always wins.
export function FoldRow({
  lead,
  title,
  tail,
  live = false,
  children,
}: {
  lead?: ReactNode;
  title: ReactNode;
  tail?: ReactNode;
  live?: boolean;
  children?: ReactNode;
}) {
  const [manual, setManual] = useState<boolean | null>(null);
  const auto = useAutoExpand();
  const has = !!children;
  const open = has && (manual ?? (live && auto));
  const setOpen = (f: (v: boolean) => boolean) => setManual(f(open));
  return (
    <div
      className={`max-w-full rounded-card transition-[background-color,box-shadow] duration-[200ms] ease-quiet ${
        open ? "bg-surface shadow-card" : ""
      }`}
    >
      <button
        type="button"
        aria-expanded={has ? open : undefined}
        disabled={!has}
        onClick={() => setOpen((v) => !v)}
        className={`flex h-8 w-full min-w-0 items-center gap-2 rounded-card px-3 text-left text-[13px] transition-colors duration-[160ms] ease-quiet disabled:cursor-default ${
          open ? "" : "enabled:hover:bg-well"
        }`}
      >
        {lead}
        {title}
        {tail}
        <span className="min-w-2 flex-1" />
        {has ? (
          <ChevronRight
            size={14}
            className={`shrink-0 text-ink-3 transition-transform duration-[320ms] ease-quiet ${open ? "rotate-90" : ""}`}
          />
        ) : null}
      </button>
      {has ? (
        <Fold open={open}>
          <div className="px-3 pb-3">
            <LiveTail live={live && open}>{children}</LiveTail>
          </div>
        </Fold>
      ) : null}
    </div>
  );
}
