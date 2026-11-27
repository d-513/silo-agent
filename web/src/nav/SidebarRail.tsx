import { Link, useLocation } from "@tanstack/react-router";
import { PanelLeftOpen, Plus, Settings2 } from "lucide-react";
import { useMemo } from "react";
import { useAuth } from "../auth";
import { useHeard } from "../bot/heardStore";
import { ActiveBar } from "../bot/SideNav";
import { useBots } from "../bots";
import { Crest } from "../Crest";
import type { Bot } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { SiloMark } from "../SiloMark";
import { canBuild, useCurrentSpace } from "../space/spaces";
import { botLook, botNow, crestState } from "../statusLook";
import { Tip, TipTitle } from "../Tip";
import { byLastMessage } from "./botOrder";
import { hit, type Page } from "./chrome";
import { foldHint } from "./navStore";

// SidebarRail is the wide sidebar folded away: Silo's mark, the way back to
// the whole sidebar, the Workspace's Bots as their crests in the sidebar's
// order, and the account. Rooms, Call and the Workspace switcher are in the
// sidebar itself, a press away.
export function SidebarRail({ page, activeBotId, onOpen }: { page: Page; activeBotId?: string; onOpen: () => void }) {
  const { bots } = useBots();
  const { space } = useCurrentSpace();
  const { pathname } = useLocation();
  const { email } = useAuth();
  const here = useMemo(() => byLastMessage((bots ?? []).filter((b) => b.spaceId === space?.id)), [bots, space?.id]);
  const initial = (email.trim()[0] ?? "?").toUpperCase();
  return (
    <aside className="flex w-14 shrink-0 flex-col items-center bg-well" aria-label={"Silo"}>
      <div className="flex h-14 shrink-0 items-center">
        <SiloMark className="h-7 w-7" />
      </div>
      <Tip content={<TipTitle aside={foldHint()}>{t("Show the sidebar")}</TipTitle>} side="right">
        <button type="button" onClick={onOpen} aria-label={t("Show the sidebar")} aria-expanded={false} className={hit(false)}>
          <PanelLeftOpen size={17} />
        </button>
      </Tip>
      {space ? (
        <Link to="/w/$spaceId" params={{ spaceId: space.id }} title={t("Workspace settings")} aria-label={t("Workspace settings")} data-tab-on={page === "workspace" || undefined} className={`mt-0.5 ${hit(page === "workspace")}`}>
          <Settings2 size={17} />
        </Link>
      ) : null}
      {space && canBuild(space.role) ? (
        <Link to="/w/$spaceId/new" params={{ spaceId: space.id }} title={t("New Bot")} aria-label={t("New Bot")} className={`mt-0.5 ${hit(pathname === `/w/${space.id}/new`)}`}>
          <Plus size={17} />
        </Link>
      ) : null}
      <nav className="mt-2 flex min-h-0 w-full flex-1 flex-col items-center gap-0.5 overflow-auto px-1.5 pb-2 [scrollbar-width:none]" aria-label={t("Bots")}>
        {bots === null ? <div className="skeleton h-11 w-11 rounded-control" /> : null}
        {here.map((b) => (
          <RailBot key={b.id} bot={b} on={b.id === activeBotId} />
        ))}
      </nav>
      <div className="flex w-full shrink-0 justify-center p-2 shadow-[inset_0_1px_0_var(--color-line)]">
        <Link
          to="/account"
          title={email ? `${t("Account")} · ${email}` : t("Account")}
          aria-label={t("Account")}
          data-tab-on={page === "account" || undefined}
          className={hit(page === "account")}
        >
          <span className="flex h-6 w-6 items-center justify-center rounded-full bg-ink text-micro font-semibold text-on-ink">{initial}</span>
        </Link>
      </div>
    </aside>
  );
}

// RailBot is one Bot as its crest: the lamp says its state, a dot that
// something on its Dashboard or Feed is unread, and the Tip the rest.
function RailBot({ bot, on }: { bot: Bot; on: boolean }) {
  const heard = useHeard(bot.id);
  const look = botLook(bot);
  const now = botNow(bot);
  const unread = bot.dashboardAsks + bot.feedUnread;
  return (
    <Tip
      side="right"
      content={
        <>
          <TipTitle>{bot.name}</TipTitle>
          {now ? <p className={`mt-1 ${now.tone}`}>{now.word}</p> : null}
          {unread > 0 ? <p className="mt-1">{t("{{count}} unread", { count: unread })}</p> : null}
        </>
      }
    >
      <Link
        to="/bots/$botId/run"
        params={{ botId: bot.id }}
        aria-label={bot.name}
        aria-current={on ? "page" : undefined}
        className={`blink relative flex h-11 w-11 shrink-0 items-center justify-center rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${
          on ? "bg-surface shadow-card" : "hover:bg-pressed"
        }`}
      >
        <ActiveBar on={on} />
        <span className="relative flex h-8 w-8">
          <Crest index={bot.crest} size={32} state={crestState(bot, heard)} />
          <Lamp status={look.lamp} onCrest={on ? "surface" : "well"} className="absolute -top-0.5 -right-0.5" />
        </span>
        {unread > 0 && !on ? <span aria-hidden className="absolute right-1 bottom-1 h-2 w-2 rounded-full bg-cobalt ring-2 ring-well" /> : null}
      </Link>
    </Tip>
  );
}
