import { useQuery } from "@connectrpc/connect-query";
import { RefreshCw } from "lucide-react";
import { useState } from "react";
import { Btn } from "../../Btn";
import { ChipRadio } from "../../ChipRadio";
import { ErrorWell, SettingGroup } from "../../Field";
import { fail } from "../../errors";
import { UI } from "../../gen/silo/v1/ui_pb";
import { msg, t } from "../../i18n.ts";
import { reload } from "../../query";
import { clock, priorityTone } from "./siloosView";

const sources = [
  { value: "silo", label: msg("Silo") },
  { value: "silodb", label: msg("Database") },
  { value: "silocaddy", label: msg("HTTPS (Caddy)") },
  { value: "updates", label: msg("Updates") },
  { value: "siloosd", label: msg("Host daemon") },
  { value: "system", label: msg("System warnings") },
] as const;

type Source = (typeof sources)[number]["value"];

// LogsPanel reads the machine's journal, one of a fixed set of sources, the
// newest lines last.
export function LogsPanel() {
  const [source, setSource] = useState<Source>("silo");
  const q = useQuery(UI.method.readSiloOSLogs, { source, lines: 300 });
  const lines = q.data?.lines ?? [];
  const now = new Date();
  return (
    <SettingGroup title={t("Logs")}>
      <div className="grid gap-3 px-5 py-4">
        <div className="flex flex-wrap items-center gap-2">
          <ChipRadio size="sm" label={t("Which log")} value={source} onChange={setSource} className="w-fit flex-wrap" options={sources.map((s) => ({ value: s.value, label: t(s.label) }))} />
          <Btn size="sm" kind="ghost" className="ml-auto" icon={<RefreshCw size={12} />} onClick={() => void reload(UI.method.readSiloOSLogs, { source, lines: 300 })}>
            {t("Refresh")}
          </Btn>
        </div>
        {q.error ? <ErrorWell>{fail(q.error)}</ErrorWell> : null}
        <div className="max-h-[480px] overflow-auto rounded-sm bg-well p-3 font-mono text-label leading-5">
          {q.isPending ? (
            <span className="text-ink-3">{t("Reading…")}</span>
          ) : lines.length === 0 ? (
            <span className="text-ink-3">{t("Nothing logged.")}</span>
          ) : (
            lines.map((l, i) => (
              <div key={i} className="whitespace-pre-wrap break-words">
                <span className="text-ink-3">{clock(l.time, now)} </span>
                {source === "system" || source === "updates" ? <span className="text-ink-3">{l.unit} </span> : null}
                <span className={priorityTone(l.priority)}>{l.message}</span>
              </div>
            ))
          )}
        </div>
      </div>
    </SettingGroup>
  );
}
