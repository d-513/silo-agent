import { Link } from "@tanstack/react-router";
import { Brain, Folder, GitCompareArrows, Inbox, Library, Mail, Monitor, Package, Power, Radio, Settings, SquareTerminal, Timer, type LucideIcon } from "lucide-react";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { onSettings, type PaneKind, type SideTab, type Tab } from "./tabs";
import { msg, t } from "../i18n.ts";
import { powerWord, machineStarting } from "../statusLook";
import { UnreadBadge } from "./SideNav";

export const paneMeta: Record<PaneKind, { label: string; icon: LucideIcon }> = {
  files: { label: msg("Files"), icon: Folder },
  desktop: { label: msg("Desktop"), icon: Monitor },
  console: { label: msg("Console"), icon: SquareTerminal },
  changes: { label: msg("Changes"), icon: GitCompareArrows },
  mail: { label: msg("Mail"), icon: Mail },
};

// The Bot's pages beside its conversations, in the rail's order.
type PageTab = Exclude<SideTab, "dashboard"> | "channels";
export const railPages: readonly { tab: PageTab; to: `/bots/$botId/${PageTab}`; label: string; icon: LucideIcon }[] = [
  { tab: "automations", to: "/bots/$botId/automations", label: msg("Automations"), icon: Timer },
  { tab: "artifacts", to: "/bots/$botId/artifacts", label: msg("Artifacts"), icon: Package },
  { tab: "memories", to: "/bots/$botId/memories", label: msg("Memories"), icon: Brain },
  { tab: "knowledge", to: "/bots/$botId/knowledge", label: msg("Knowledge"), icon: Library },
  { tab: "feed", to: "/bots/$botId/feed", label: msg("Feed"), icon: Inbox },
  { tab: "channels", to: "/bots/$botId/channels", label: msg("Channels"), icon: Radio },
];

function hit(on: boolean) {
  return `relative flex h-9 w-9 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow,transform] duration-[160ms] ease-quiet active:scale-[.94] active:duration-[70ms] motion-reduce:active:scale-100 disabled:cursor-not-allowed disabled:opacity-50 ${
    on ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
  }`;
}

// The open item's cobalt bar, on the side its page or pane opens toward.
const bar = <span aria-hidden className="absolute inset-y-2 -left-1.5 w-[3px] rounded-full bg-cobalt" />;

// The wide layout's right edge, on every page of a Bot. From the top: its
// pages beside the conversations (Automations, Artifacts, Memories,
// Knowledge, Feed, Channels), each a page of its own; a hairline; the
// machine's panes (Files, Desktop, Console, Changes, and Mail while the Bot
// has an address), which open beside the page; then Settings and Start/Stop
// at the foot.
export function MachineRail({
  bot,
  tab,
  open,
  onPick,
  onStart,
  onStop,
}: {
  bot: Bot;
  // The page showing now.
  tab: Tab;
  // The pane showing now, docked or as its own page.
  open: PaneKind | "";
  onPick: (kind: PaneKind) => void;
  onStart: () => void;
  onStop: () => void;
}) {
  const starting = machineStarting(bot);
  const power = powerWord(bot);
  const settingsOn = onSettings(tab);
  return (
    <aside className="hidden w-12 shrink-0 flex-col items-center gap-1 overflow-x-hidden overflow-y-auto bg-well pt-2.5 pb-4 wide:flex" aria-label={t("Pages and machine")}>
      {railPages.map(({ tab: page, to, label, icon: Icon }) => {
        const on = tab === page;
        const unread = page === "feed" && !on ? bot.feedUnread : 0;
        return (
          <Link key={page} to={to} params={{ botId: bot.id }} title={t(label)} aria-label={t(label)} data-tab-on={on || undefined} className={hit(on)}>
            {on ? bar : null}
            <Icon size={18} />
            {unread > 0 ? <UnreadBadge n={unread} dot /> : null}
          </Link>
        );
      })}
      <span aria-hidden className="my-1.5 h-px w-6 shrink-0 bg-line" />
      {(Object.keys(paneMeta) as PaneKind[])
        // Mail has no machine: its icon is only here while the Bot has an address.
        .filter((k) => k !== "mail" || bot.mailAddress || open === "mail")
        .map((k) => {
          const { icon: Icon } = paneMeta[k];
          const label = t(paneMeta[k].label);
          const on = open === k;
          return (
            <button key={k} type="button" title={label} aria-label={label} aria-pressed={on} data-tab-on={on || undefined} className={hit(on)} onClick={() => onPick(k)}>
              {on ? bar : null}
              <Icon size={18} />
            </button>
          );
        })}
      <Link
        to="/bots/$botId/settings"
        params={{ botId: bot.id }}
        title={t("Settings")}
        aria-label={t("Settings")}
        data-tab-on={settingsOn || undefined}
        className={`${hit(settingsOn)} mt-auto`}
      >
        {settingsOn ? bar : null}
        <Settings size={18} />
        {bot.auditUnseen > 0 && !settingsOn ? <UnreadBadge n={bot.auditUnseen} dot /> : null}
      </Link>
      <button type="button" title={power} aria-label={power} className={hit(false)} disabled={!bot.workerConnected && starting} onClick={bot.workerConnected ? onStop : onStart}>
        <Power size={17} />
      </button>
    </aside>
  );
}
