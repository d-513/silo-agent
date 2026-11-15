import { Moon, Sun } from "lucide-react";
import { ui } from "../api";
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
