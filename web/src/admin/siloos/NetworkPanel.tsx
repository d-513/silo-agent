import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useState } from "react";
import { Btn } from "../../Btn";
import { ChipRadio } from "../../ChipRadio";
import { ErrorWell, Field, SettingRow, inputClass, textareaClass } from "../../Field";
import { Group } from "../kit";
import { Status } from "../../Lamp";
import { ToggleRow } from "../../Switch";
import { ui } from "../../api";
import { fail } from "../../errors";
import type { NetDevice, Network } from "../../gen/silo/v1/siloos_pb";
import { UI } from "../../gen/silo/v1/ui_pb";
import { t } from "../../i18n.ts";
import { patch } from "../../query";
import { netDeviceLook } from "../../statusLook";
import { editable, formOf, requestOf, secondsLeft, type NetForm } from "./siloosView";

// NetworkPanel lists the machine's network devices and edits a wired one. A
// change runs under a NetworkManager checkpoint (siloosd): it is undone by
// itself unless kept from here in time, so a change that cuts this page off
// cannot strand the machine.
export function NetworkPanel() {
  const q = useQuery(UI.method.getSiloOSNetwork, {}, { refetchInterval: (query) => (query.state.data?.pending ? 2_000 : 30_000) });
  const net = q.data;
  return (
    <Group title={t("Network")} note={t("Wired connections can be changed here. A change is undone by itself unless you keep it in time.")}>
      {q.error ? (
        <div className="px-5 py-4">
          <ErrorWell>{fail(q.error)}</ErrorWell>
        </div>
      ) : null}
      {net?.pending ? <Pending net={net} /> : null}
      {net && net.devices.length === 0 ? <div className="px-5 py-4 text-small text-ink-3">{t("NetworkManager lists no network devices.")}</div> : null}
      {net?.devices.map((d) => <Device key={d.name} dev={d} locked={!!net.pending} />)}
    </Group>
  );
}

function Pending({ net }: { net: Network }) {
  const [now, setNow] = useState(Date.now());
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  const left = secondsLeft(net.pending?.expiresAt ?? "", now);
  async function act(keep: boolean) {
    setErr("");
    setBusy(true);
    try {
      const n = keep ? await ui.confirmSiloOSNetwork({}) : await ui.rollBackSiloOSNetwork({});
      patch(UI.method.getSiloOSNetwork, {}, () => n);
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }
  return (
    <SettingRow
      label={t("Keep the new network settings?")}
      hint={left > 0 ? t("They are undone in {{seconds}} s unless you keep them.", { seconds: left }) : t("Time is up: the old settings are coming back.")}
      error={err}
    >
      <div className="flex gap-2">
        <Btn kind="ghost" disabled={busy} onClick={() => void act(false)}>
          {t("Undo now")}
        </Btn>
        <Btn kind="primary" disabled={busy || left === 0} onClick={() => void act(true)}>
          {t("Keep")}
        </Btn>
      </div>
    </SettingRow>
  );
}

function Device({ dev, locked }: { dev: NetDevice; locked: boolean }) {
  const [form, setForm] = useState<NetForm | null>(null);
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const conn = dev.connection;
  const facts = [
    dev.addresses.join(", "),
    dev.gateway ? t("gateway {{gateway}}", { gateway: dev.gateway }) : "",
    dev.dns.length ? t("DNS {{dns}}", { dns: dev.dns.join(", ") }) : "",
  ].filter(Boolean);

  async function apply() {
    if (!form || !conn) return;
    setErr("");
    setBusy(true);
    try {
      const n = await ui.applySiloOSConnection(requestOf(conn.uuid, form));
      patch(UI.method.getSiloOSNetwork, {}, () => n);
      setForm(null);
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  const set = (p: Partial<NetForm>) => setForm((f) => (f ? { ...f, ...p } : f));
  return (
    <div>
      <SettingRow
        label={
          <span className="inline-flex items-center gap-2">
            <span className="font-mono">{dev.name}</span>
            <span className="text-meta font-normal text-ink-3">{conn?.name || dev.type}</span>
          </span>
        }
        hint={
          <>
            {facts.length ? <span className="block font-mono text-small">{facts.join(" · ")}</span> : null}
            <span className="block">
              {conn ? (conn.method === "manual" ? t("Set by hand") : conn.method === "auto" ? t("From DHCP") : t("Not editable here")) : t("No connection profile")}
              {dev.mac ? ` · ${dev.mac}` : ""}
            </span>
          </>
        }
        meta={<Status look={netDeviceLook(dev.state)} />}
      >
        {editable(conn) && !form ? (
          <Btn size="sm" disabled={locked} onClick={() => conn && setForm(formOf(conn))}>
            {t("Change")}
          </Btn>
        ) : null}
      </SettingRow>
      {form ? (
        <div className="grid gap-4 bg-well px-5 py-4">
          <ChipRadio
            label={t("How the address is set")}
            value={form.method}
            onChange={(m) => set({ method: m })}
            options={[
              { value: "auto", label: t("DHCP") },
              { value: "manual", label: t("By hand") },
            ]}
          />
          {form.method === "manual" ? (
            <>
              <Field label={t("Addresses")} hint={t("One per line, with the prefix length: 192.168.1.10/24.")}>
                <textarea className={`${textareaClass} font-mono text-small`} rows={2} spellCheck={false} value={form.addresses} onChange={(e) => set({ addresses: e.target.value })} />
              </Field>
              <Field label={t("Gateway")}>
                <input className={`${inputClass} font-mono text-small`} spellCheck={false} placeholder="192.168.1.1" value={form.gateway} onChange={(e) => set({ gateway: e.target.value })} />
              </Field>
            </>
          ) : null}
          <Field label={t("DNS servers")} hint={form.method === "auto" ? t("Used beside the ones DHCP hands out, unless you turn those off below.") : t("Separated by commas.")}>
            <input className={`${inputClass} font-mono text-small`} spellCheck={false} placeholder="1.1.1.1, 9.9.9.9" value={form.dns} onChange={(e) => set({ dns: e.target.value })} />
          </Field>
          {form.method === "auto" ? (
            <ToggleRow label={t("Ignore DHCP's DNS servers")} on={form.ignoreAutoDns} onChange={(v) => set({ ignoreAutoDns: v })} />
          ) : null}
          {err ? <ErrorWell>{err}</ErrorWell> : null}
          <div className="flex items-center justify-end gap-2">
            <span className="mr-auto text-meta text-ink-3">{t("You have two minutes to keep it, from wherever this page then answers.")}</span>
            <Btn kind="ghost" disabled={busy} onClick={() => setForm(null)}>
              {t("Cancel")}
            </Btn>
            <Btn kind="primary" disabled={busy} onClick={() => void apply()}>
              {t("Apply")}
            </Btn>
          </div>
        </div>
      ) : null}
    </div>
  );
}
