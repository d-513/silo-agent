import { Link } from "@tanstack/react-router";
import { Menu, Plus, Power } from "lucide-react";
import { useEffect, useRef, type ReactNode, type TouchEvent } from "react";
import { Crest } from "../Crest";
import type { Bot, Chat } from "../gen/silo/v1/ui_pb";
import { msg, t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { Rail } from "../Shell";
import { paneMeta } from "./MachineRail";
import { onSettings, paneKinds, type PaneKind, type Tab } from "./tabs";

// The narrow layout's way around a Bot. A wide screen has the sidebar, the
// page and the machine rail side by side; a phone has room for one, so the
// page gets the screen and everything else waits in a drawer: one top bar
// (the Bot, the page you are on, New chat) whose press slides it in.

const pageWords: Partial<Record<Tab, string>> = {
  dashboard: msg("Dashboard"),
  automations: msg("Automations"),
  artifacts: msg("Artifacts"),
  memories: msg("Memories"),
  knowledge: msg("Knowledge"),
  feed: msg("Feed"),
  channels: msg("Channels"),
};

/** What the top bar says you are looking at. */
function pageLabel(tab: Tab, chat: Chat | undefined, fresh: boolean): string {
  if (tab === "run") {
    if (!chat) return fresh ? t("New chat") : "";
    return chat.main ? t("Main chat") : chat.title || t("New chat");
  }
  if (onSettings(tab)) return t("Settings");
  const pane = paneKinds.find((k) => k === tab);
  if (pane) return t(paneMeta[pane].label);
  const word = pageWords[tab];
  return word ? t(word) : "";
}

export function BotTopBar({
  bot,
  tab,
  chat,
  fresh,
  attention,
  open,
  onOpen,
  onNewChat,
}: {
  bot: Bot;
  tab: Tab;
  chat?: Chat;
  fresh: boolean;
  // What waits in the drawer: an approval (needs you), or something new.
  attention: "needs" | "new" | "";
  open: boolean;
  onOpen: () => void;
  onNewChat: () => void;
}) {
  const label = pageLabel(tab, chat, fresh);
  const menu = t("Open {{name}} menu", { name: bot.name });
  return (
    <header className="flex h-[calc(3rem+env(safe-area-inset-top))] shrink-0 items-center gap-1 bg-well pt-[env(safe-area-inset-top)] pr-1.5 pl-1 shadow-[inset_0_-1px_0_var(--color-line)] wide:hidden">
      <button
        type="button"
        aria-label={menu}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={onOpen}
        className="flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-control pr-2 pl-2 text-left transition-[background-color,transform] duration-[160ms] ease-quiet active:scale-[.985] active:bg-pressed motion-reduce:active:scale-100"
      >
        <span className="relative flex h-9 w-6 shrink-0 items-center justify-center text-ink-2">
          <Menu size={20} />
          {attention ? (
            <span
              aria-hidden
              className={`absolute top-1 -right-1 h-2 w-2 rounded-full ring-2 ring-well ${attention === "needs" ? "bg-vermilion" : "bg-cobalt"}`}
            />
          ) : null}
        </span>
        <span className="relative flex h-7 w-7 shrink-0">
          <Crest index={bot.crest} size={28} />
          <Lamp status={bot.status} onCrest="well" className="absolute -right-0.5 -bottom-0.5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="block truncate text-[14.5px] leading-[18px] font-semibold text-ink">{bot.name}</span>
          {label ? <span className="block truncate text-[12.5px] leading-4 text-ink-3">{label}</span> : null}
        </span>
      </button>
      {tab === "run" && fresh && !chat ? null : (
        <button
          type="button"
          title={t("New chat")}
          aria-label={t("New chat")}
          onClick={onNewChat}
          className="flex h-10 w-10 shrink-0 items-center justify-center rounded-control bg-surface text-ink shadow-card transition-transform duration-[160ms] ease-quiet active:scale-[.94] motion-reduce:active:scale-100"
        >
          <Plus size={18} strokeWidth={2.25} />
        </button>
      )}
    </header>
  );
}

const paneTo = {
  files: "/bots/$botId/files",
  desktop: "/bots/$botId/desktop",
  console: "/bots/$botId/console",
  changes: "/bots/$botId/changes",
  mail: "/bots/$botId/mail",
} as const;

const tile =
  "relative flex h-10 min-w-0 items-center gap-2 rounded-control px-2.5 text-[13px] font-medium transition-[background-color,color,box-shadow] duration-[160ms] ease-quiet outline-offset-[-2px] disabled:opacity-50";

// MachineTiles are the drawer's way to the machine: each pane as its own
// page (there is no room to dock one), and Start/Stop.
export function MachineTiles({ bot, tab, onStart, onStop }: { bot: Bot; tab: Tab; onStart: () => void; onStop: () => void }) {
  const starting = bot.status === "starting";
  const power = bot.workerConnected ? t("Stop Bot") : starting ? t("Starting…") : t("Start Bot");
  const kinds = paneKinds.filter((k) => k !== "mail" || bot.mailAddress || tab === "mail");
  return (
    <section aria-label={t("Machine")} className="mt-4 px-2">
      <h2 className="flex h-9 items-center px-3 text-label-caps leading-4 text-ink-3 uppercase">{t("Machine")}</h2>
      <div className="grid grid-cols-3 gap-1">
        {kinds.map((k: PaneKind) => {
          const { icon: Icon } = paneMeta[k];
          const on = tab === k;
          return (
            <Link
              key={k}
              to={paneTo[k]}
              params={{ botId: bot.id }}
              data-tab-on={on || undefined}
              className={`${tile} ${on ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"}`}
            >
              <Icon size={16} className="shrink-0" />
              <span className="truncate">{t(paneMeta[k].label)}</span>
            </Link>
          );
        })}
        <button
          type="button"
          className={`${tile} ${bot.workerConnected ? "text-ink-2 hover:bg-pressed hover:text-ink" : "text-emerald hover:bg-pressed"}`}
          disabled={!bot.workerConnected && starting}
          onClick={bot.workerConnected ? onStop : onStart}
        >
          <Power size={15} className="shrink-0" />
          <span className="truncate">{power}</span>
        </button>
      </div>
    </section>
  );
}

// BotDrawer slides in from the left over the page: the rail (Workspaces and
// Bots) on top, then the Bot's sidebar. A press outside, Escape, a swipe back
// to the left, or any move to another page puts it away.
export function BotDrawer({ open, botId, label, onClose, children }: { open: boolean; botId: string; label: string; onClose: () => void; children: ReactNode }) {
  const panel = useRef<HTMLDivElement>(null);
  const drag = useRef<{ x: number; y: number; dx: number; on: boolean | null } | null>(null);

  const close = useRef(onClose);
  close.current = onClose;
  useEffect(() => {
    if (!open) return;
    panel.current?.focus({ preventScroll: true });
  }, [open]);
  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") close.current();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);

  // A swipe toward the edge follows the finger and closes past a third of
  // the way; a mostly vertical move is left to the scroll.
  function onTouchStart(e: TouchEvent) {
    // The rail's row of Bots scrolls sideways itself.
    if (e.target instanceof Element && e.target.closest(".silo-scroll-x")) {
      drag.current = null;
      return;
    }
    const p = e.touches[0];
    drag.current = { x: p.clientX, y: p.clientY, dx: 0, on: null };
  }
  function onTouchMove(e: TouchEvent) {
    const d = drag.current;
    const el = panel.current;
    if (!d || !el) return;
    const p = e.touches[0];
    const dx = p.clientX - d.x;
    const dy = p.clientY - d.y;
    if (d.on === null) {
      if (Math.abs(dx) < 8 && Math.abs(dy) < 8) return;
      d.on = Math.abs(dx) > Math.abs(dy) && dx < 0;
    }
    if (!d.on) return;
    d.dx = Math.min(0, dx);
    el.style.transition = "none";
    el.style.transform = `translateX(${d.dx}px)`;
  }
  function onTouchEnd() {
    const d = drag.current;
    const el = panel.current;
    drag.current = null;
    if (!d?.on || !el) return;
    el.style.transition = "";
    el.style.transform = "";
    if (-d.dx > el.offsetWidth / 3) onClose();
  }

  return (
    <div className={`fixed inset-0 z-40 wide:hidden ${open ? "" : "pointer-events-none"}`} inert={!open}>
      <div
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
        data-bot={botId}
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={onTouchEnd}
        onTouchCancel={onTouchEnd}
        className={`absolute inset-y-0 left-0 flex w-[min(22rem,88vw)] flex-col bg-well pb-[env(safe-area-inset-bottom)] outline-none transition-transform duration-[320ms] ease-quiet motion-reduce:transition-none ${
          open ? "translate-x-0 shadow-slip" : "-translate-x-full"
        }`}
      >
        <Rail page="bots" activeBotId={botId} />
        {children}
      </div>
    </div>
  );
}
