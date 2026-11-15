import { Boxes, LogOut, Moon, Plus, Sun, UserRound, Wrench } from "lucide-react";
import { Link, Outlet, useLocation } from "@tanstack/react-router";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { AccountPage } from "./account/AccountPage";
import { useAuth } from "./auth";
import { useBots } from "./bots";
import { Crest } from "./Crest";
import { Lamp } from "./Lamp";
import { botLook, crestState } from "./statusLook";
import { preloadMarkdown } from "./mdPlugins";
import { OfflineBanner } from "./Offline";
import { type Page, signOut } from "./nav/chrome";
import { Sidebar } from "./nav/Sidebar";
import { byLastMessage } from "./nav/botOrder";
import { canBuild, useCurrentSpace } from "./space/spaces";
import { SpaceSwitch } from "./space/SpaceTile";
import { flipTheme, setTheme, useTheme } from "./theme";
import { t } from "./i18n.ts";
import { useDismiss } from "./useMenu";
import { useWide } from "./useWide";

// Rail items: 40px hit areas. The active one is a lifted surface well with a
// 3px cobalt bar on its bottom edge.
function railHit(active: boolean, extra = "") {
  return `relative flex h-10 w-10 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow] duration-[160ms] ease-quiet ${
    active ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
  } ${extra}`;
}

function railBar(on: boolean) {
  if (!on) return null;
  return <span aria-hidden className="absolute inset-x-2.5 -bottom-1 h-[3px] rounded-full bg-cobalt" />;
}

const menuRow =
  "flex h-11 w-full items-center gap-3 rounded-sm px-2.5 text-left text-body text-ink transition-colors duration-[160ms] hover:bg-well";

