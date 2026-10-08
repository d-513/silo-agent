import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Btn } from "../Btn";
import { Field, Panel, inputClass } from "../Field";
import { Spinner } from "../Feedback";
import { Select } from "../Select";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { modelOptions } from "../modelOptions";
import { SourceChips } from "./FieldRow";
import { HINTS, LABELS, PLACEHOLDERS } from "./fields";
import { useSettingsForm } from "./useAdminSettings";

// The roles a chat model can be given. All but the default may be left to
// follow another one.
const ROLES: { key: string; unset?: string; hint?: string }[] = [
  { key: "model" },
  { key: "model_title", unset: "Same as default" },
  { key: "model_approval", unset: "Same as title model", hint: "Decides rules set to Auto." },
  { key: "model_subagent", unset: "Same as the lead", hint: "Default for subagents a lead starts. A cheaper model keeps parallel work affordable; the lead can still pick another." },
  { key: "model_memory", unset: "Same as title model", hint: "Reads chats once they go quiet and saves the facts and lessons the Bot missed. A cheap model is enough." },
];

// Embedding and speech models are not chat models, so they are free text, not
// picked from the allowlist.
const FREE = ["embedding_model", "transcribe_model"];

export function ModelSettings() {
  const { fields, values, setValue, models } = useSettingsForm();
  const field = (key: string) => fields.find((f) => f.key === key);
  const picks = models.map((m) => ({ value: m.id, label: m.label || m.id }));

  return (
    <>
      <Panel title="Default models" note="Which allowed model does which job. A Bot or a chat can pick another for itself.">
        <div className="grid gap-x-4 gap-y-5 sm:grid-cols-2">
          {ROLES.map((r) => {
            const f = field(r.key);
            return (
              <Field key={r.key} label={LABELS[r.key]} hint={r.hint} headerRight={f ? <SourceChips field={f} /> : undefined}>
                <Select
                  value={values[r.key] ?? ""}
                  onChange={(v) => setValue(r.key, v)}
                  disabled={f?.source === ConfigSource.ENV}
                  placeholder={r.unset ?? (models.length ? "Select…" : "No models allowed yet")}
                  emptyLabel={r.unset ?? "No models allowed yet"}
                  options={r.unset ? [{ value: "", label: r.unset }, ...picks] : modelOptions(models)}
                />
              </Field>
            );
          })}
        </div>
      </Panel>

      <AllowedModels />

      <Panel title="Embedding and speech" note="Not chat models: type the id. Any OpenAI-compatible provider works.">
        <div className="grid gap-5">
          {FREE.map((key) => {
            const f = field(key);
            return (
              <Field key={key} label={LABELS[key]} hint={HINTS[key]} headerRight={f ? <SourceChips field={f} /> : undefined}>
                <input
                  className={`${inputClass} font-mono text-[13px]`}
                  autoComplete="off"
                  spellCheck={false}
                  placeholder={PLACEHOLDERS[key]}
                  value={values[key] ?? ""}
                  disabled={f?.source === ConfigSource.ENV}
                  onChange={(e) => setValue(key, e.target.value)}
                />
              </Field>
            );
          })}
        </div>
      </Panel>
    </>
  );
}

// The allowlist: what the pickers above and every chat's model menu offer. A
// change here saves at once, on its own.
function AllowedModels() {
  const { models, providers, setModel, busyModels } = useSettingsForm();
  const [provider, setProvider] = useState("");
  const [name, setName] = useState("");
  const pick = provider || providers[0]?.id || "";
  const id = `${pick}/${name.trim()}`.replace(/\/+$/, "");
  const known = models.some((m) => m.id === id);

  function add() {
    if (!pick || !name.trim() || known) return;
    setName("");
    void setModel(id, true);
  }

  return (
    <Panel
      title="Allowed models"
      note="The models a chat, a Bot and the pickers above can use. A change here is saved at once."
      padded={false}
    >
      {models.length === 0 ? (
        <p className="px-5 py-4 text-[13px] text-ink-2">No models allowed yet.</p>
      ) : (
        <ul className="divide-y divide-line">
          {models.map((m) => (
            <li key={m.id} className="flex h-11 items-center gap-3 pr-3 pl-5 text-[13px]">
              <span className="min-w-0 flex-1 truncate font-mono text-ink">{m.id}</span>
              {busyModels.has(m.id) ? (
                <span className="flex w-8 justify-center">
                  <Spinner size={13} />
                </span>
              ) : (
                <Btn kind="ghost" size="sm" iconOnly title={`Remove ${m.id}`} aria-label={`Remove ${m.id}`} className="hover:text-vermilion" onClick={() => void setModel(m.id, false)}>
                  <Trash2 size={14} />
                </Btn>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="bg-well px-5 py-4 shadow-[inset_0_1px_0_var(--color-line)]">
        <p className="mb-3 text-[12.5px] leading-[18px] text-ink-2">
          Pick from a provider's own list under{" "}
          <Link to="/admin/settings/$section" params={{ section: "providers" }} className="text-cobalt hover:text-cobalt-deep">
            Providers
          </Link>
          , or add a model by id.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <div className="w-[150px]">
            <Select value={pick} onChange={setProvider} ariaLabel="Provider" options={providers.map((p) => ({ value: p.id, label: p.id }))} />
          </div>
          <span className="text-ink-3">/</span>
          <input
            className={`${inputClass} min-w-[180px] flex-1 font-mono text-[13px]`}
            placeholder="model name, e.g. gpt-5.6-luna"
            aria-label="Model name"
            autoComplete="off"
            spellCheck={false}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
          <Btn kind="primary" onClick={add} disabled={!name.trim() || known}>
            <Plus size={14} /> Add
          </Btn>
        </div>
      </div>
    </Panel>
  );
}
