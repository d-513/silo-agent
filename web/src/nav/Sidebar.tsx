import { Link, useLocation, useNavigate } from "@tanstack/react-router";
import { PanelLeftClose, Phone, Plus, Settings2 } from "lucide-react";
import { useMemo, type ReactNode } from "react";
import { ActivityStrip } from "../bot/ActivityStrip";
import { useHeard } from "../bot/heardStore";
import { ActiveBar, UnreadBadge } from "../bot/SideNav";
import { useBots } from "../bots";
import { pressClass } from "../Btn";
import { Clock } from "../call/CallBar";
import { ring, useOnAir, type OnAir } from "../call/ring";
import { Crest } from "../Crest";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { locale, t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { SiloMark } from "../SiloMark";
import { canBuild, useCurrentSpace } from "../space/spaces";
import { SpaceSwitch } from "../space/SpaceTile";
import { botLook, botNow, crestState } from "../statusLook";
import { Tip, TipTitle } from "../Tip";
import { RoomList } from "../room/RoomList";
import { byLastMessage, shortWhen } from "./botOrder";
import { AccountControls, hit, type Page } from "./chrome";
import { closeNav, foldHint } from "./navStore";

// Sidebar is the wide layout's left column, on every signed-in page: Silo,
// the Workspace the console is in (its name opens the switcher, the gear its
// settings), that Workspace's Bots as a list of conversations (the newest
// message first, each with its Call), its rooms, and the account at the foot. Below 960
// it is the left sheet (NavSheet), at the sheet's width. From 960 up the head
// folds it to a rail of crests (SidebarRail), which the reader's browser keeps.
export function Sidebar({
  page,
  activeBotId,
  activeRoomId,
  className = "w-[280px]",
  onFold,
}: {
  page: Page;
  activeBotId?: string;
  activeRoomId?: string;
  className?: string;
  /** Folds the sidebar to its rail (SidebarRail): the wide layout only. */
  onFold?: () => void;
}) {
  const { bots, callEnabled } = useBots();
  const { space, spaces } = useCurrentSpace();
  const { pathname } = useLocation();
  const nav = useNavigate();
  const air = useOnAir();
  const here = useMemo(() => byLastMessage((bots ?? []).filter((b) => b.spaceId === space?.id)), [bots, space?.id]);
  const newBotOn = !!space && pathname === `/w/${space.id}/new`;

  // Call rings the Bot's own page, which holds the call: this one if it is
  // open, else the one the press opens.
  function call(botId: string) {
    closeNav();
    ring(botId);
    if (botId !== activeBotId) void nav({ to: "/bots/$botId/run", params: { botId } });
  }

  return (
    <aside className={`flex shrink-0 flex-col bg-well ${className}`} aria-label={"Silo"}>
      <SidebarHead>
        {onFold ? (
          <Tip content={<TipTitle aside={foldHint()}>{t("Hide the sidebar")}</TipTitle>}>
            <button type="button" onClick={onFold} aria-label={t("Hide the sidebar")} aria-expanded className={hit(false)}>
              <PanelLeftClose size={17} />
            </button>
          </Tip>
        ) : null}
      </SidebarHead>
      <div className="flex shrink-0 items-center gap-1 px-2">
        <SpaceSwitch space={space} spaces={spaces} />
        {space ? (
          <Link to="/w/$spaceId" params={{ spaceId: space.id }} title={t("Workspace settings")} aria-label={t("Workspace settings")} data-tab-on={page === "workspace" || undefined} className={hit(page === "workspace")}>
            <Settings2 size={17} />
          </Link>
        ) : null}
      </div>
      <div className="mt-4 flex h-8 shrink-0 items-center gap-2 pr-2 pl-4">
        <h2 className="min-w-0 flex-1 truncate text-section text-ink-3">{t("Bots")}</h2>
        {space && canBuild(space.role) ? (
          <Link to="/w/$spaceId/new" params={{ spaceId: space.id }} title={t("New Bot")} aria-label={t("New Bot")} className={hit(newBotOn)}>
            <Plus size={17} />
          </Link>
        ) : null}
      </div>
      <nav className="min-h-0 flex-1 space-y-0.5 overflow-auto px-2 pb-2" aria-label={t("Bots")}>
        {bots === null ? (
          <>
            <div className="skeleton h-[50px] rounded-control" />
            <div className="skeleton h-[50px] rounded-control" />
          </>
        ) : null}
        {here.map((b) => (
          <BotRow key={b.id} bot={b} on={b.id === activeBotId} air={air?.botId === b.id ? air : null} canCall={callEnabled} onCall={() => call(b.id)} />
        ))}
        {bots !== null && here.length === 0 ? (
          <p className="px-2 py-3 text-meta leading-[18px] text-ink-3">
            {space && canBuild(space.role) ? t("No Bots here yet. Make one with the plus above.") : t("You are not on any Bot in this workspace yet.")}
          </p>
        ) : null}
        {space && here.length > 0 ? <RoomList spaceId={space.id} activeRoomId={activeRoomId} newOn={pathname === `/w/${space.id}/rooms/new`} /> : null}
      </nav>
      <div className="flex shrink-0 items-center gap-0.5 p-2 shadow-[inset_0_1px_0_var(--color-line)]">
        <AccountControls page={page} />
      </div>
    </aside>
  );
}

// SidebarHead is Silo's mark and name at the top of a sidebar (Admin has
// one of its own).
export function SidebarHead({ children }: { children?: ReactNode }) {
  return (
    <div className="flex h-14 shrink-0 items-center gap-2.5 pr-2 pl-4">
      <SiloMark className="h-7 w-7" />
      <span className="min-w-0 flex-1 text-card-title text-ink">{"Silo"}</span>
      {children}
    </div>
  );
}

// BotRow is one Bot as a conversation: its crest and lamp, its name with when
// it last said something, and under it what it is doing now, else the last
// thing said. The row opens the Bot; Call sits on it, outside the link.
function BotRow({ bot, on, air, canCall, onCall }: { bot: Bot; on: boolean; air: OnAir | null; canCall: boolean; onCall: () => void }) {
  const heard = useHeard(bot.id);
  const look = botLook(bot);
  const now = botNow(bot);
  const unread = bot.dashboardAsks + bot.feedUnread;
  const when = shortWhen(bot.lastMessageAt, new Date(), locale());
  // The link keeps clear of Call, which sits over the row's end.
  const room = !canCall ? "pr-3" : air && !air.connecting ? "pr-[80px]" : "pr-[46px]";
  return (
    <div className={`group relative rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${on ? "bg-surface shadow-card" : "hover:bg-pressed"}`}>
      <ActiveBar on={on} />
      <Link
        to="/bots/$botId/run"
        params={{ botId: bot.id }}
        aria-current={on ? "page" : undefined}
        className={`blink flex min-w-0 items-center gap-3 rounded-control py-2 pl-3 outline-offset-[-2px] in-data-drawer:py-2.5 ${room}`}
      >
        <span className="relative flex h-[36px] w-[36px] shrink-0">
          <Crest index={bot.crest} size={36} state={crestState(bot, heard)} />
          <Lamp status={look.lamp} onCrest={on ? "surface" : "well"} className="absolute -top-0.5 -right-0.5" />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex min-w-0 items-baseline gap-2">
            <span className="min-w-0 flex-1 truncate text-row leading-5 font-medium text-ink">{bot.name}</span>
            {when ? <span className="shrink-0 text-micro leading-4 text-ink-3 tabular-nums">{when}</span> : null}
          </span>
          <span className="flex min-w-0 items-center gap-2">
            <BotLine bot={bot} now={now} />
            {unread > 0 && !on ? <UnreadBadge n={unread} /> : null}
          </span>
        </span>
      </Link>
      {on ? <ActivityStrip bot={bot} className="mr-2 mb-2 ml-[52px] border-t border-line pt-1.5" /> : null}
      {canCall ? <CallButton name={bot.name} air={air} lit={on} onCall={onCall} /> : null}
    </div>
  );
}

// BotLine is the row's second line: what the Bot is up to while that is
// anything, else the last message (yours marked), else that there is none.
function BotLine({ bot, now }: { bot: Bot; now: ReturnType<typeof botNow> }) {
  const line = "min-w-0 flex-1 truncate text-meta leading-[18px]";
  if (now) {
    return (
      <span className={`${line} font-medium ${now.tone}`} title={now.word}>
        {now.word}
      </span>
    );
  }
  if (!bot.lastMessage) return <span className={`${line} text-ink-3`}>{t("No messages yet")}</span>;
  return (
    <span className={`${line} text-ink-3`}>
      {bot.lastMessageMine ? <span className="text-ink-2">{t("You:")} </span> : null}
      {bot.lastMessage}
    </span>
  );
}

// CallButton rings the Bot in your main chat. It carries the call: a
// breathing dot while it connects, then the call's clock, which leads back.
function CallButton({ name, air, lit, onCall }: { name: string; air: OnAir | null; lit: boolean; onCall: () => void }) {
  // Centered on the row's first 50px (57px in the narrow sheet), so it stays
  // put when the row grows.
  const at = "absolute top-[11px] right-2.5 in-data-drawer:top-[14px]";
  if (air && !air.connecting) {
    return (
      <Tip content={t("Back to the call")}>
        <button type="button" onClick={onCall} aria-label={t("Back to the call")} className={`${at} flex h-8 items-center gap-1.5 rounded-full bg-well px-2.5 shadow-card ${pressClass}`}>
          <Lamp status="online" />
          <Clock since={air.since} />
        </button>
      </Tip>
    );
  }
  if (air) {
    return (
      <button type="button" onClick={onCall} aria-label={t("Calling…")} title={t("Calling…")} className={`${at} flex h-8 w-8 items-center justify-center rounded-full bg-well shadow-card ${pressClass}`}>
        <span aria-hidden className="breathe h-2 w-2 rounded-full bg-ink-3" />
      </button>
    );
  }
  return (
    <Tip
      content={
        <>
          <TipTitle>{t("Call {{name}}", { name })}</TipTitle>
          <p className="mt-1">{t("A voice call in your main chat. What you both say is written there.")}</p>
        </>
      }
    >
      <button
        type="button"
        onClick={onCall}
        aria-label={t("Call {{name}}", { name })}
        className={`${at} flex h-8 w-8 items-center justify-center rounded-full ${pressClass} ${
          lit ? "bg-ink text-on-ink hover:bg-ink-deep" : "bg-surface text-ink shadow-card group-hover:bg-ink group-hover:text-on-ink group-hover:shadow-none"
        }`}
      >
        <Phone size={14} strokeWidth={2.25} />
      </button>
    </Tip>
  );
}
