import { useId, useState } from "react";
import { Btn } from "../../Btn";
import { SettingGroup, SettingRow, inputClass } from "../../Field";
import { Switch } from "../../Switch";
import { ui } from "../../api";
import { fail } from "../../errors";
import type { SystemInfo } from "../../gen/silo/v1/siloos_pb";
import { UI } from "../../gen/silo/v1/ui_pb";
import { t } from "../../i18n.ts";
import { patch } from "../../query";
import { zoneNames } from "../../zones";

// NameAndTime is the machine's host name and clock. The Bots' own zones are
// theirs (Settings → Time zone); this is only the machine's, which is what
// the update policy's hour is in.
export function NameAndTime({ sys }: { sys: SystemInfo }) {
  const [name, setName] = useState<string | null>(null);
  const [zone, setZone] = useState<string | null>(null);
  const [err, setErr] = useState<{ name?: string; zone?: string; ntp?: string }>({});
  const [busy, setBusy] = useState(false);
  const listId = useId();

  async function save(what: "name" | "zone" | "ntp", call: () => Promise<SystemInfo>) {
    setErr({});
    setBusy(true);
    try {
      const s = await call();
      patch(UI.method.getSiloOS, {}, (st) => ({ ...st, system: s }));
      if (what === "name") setName(null);
      if (what === "zone") setZone(null);
    } catch (e) {
      setErr({ [what]: fail(e) });
    } finally {
      setBusy(false);
    }
  }

  const drift = Math.round((Date.parse(sys.now) - Date.now()) / 1000);
  return (
    <SettingGroup title={t("Name and time")}>
      <SettingRow label={t("Host name")} hint={t("How the machine names itself on the network.")} error={err.name}>
        <div className="flex w-full gap-2">
          <input
            className={`${inputClass} font-mono text-small`}
            spellCheck={false}
            aria-label={t("Host name")}
            value={name ?? sys.hostname}
            onChange={(e) => setName(e.target.value)}
          />
          <Btn disabled={busy || name === null || name === sys.hostname} onClick={() => void save("name", () => ui.setSiloOSHostname({ hostname: name ?? "" }))}>
            {t("Save")}
          </Btn>
        </div>
      </SettingRow>
      <SettingRow label={t("Time zone")} hint={t("The machine's own. Bots keep theirs; the update hour below is in this one.")} error={err.zone}>
        <div className="flex w-full gap-2">
          <input
            className={`${inputClass} font-mono text-small`}
            list={listId}
            spellCheck={false}
            autoComplete="off"
            aria-label={t("Time zone")}
            value={zone ?? sys.timeZone}
            onChange={(e) => setZone(e.target.value.trim())}
          />
          <datalist id={listId}>
            {zoneNames(sys.timeZone).map((n) => (
              <option key={n} value={n} />
            ))}
          </datalist>
          <Btn disabled={busy || zone === null || zone === sys.timeZone} onClick={() => void save("zone", () => ui.setSiloOSTime({ timeZone: zone ?? "" }))}>
            {t("Save")}
          </Btn>
        </div>
      </SettingRow>
      <SettingRow
        label={t("Set the clock from the internet (NTP)")}
        hint={
          sys.ntp
            ? sys.ntpSynced
              ? t("In sync.")
              : t("Not in sync yet.")
            : Math.abs(drift) > 5
              ? t("The machine's clock is {{seconds}} s off from this device's.", { seconds: drift })
              : t("Off.")
        }
        error={err.ntp}
      >
        <Switch on={sys.ntp} disabled={busy} title={t("Set the clock from the internet (NTP)")} onChange={(v) => void save("ntp", () => ui.setSiloOSTime({ ntp: v }))} />
      </SettingRow>
    </SettingGroup>
  );
}
