import { Link } from "@tanstack/react-router";
import { LogOut, Moon, Sun, Wrench } from "lucide-react";
import { ui } from "../api";
import { useAuth } from "../auth";
import { isSignedOut } from "../errors";
import { t } from "../i18n.ts";
import { flipTheme, setTheme, useTheme } from "../theme";

// What the wide sidebar and the narrow top bar share.

/** The part of the console a page belongs to, for what the shell lights. */
export type Page = "bots" | "admin" | "account" | "workspace";

export async function signOut(setSession: (s: null) => void) {
  try {
    await ui.signOut({});
  } catch (e) {
    // Already signed out is the goal; anything else keeps the session.
    if (!isSignedOut(e)) return;
  }
  setSession(null);
}

// The theme toggle is one item whose glyph morphs like Send into Stop: the
// moon turns out as the sun turns in. It shows where a press leads.
export function ThemeToggle({ className }: { className: string }) {
  const theme = useTheme();
  const dark = theme === "dark";
  const label = dark ? t("Switch to light mode") : t("Switch to dark mode");
  const glyph = "col-start-1 row-start-1 transition-[transform,opacity] duration-[320ms] ease-settle motion-reduce:transition-opacity";
  const away = "scale-[.6] opacity-0";
  return (
    <button type="button" title={label} aria-label={label} className={className} onClick={() => setTheme(flipTheme(theme))}>
      <span className="grid place-items-center">
        <Moon size={17} className={`${glyph} ${dark ? `rotate-90 ${away}` : ""}`} />
        <Sun size={17} className={`${glyph} ${dark ? "" : `-rotate-90 ${away}`}`} />
      </span>
    </button>
  );
}

// A foot or head control of the sidebar: a 36px hit area on `well`.
export function hit(on: boolean) {
  return `relative flex h-9 w-9 shrink-0 items-center justify-center rounded-control transition-[background-color,color,box-shadow] duration-[160ms] ease-quiet ${
    on ? "bg-surface text-ink shadow-card" : "text-ink-2 hover:bg-pressed hover:text-ink"
  }`;
}

// AccountControls is the account, Admin, the theme and Sign out: the
// sidebar's foot, and the head of Admin, which has no sidebar. `wide` lets
// the account chip take the row's free room.
export function AccountControls({ page, wide = true, adminLink = true }: { page: Page; wide?: boolean; adminLink?: boolean }) {
  const { admin, email, setSession } = useAuth();
  const initial = (email.trim()[0] ?? "?").toUpperCase();
  return (
    <>
      <Link
        to="/account"
        title={email ? `${t("Account")} · ${email}` : t("Account")}
        data-tab-on={page === "account" || undefined}
        className={`flex h-10 min-w-0 items-center gap-2.5 rounded-control px-2 transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${
          wide ? "flex-1" : "max-w-[260px]"
        } ${page === "account" ? "bg-surface shadow-card" : "hover:bg-pressed"}`}
      >
        <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-ink text-micro font-semibold text-on-ink">{initial}</span>
        <span className="min-w-0 flex-1 truncate text-small text-ink-2">{email || t("Account")}</span>
      </Link>
      {admin && adminLink ? (
        <Link to="/admin/settings" title={t("Admin")} aria-label={t("Admin")} data-tab-on={page === "admin" || undefined} className={hit(page === "admin")}>
          <Wrench size={17} />
        </Link>
      ) : null}
      <ThemeToggle className={hit(false)} />
      <button type="button" title={t("Sign out")} aria-label={t("Sign out")} className={hit(false)} onClick={() => void signOut(setSession)}>
        <LogOut size={16} />
      </button>
    </>
  );
}
