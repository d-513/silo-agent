import { useLocation } from "@tanstack/react-router";
import { Menu } from "lucide-react";
import { useEffect, type MouseEvent } from "react";
import { useBots } from "../bots";
import { t } from "../i18n.ts";
import { useCurrentSpace } from "../space/spaces";
import { botLook } from "../statusLook";
import type { Page } from "./chrome";
import { closeNav, openNav, useNavOpen } from "./navStore";
import { Sheet } from "./Sheet";
import { Sidebar } from "./Sidebar";

// Below 960 the sidebar is a sheet from the left: the same Workspace, the
// same Bots as conversations with their Call, the same account at the foot.
// Whatever top bar the page has opens it with NavButton; a swipe in from the
// left edge pulls it out.

export function NavSheet({ page, activeBotId }: { page: Page; activeBotId?: string }) {
  const shown = useNavOpen();
  const { pathname } = useLocation();
  // Any move to another page puts it away.
  useEffect(() => closeNav(), [pathname]);
  // A press on a row opens what it opens even when that is the page already.
  function onClick(e: MouseEvent) {
    if (e.target instanceof Element && e.target.closest("a[href]")) closeNav();
  }
  return (
    <Sheet side="left" open={shown} edge label={t("Bots")} onOpen={openNav} onClose={closeNav}>
      <div className="flex min-h-0 flex-1" onClickCapture={onClick}>
        <Sidebar page={page} activeBotId={activeBotId} className="min-w-0 flex-1" />
      </div>
    </Sheet>
  );
}

// NavButton opens the sheet. Its dot says something waits on another Bot of
// this Workspace: vermilion when one needs you, else cobalt for what is
// unread on one.
export function NavButton({ activeBotId }: { activeBotId?: string }) {
  const { bots } = useBots();
  const { space } = useCurrentSpace();
  const others = (bots ?? []).filter((b) => b.spaceId === space?.id && b.id !== activeBotId);
  const needs = others.some((b) => botLook(b).lamp === "needs_you");
  const fresh = !needs && others.some((b) => b.dashboardAsks + b.feedUnread > 0);
  const shown = useNavOpen();
  return (
    <button
      type="button"
      title={t("Bots")}
      aria-label={t("Bots")}
      aria-haspopup="dialog"
      aria-expanded={shown}
      onClick={openNav}
      className="relative flex h-10 w-10 shrink-0 items-center justify-center rounded-control text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet active:scale-[.94] active:bg-pressed motion-reduce:active:scale-100"
    >
      <Menu size={20} />
      {needs || fresh ? (
        <span aria-hidden className={`absolute top-2 right-1.5 h-2 w-2 rounded-full ring-2 ring-well ${needs ? "bg-vermilion" : "bg-cobalt"}`} />
      ) : null}
    </button>
  );
}
