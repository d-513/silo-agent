import { Folder, Monitor, Power, SquareTerminal, type LucideIcon } from "lucide-react";
import type { Bot } from "../gen/silo/v1/ui_pb";
import type { PaneKind } from "./tabs";

export const paneMeta: Record<PaneKind, { label: string; icon: LucideIcon }> = {
  files: { label: "Files", icon: Folder },
  desktop: { label: "Desktop", icon: Monitor },
  console: { label: "Console", icon: SquareTerminal },
};

function hit(on: boolean) {
  return `relative flex h-9 w-9 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow,transform] duration-[160ms] ease-quiet active:scale-[.94] active:duration-[70ms] motion-reduce:active:scale-100 disabled:cursor-not-allowed disabled:opacity-50 ${
    on ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
  }`;
}

// The wide layout's right edge: the Bot's machine. Files, Desktop and Console
// open as a pane beside the chat (the open one carries the cobalt bar on the
// side it opens toward), and Start/Stop sits at the foot.
export function MachineRail({
  bot,
  open,
  onPick,
  onStart,
  onStop,
}: {
  bot: Bot;
  // The pane showing now, docked or as its own page.
  open: PaneKind | "";
  onPick: (kind: PaneKind) => void;
  onStart: () => void;
  onStop: () => void;
}) {
  const starting = bot.status === "starting";
  const power = bot.workerConnected ? "Stop Bot" : starting ? "Starting…" : "Start Bot";
  return (
    <aside className="hidden w-12 shrink-0 flex-col items-center gap-1 bg-well pt-2.5 pb-4 wide:flex" aria-label="Machine">
      {(Object.keys(paneMeta) as PaneKind[]).map((k) => {
        const { label, icon: Icon } = paneMeta[k];
        const on = open === k;
        return (
          <button key={k} type="button" title={label} aria-label={label} aria-pressed={on} data-tab-on={on || undefined} className={hit(on)} onClick={() => onPick(k)}>
            {on ? <span aria-hidden className="absolute inset-y-2 -left-1.5 w-[3px] rounded-full bg-cobalt" /> : null}
            <Icon size={18} />
          </button>
        );
      })}
      <button
        type="button"
        title={power}
        aria-label={power}
        className={`${hit(false)} mt-auto`}
        disabled={!bot.workerConnected && starting}
        onClick={bot.workerConnected ? onStop : onStart}
      >
        <Power size={17} />
      </button>
    </aside>
  );
}