// AccountMenu is the top bar's one button for what the wide sidebar lists at
// its foot (Account, Admin, the theme, Sign out): four icons there would leave
// no room for the Bots.
function AccountMenu({ page }: { page: Page }) {
  const { admin, email, setSession } = useAuth();
  const theme = useTheme();
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<CSSProperties>({});
  useDismiss(open, trigger, menu, () => setOpen(false));
  useEffect(() => {
    if (!open) return;
    const r = trigger.current?.getBoundingClientRect();
    if (r) setBox({ right: Math.max(8, window.innerWidth - r.right), top: r.bottom + 6 });
    menu.current?.focus();
  }, [open]);
  const initial = (email.trim()[0] ?? "?").toUpperCase();
  const dark = theme === "dark";
  const label = email ? `${t("Account")} · ${email}` : t("Account");
  return (
    <>
      <button
        ref={trigger}
        type="button"
        title={label}
        aria-label={label}
        aria-haspopup="menu"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className={railHit(page === "account" || page === "admin")}
      >
        {railBar(page === "account" || page === "admin")}
        <span className="flex h-7 w-7 items-center justify-center rounded-full bg-ink text-label font-semibold text-on-ink">{initial}</span>
      </button>
      {open
        ? createPortal(
            <div
              ref={menu}
              role="menu"
              tabIndex={-1}
              aria-label={t("Account")}
              onKeyDown={(e) => {
                if (e.key === "Escape") {
                  setOpen(false);
                  trigger.current?.focus();
                }
              }}
              className="fixed z-50 w-[min(280px,calc(100vw-16px))] rounded-control bg-surface p-1 shadow-slip outline-none"
              style={box}
            >
              <div className="rise">
                {email ? <div className="truncate px-2.5 pt-2 pb-1.5 text-meta text-ink-3">{email}</div> : null}
                <Link to="/account" role="menuitem" onClick={() => setOpen(false)} className={menuRow}>
                  <UserRound size={17} className="text-ink-2" />
                  {t("Account")}
                </Link>
                {admin ? (
                  <Link to="/admin/settings" role="menuitem" onClick={() => setOpen(false)} className={menuRow}>
                    <Wrench size={17} className="text-ink-2" />
                    {t("Admin")}
                  </Link>
                ) : null}
                <button type="button" role="menuitem" className={menuRow} onClick={() => setTheme(flipTheme(theme))}>
                  {dark ? <Sun size={17} className="text-ink-2" /> : <Moon size={17} className="text-ink-2" />}
                  {dark ? t("Switch to light mode") : t("Switch to dark mode")}
                </button>
                <span aria-hidden className="mx-2 my-1 block h-px bg-line" />
                <button
                  type="button"
                  role="menuitem"
                  className={menuRow}
                  onClick={() => {
                    setOpen(false);
                    void signOut(setSession);
                  }}
                >
                  <LogOut size={17} className="text-ink-2" />
                  {t("Sign out")}
                </button>
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}

// Rail is the narrow layout's way between Workspaces and Bots: a top bar, and
// the head of a Bot's drawer. From 960 up the Sidebar stands in its place.
export function Rail({ page, activeBotId, className = "" }: { page: Page; activeBotId?: string; className?: string }) {
  const { bots } = useBots();
  const { space, spaces } = useCurrentSpace();
  const loc = useLocation();
  const newBotPath = space ? `/w/${space.id}/new` : "";
  // The rail holds the Bots of the Workspace the console is in, in the
  // sidebar's order.
  const here = byLastMessage((bots ?? []).filter((b) => b.spaceId === space?.id));
  return (
    <aside className={`flex h-[calc(3rem+env(safe-area-inset-top))] w-full shrink-0 flex-row items-center gap-1 bg-well pt-[env(safe-area-inset-top)] shadow-[inset_0_-1px_0_var(--color-line)] ${className}`}>
      <SpaceSwitch space={space} spaces={spaces} />
      <nav className="flex min-h-0 min-w-0 flex-1 flex-row items-center gap-1">
        {space ? (
          <Link to="/w/$spaceId" params={{ spaceId: space.id }} title={t("Workspace settings")} className={railHit(page === "workspace")}>
            {railBar(page === "workspace")}
            <Boxes size={20} />
          </Link>
        ) : null}
        <span aria-hidden className="mx-1 h-6 w-px shrink-0 bg-line" />
        <div className="silo-scroll-x flex min-h-0 min-w-0 flex-1 flex-row items-center gap-1 py-1">
          {here.map((b) => {
            const on = b.id === activeBotId;
            const look = botLook(b);
            return (
              <Link key={b.id} to="/bots/$botId/run" params={{ botId: b.id }} title={`${b.name} · ${look.word}`} className={railHit(on, "blink")}>
                {railBar(on)}
                <span className="relative flex h-7 w-7">
                  <Crest index={b.crest} size={28} state={crestState(b)} name={b.name} />
                  <Lamp status={look.lamp} onCrest={on ? "surface" : "well"} className="absolute -top-0.5 -right-0.5" />
                </span>
              </Link>
            );
          })}
          {space && canBuild(space.role) ? (
            <Link to="/w/$spaceId/new" params={{ spaceId: space.id }} title={t("New Bot")} className={railHit(loc.pathname === newBotPath)}>
              {railBar(loc.pathname === newBotPath)}
              <Plus size={20} />
            </Link>
          ) : null}
        </div>
      </nav>
      <div className="flex shrink-0 items-center pr-2 pl-1">
        <AccountMenu page={page} />
      </div>
    </aside>
  );
}

// Shell is every signed-in page: the sidebar (a top bar when narrow), and the
// page beside it.
export function Shell() {
  const { pathname } = useLocation();
  const wide = useWide();
  const activeBotId = pathname.match(/^\/bots\/([^/]+)/)?.[1];
  const page: Page = pathname.startsWith("/admin")
    ? "admin"
    : pathname.startsWith("/account")
      ? "account"
      : pathname.startsWith("/w/") && pathname !== "/w/new" && !/^\/w\/[^/]+\/new$/.test(pathname)
        ? "workspace"
        : "bots";
  // Replies with code are common: fetch the highlighter once the browser is idle.
  useEffect(() => preloadMarkdown(), []);
  return (
    <div className="flex h-dvh overflow-hidden max-wide:flex-col">
      {wide ? (
        <Sidebar page={page} activeBotId={activeBotId} />
      ) : activeBotId ? null : (
        // On a narrow screen a Bot page carries its own top bar, whose drawer
        // holds this rail: two bars would leave the chat too little room.
        <Rail page={page} />
      )}
      {/* A Bot page fills the height and scrolls inside its own panes. */}
      <main className={`min-w-0 flex-1 ${activeBotId ? "min-h-0 overflow-hidden" : "overflow-auto"}`}>
        <Outlet />
      </main>
      <OfflineBanner />
    </div>
  );
}

export function AccountRoute() {
  return <AccountPage />;
}
