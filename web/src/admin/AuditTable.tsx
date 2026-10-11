import { Crest } from "../Crest";
import { type AuditRow } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";

// The operator-wide log of approval decisions, newest first.
export function AuditTable({ audit }: { audit: AuditRow[] }) {
  return (
    <>
      {audit.length === 0 ? (
        <p className="text-ink-2">{t("No decisions yet.")}</p>
      ) : (
        <div className="silo-scroll-x">
          <table className="w-full min-w-[36rem] text-left text-[13px]">
            <thead className="bg-well text-ink-3">
              <tr>
                <th className="p-2">{t("When")}</th>
                <th className="p-2">{t("Bot")}</th>
                <th className="p-2">{t("Actor")}</th>
                <th className="p-2">{t("Action")}</th>
                <th className="p-2">{t("Decision")}</th>
              </tr>
            </thead>
            <tbody>
              {audit.map((r) => (
                <tr key={r.id} className="border-b border-line-strong">
                  <td className="p-2">{r.at}</td>
                  <td className="flex items-center gap-2 p-2">
                    <Crest index={r.crest} size={20} />
                    {r.botName}
                  </td>
                  <td className="p-2">{r.actor}</td>
                  <td className="p-2 font-mono">{r.action}</td>
                  <td className="p-2">{r.decision}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </>
  );
}
