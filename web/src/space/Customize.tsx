import { Link, Outlet, useLocation, useParams } from "@tanstack/react-router";
import { Book, HardDrive, Plug, type LucideIcon } from "lucide-react";
import { TabStrip } from "../bot/SectionTabs";
import { msg, t } from "../i18n.ts";

// The Workspace's Customize hub: what its Bots share, one tab per kind. Each
// thing is kept once here and given to the Bots that may use it.
type HubTab = "connectors" | "skills" | "drives";
const tabs: readonly { tab: HubTab; to: `/w/$spaceId/customize/${HubTab}`; label: string; icon: LucideIcon }[] = [
  { tab: "connectors", to: "/w/$spaceId/customize/connectors", label: msg("Connectors"), icon: Plug },
  { tab: "skills", to: "/w/$spaceId/customize/skills", label: msg("Skills"), icon: Book },
  { tab: "drives", to: "/w/$spaceId/customize/drives", label: msg("Drives"), icon: HardDrive },
];

export function SpaceCustomizeLayout() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/customize" });
  const { pathname } = useLocation();
  const open = tabs.find((x) => pathname.startsWith(`/w/${spaceId}/customize/${x.tab}`))?.tab ?? "";
  return (
    <section className="flex h-full min-h-0 min-w-0 flex-col">
      <header className="flex h-12 shrink-0 items-center gap-4 px-2 shadow-[inset_0_-1px_0_var(--color-line)] wide:h-14 wide:px-5">
        <h1 className="hidden shrink-0 text-card-title leading-5 wide:block">{t("Customize")}</h1>
        <TabStrip tab={open}>
          {tabs.map(({ tab, to, label, icon: Icon }) => {
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
