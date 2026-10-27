import { useQuery } from "@connectrpc/connect-query";
import { useParams } from "@tanstack/react-router";
import { HardDrive, Plug } from "lucide-react";
import { useState } from "react";
import { ui } from "../api";
import { useManagedBots } from "../connectors/BotPick";
import { Crest } from "../Crest";
import { fail } from "../errors";
import { ErrorWell, SkeletonRows } from "../Field";
import { UI } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { reload } from "../query";
import { Switch } from "../Switch";

type Cell = { kind: "connector" | "drive"; id: string; name: string; usedBy: string[] };

// /w/$spaceId/customize/access: what each Bot you manage is given, in one
// grid: a row per connector and drive, a column per Bot, a switch per cell.
// A refusal (someone's personal sign-in) is said above the grid.
export function SpaceAccess() {
  const { spaceId } = useParams({ from: "/_authed/w/$spaceId/customize/access" });
  const bots = useManagedBots(spaceId);
  const conns = useQuery(UI.method.listSpaceConnectors, { spaceId });
  const drives = useQuery(UI.method.listSpaceDrives, { spaceId, draftId: "" });
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState("");
  const rows: Cell[] = [
    ...(conns.data?.connectors ?? []).map((c) => ({ kind: "connector" as const, id: c.id, name: c.name, usedBy: c.usedBy })),
    ...(drives.data?.drives ?? []).map((d) => ({ kind: "drive" as const, id: d.id, name: d.name, usedBy: d.usedBy })),
  ];

  async function flip(r: Cell, botId: string, on: boolean) {
    setErr("");
    setBusy(`${r.id}:${botId}`);
    try {
      if (r.kind === "connector") {
        await (on ? ui.grantConnector({ botId, connectorId: r.id }) : ui.revokeConnector({ botId, connectorId: r.id }));
        await reload(UI.method.listSpaceConnectors, { spaceId });
      } else {
        await (on ? ui.grantDrive({ botId, driveId: r.id }) : ui.revokeDrive({ botId, driveId: r.id }));
        await reload(UI.method.listSpaceDrives, { spaceId });
      }
    } catch (ex) {
      setErr(`${r.name}: ${fail(ex)}`);
    }
    setBusy("");
  }

  return (
    <div className="silo-page silo-page-wide rise">
      <h2 className="text-title text-ink">{t("Access")}</h2>
      <p className="mb-5 mt-1 max-w-[560px] text-[13.5px] leading-[21px] text-ink-2">
        {t("Which of your Bots gets each of the workspace's connectors and drives. A switch gives or takes it at once.")}
      </p>
      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}
      {bots === null || !conns.data || !drives.data ? (
        <SkeletonRows rows={3} height={44} />
      ) : rows.length === 0 || bots.length === 0 ? (
        <p className="text-[13.5px] text-ink-3">{bots.length === 0 ? t("You manage no Bot in this workspace.") : t("The workspace has no connectors or drives yet.")}</p>
      ) : (
        <div className="overflow-auto rounded-card bg-surface shadow-card">
          <table className="w-full border-collapse text-[13px]">
            <thead>
              <tr>
                <th className="sticky left-0 bg-surface px-4 py-3 text-left font-medium text-ink-3" />
                {bots.map((b) => (
                  <th key={b.id} className="px-3 py-3 text-center font-medium text-ink-2">
                    <span className="flex flex-col items-center gap-1">
                      <Crest index={b.crest} size={22} />
                      <span className="max-w-24 truncate text-[12px]">{b.name}</span>
                    </span>
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => (
                <tr key={r.id} className="border-t border-line">
                  <td className="sticky left-0 bg-surface px-4 py-2.5">
                    <span className="flex items-center gap-2 text-ink">
                      {r.kind === "connector" ? <Plug size={14} className="text-ink-3" /> : <HardDrive size={14} className="text-ink-3" />}
                      {r.name}
                    </span>
                  </td>
                  {bots.map((b) => {
                    const on = r.usedBy.includes(b.id);
                    return (
                      <td key={b.id} className="px-3 py-2.5 text-center">
                        <span className="inline-flex">
                          <Switch
                            on={on}
                            disabled={busy === `${r.id}:${b.id}`}
                            title={on ? t("Take it away from {{name}}", { name: b.name }) : t("Give it to {{name}}", { name: b.name })}
                            onChange={(v) => void flip(r, b.id, v)}
                          />
                        </span>
                      </td>
                    );
                  })}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}
