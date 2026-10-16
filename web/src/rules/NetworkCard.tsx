import { useQuery } from "@connectrpc/connect-query";
import { Lock, X } from "lucide-react";
import { useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { inputClass, Panel } from "../Field";
import { UI, type HostPattern } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { Select } from "../Select";
import { ToggleRow } from "../Switch";
import { msg, t } from "../i18n.ts";

// The Bot's own share of the network filter. The operator's entries are shown
// beside it and cannot be taken off; what the owner adds can be.
const KINDS = [
  { id: "block", label: msg("Block"), list: msg("Blocked"), hint: msg("The Bot cannot reach these at all.") },
  { id: "trust", label: msg("Trust"), list: msg("Trusted"), hint: msg("Reached without being looked at, where a rule would otherwise check them.") },
  { id: "pass", label: msg("Pass through"), list: msg("Passed through"), hint: msg("Forwarded untouched, for a service the gateway would otherwise break.") },
] as const;

export function NetworkCard({ botId }: { botId: string }) {
  const q = useQuery(UI.method.getBotNetwork, { botId }, { refetchInterval: 20000 });
  const net = q.data;
  const [err, setErr] = useState("");
  const [kind, setKind] = useState<string>("block");
  const [pattern, setPattern] = useState("");
  const [busy, setBusy] = useState(false);

  async function send(fn: () => Promise<unknown>) {
    setErr("");
    setBusy(true);
    try {
      await fn();
      await reload(UI.method.getBotNetwork, { botId });
    } catch (ex) {
      setErr(fail(ex));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Panel
      className="mb-5"
      title={t("Network")}
      note={t("Your Bot's machine has its own network: it can reach other machines on the internet but nothing on yours, and every connection leaves through a gateway that records the host. These lists say where it may go. Anything the operator set is shown locked and stays as it is.")}
    >
      {err ? <p className="mb-3 text-[13px] text-vermilion">{err}</p> : null}
      {net && !net.enabled ? (
        <p className="mb-3 rounded-card bg-well p-3 text-[13px] text-ink-2">
          {t("The operator has the network filter turned off, so connections are not being watched. These lists are kept and take effect when it is on again.")}
        </p>
      ) : null}

      <div className="space-y-4">
        {KINDS.map((k) => (
          <HostList
            key={k.id}
            label={t(k.list)}
            hint={t(k.hint)}
            entries={(net?.patterns ?? []).filter((p) => p.kind === k.id)}
            busy={busy}
            onRemove={(p) => send(() => ui.removeHostPattern({ botId, kind: p.kind, pattern: p.pattern }))}
          />
        ))}
      </div>

      <div className="mt-4 flex flex-wrap items-end gap-2 border-t border-line pt-4">
        <label className="min-w-0 flex-1">
          <span className="mb-1 block text-[12.5px] font-medium text-ink-2">{t("Add a host")}</span>
          <input
            value={pattern}
            onChange={(e) => setPattern(e.target.value)}
            placeholder="example.com, *.example.com, 10.0.0.1"
            spellCheck={false}
            className={`${inputClass} h-9 font-mono text-[13px]`}
          />
        </label>
        <label className="w-[9.5rem] shrink-0">
          <span className="mb-1 block text-[12.5px] font-medium text-ink-2">{t("List")}</span>
          <Select value={kind} onChange={setKind} options={KINDS.map((k) => ({ value: k.id, label: t(k.label) }))} />
        </label>
        <Btn
          kind="primary"
          disabled={busy || pattern.trim() === ""}
          onClick={() =>
            void send(async () => {
              await ui.addHostPattern({ botId, kind, pattern: pattern.trim() });
              setPattern("");
            })
          }
        >
          {t("Add")}
        </Btn>
      </div>

      <ToggleRow
        className="mt-5 border-t border-line pt-4"
        label={t("Anything that is not HTTP")}
        hint={t("Lets the Bot open raw connections (a database, SSH, a game server) through the gateway, which then sees the host but not what is said. Off, DNS, HTTP and HTTPS are all that get out.")}
        on={net?.otherProtocols ?? false}
        disabled={busy || !net}
        onChange={(on) => void send(() => ui.setOtherProtocols({ botId, on }))}
      />
    </Panel>
  );
}

function HostList({
  label,
  hint,
  entries,
  busy,
  onRemove,
}: {
  label: string;
  hint: string;
  entries: HostPattern[];
  busy: boolean;
  onRemove: (p: HostPattern) => void;
}) {
  return (
    <div>
      <div className="flex flex-wrap items-baseline gap-x-2">
        <span className="text-[13.5px] font-medium text-ink">{label}</span>
        <span className="text-[12px] text-ink-3">{hint}</span>
      </div>
      {entries.length === 0 ? (
        <p className="mt-1 text-[12.5px] text-ink-3">{t("Nothing here.")}</p>
      ) : (
        <ul className="mt-1.5 flex flex-wrap gap-1.5">
          {entries.map((p) => (
            <li
              key={p.kind + "\u0000" + p.pattern}
              className={`flex items-center gap-1.5 rounded-sm py-1 pr-1 pl-2 font-mono text-[12.5px] ${
                p.fromOperator ? "bg-well text-ink-2" : "bg-cobalt-pale text-ink"
              }`}
            >
              {p.fromOperator ? <Lock size={11} className="shrink-0 text-ink-3" /> : null}
              {p.pattern}
              {p.fromOperator ? (
                <span className="font-sans text-[11px] text-ink-3">{t("from the operator")}</span>
              ) : (
                <button
                  type="button"
                  aria-label={t("Remove {{host}}", { host: p.pattern })}
                  disabled={busy}
                  onClick={() => onRemove(p)}
                  className="flex h-5 w-5 items-center justify-center rounded-xs text-ink-2 hover:bg-vermilion-pale hover:text-vermilion disabled:opacity-40"
                >
                  <X size={12} />
                </button>
              )}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
