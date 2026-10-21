import { ui } from "../api";
import { fail } from "../errors";
import { ArmedButton, SaveButton } from "../Feedback";
import { Field, inputClass, Panel, SkeletonRows } from "../Field";
import { ToggleRow } from "../Switch";
import { TabPill, TabPills } from "../TabPills";
import { parseHosts, type SecretsState } from "./useSecrets";
import { t } from "../i18n.ts";

// The Secrets tab: names and last use only; values go to the Bot after you
// allow it and are masked before the model sees output. A surrogate secret is
// one the Bot may hold a stand-in for, with the real value sent only from the
// network filter to the hosts you list: in request headers, and in bodies too
// only when you allow it.
export function SecretsPane({ botId, state, onError }: { botId: string; state: SecretsState; onError: (message: string) => void }) {
  const {
    secrets, removed, reload, saver,
    name, setName, value, setValue, mode, setMode, hosts, setHosts, swapBody, setSwapBody, reset,
    editing, startEdit, stopEdit, editField,
  } = state;

  return (
    <div className="silo-page silo-page-wide">
      <h2 className="text-title">{t("Secrets")}</h2>
      <p className="mb-6 text-ink-2">{t("Handed to the Bot only after you allow it. Masked before the model sees output.")}</p>
      <Panel title={t("Stored secrets")} padded={false}>
        {secrets === null ? (
          <SkeletonRows rows={2} height={52} className="p-3" />
        ) : secrets.length === 0 ? (
          <p className="px-5 py-4 text-[13px] text-ink-3">{t("No secrets on this Bot yet.")}</p>
        ) : (
          secrets.map((s) => (
            <div key={s.id} className="px-5 py-3 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
              {editing?.id === s.id ? (
                <div className="flex flex-col gap-3">
                  <div className="flex flex-wrap items-end gap-3">
                    <Field label={t("Mode")} className="w-fit">
                      <TabPills>
                        <TabPill on={editing.mode === "surrogate"} onClick={() => editField({ mode: "surrogate" })}>
                          {t("Surrogate")}
                        </TabPill>
                        <TabPill on={editing.mode === "plain"} onClick={() => editField({ mode: "plain" })}>
                          {t("Plain")}
                        </TabPill>
                      </TabPills>
                    </Field>
                    {editing.mode === "surrogate" ? (
                      <Field label={t("Allowed hosts")} className="min-w-[240px] flex-1">
                        <input
                          className={`${inputClass} font-mono`}
                          placeholder={"api.github.com, *.githubusercontent.com"}
                          value={editing.hosts}
                          onChange={(e) => editField({ hosts: e.target.value })}
                        />
                      </Field>
                    ) : null}
                    <Field label={t("New value")} className="min-w-[200px] flex-1">
                      <input
                        type="password"
                        className={`${inputClass} font-mono`}
                        placeholder={t("Leave blank to keep the current value")}
                        value={editing.value}
                        onChange={(e) => editField({ value: e.target.value })}
                      />
                    </Field>
                  </div>
                  {editing.mode === "surrogate" ? (
                    <BodySwap on={editing.swapBody} onChange={(v) => editField({ swapBody: v })} />
                  ) : null}
                  <div className="flex items-center gap-2">
                    <SaveButton
                      type="button"
                      state={saver.state}
                      savedLabel={t("Saved")}
                      disabled={editing.mode === "surrogate" && parseHosts(editing.hosts).length === 0}
                      onClick={async () => {
                        try {
                          await saver.run(() =>
                            ui.updateSecret({
                              botId,
                              id: editing.id,
                              value: editing.value || undefined,
                              mode: editing.mode,
                              hosts: parseHosts(editing.hosts),
                              swapBody: editing.swapBody,
                            }),
                          );
                          stopEdit();
                          await reload();
                        } catch (ex) {
                          onError(fail(ex));
                        }
                      }}
                    >
                      {t("Save")}
                    </SaveButton>
                    <button type="button" className="text-[13px] text-ink-3 hover:text-ink" onClick={stopEdit}>
                      {t("Cancel")}
                    </button>
                  </div>
                </div>
              ) : (
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <div className="truncate font-mono text-[13px] font-medium">{s.name}</div>
                    <div className="truncate text-[12px] text-ink-3">
                      {s.mode === "surrogate" ? (
                        <span title={t("A stand-in the Bot holds; the network filter swaps in the real value for these hosts.")}>
                          {t("Surrogate")}
                          {s.hosts.length > 0 ? ` · ${s.hosts.join(", ")}` : ""}
                          {s.swapBody ? ` · ${t("also in bodies")}` : ""}
                        </span>
                      ) : (
                        <span title={t("The Bot holds the real value. For keys and passwords nothing can stand in for it.")}>{t("Plain")}</span>
                      )}
                      {` · ${s.lastUsedAt || t("never used")}`}
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-1">
                    <button
                      type="button"
                      className="rounded-md px-2.5 py-1 text-[12.5px] font-medium text-ink-3 hover:bg-pressed hover:text-ink"
                      onClick={() => startEdit({ id: s.id, name: s.name, mode: s.mode || "plain", hosts: s.hosts, swapBody: s.swapBody })}
                    >
                      {t("Edit")}
                    </button>
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
                </div>
              )}
            </div>
          ))
        )}
      </Panel>
      <Panel title={t("Add a secret")} className="mt-4">
        <form
          className="flex flex-col gap-4"
          onSubmit={async (e) => {
            e.preventDefault();
            try {
              await saver.run(() => ui.addSecret({ botId, name, value, mode, hosts: parseHosts(hosts), swapBody: mode === "surrogate" && swapBody }));
              reset();
              await reload();
            } catch (ex) {
              onError(fail(ex));
            }
          }}
        >
          <div className="flex flex-col gap-3 wide:flex-row wide:items-end">
            <Field label={t("Name")} className="flex-1">
              <input className={`${inputClass} font-mono`} placeholder={"vendor_password"} value={name} onChange={(e) => setName(e.target.value)} />
            </Field>
            <Field label={t("Value")} className="flex-1">
              <input type="password" className={`${inputClass} font-mono`} placeholder="••••••••" value={value} onChange={(e) => setValue(e.target.value)} />
            </Field>
          </div>
          <div className="flex flex-col gap-3 wide:flex-row wide:items-end">
            <Field label={t("Mode")} className="w-fit">
              <TabPills>
                <TabPill on={mode === "surrogate"} onClick={() => setMode("surrogate")}>
                  {t("Surrogate")}
                </TabPill>
                <TabPill on={mode === "plain"} onClick={() => setMode("plain")}>
                  {t("Plain")}
                </TabPill>
              </TabPills>
            </Field>
            {mode === "surrogate" ? (
              <Field label={t("Allowed hosts")} className="flex-1">
                <input
                  className={`${inputClass} font-mono`}
                  placeholder={"api.github.com, *.githubusercontent.com"}
                  value={hosts}
                  onChange={(e) => setHosts(e.target.value)}
                />
              </Field>
            ) : (
              <p className="flex-1 pb-2 text-[12.5px] text-ink-3">
                {t("The Bot holds the real value. For keys and passwords nothing can stand in for it.")}
              </p>
            )}
          </div>
          {mode === "surrogate" ? <BodySwap on={swapBody} onChange={setSwapBody} /> : null}
          <div className="flex items-end justify-between gap-3">
            <p className="text-[12.5px] text-ink-3">
              {mode === "surrogate"
                ? t("A stand-in the Bot holds; the network filter swaps in the real value for these hosts.")
                : ""}
            </p>
            <SaveButton
              type="submit"
              state={saver.state}
              savedLabel={t("Added")}
              disabled={!name.trim() || !value || (mode === "surrogate" && parseHosts(hosts).length === 0)}
            >
              {t("Add")}
            </SaveButton>
          </div>
        </form>
      </Panel>
    </div>
  );
}

// The filter swaps the real value into request headers only, unless the owner
// allows bodies for this secret: a body to an allowed host can still publish
// what it carries (an issue comment, a gist).
function BodySwap({ on, onChange }: { on: boolean; onChange: (v: boolean) => void }) {
  return (
    <ToggleRow
      label={t("Also swap in request bodies")}
      hint={t("Off, the real value goes only in headers such as Authorization. Turn on for an API that wants the key in the body; a body can publish what it carries.")}
      on={on}
      onChange={onChange}
    />
  );
}
