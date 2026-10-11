import { ui } from "../api";
import { fail } from "../errors";
import { ArmedButton, SaveButton } from "../Feedback";
import { Field, inputClass, Panel, SkeletonRows } from "../Field";
import type { SecretsState } from "./useSecrets";
import { t } from "../i18n.ts";

// The Secrets tab: names and last use only; values go to the Bot after you
// allow it and are masked before the model sees output.
export function SecretsPane({ botId, state, onError }: { botId: string; state: SecretsState; onError: (message: string) => void }) {
  const { secrets, removed, reload, saver, name, setName, value, setValue } = state;
  return (
    <div className="silo-page">
      <h2 className="text-title">{t("Secrets")}</h2>
      <p className="mb-6 text-ink-2">{t("Handed to the Bot only after you allow it. Masked before the model sees output.")}</p>
      <Panel title={t("Stored secrets")} padded={false}>
        {secrets === null ? (
          <SkeletonRows rows={2} height={52} className="p-3" />
        ) : secrets.length === 0 ? (
          <p className="px-5 py-4 text-[13px] text-ink-3">{t("No secrets on this Bot yet.")}</p>
        ) : (
          secrets.map((s) => (
            <div key={s.id} className="flex items-center justify-between gap-3 px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
              <div className="min-w-0">
                <div className="truncate font-mono text-[13px] font-medium">{s.name}</div>
                <div className="font-mono text-[12px] text-ink-3">•••••••• · {s.lastUsedAt || t("never used")}</div>
              </div>
              <ArmedButton
                kind="ghost"
                size="sm"
                onConfirm={async () => {
                  await ui.deleteSecret({ botId, id: s.id });
                  removed(s.id);
                }}
              >
                {t("Delete")}
              </ArmedButton>
            </div>
          ))
        )}
      </Panel>
      <Panel title={t("Add a secret")} className="mt-4">
        <form
          className="flex flex-col gap-3 wide:flex-row wide:items-end"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await saver.run(() => ui.addSecret({ botId, name, value }));
              setName("");
              setValue("");
              await reload();
            } catch (ex) {
              onError(fail(ex));
            }
          }}
        >
          <Field label={t("Name")} className="flex-1">
            <input className={`${inputClass} font-mono`} placeholder={"vendor_password"} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label={t("Value")} className="flex-1">
            <input type="password" className={`${inputClass} font-mono`} placeholder="••••••••" value={value} onChange={(e) => setValue(e.target.value)} />
          </Field>
          <SaveButton type="submit" state={saver.state} savedLabel={t("Added")} disabled={!name.trim() || !value}>
            {t("Add")}
          </SaveButton>
        </form>
      </Panel>
    </div>
  );
}
