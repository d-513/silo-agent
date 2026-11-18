import { Link } from "@tanstack/react-router";
import { PanelRight, Phone, Power } from "lucide-react";
import { Clock } from "../call/CallBar";
import type { Call } from "../call/useCall";
import { Crest } from "../Crest";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp, Status, StatusWord } from "../Lamp";
import { NavButton } from "../nav/NavSheet";
import { sourceLabel } from "../sources";
import { botLook, crestState, machineStarting, pausedOnYou, powerWord } from "../statusLook";
import { ActivityStrip } from "./ActivityStrip";
import { People } from "./ChatTabs";
import { useHeard } from "./heardStore";
import { isPageKey, type PageKey } from "./layout";
import { ActiveBar, UnreadBadge } from "./SideNav";
import type { TabControl } from "./tabControl";
import { keyLink, keyMeta } from "./tabMeta";
import { onSettings, type Tab } from "./tabs";

// The narrow layout's way around a Bot. A wide screen shows the sidebar, the
// tab bar, the page and the right rail side by side; a phone shows the page
// under one top bar and the tab strip, and keeps the two columns at its
// edges: the menu (or a swipe in from the left) is the sidebar, the Bot's
// name (or the rail glyph, or a swipe in from the right) its rail, each as a
// sheet on the side it stands on from 960 up.

/** The page a top bar names, when it is not a conversation. */
function pageLabel(tab: Tab): string {
  if (onSettings(tab)) return t("Settings");
  return isPageKey(tab) ? t(keyMeta[tab].label) : "";
}

const square = "flex h-10 w-10 shrink-0 items-center justify-center rounded-control transition-[background-color,transform] duration-[160ms] ease-quiet active:scale-[.94] motion-reduce:active:scale-100";

export function BotTopBar({
  bot,
  tab,
  railOpen,
  onRail,
  call,
  onCall,
}: {
  bot: Bot;
  tab: Tab;
  railOpen: boolean;
  onRail: () => void;
  // The Bot's call while calls are on; its button rings the main chat.
  call?: Call;
  onCall: () => void;
}) {
  const heard = useHeard(bot.id);
  const look = botLook(bot);
  // On a conversation the line under the name is the Bot's own, the way a
  // messenger says someone is typing; on any other page it names the page.
  const label = tab === "run" || tab === "dashboard" ? "" : pageLabel(tab);
  const railDot = bot.feedUnread + bot.auditUnseen > 0;
  return (
    <header className="flex h-[calc(3rem+env(safe-area-inset-top))] shrink-0 items-center gap-0.5 bg-well pt-[env(safe-area-inset-top)] pr-1 pl-1 wide:hidden">
      <NavButton activeBotId={bot.id} />
      <button
        type="button"
        aria-label={t("Pages and machine")}
        aria-haspopup="dialog"
        aria-expanded={railOpen}
        onClick={onRail}
        className="flex h-11 min-w-0 flex-1 items-center gap-2.5 rounded-control pr-2 pl-1.5 text-left transition-[background-color,transform] duration-[160ms] ease-quiet active:scale-[.985] active:bg-pressed motion-reduce:active:scale-100"
      >
        <span className="relative flex h-8 w-8 shrink-0">
          <Crest index={bot.crest} size={32} state={crestState(bot, heard)} />
          <Lamp status={look.lamp} onCrest="well" className="absolute -top-0.5 -right-0.5" />
        </span>
        <span className="flex min-w-0 flex-1 flex-col">
          <span className="truncate text-body-lg leading-[18px] font-semibold text-ink">{bot.name}</span>
          {label ? <span className="truncate text-label leading-4 font-medium text-ink-3">{label}</span> : <StatusWord look={look} />}
        </span>
      </button>
      {call ? (
        call.live && call.snap ? (
          <button
            type="button"
            aria-label={t("Back to the call")}
            onClick={onCall}
            className="flex h-9 shrink-0 items-center gap-1.5 rounded-full bg-surface px-3 text-emerald shadow-card transition-transform duration-[160ms] ease-quiet active:scale-[.94] motion-reduce:active:scale-100"
          >
            <Lamp status="online" />
            <Clock since={call.snap.startedAt} />
          </button>
        ) : (
          <button type="button" title={t("Call")} aria-label={t("Call {{name}}", { name: bot.name })} onClick={onCall} className={`${square} text-ink active:bg-pressed`}>
            <span className="flex h-8 w-8 items-center justify-center rounded-full bg-ink text-on-ink">
              <Phone size={15} strokeWidth={2.25} />
            </span>
          </button>
        )
      ) : null}
      <button
        type="button"
        title={t("Pages and machine")}
        aria-label={t("Pages and machine")}
        aria-haspopup="dialog"
        aria-expanded={railOpen}
        onClick={onRail}
        className={`relative ${square} text-ink-2 active:bg-pressed`}
      >
        <PanelRight size={19} />
        {railDot ? <span aria-hidden className="absolute top-2 right-2 h-2 w-2 rounded-full bg-cobalt ring-2 ring-well" /> : null}
      </button>
    </header>
  );
}

const tile =
  "relative flex h-[60px] min-w-0 flex-col items-center justify-center gap-1.5 rounded-control text-label font-medium transition-[background-color,color,box-shadow,transform] duration-[160ms] ease-quiet outline-offset-[-2px] active:scale-[.96] motion-reduce:active:scale-100 disabled:opacity-50";

