import { useQuery } from "@connectrpc/connect-query";
import { ShieldAlert, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { Panel, SkeletonRows } from "../Field";
import { CopyButton, SaveButton, useSave } from "../Feedback";
import { UI, type EgressStatus, type FeedStatus } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { FieldGroup } from "./FieldGroup";
import { NETWORK_NOTE } from "./fields";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

// Admin → Settings → Network: the gateway every Bot's machine leaves through.
// The fields are the form's; what the gateway itself is doing is read here.
export function NetworkSettings() {
  const { fields } = useSettingsForm();
  return (
    <>
      <FieldGroup title={t("Network filter")} note={t(NETWORK_NOTE)} rows={fields.filter((f) => f.key.startsWith("egress."))} />
      <EgressHealth />
    </>
  );
}

const noFeeds: FeedStatus[] = [];

function EgressHealth() {
  const q = useQuery(UI.method.getEgressStatus, {}, { refetchInterval: 15000 });
  const status: EgressStatus | undefined = q.data;
  const [err, setErr] = useState("");
  const saver = useSave();

  async function rotate() {
    setErr("");
    try {
      await saver.run(() => ui.rotateEgressCA({}));
      void reload(UI.method.getEgressStatus);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  if (q.isPending) return <SkeletonRows rows={2} height={44} />;
  const feeds = status?.feeds ?? noFeeds;
  const off = status && !status.enabled;

  return (
    <Panel
      title={t("Gateway")}
      note={t("Every Bot's own network meets the gateway here. It answers DNS, keeps the threat lists below current, and records what it blocks or flags on the Bot's timeline.")}
      action={
        status ? (
          <span className="flex items-center gap-2 text-[12.5px]">
            {off ? <ShieldAlert size={16} className="text-vermilion" /> : <ShieldCheck size={16} className="text-emerald" />}
            <span className={off ? "text-vermilion" : "text-ink-2"}>{off ? t("Off") : t("Serving")}</span>
          </span>
        ) : null
      }
    >
      {err ? <p className="mb-3 text-[13px] text-vermilion">{err}</p> : null}
      {status?.problem ? <p className="mb-3 text-[13px] text-vermilion">{status.problem}</p> : null}
      <dl className="mb-4 grid grid-cols-2 gap-x-4 gap-y-2 text-[13px] wide:grid-cols-3">
        <Fact label={t("Bots on their own network")}>{status?.networks ?? 0}</Fact>
        <Fact label={t("Last word from the gateway")}>{status?.healthyAt || t("never")}</Fact>
        <Fact label={t("Gateway certificate")}>
          {status?.caFingerprint ? (
            <span className="flex items-center gap-1">
              <span className="font-mono text-[12px] break-all">{status.caFingerprint}</span>
              <CopyButton text={status.caFingerprint} title={t("Copy")} />
            </span>
          ) : (
            t("made at the first start")
          )}
        </Fact>
      </dl>

      <div className="silo-scroll-x mb-3">
        <table className="w-full min-w-[30rem] text-left text-[13px]">
          <thead className="text-ink-3">
            <tr>
              <th className="py-1.5 pr-3 font-medium">{t("Threat list")}</th>
              <th className="py-1.5 pr-3 text-right font-medium">{t("Entries")}</th>
              <th className="py-1.5 font-medium">{t("Last loaded")}</th>
            </tr>
          </thead>
          <tbody>
            {feeds.map((f) => (
              <tr key={f.id} className="border-t border-line">
                <td className="py-2 pr-3">
                  <span className="font-mono">{f.id}</span>
                  {f.error ? <span className="ml-2 text-vermilion">{f.error}</span> : null}
                </td>
                <td className="py-2 pr-3 text-right tabular-nums">{f.entries}</td>
                <td className={`py-2 ${f.updatedAt ? "text-ink-2" : "text-ink-3"}`}>{f.updatedAt || t("never")}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex items-center justify-between gap-3 border-t border-line pt-3">
        <p className="text-[12.5px] leading-[18px] text-ink-3">
          {t("Rotating makes a new certificate for the gateway. Existing machines and connector sidecars trust only the old one: HTTPS through the gateway fails there until each is recreated (Containers → Remove all containers).")}
        </p>
        <SaveButton kind="secondary" state={saver.state} onClick={() => void rotate()}>
          {t("Rotate certificate")}
        </SaveButton>
      </div>
    </Panel>
  );
}

function Fact({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="min-w-0">
      <dt className="text-[11.5px] tracking-wide text-ink-3 uppercase">{label}</dt>
      <dd className="mt-0.5 text-ink">{children}</dd>
    </div>
  );
}
