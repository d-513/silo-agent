import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { Power, RotateCcw } from "lucide-react";
import { useState } from "react";
import { ArmedButton } from "../../Feedback";
import { ErrorWell, SettingRow, SkeletonRows } from "../../Field";
import { AdminHead, Empty, Group, Groups } from "../kit";
import { ui } from "../../api";
import { fail } from "../../errors";
import { fmtBytes } from "../../format";
import { UI } from "../../gen/silo/v1/ui_pb";
import type { SystemInfo } from "../../gen/silo/v1/siloos_pb";
import { t } from "../../i18n.ts";
import { ProxyLines, RootButton } from "../CaddyHealth";
import { LogsPanel } from "./LogsPanel";
import { NetworkPanel } from "./NetworkPanel";
import { NameAndTime } from "./NameAndTime";
import { UpdatesPanel } from "./UpdatesPanel";
import { uptime, used } from "./siloosView";

// SiloOSPage is Admin → SiloOS: the machine Silo runs on, through siloosd.
// It is listed only while the control plane runs on SiloOS (GetSiloOS).
export function SiloOSPage() {
  const q = useQuery(UI.method.getSiloOS, {}, { refetchInterval: 15_000 });
  const st = q.data;
  if (!st) return q.error ? <ErrorWell>{fail(q.error)}</ErrorWell> : <SkeletonRows rows={4} height={88} />;
  if (!st.available) {
    return (
      <>
        <AdminHead title={t("SiloOS")} note={t("This control plane does not run on SiloOS, so there is no machine to manage from here.")} />
        <Group>
          <Empty>{t("SiloOS is a bootable image that runs Silo and nothing else. See images/os/README.md in the repository.")}</Empty>
        </Group>
      </>
    );
  }
  return (
    <>
      <AdminHead title={t("SiloOS")} note={t("The machine Silo runs on: its network, name, clock, updates and power.")} />
      <Groups>
        {st.error ? <ErrorWell>{t("The host daemon (siloosd) is not answering: {{error}}", { error: st.error })}</ErrorWell> : st.system ? <Summary sys={st.system} /> : null}
        <Group title={t("HTTPS")} note={t("Caddy in front of Silo, configured by Silo from Admin → Settings → HTTPS.")} action={st.proxy ? <RootButton proxy={st.proxy} /> : undefined}>
          <SettingRow label={t("Certificates")} hint={t("Install the root certificate on a device to trust Silo's own certificates there.")}>
            <Link to="/admin/settings/$section" params={{ section: "https" }} className="text-small font-medium text-cobalt hover:text-cobalt-deep">
              {t("HTTPS settings")}
            </Link>
          </SettingRow>
          {st.proxy?.configured ? <ProxyLines proxy={st.proxy} /> : null}
        </Group>
        {st.error ? null : (
          <>
            <NetworkPanel />
            {st.system ? <NameAndTime sys={st.system} /> : null}
            <UpdatesPanel />
            <PowerGroup />
            <LogsPanel />
          </>
        )}
      </Groups>
    </>
  );
}

function Stat({ label, value, note }: { label: string; value: string; note?: string }) {
  return (
    <div className="min-w-0">
      <div className="text-label text-ink-3">{label}</div>
      <div className="mt-0.5 truncate text-row font-medium text-ink" title={value}>
        {value}
      </div>
      {note ? <div className="truncate text-meta text-ink-3">{note}</div> : null}
    </div>
  );
}

function Summary({ sys }: { sys: SystemInfo }) {
  const memTotal = Number(sys.memoryTotal);
  const memFree = Number(sys.memoryAvailable);
  return (
    <div className="grid grid-cols-2 gap-x-6 gap-y-4 rounded-card bg-well px-5 py-4 @2xl/page:grid-cols-5">
      <Stat label={t("Machine")} value={sys.hostname || "—"} note={sys.osName} />
      <Stat label={t("Version")} value={sys.imageVersion || "—"} note={sys.image} />
      <Stat label={t("Up")} value={uptime(Number(sys.uptimeSeconds))} note={t("load {{load}} on {{cpus}} CPUs", { load: sys.load1.toFixed(2), cpus: sys.cpus })} />
      <Stat label={t("Memory")} value={`${used(memTotal, memFree)}%`} note={t("{{free}} free of {{total}}", { free: fmtBytes(memFree), total: fmtBytes(memTotal) })} />
      {sys.disks.map((d) => (
        <Stat
          key={d.path}
          label={d.label === "system" ? t("System disk") : t("Data disk")}
          value={`${used(Number(d.total), Number(d.free))}%`}
          note={t("{{free}} free of {{total}}", { free: fmtBytes(Number(d.free)), total: fmtBytes(Number(d.total)) })}
        />
      ))}
    </div>
  );
}

function PowerGroup() {
  const [err, setErr] = useState("");
  const [gone, setGone] = useState("");
  async function power(action: "reboot" | "poweroff") {
    setErr("");
    try {
      await ui.siloOSPower({ action });
      setGone(action === "reboot" ? t("Restarting. Silo comes back in a minute or two; this page reconnects by itself.") : t("Shutting down. Silo stays off until the machine is switched on again."));
    } catch (e) {
      setErr(fail(e));
    }
  }
  return (
    <Group title={t("Power")} tone="danger" note={t("Every Bot stops with the machine, and starts again with it.")}>
      <SettingRow label={t("Restart the machine")} hint={gone || t("Also how a downloaded update is put to use.")} error={err}>
        <ArmedButton kind="secondary" armedLabel={t("Click again to restart")} icon={<RotateCcw size={13} />} onConfirm={() => void power("reboot")} disabled={!!gone}>
          {t("Restart")}
        </ArmedButton>
      </SettingRow>
      <SettingRow label={t("Shut down")} hint={t("Someone has to switch it on again by hand.")}>
        <ArmedButton kind="deny" armedLabel={t("Click again to shut down")} icon={<Power size={13} />} onConfirm={() => void power("poweroff")} disabled={!!gone}>
          {t("Shut down")}
        </ArmedButton>
      </SettingRow>
    </Group>
  );
}
