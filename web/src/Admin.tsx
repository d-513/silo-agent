import { useQuery } from "@connectrpc/connect-query";
import { Link, Outlet, type LinkProps } from "@tanstack/react-router";
import { ArrowLeft, Book, Bug, Container, HardDrive, History, Plug, SlidersHorizontal, Users, type LucideIcon } from "lucide-react";
import { type ReactNode } from "react";
import { ActiveBar } from "./bot/SideNav";
import { btnClass } from "./Btn";
import { UI } from "./gen/silo/v1/ui_pb";
import { msg, t } from "./i18n.ts";
import { AccountControls } from "./nav/chrome";
import { SidebarHead } from "./nav/Sidebar";
import { useWide } from "./useWide";

// One entry per admin page. What silo.yaml holds is Settings (with categories
// of its own); the users and the libraries live in the database.
const pages: readonly { to: LinkProps["to"]; label: string; icon: LucideIcon; debug?: true }[] = [
  { to: "/admin/settings", label: msg("Settings"), icon: SlidersHorizontal },
  { to: "/admin/users", label: msg("Users"), icon: Users },
  { to: "/admin/connectors", label: msg("Connectors Library"), icon: Plug },
  { to: "/admin/skills", label: msg("Skills Library"), icon: Book },
  { to: "/admin/drives", label: msg("Drives"), icon: HardDrive },
  { to: "/admin/containers", label: msg("Containers"), icon: Container },
  { to: "/admin/audit", label: msg("Audit"), icon: History },
  { to: "/admin/debug", label: msg("Debug"), icon: Bug, debug: true },
];

// A page as a sidebar row: the look of a Bot page's SideLink.
function AdminRow({ to, icon: Icon, children }: { to: LinkProps["to"]; icon: LucideIcon; children: ReactNode }) {
  const row = "relative flex h-9 items-center gap-2.5 rounded-control px-3 text-row font-medium transition-[background-color,color] duration-[160ms] ease-quiet outline-offset-[-2px]";
  return (
    <Link to={to} className={row} activeProps={{ className: "bg-surface text-ink", "data-tab-on": true }} inactiveProps={{ className: "text-ink-2 hover:bg-pressed hover:text-ink" }}>
      {({ isActive }) => (
        <>
          <ActiveBar on={isActive} />
          <Icon size={15} />
          <span className="min-w-0 flex-1 truncate">{children}</span>
        </>
      )}
    </Link>
  );
}

// A page as a tab, below 960 where there is no sidebar.
function AdminTab({ to, children }: { to: LinkProps["to"]; children: ReactNode }) {
  return (
    <Link
      to={to}
      className="shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-small font-medium transition-colors duration-[160ms] ease-quiet"
      activeProps={{ className: "border-cobalt text-ink", "data-tab-on": true }}
      inactiveProps={{ className: "border-transparent text-ink-3 hover:text-ink" }}
    >
      {children}
    </Link>
  );
}

function BackToBots() {
  return (
    <Link to="/" className={btnClass("secondary", "w-full justify-center max-wide:w-auto")}>
      <ArrowLeft size={16} />
      {t("Back to Bots")}
    </Link>
  );
}

// AdminLayout is the frame of every admin page. The Shell draws no Bot
// sidebar here: from 960 up Admin has its own in the same place (Silo, the
// way back to the Bots, the admin pages, and the account at the foot where it
// always is); below 960 a head row with the way back and the account, and the
// pages as tabs.
export function AdminLayout() {
  const wide = useWide();
  const debug = useQuery(UI.method.getSettings, {}).data?.fields.some((f) => f.key === "debug" && f.value === "true") ?? false;
  const shown = pages.filter((p) => debug || !p.debug);
  if (wide) {
    return (
      <div className="flex h-full">
        <aside className="flex w-[280px] shrink-0 flex-col bg-well" aria-label={t("Admin")}>
          <SidebarHead />
          <div className="shrink-0 px-2 pt-1">
            <BackToBots />
          </div>
          <div className="mt-4 flex h-8 shrink-0 items-center pr-2 pl-4">
            <h2 className="min-w-0 flex-1 truncate text-section text-ink-3">{t("Admin")}</h2>
          </div>
          <nav className="min-h-0 flex-1 space-y-0.5 overflow-auto px-2 pb-2" aria-label={t("Admin")}>
            {shown.map((p) => (
              <AdminRow key={p.to} to={p.to} icon={p.icon}>
                {t(p.label)}
              </AdminRow>
            ))}
          </nav>
          <div className="flex shrink-0 items-center gap-0.5 p-2 shadow-[inset_0_1px_0_var(--color-line)]">
            <AccountControls page="admin" />
          </div>
        </aside>
        <div className="min-w-0 flex-1 overflow-auto">
          <div className="silo-page silo-page-lg">
            <Outlet />
          </div>
        </div>
      </div>
    );
  }
  return (
    <div className="h-full overflow-auto">
      <div className="silo-page silo-page-lg">
        <div className="mb-5 flex items-center gap-3 pb-4 shadow-[inset_0_-1px_0_var(--color-line)]">
          <BackToBots />
          <div className="ml-auto flex min-w-0 items-center gap-0.5">
            <AccountControls page="admin" wide={false} adminLink={false} />
          </div>
        </div>
        <h1 className="text-title">{t("Admin")}</h1>
        <nav className="silo-scroll-x mb-6 mt-4 flex gap-1 shadow-[inset_0_-1px_0_var(--color-line)]">
          {shown.map((p) => (
            <AdminTab key={p.to} to={p.to}>
              {t(p.label)}
            </AdminTab>
          ))}
        </nav>
        <Outlet />
      </div>
    </div>
  );
}
