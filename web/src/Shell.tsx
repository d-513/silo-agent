import { Book, LayoutGrid, LogOut, Moon, Plus, Sun, Wrench } from "lucide-react";
import { Link, Outlet, useLocation } from "@tanstack/react-router";
import { useEffect } from "react";
import { AccountPage } from "./account/AccountPage";
import { ui } from "./api";
import { useAuth } from "./auth";
import { useBots } from "./bots";
import { Crest } from "./Crest";
import { isSignedOut } from "./errors";
import { Lamp } from "./Lamp";
import { preloadMarkdown } from "./mdPlugins";
import { OfflineBanner } from "./Offline";
import { SiloMark } from "./SiloMark";
import { flipTheme, setTheme, useTheme } from "./theme";

// Rail items: 40px hit areas. The active one is a lifted surface well with a
// 3px cobalt bar on the leading edge (bottom edge in the narrow top bar).
function railHit(active: boolean, extra = "") {
  return `relative flex h-10 w-10 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow] duration-[160ms] ease-quiet ${
    active ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
  } ${extra}`;
}

function railBar(on: boolean) {
  if (!on) return null;
  return (
    <span
      aria-hidden
      className="absolute rounded-full bg-cobalt max-wide:inset-x-2.5 max-wide:-bottom-1 max-wide:h-[3px] wide:inset-y-2.5 wide:-left-3 wide:w-[3px]"
    />
  );
}

// The theme toggle is one rail item whose glyph morphs like Send into Stop:
// the moon turns out as the sun turns in. It shows where a press leads.
function ThemeToggle() {
  const theme = useTheme();
  const dark = theme === "dark";
  const label = dark ? "Switch to light mode" : "Switch to dark mode";
  const glyph = "col-start-1 row-start-1 transition-[transform,opacity] duration-[320ms] ease-settle motion-reduce:transition-opacity";
  const away = "scale-[.6] opacity-0";
  return (
    <button type="button" title={label} aria-label={label} className={railHit(false)} onClick={() => setTheme(flipTheme(theme))}>
      <span className="grid place-items-center">
        <Moon size={18} className={`${glyph} ${dark ? `rotate-90 ${away}` : ""}`} />
        <Sun size={18} className={`${glyph} ${dark ? "" : `-rotate-90 ${away}`}`} />
      </span>
    </button>
  );
}

type Page = "bots" | "admin" | "account" | "skills";

function Rail({ page, activeBotId }: { page: Page; activeBotId?: string }) {
  const { admin, email, setSession } = useAuth();
  const { bots } = useBots();
  const loc = useLocation();
  const homeActive = page === "bots" && !activeBotId && loc.pathname !== "/new";
  const initial = (email.trim()[0] ?? "?").toUpperCase();
  return (
    <aside className="flex shrink-0 bg-well max-wide:h-[calc(3rem+env(safe-area-inset-top))] max-wide:w-full max-wide:flex-row max-wide:items-center max-wide:gap-1 max-wide:pt-[env(safe-area-inset-top)] max-wide:shadow-[inset_0_-1px_0_var(--color-line)] wide:w-16 wide:flex-col wide:items-center">
      <SiloMark />
      <nav className="flex min-h-0 min-w-0 flex-1 items-center gap-1 max-wide:flex-row wide:mt-5 wide:flex-col">
        <Link to="/" title="Bots" className={railHit(homeActive)}>
          {railBar(homeActive)}
          <LayoutGrid size={20} />
        </Link>
        <Link to="/skills" title="Skills" className={railHit(page === "skills")}>
          {railBar(page === "skills")}
          <Book size={20} />
        </Link>
        <span aria-hidden className="shrink-0 bg-line max-wide:mx-1 max-wide:h-6 max-wide:w-px wide:my-1.5 wide:h-px wide:w-6" />
        <div className="silo-scroll-x flex min-h-0 min-w-0 flex-1 gap-1 max-wide:flex-row max-wide:items-center max-wide:py-1 wide:flex-col wide:items-center wide:overflow-x-hidden wide:overflow-y-auto wide:px-3 wide:py-0.5">
          {(bots ?? []).map((b) => {
            const on = b.id === activeBotId;
            return (
              <Link
                key={b.id}
                to="/bots/$botId/run"
                params={{ botId: b.id }}
                title={b.name}
                className={railHit(on, `blink ${b.status === "working" ? "blink-idle" : ""}`)}
              >
                {railBar(on)}
                <span className="relative flex h-7 w-7">
                  <Crest index={b.crest} size={28} />
                  <Lamp status={b.status} onCrest={on ? "surface" : "well"} className="absolute -right-0.5 -bottom-0.5" />
                </span>
              </Link>
            );
          })}
          <Link to="/new" title="New Bot" className={railHit(loc.pathname === "/new")}>
            {railBar(loc.pathname === "/new")}
            <Plus size={20} />
          </Link>
        </div>
      </nav>
      <div className="flex items-center gap-1 max-wide:pr-2 wide:mb-4 wide:flex-col">
        {admin && (
          <Link to="/admin/settings" title="Admin" className={railHit(page === "admin")}>
            {railBar(page === "admin")}
            <Wrench size={20} />
          </Link>
        )}
        <ThemeToggle />
        <Link to="/account" title={email ? `Account · ${email}` : "Account"} className={railHit(page === "account")}>
          {railBar(page === "account")}
          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-ink text-[12px] font-semibold text-on-ink">{initial}</span>
        </Link>
        <button
          type="button"
          title="Sign out"
          className={railHit(false)}
          onClick={async () => {
            try {
              await ui.signOut({});
            } catch (e) {
              // Already signed out is the goal; anything else keeps the session.
              if (!isSignedOut(e)) return;
            }
            setSession(null);
          }}
        >
          <LogOut size={18} />
        </button>
      </div>
    </aside>
  );
}

// Shell is every signed-in page: the rail, and the page beside it.
export function Shell() {
  const { pathname } = useLocation();
  const activeBotId = pathname.match(/^\/bots\/([^/]+)/)?.[1];
  const page: Page = pathname.startsWith("/admin") ? "admin" : pathname.startsWith("/account") ? "account" : pathname.startsWith("/skills") ? "skills" : "bots";
  // Replies with code are common: fetch the highlighter once the browser is idle.
  useEffect(() => preloadMarkdown(), []);
  return (
    <div className="flex h-dvh overflow-hidden max-wide:flex-col">
      <Rail page={page} activeBotId={activeBotId} />
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
