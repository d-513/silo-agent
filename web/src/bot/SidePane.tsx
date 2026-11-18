import { Columns2, Maximize2, X, type LucideIcon } from "lucide-react";
import { Suspense, useEffect, useRef, useState, type CSSProperties, type PointerEvent, type ReactNode } from "react";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { lazyNamed } from "../lazyNamed";
import { PaneFallback } from "../PaneFallback";
import { clampShare, setShare } from "./layoutStore";
import { paneKinds, type PaneKind } from "./tabs";
import { msg, t } from "../i18n.ts";

const MachinePane = lazyNamed(() => import("./MachinePane"), "MachinePane");
const FilesPane = lazyNamed(() => import("../Files"), "FilesPane");
const MailPane = lazyNamed(() => import("../Mail"), "MailPane");
const ChangesPane = lazyNamed(() => import("../Changes"), "ChangesPane");

// The split's share is a share of the row it divides with the page.
const widthOf = (share: number) => `${share * 100}%`;

// Where something stands in the Bot page's row: the page, the split beside
// it, or nowhere (kept, hidden).
export type Slot = "main" | "side" | "";

/** The flex item a slot is: the page fills the row, the split takes its share. */
export function slotBox(slot: Slot, share: number): { className: string; style?: CSSProperties } {
  if (slot === "main") return { className: "relative order-1 flex min-h-0 min-w-0 flex-1 flex-col" };
  if (slot === "side") {
    return {
      className: "relative order-3 flex min-h-0 max-w-[calc(100%-360px)] min-w-[320px] shrink-0 flex-col shadow-[inset_1px_0_0_var(--color-line)]",
      style: { width: widthOf(share) },
    };
  }
  return { className: "hidden" };
}

const captions: Record<PaneKind, string> = {
  desktop: msg("Same browser the Bot uses. You can type and click."),
  console: msg("A shell on this Bot, started in /workspace."),
  files: "",
  changes: msg("What changed in /workspace, compared before and after each run."),
  mail: msg("What arrives at the Bot's address. It only receives."),
};

