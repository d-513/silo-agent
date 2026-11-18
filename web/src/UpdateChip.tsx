import { Tip, TipTitle } from "./Tip";
import { msg, t } from "./i18n.ts";

// When each kind of container is made again on the newer image.
const WHEN: Record<string, string> = {
  bot: msg("It is made again the next time the Bot's machine starts: Stop, then Start. The workspace, drives and connectors stay."),
  drive: msg("It is made again the next time the Bot's machine starts."),
  mcp: msg("It is made again the next time the Bot uses this connector."),
  egress: msg("It is made again within a minute; the Bots' network pauses for a few seconds."),
};

// UpdateChip marks a container made from an older image, or another way,
// than this Silo would make it now (BotContainer.outdated).
export function UpdateChip({ kind }: { kind: string }) {
  return (
    <Tip
      content={
        <>
          <TipTitle>{t("Update waiting")}</TipTitle>
          <p className="mt-1">{t("This container runs an older image than this Silo release.")}</p>
          <p className="mt-1">{t(WHEN[kind] ?? WHEN.mcp)}</p>
        </>
      }
    >
      <span tabIndex={0} className="rounded-sm bg-cobalt-pale px-1.5 py-0.5 text-micro font-medium text-cobalt-deep">
        {t("Update waiting")}
      </span>
    </Tip>
  );
}
