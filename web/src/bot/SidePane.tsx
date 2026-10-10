import { Maximize2, Minimize2, X, type LucideIcon } from "lucide-react";
import { Suspense, useEffect, useRef, useState, type PointerEvent, type RefObject } from "react";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { lazyNamed } from "../lazyNamed";
import { PaneFallback } from "../PaneFallback";
import { clampShare, setShare } from "./paneStore";
import type { PaneKind } from "./tabs";

const MachinePane = lazyNamed(() => import("./MachinePane"), "MachinePane");
const FilesPane = lazyNamed(() => import("../Files"), "FilesPane");
const MailPane = lazyNamed(() => import("../Mail"), "MailPane");
const ChangesPane = lazyNamed(() => import("../Changes"), "ChangesPane");

// What the docked pane's share is a share of: the row without the sidebar
// (248) and the machine rail (48).
const chrome = 296;
const widthOf = (share: number) => `calc((100% - ${chrome}px) * ${share})`;

const captions: Record<PaneKind, string> = {
  desktop: "Same browser the Bot uses. You can type and click.",
  console: "A shell on this Bot, started in /workspace.",
  files: "",
  changes: "What changed in /workspace, compared before and after each run.",
  mail: "What arrives at the Bot's address. It only receives.",
};

function Act({ dark, label, icon: Icon, onClick }: { dark: boolean; label: string; icon: LucideIcon; onClick: () => void }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className={`flex h-7 w-7 shrink-0 items-center justify-center rounded-sm transition-colors duration-[160ms] ease-quiet ${
        dark ? "text-white/70 hover:bg-white/10 hover:text-white" : "text-ink-2 hover:bg-well hover:text-ink"
      }`}
    >
      <Icon size={14} />
    </button>
  );
}

// The edge between the chat and the docked pane: a hairline that takes the
// drag. The width follows the pointer on the element itself and is saved once
// on release, so a drag never re-renders the thread. Double-click evens it.
function Grip({ pane }: { pane: RefObject<HTMLElement | null> }) {
  const [drag, setDrag] = useState(false);
  const last = useRef(0);
  function move(e: PointerEvent) {
    const el = pane.current;
    const room = (el?.parentElement?.getBoundingClientRect().width ?? 0) - chrome;
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
      role="separator"
      aria-orientation="vertical"
      aria-label="Resize the pane"
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

// SidePane is the Bot's machine in the page: Files, the Desktop, the Console or
// what changed in the workspace (and Mail, which needs no machine).
// Docked, it shares the row with the chat and its left edge drags; as its own
// page (`max`) it takes the row. Either way it is this one element, so going
// between the two keeps the desktop's connection and the files' place, and a
// pane stays mounted (hidden) once opened so a trip elsewhere does too.
export function SidePane({
  bot,
  kind,
  max,
  share,
  openAt,
  canDock,
  onStart,
  onMax,
  onDock,
  onClose,
  onError,
}: {
  bot: Bot;
  // The pane showing now; "" shows none.
  kind: PaneKind | "";
  max: boolean;
  share: number;
  // The folder Files opens in (Drives → Open in Files).
  openAt: string;
  // There is room to sit beside the chat (the wide layout).
  canDock: boolean;
  onStart: () => void;
  onMax: () => void;
  onDock: () => void;
  onClose: () => void;
  onError: (s: string) => void;
}) {
  const ref = useRef<HTMLElement>(null);
  const [kept, setKept] = useState<ReadonlySet<PaneKind>>(new Set());
  useEffect(() => {
    if (kind) setKept((s) => (s.has(kind) ? s : new Set(s).add(kind)));
  }, [kind]);
  const has = (k: PaneKind) => kind === k || kept.has(k);
  const online = bot.workerConnected;

  const actions = (dark: boolean) =>
    max ? (
      canDock ? <Act dark={dark} label="Dock beside the chat" icon={Minimize2} onClick={onDock} /> : null
    ) : (
      <>
        <Act dark={dark} label="Open as a page" icon={Maximize2} onClick={onMax} />
        <Act dark={dark} label="Close" icon={X} onClick={onClose} />
      </>
    );

  const machine = (k: "desktop" | "console") =>
    has(k) ? (
      <div className={`min-h-0 flex-1 flex-col p-3 ${kind === k ? "flex" : "hidden"}`}>
        <MachinePane bot={bot} onStart={onStart} visible={kind === k} kind={k} actions={actions(true)} />
        {max ? <p className="mt-2 text-[12.5px] text-ink-2">{captions[k]}</p> : null}
      </div>
    ) : null;

  return (
    <aside
      ref={ref}
      aria-label={kind ? "Side pane" : undefined}
      className={`relative min-h-0 min-w-0 flex-col ${kind ? "flex" : "hidden"} ${max ? "flex-1" : "max-w-[calc(100%-656px)] min-w-[320px] shrink-0"}`}
      style={max ? undefined : { width: widthOf(share) }}
    >
      {max ? null : <Grip pane={ref} />}
      <Suspense fallback={<PaneFallback />}>
        {machine("desktop")}
        {machine("console")}
        {has("files") ? (
          <div className={`min-h-0 flex-1 flex-col ${online ? "" : "p-3"} ${kind === "files" ? "flex" : "hidden"}`}>
            <FilesPane bot={bot} onStart={onStart} openAt={openAt} actions={actions(false)} />
          </div>
        ) : null}
        {has("changes") ? (
          <div className={`min-h-0 flex-1 flex-col ${online ? "" : "p-3"} ${kind === "changes" ? "flex" : "hidden"}`}>
            <ChangesPane bot={bot} visible={kind === "changes"} onStart={onStart} onError={onError} actions={actions(false)} />
          </div>
        ) : null}
        {/* Mail needs no machine: it shows whether the Bot is up or not. */}
        {has("mail") ? (
          <div className={`min-h-0 flex-1 flex-col ${kind === "mail" ? "flex" : "hidden"}`}>
            <MailPane bot={bot} onError={onError} actions={actions(false)} />
            {max ? <p className="mt-2 px-3 pb-3 text-[12.5px] text-ink-2">{captions.mail}</p> : null}
          </div>
        ) : null}
      </Suspense>
      {/* A stopped Bot has no pane header to carry these. Mail has its own. */}
      {kind && kind !== "mail" && !online ? <div className="absolute top-5 right-5 flex items-center gap-0.5">{actions(false)}</div> : null}
    </aside>
  );
}