export function Act({ dark = false, label, icon: Icon, onClick }: { dark?: boolean; label: string; icon: LucideIcon; onClick: () => void }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-sm transition-colors duration-[160ms] ease-quiet ${
        dark ? "text-on-hatch/70 hover:bg-on-hatch/10 hover:text-on-hatch" : "text-ink-2 hover:bg-well hover:text-ink"
      }`}
    >
      <Icon size={14} />
    </button>
  );
}

// The edge between the page and the split: a hairline that takes the drag.
// It sizes the split it stands in (its parent): the width follows the pointer
// on the element itself and is saved once on release, so a drag never
// re-renders the thread. Double-click evens it.
export function Grip() {
  const self = useRef<HTMLDivElement>(null);
  const [drag, setDrag] = useState(false);
  const last = useRef(0);
  function move(e: PointerEvent) {
    const el = self.current?.parentElement;
    const room = el?.parentElement?.getBoundingClientRect().width ?? 0;
    if (!el || room <= 0) return;
    last.current = clampShare((el.getBoundingClientRect().right - e.clientX) / room);
    el.style.width = widthOf(last.current);
  }
  function end() {
    if (!drag) return;
    setDrag(false);
    if (last.current) setShare(last.current);
  }
  return (
    <div
      ref={self}
      role="separator"
      aria-orientation="vertical"
      aria-label={t("Resize the split")}
      className="group/grip absolute inset-y-0 left-0 z-10 w-2 cursor-col-resize touch-none"
      onPointerDown={(e) => {
        e.preventDefault();
        try {
          e.currentTarget.setPointerCapture(e.pointerId);
        } catch {
          /* a pointer that is already gone: the drag just ends early */
        }
        last.current = 0;
        setDrag(true);
      }}
      onPointerMove={(e) => {
        if (drag) move(e);
      }}
      onPointerUp={end}
      onPointerCancel={end}
      onDoubleClick={() => setShare(0.5)}
    >
      <span
        aria-hidden
        className={`absolute inset-y-0 left-0 transition-[width,background-color] duration-[160ms] ease-quiet group-hover/grip:w-[3px] group-hover/grip:bg-line-strong ${
          drag ? "w-[3px] bg-line-strong" : "w-px bg-line"
        }`}
      />
    </div>
  );
}

/** What a pane's head offers: from the split, to be the page or to leave; as the page, to move to the split. */
export function slotActions(slot: Slot, dark: boolean, a: { onMain: () => void; onSplit?: () => void; onClose: () => void }): ReactNode {
  if (slot === "side") {
    return (
      <>
        <Act dark={dark} label={t("Open as the page")} icon={Maximize2} onClick={a.onMain} />
        <Act dark={dark} label={t("Close split view")} icon={X} onClick={a.onClose} />
      </>
    );
  }
  return a.onSplit ? <Act dark={dark} label={t("Open in split view")} icon={Columns2} onClick={a.onSplit} /> : null;
}

// Panes are the Bot's machine in the page: Files, the Desktop, the Console,
// what changed in the workspace, and Mail (which needs no machine). Each is
// one element in the Bot page's row for as long as the page lives: it stands
// as the page, in the split, or hidden (kept once opened), so moving it
// between those keeps the desktop's connection and the files' place, and two
// of them can be open at once.
export function Panes({
  bot,
  main,
  side,
  share,
  openAt,
  wide,
  onStart,
  onOpen,
  onSplit,
  onUnsplit,
  onError,
}: {
  bot: Bot;
  // The pane that is the page, and the one in the split ("" for none).
  main: PaneKind | "";
  side: PaneKind | "";
  share: number;
  // The folder Files opens in (Drives → Open in Files).
  openAt: string;
  // There is room for a split.
  wide: boolean;
  onStart: () => void;
  onOpen: (k: PaneKind) => void;
  onSplit: (k: PaneKind) => void;
  onUnsplit: () => void;
  onError: (s: string) => void;
}) {
  const [kept, setKept] = useState<ReadonlySet<PaneKind>>(new Set());
  useEffect(() => {
    const add = [main, side].filter((k): k is PaneKind => !!k);
    if (add.length) setKept((s) => (add.every((k) => s.has(k)) ? s : new Set([...s, ...add])));
  }, [main, side]);
  const online = bot.workerConnected;
  const slotOf = (k: PaneKind): Slot => (k === main ? "main" : k === side ? "side" : "");

  return (
    <Suspense fallback={null}>
      {paneKinds
        .filter((k) => kept.has(k) || k === main || k === side)
        .map((k) => {
          const slot = slotOf(k);
          const box = slotBox(slot, share);
          const actions = (dark: boolean) => slotActions(slot, dark, { onMain: () => onOpen(k), onSplit: wide ? () => onSplit(k) : undefined, onClose: onUnsplit });
          const visible = slot !== "";
          const caption = slot === "main" && captions[k] ? <p className="mt-2 text-meta text-ink-2">{t(captions[k])}</p> : null;
          let body: ReactNode;
          if (k === "desktop" || k === "console") {
            body = (
              <div className="flex min-h-0 flex-1 flex-col p-3">
                <MachinePane bot={bot} onStart={onStart} visible={visible} kind={k} actions={actions(true)} onError={onError} />
                {caption}
              </div>
            );
          } else if (k === "files") {
            body = (
              <div className={`flex min-h-0 flex-1 flex-col ${online ? "" : "p-3"}`}>
                <FilesPane bot={bot} visible={visible} onStart={onStart} openAt={openAt} actions={actions(false)} />
              </div>
            );
          } else if (k === "changes") {
            // Changes keeps its header when the Bot is stopped: the drive
            // journal reads without the machine.
            body = (
              <div className="flex min-h-0 flex-1 flex-col">
                <ChangesPane bot={bot} visible={visible} onStart={onStart} onError={onError} actions={actions(false)} />
              </div>
            );
          } else {
            // Mail needs no machine: it shows whether the Bot is up or not.
            body = (
              <div className="flex min-h-0 flex-1 flex-col">
                <MailPane bot={bot} onError={onError} actions={actions(false)} />
                {caption ? <div className="px-3 pb-3">{caption}</div> : null}
              </div>
            );
          }
          return (
            <section key={k} aria-label={slot === "side" ? t("Split view") : undefined} className={box.className} style={box.style}>
              {slot === "side" ? <Grip /> : null}
              <Suspense fallback={<PaneFallback />}>{body}</Suspense>
              {/* A stopped Bot has no pane header to carry these. Mail and Changes have their own. */}
              {visible && k !== "mail" && k !== "changes" && !online ? <div className="absolute top-5 right-5 flex items-center gap-0.5">{actions(false)}</div> : null}
            </section>
          );
        })}
    </Suspense>
  );
}
