import { Link, Navigate, Outlet, useLocation, useParams } from "@tanstack/react-router";
import { Book, HardDrive, History, ListChecks, Plug, SlidersHorizontal, Users, type LucideIcon } from "lucide-react";
import { TabStrip } from "../bot/SectionTabs";
import { msg, t } from "../i18n.ts";
import { PaneFallback } from "../PaneFallback";
import { canAdmin, canBuild, useSpaces } from "./spaces";

// The Workspace page (/w/$spaceId): everything that is the Workspace's, one
// tab each. What its Bots share (connectors, skills, drives) is kept here once
// and given to the Bots that may use it; then its people, its guardrails,
// what happened in it, and its own settings. A guest sees only People and
// Settings.
type Tab = "connectors" | "skills" | "drives" | "people" | "rules" | "activity" | "settings";
type Need = "view" | "build" | "admin";
const tabs: readonly { tab: Tab; to: `/w/$spaceId/${Tab}`; label: string; icon: LucideIcon; need: Need }[] = [
  { tab: "connectors", to: "/w/$spaceId/connectors", label: msg("Connectors"), icon: Plug, need: "build" },
  { tab: "skills", to: "/w/$spaceId/skills", label: msg("Skills"), icon: Book, need: "build" },
  { tab: "drives", to: "/w/$spaceId/drives", label: msg("Drives"), icon: HardDrive, need: "build" },
  { tab: "people", to: "/w/$spaceId/people", label: msg("People"), icon: Users, need: "view" },
  { tab: "rules", to: "/w/$spaceId/rules", label: msg("Rules"), icon: ListChecks, need: "admin" },
  { tab: "activity", to: "/w/$spaceId/activity", label: msg("Activity"), icon: History, need: "admin" },
  { tab: "settings", to: "/w/$spaceId/settings", label: msg("Settings"), icon: SlidersHorizontal, need: "view" },
];

function allowed(need: Need, role: string) {
  return need === "view" || (need === "build" ? canBuild(role) : canAdmin(role));
}

function useRole(spaceId: string) {
  const { spaces } = useSpaces();
  return { space: spaces?.find((s) => s.id === spaceId), loaded: spaces !== null };
}

export function SpaceLayout() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId" });
  const { pathname } = useLocation();
  const { space } = useRole(spaceId);
  const role = space?.role ?? "";
  const open = tabs.find((x) => pathname.startsWith(`/w/${spaceId}/${x.tab}`))?.tab ?? "";
  return (
    <section className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-4 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:px-5">
        <h1 className="hidden max-w-48 shrink-0 truncate text-card-title leading-5 wide:block">{space?.name ?? t("Workspace")}</h1>
        <TabStrip tab={open}>
          {tabs
            .filter((x) => allowed(x.need, role))
            .map(({ tab, to, label, icon: Icon }) => {
              const on = tab === open;
              return (
                <Link
                  key={tab}
                  to={to}
                  params={{ spaceId }}
                  data-tab-on={on || undefined}
                  className={`flex h-8 shrink-0 items-center gap-1.5 self-center whitespace-nowrap rounded-sm px-2.5 text-[13px] font-medium transition-[background-color,color] duration-[160ms] ease-quiet ${
                    on ? "text-ink" : "text-ink-3 hover:bg-well hover:text-ink"
                  }`}
                >
                  <Icon size={15} />
                  {t(label)}
                </Link>
              );
            })}
        </TabStrip>
      </header>
      <div className="min-h-0 flex-1 overflow-auto">
        <Outlet />
      </div>
    </section>
  );
}

// /w/$spaceId opens on the first tab the role has.
export function SpaceIndex() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/" });
  const { space, loaded } = useRole(spaceId);
  if (!loaded) return <PaneFallback />;
  if (!space) return <Navigate to="/" replace />;
  return canBuild(space.role) ? (
    <Navigate to="/w/$spaceId/connectors" params={{ spaceId }} replace />
  ) : (
    <Navigate to="/w/$spaceId/people" params={{ spaceId }} replace />
  );
}
