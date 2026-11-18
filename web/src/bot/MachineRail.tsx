import { Power } from "lucide-react";
import { useState, type DragEvent } from "react";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { chatDoing, machineStarting, powerWord, runLook } from "../statusLook";
import { Tip, TipAction, TipTitle } from "../Tip";
import { isPageKey, type PageKey, type TabKey } from "./layout";
import { useDashFresh } from "./ChatTabs";
import { UnreadBadge } from "./SideNav";
import { tabMenu, type TabControl } from "./tabControl";
import { keyMeta } from "./tabMeta";
import { carriesTab, dragEnd, dragStart, dropped, onMenu, useDragging } from "./TabMenu";

function hit(on: boolean, beside: boolean) {
  return `relative flex h-9 w-9 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow,transform] duration-[160ms] ease-quiet active:scale-[.94] active:duration-[70ms] motion-reduce:active:scale-100 disabled:cursor-not-allowed disabled:opacity-50 ${
    on ? "bg-surface text-ink shadow-card" : beside ? "bg-surface/60 text-ink shadow-[inset_0_0_0_1px_var(--color-line)]" : "text-ink-2 hover:bg-pressed hover:text-ink"
  }`;
}

// The open item's cobalt bar, on the side its page opens toward; faint for
// the one in the split.
const bar = (faint: boolean) => <span aria-hidden className={`absolute inset-y-2 -left-1.5 w-[3px] rounded-full bg-cobalt ${faint ? "opacity-40" : ""}`} />;

// The wide layout's right edge, on every page of a Bot: every page that is
// not pinned to the tab bar, as an icon. From the top: the conversations' and
// the pages beside them (the Dashboard and the main chat once unpinned,
// Automations, Artifacts, Memories, Knowledge, Feed, Channels); a hairline;
// the machine (Files, Desktop, Console, Changes, and Mail while the Bot has
// an address); then Settings and Start/Stop at the foot. A press opens the
// page as a tab; a right-click is its menu (open in the split, pin); an icon
// drags onto the bar or the page, and a tab dragged here is put away.
export function MachineRail({
  bot,
  ctl,
  rail,
  pendingCount,
  onStart,
  onStop,
}: {
  bot: Bot;
  ctl: TabControl;
  rail: { pages: PageKey[]; machine: PageKey[]; foot: PageKey[] };
  pendingCount: number;
  onStart: () => void;
  onStop: () => void;
}) {
  const starting = machineStarting(bot);
  const power = powerWord(bot);
  const dash = useDashFresh(bot);
  const dragging = useDragging();
  const [taking, setTaking] = useState(false);
  const takes = !!dragging && isPageKey(dragging);

  function mark(k: PageKey, on: boolean) {
    if (on) return null;
    if (k === "feed" && bot.feedUnread > 0) return <UnreadBadge n={bot.feedUnread} dot />;
    if (k === "settings" && bot.auditUnseen > 0) return <UnreadBadge n={bot.auditUnseen} dot />;
    if (k === "dashboard" && pendingCount + dash.fresh > 0) return <UnreadBadge n={pendingCount + dash.fresh} dot />;
    if (k === "main") {
      const d = chatDoing(bot, ctl.mainId);
      return d ? <Lamp status={runLook(d).lamp} className="absolute top-1.5 right-1.5" /> : null;
    }
    return null;
  }

  const item = (k: PageKey) => {
    const label = t(keyMeta[k].label);
    const Icon = keyMeta[k].icon;
    const on = ctl.current === k;
    const beside = ctl.split === k;
    return (
      <Tip
        key={k}
        side="left"
        content={
          <>
            <TipTitle aside={beside ? t("Split") : undefined}>{label}</TipTitle>
            <TipAction>{t("Right-click to pin it or open it in the split view.")}</TipAction>
          </>
        }
      >
        <button
          type="button"
          aria-label={label}
          aria-pressed={on || beside}
          data-tab-on={on || undefined}
          draggable
          onDragStart={(e) => dragStart(e, k)}
          onDragEnd={dragEnd}
          onContextMenu={onMenu(label, () => tabMenu(k, ctl, "rail"))}
          onClick={() => ctl.open(k)}
          className={hit(on, beside)}
        >
          {on || beside ? bar(!on) : null}
          <Icon size={18} />
          {mark(k, on)}
        </button>
      </Tip>
    );
  };

  function over(e: DragEvent) {
    if (!carriesTab(e) || !takes) return;
    e.preventDefault();
    e.dataTransfer.dropEffect = "move";
    setTaking(true);
  }
  function drop(e: DragEvent) {
    e.preventDefault();
    setTaking(false);
    const k = dropped(e);
    if (k && isPageKey(k)) stow(k);
  }
  // A tab dragged here goes back to the rail: unpinned, and closed.
  function stow(k: TabKey) {
    ctl.unpin(k);
    ctl.close(k);
  }

  return (
    <aside
      onDragOver={over}
      onDragLeave={(e) => {
        if (!(e.relatedTarget instanceof Node && e.currentTarget.contains(e.relatedTarget))) setTaking(false);
      }}
      onDrop={drop}
      className={`relative hidden w-12 shrink-0 flex-col items-center gap-1 overflow-x-hidden overflow-y-auto pt-2.5 pb-4 transition-[background-color] duration-[160ms] ease-quiet wide:flex ${
        takes && taking ? "bg-cobalt-pale" : "bg-well"
      }`}
      aria-label={t("Pages and machine")}
    >
      {rail.pages.map(item)}
      {rail.pages.length && rail.machine.length ? <span aria-hidden className="my-1.5 h-px w-6 shrink-0 bg-line" /> : null}
      {rail.machine.map(item)}
      <span className="mt-auto" />
      {rail.foot.map(item)}
      <Tip side="left" content={<TipTitle>{power}</TipTitle>}>
        <button type="button" aria-label={power} className={hit(false, false)} disabled={!bot.workerConnected && starting} onClick={bot.workerConnected ? onStop : onStart}>
          <Power size={17} />
        </button>
      </Tip>
    </aside>
  );
}
