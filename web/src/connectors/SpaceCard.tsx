import type { ReactNode } from "react";
import type { Connector } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { Lamp } from "../Lamp";
import { UsedBy } from "./BotPick";
import { CategoryChip, ConnectorMark, CustomChip, McpChip } from "./form";
import { isCustom } from "./model";

// One of a Workspace's connections: what it is, whose sign-in it carries,
// which Bots use it, and what can be done with it here.
export function SpaceCard({ c, actions }: { c: Connector; actions: ReactNode }) {
  const needsSignIn = c.auth === "oauth" && !c.signedIn;
  return (
    <div className="flex min-h-[140px] flex-col justify-between rounded-card bg-surface p-5 shadow-card transition-[box-shadow] hover:shadow-float">
      <div className="flex items-start gap-4">
        <div className="shrink-0 pt-0.5">
          <ConnectorMark id={c.id} hasImage={c.hasImage} size={48} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[15px] font-semibold tracking-tight text-ink">{c.name}</span>
            <McpChip transport={c.transport} />
            {isCustom(c) && <CustomChip />}
            {c.category && <CategoryChip label={c.category} />}
          </div>
          <p className="mt-1.5 line-clamp-2 text-xs leading-relaxed text-ink-2">{c.description || (c.transport === "http" ? c.httpUrl : c.stdioCommand)}</p>
          <div className="mt-2.5 flex items-center gap-1.5 text-[11px] font-medium text-ink-3">
            {needsSignIn ? (
              <>
                <Lamp status="needs_you" />
                <span className="text-vermilion">{t("Needs sign-in")}</span>
              </>
            ) : c.lentBy ? (
              <span>{t("Signed in by {{who}}", { who: c.lentBy })}</span>
            ) : null}
          </div>
        </div>
      </div>
      <div className="mt-4 flex flex-wrap items-center justify-between gap-2 border-t border-line/80 pt-3.5">
        <UsedBy ids={c.usedBy} hidden={c.usedByHidden} />
        <div className="flex items-center gap-1.5">{actions}</div>
      </div>
    </div>
  );
}
