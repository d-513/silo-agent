import { Book, LayoutGrid, LogOut, Plus, Wrench } from "lucide-react";
import type { ReactNode } from "react";
import { Link, useLocation } from "react-router-dom";
import { ui } from "./api";
import { useAuth } from "./auth";
import { useBots } from "./bots";
import { Crest } from "./Crest";
import { ErrorBoundary } from "./ErrorBoundary";
import { isSignedOut } from "./errors";
import { Lamp } from "./Lamp";
import { SiloMark } from "./SiloMark";

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

function Rail({ page }: { page: "bots" | "admin" | "account" | "skills" }) {
  const { admin, email, setSession } = useAuth();
  const { bots } = useBots();
  const loc = useLocation();
  const botMatch = loc.pathname.match(/^\/bots\/([^/]+)/);
  const activeBotId = botMatch?.[1];
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
                to={`/bots/${b.id}/run`}
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
          <Link to="/admin" title="Admin" className={railHit(page === "admin")}>
            {railBar(page === "admin")}
            <Wrench size={20} />
          </Link>
        )}
        <Link to="/account" title={email ? `Account · ${email}` : "Account"} className={railHit(page === "account")}>
          {railBar(page === "account")}
          <span className="flex h-7 w-7 items-center justify-center rounded-full bg-ink text-[12px] font-semibold text-white">{initial}</span>
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

export function Shell({ page, fill, children }: { page: "bots" | "admin" | "account" | "skills"; fill?: boolean; children: ReactNode }) {
  const { pathname } = useLocation();
  return (
    <div className="flex h-dvh overflow-hidden max-wide:flex-col">
      <Rail page={page} />
      <main className={`min-w-0 flex-1 ${fill ? "min-h-0 overflow-hidden" : "overflow-auto"}`}>
        <ErrorBoundary resetKey={pathname}>{children}</ErrorBoundary>
      </main>
    </div>
  );
}