// MachineTiles are the sheet's way to the machine: each pane not pinned to
// the strip as its own page (there is no room for a split), and Start/Stop.
function MachineTiles({ bot, ctl, kinds, onStart, onStop }: { bot: Bot; ctl: TabControl; kinds: PageKey[]; onStart: () => void; onStop: () => void }) {
  const starting = machineStarting(bot);
  const power = powerWord(bot);
  return (
    <section aria-label={t("Machine")} className="px-2">
      <h2 className="flex h-9 items-center px-3 text-section leading-4 text-ink-3">{t("Machine")}</h2>
      <div className="grid grid-cols-3 gap-1.5 px-1">
        {kinds.map((k) => {
          const { icon: Icon } = keyMeta[k];
          const on = ctl.current === k;
          return (
            <Link
              key={k}
              {...keyLink(k, bot.id, ctl.mainId)}
              data-tab-on={on || undefined}
              className={`${tile} ${on ? "bg-surface text-ink shadow-card" : "bg-surface/55 text-ink-2 hover:bg-pressed hover:text-ink"}`}
            >
              <Icon size={18} className="shrink-0" />
              <span className="max-w-full truncate px-1">{t(keyMeta[k].label)}</span>
            </Link>
          );
        })}
        <button
          type="button"
          className={`${tile} bg-surface/55 ${bot.workerConnected ? "text-ink-2 hover:bg-pressed hover:text-ink" : "text-emerald hover:bg-pressed"}`}
          disabled={!bot.workerConnected && starting}
          onClick={bot.workerConnected ? onStop : onStart}
        >
          <Power size={17} className="shrink-0" />
          <span className="max-w-full truncate px-1">{power}</span>
        </button>
      </div>
    </section>
  );
}

// A page of the rail as a row of the sheet, with its words.
function SheetRow({ bot, ctl, k, badge = 0 }: { bot: Bot; ctl: TabControl; k: PageKey; badge?: number }) {
  const on = ctl.current === k;
  const { icon: Icon, label } = keyMeta[k];
  return (
    <Link
      {...keyLink(k, bot.id, ctl.mainId)}
      data-tab-on={on || undefined}
      className={`relative flex h-10 items-center gap-2.5 rounded-control px-3 text-row font-medium transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px] ${
        on ? "bg-surface text-ink" : "text-ink-2 hover:bg-pressed hover:text-ink"
      }`}
    >
      <ActiveBar on={on} />
      <Icon size={15} />
      <span className="min-w-0 flex-1 truncate">{t(label)}</span>
      {badge > 0 ? <UnreadBadge n={badge} /> : null}
    </Link>
  );
}

// RailSheet is the right rail with room for words: who the Bot is and what
// it is doing, its work with no tab of its own, the pages not pinned to the
// strip, the machine, and Settings at the foot.
export function RailSheet({
  bot,
  ctl,
  rail,
  onStart,
  onStop,
}: {
  bot: Bot;
  ctl: TabControl;
  rail: { pages: PageKey[]; machine: PageKey[]; foot: PageKey[] };
  onStart: () => void;
  onStop: () => void;
}) {
  const heard = useHeard(bot.id);
  const look = botLook(bot);
  const paused = bot.activity.find(pausedOnYou);
  const where = paused ? sourceLabel(paused.kind, paused.title, t("New chat")) : "";
  const badge = (k: PageKey) => (ctl.current === k ? 0 : k === "feed" ? bot.feedUnread : k === "settings" ? bot.auditUnseen : 0);
  return (
    <>
      <div className="min-h-0 flex-1 overflow-y-auto overscroll-contain pb-3">
        <div className="flex items-center gap-3 px-4 pt-4 pb-3">
          <Crest index={bot.crest} size={44} state={crestState(bot, heard)} />
          <span className="min-w-0 flex-1">
            <h2 className="truncate text-card-title leading-6 text-ink">{bot.name}</h2>
            <span className="flex min-w-0 items-center gap-1.5">
              <Status look={look} className="shrink-0" />
              {where ? <span className="min-w-0 truncate text-label leading-4 text-ink-3">{`· ${where}`}</span> : null}
            </span>
          </span>
          {bot.people.length > 1 ? <People people={bot.people} /> : null}
        </div>
        <ActivityStrip bot={bot} className="mx-3 mb-2 rounded-control bg-surface/60 px-1 py-1" />
        {rail.pages.length ? (
          <nav aria-label={t("Pages")} className="space-y-0.5 px-2">
            {rail.pages.map((k) => (
              <SheetRow key={k} bot={bot} ctl={ctl} k={k} badge={badge(k)} />
            ))}
          </nav>
        ) : null}
        <div className="mt-3">
          <MachineTiles bot={bot} ctl={ctl} kinds={rail.machine} onStart={onStart} onStop={onStop} />
        </div>
      </div>
      {rail.foot.length ? (
        <div className="shrink-0 px-2 py-2 shadow-[inset_0_1px_0_var(--color-line)]">
          {rail.foot.map((k) => (
            <SheetRow key={k} bot={bot} ctl={ctl} k={k} badge={badge(k)} />
          ))}
        </div>
      ) : null}
    </>
  );
}
