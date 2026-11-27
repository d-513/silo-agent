import { Outlet, useLocation } from "@tanstack/react-router";
import { useEffect } from "react";
import { AccountPage } from "./account/AccountPage";
import { preloadMarkdown } from "./mdPlugins";
import { OfflineBanner } from "./Offline";
import { type Page } from "./nav/chrome";
import { NavButton, NavSheet } from "./nav/NavSheet";
import { isFoldKey, setFolded, toggleFolded, useFolded } from "./nav/navStore";
import { Sidebar } from "./nav/Sidebar";
import { SidebarRail } from "./nav/SidebarRail";
import { SiloMark } from "./SiloMark";
import { t } from "./i18n.ts";
import { useWide } from "./useWide";

// NarrowBar heads a page with no top bar of its own below 960 (the
// Workspace's settings, Account, New Bot): the sidebar's sheet, and where
// you are.
function NarrowBar({ page }: { page: Page }) {
  const { pathname } = useLocation();
  const where =
    pathname === "/w/new"
      ? t("New workspace")
      : /^\/w\/[^/]+\/new$/.test(pathname)
        ? t("New Bot")
        : /^\/w\/[^/]+\/rooms\/new$/.test(pathname)
          ? t("New room")
        : page === "account"
          ? t("Account")
          : page === "workspace"
            ? t("Workspace settings")
            : "";
  return (
    <header className="flex h-[calc(3rem+env(safe-area-inset-top))] shrink-0 items-center gap-1.5 bg-well pt-[env(safe-area-inset-top)] pr-3 pl-1 shadow-[inset_0_-1px_0_var(--color-line)]">
      <NavButton />
      <SiloMark className="h-6 w-6 shrink-0" />
      <span className="min-w-0 flex-1 truncate text-body-lg font-semibold text-ink">{where || "Silo"}</span>
    </header>
  );
}

// Shell is every signed-in page: the sidebar (folded to a rail of crests when
// the reader hid it; a sheet from the left when narrow), and the page beside it.
export function Shell() {
  const { pathname } = useLocation();
  const wide = useWide();
  const activeBotId = pathname.match(/^\/bots\/([^/]+)/)?.[1];
  // A room draws its own top bar and scrolls inside, like a Bot page.
  const activeRoomId = pathname.match(/^\/rooms\/([^/]+)/)?.[1];
  const page: Page = pathname.startsWith("/admin")
    ? "admin"
    : pathname.startsWith("/account")
      ? "account"
      : pathname.startsWith("/w/") && pathname !== "/w/new" && !/^\/w\/[^/]+\/(rooms\/)?new$/.test(pathname)
        ? "workspace"
        : "bots";
  // Replies with code are common: fetch the highlighter once the browser is idle.
  useEffect(() => preloadMarkdown(), []);
  const folded = useFolded();
  // Ctrl/⌘+B folds the wide sidebar and brings it back.
  useEffect(() => {
    if (!wide || page === "admin") return;
    const onKey = (e: KeyboardEvent) => {
      const el = e.target instanceof HTMLElement ? e.target : null;
      const typing = !!el && (el.isContentEditable || /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName));
      if (!isFoldKey(e, typing)) return;
      e.preventDefault();
      toggleFolded();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [wide, page]);
  return (
    <div className="flex h-dvh overflow-hidden max-wide:flex-col">
      {page === "admin" ? null : wide ? (
        // Admin draws a sidebar of its own (AdminLayout).
        folded ? (
          <SidebarRail page={page} activeBotId={activeBotId} onOpen={() => setFolded(false)} />
        ) : (
          <Sidebar page={page} activeBotId={activeBotId} activeRoomId={activeRoomId} onFold={() => setFolded(true)} />
        )
      ) : activeBotId || activeRoomId ? null : (
        // A Bot page carries its own top bar, which opens the same sheet.
        <NarrowBar page={page} />
      )}
      {/* A Bot page fills the height and scrolls inside its own panes. */}
      {/* Admin draws its own sidebar and scrolls its own page. */}
      <main className={`min-w-0 flex-1 ${activeBotId || activeRoomId || page === "admin" ? "min-h-0 overflow-hidden" : "overflow-auto"}`}>
        <Outlet />
      </main>
      {page === "admin" || wide ? null : <NavSheet page={page} activeBotId={activeBotId} activeRoomId={activeRoomId} />}
      <OfflineBanner />
    </div>
  );
}

export function AccountRoute() {
  return <AccountPage />;
}
