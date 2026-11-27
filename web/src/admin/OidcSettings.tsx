import { useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { CopyButton } from "../Feedback";
import { SettingRow } from "../Field";
import { FieldGroup } from "./FieldGroup";
import { Block } from "./kit";
import { OIDC_NOTE, groupOf } from "./fields";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

// The OIDC provider's settings, the callback address it has to be told, and a
// button that asks the provider whether the saved settings reach it.
export function OidcSettings() {
  const { fields, oidcRedirectUrl, dirty } = useSettingsForm();
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<{ ok: boolean; text: string } | null>(null);

  async function check() {
    setBusy(true);
    setResult(null);
    try {
      const r = await ui.checkOIDC({});
      setResult({ ok: true, text: t("The provider answered as {{issuer}}.", { issuer: r.issuer }) });
    } catch (e) {
      setResult({ ok: false, text: fail(e) });
    } finally {
      setBusy(false);
    }
  }
  return (
    <FieldGroup
      title={t("OIDC")}
      note={t(OIDC_NOTE)}
      rows={fields.filter((f) => groupOf(f.key) === "oidc")}
      action={
        <Btn size="sm" type="button" disabled={busy} onClick={() => void check()}>
          {busy ? t("Checking…") : t("Check")}
        </Btn>
      }
    >
      <SettingRow label={t("Redirect URI")} hint={t("Register this at the provider. It follows the Public URL under Server.")}>
        <div className="flex w-full items-center gap-1 rounded-control bg-well py-1 pr-1 pl-3">
          <code className="min-w-0 flex-1 font-mono text-label leading-5 break-all text-ink">{oidcRedirectUrl}</code>
          <CopyButton text={oidcRedirectUrl} title={t("Copy the redirect URI")} className="shrink-0" />
        </div>
      </SettingRow>
      {result ? (
        <Block>
          <p role="status" className={`text-small leading-5 ${result.ok ? "text-ink-2" : "text-vermilion"}`}>
            {result.text}
          </p>
        </Block>
      ) : dirty > 0 ? (
        <Block>
          <p className="text-meta leading-[18px] text-ink-3">{t("Check asks the provider with the saved settings. Save first.")}</p>
        </Block>
      ) : null}
    </FieldGroup>
  );
}
