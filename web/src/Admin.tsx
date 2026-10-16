import { useQuery } from "@connectrpc/connect-query";
import { type ReactNode } from "react";
import { Link, Outlet, type LinkProps } from "@tanstack/react-router";
import { UI } from "./gen/silo/v1/ui_pb";
import { t } from "./i18n.ts";

function AdminTab({ to, children }: { to: LinkProps["to"]; children: ReactNode }) {
  return (
    <Link
      to={to}
      className="shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet"
      activeProps={{ className: "border-cobalt text-ink" }}
      inactiveProps={{ className: "border-transparent text-ink-3 hover:text-ink" }}
    >
      {children}
    </Link>
  );
}

// AdminLayout is the frame of every admin page: the title and one tab per
// page. What silo.yaml holds is Settings (with categories of its own); the
// users and the libraries live in the database and are tabs.
export function AdminLayout() {
  const debug = useQuery(UI.method.getSettings, {}).data?.fields.some((f) => f.key === "debug" && f.value === "true") ?? false;
  return (
    <div className="silo-page silo-page-lg">
      <h1 className="text-title">{t("Admin")}</h1>
      <nav className="silo-scroll-x mb-6 mt-4 flex gap-1 shadow-[inset_0_-1px_0_var(--color-line)]">
        <AdminTab to="/admin/settings">{t("Settings")}</AdminTab>
        <AdminTab to="/admin/users">{t("Users")}</AdminTab>
        <AdminTab to="/admin/connectors">{t("Connectors Library")}</AdminTab>
        <AdminTab to="/admin/skills">{t("Skills Library")}</AdminTab>
        <AdminTab to="/admin/drives">{t("Drives")}</AdminTab>
        <AdminTab to="/admin/containers">{t("Containers")}</AdminTab>
        <AdminTab to="/admin/audit">{t("Audit")}</AdminTab>
        {debug && <AdminTab to="/admin/debug">{t("Debug")}</AdminTab>}
      </nav>
      <Outlet />
    </div>
  );
}
