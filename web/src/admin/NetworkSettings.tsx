import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { SettingRow, SkeletonRows } from "../Field";
import { CopyButton, SaveButton, useSave } from "../Feedback";
import { UI, type EgressStatus, type FeedStatus } from "../gen/silo/v1/ui_pb";
import { reload } from "../query";
import { FieldGroup } from "./FieldGroup";
import { Block, Empty, Fact, Facts, Group } from "./kit";
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
    <>
      <Group
        title={t("Gateway")}
        note={t("Every Bot's own network meets the gateway here. It answers DNS, keeps the threat lists below current, and records what it blocks or flags on the Bot's timeline.")}
        action={
          status ? (
            <span className="flex items-center gap-1.5 text-label">
              <span aria-hidden className={`h-[7px] w-[7px] rounded-full ${off ? "bg-vermilion" : "bg-lamp"}`} />
              <span className={off ? "text-vermilion" : "text-emerald"}>{off ? t("Off") : t("Serving")}</span>
            </span>
          ) : null
        }
      >
        {err || status?.problem ? (
          <Block className="grid gap-1 text-small leading-5 text-vermilion">
            {err ? <p>{err}</p> : null}
            {status?.problem ? <p>{status.problem}</p> : null}
          </Block>
        ) : null}
        <Facts>
          <Fact label={t("Bots on their own network")}>{status?.networks ?? 0}</Fact>
          <Fact label={t("Last word from the gateway")}>{status?.healthyAt || t("never")}</Fact>
          <Fact label={t("DNS servers")}>
            {status?.dnsServers.length ? (
              <>
                <span className="font-mono text-label break-all">{status.dnsServers.join(", ")}</span>
                <span className="ml-1.5 text-ink-3">{status.dnsSource === "settings" ? t("(set above)") : t("(the system's)")}</span>
              </>
            ) : (
              <span className="text-vermilion">{t("none found; set them above")}</span>
            )}
          </Fact>
        </Facts>
        <SettingRow
          label={t("Gateway certificate")}
          hint={t("Rotating makes a new certificate for the gateway, which signs with it at once. A running machine or connector sidecar still trusts the old one, so HTTPS through the gateway fails there until its next start, which recreates it with the new certificate (files, drives and caches are kept).")}
        >
          <div className="flex w-full flex-col gap-2 @2xl/page:items-end">
            {status?.caFingerprint ? (
              <span className="flex min-w-0 items-center gap-1">
                <span className="min-w-0 font-mono text-label leading-4 break-all text-ink-2">{status.caFingerprint}</span>
                <CopyButton text={status.caFingerprint} title={t("Copy")} />
              </span>
            ) : (
              <span className="text-small text-ink-3">{t("made at the first start")}</span>
            )}
            <SaveButton kind="secondary" size="sm" state={saver.state} onClick={() => void rotate()}>
              {t("Rotate certificate")}
            </SaveButton>
          </div>
        </SettingRow>
      </Group>

      <Group title={t("Threat lists")} note={t("What the gateway blocks by reputation, and when each list was last loaded.")}>
        {feeds.length === 0 ? (
          <Empty>{t("No threat lists are loaded.")}</Empty>
        ) : (
          feeds.map((f) => (
            <div key={f.id} className="grid grid-cols-[minmax(0,1fr)_auto] items-baseline gap-x-6 gap-y-0.5 px-5 py-3 text-small leading-5 @2xl/page:grid-cols-[minmax(0,1fr)_7rem_11rem]">
              <span className="min-w-0">
                <span className="font-mono break-all text-ink">{f.id}</span>
                {f.error ? <span className="mt-0.5 block text-meta leading-[18px] text-vermilion">{f.error}</span> : null}
              </span>
              <span className="text-right font-mono text-label text-ink-2 tabular-nums">{t("{{count}} entries", { count: f.entries })}</span>
              <span className={`@max-2xl/page:col-span-2 @2xl/page:text-right ${f.updatedAt ? "text-ink-2" : "text-ink-3"}`}>{f.updatedAt || t("never")}</span>
            </div>
          ))
        )}
      </Group>
    </>
  );
}
