import { useState } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { CopyButton } from "../Feedback";
import { FieldGroup } from "./FieldGroup";
import { OIDC_NOTE, groupOf } from "./fields";
import { useSettingsForm } from "./useAdminSettings";

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
      setResult({ ok: true, text: `The provider answered as ${r.issuer}.` });
    } catch (e) {
      setResult({ ok: false, text: fail(e) });
    } finally {
      setBusy(false);
    }
  }
  return (
    <FieldGroup
      title="OIDC"
      note={OIDC_NOTE}
      rows={fields.filter((f) => groupOf(f.key) === "oidc")}
      action={
        <Btn size="sm" type="button" disabled={busy} onClick={() => void check()}>
          {busy ? "Checking…" : "Check"}
        </Btn>
      }
    >
      <div className="px-5 py-3.5 shadow-[inset_0_1px_0_var(--color-line-strong)]">
        <div className="mb-1.5 text-[12px] font-medium text-ink-3">Redirect URI</div>
        <div className="flex items-center gap-2 rounded-sm bg-well py-1 pl-3 pr-1">
          <code className="min-w-0 flex-1 break-all font-mono text-[12.5px] leading-5 text-ink">{oidcRedirectUrl}</code>
          <CopyButton text={oidcRedirectUrl} title="Copy the redirect URI" className="shrink-0" />
        </div>
        <p className="mt-1.5 text-[12.5px] leading-[18px] text-ink-3">Register this at the provider. It follows the Public URL under Server.</p>
        {result ? (
          <p role="status" className={`mt-3 text-[13px] leading-5 ${result.ok ? "text-ink-2" : "text-vermilion"}`}>
            {result.text}
          </p>
        ) : dirty > 0 ? (
          <p className="mt-3 text-[12.5px] leading-[18px] text-ink-3">Check asks the provider with the saved settings. Save first.</p>
        ) : null}
      </div>
    </FieldGroup>
  );
}
