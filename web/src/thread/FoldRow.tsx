import { ChevronRight } from "lucide-react";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { useAutoExpand } from "../autoExpand";
import { Collapse } from "../Collapse";

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
//
// The row is flush with the reply text above and below it: its hover tone
// bleeds 8px past the column instead of insetting the icon. Open, the body
// hangs under the icon on a hairline, so a row reads as one line of the thread
// that unfolds, not a card set into it. Parts of a title can key off
// `group-hover/row` to brighten with the row.
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
    <div className="-mx-2 min-w-0">
      <button
        type="button"
        aria-expanded={has ? open : undefined}
        disabled={!has}
        onClick={() => setOpen((v) => !v)}
        className="group/row flex h-8 w-full min-w-0 items-center gap-2 rounded-control px-2 text-left text-[13px] transition-colors duration-[160ms] ease-quiet enabled:hover:bg-well disabled:cursor-default"
      >
        {lead}
        {title}
        {tail}
        <span className="min-w-2 flex-1" />
        {has ? (
          <ChevronRight
            size={14}
            className={`shrink-0 text-ink-3 transition-[transform,opacity] duration-[260ms] ease-quiet motion-reduce:transition-opacity ${
              open ? "rotate-90" : "pointer-fine:opacity-0 pointer-fine:group-hover/row:opacity-100 pointer-fine:group-focus-visible/row:opacity-100"
            }`}
          />
        ) : null}
      </button>
      {has ? (
        <Collapse open={open}>
          <div className="mr-2 ml-[15px] border-l border-line pt-0.5 pb-2 pl-4">
            <LiveTail live={live && open}>{children}</LiveTail>
          </div>
        </Collapse>
      ) : null}
    </div>
  );
}
