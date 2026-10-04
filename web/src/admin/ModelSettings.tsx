import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Btn } from "../Btn";
import { Field, Panel, inputClass } from "../Field";
import { Select } from "../Select";
import { ConfigSource, type ConfigField, type ModelOption } from "../gen/silo/v1/ui_pb";
import { SourceChips } from "./FieldRow";
import { HINTS, PLACEHOLDERS } from "./fields";

export function ModelSettings({
  models,
  defaultModel,
  titleModel,
  approvalModel,
  defaultField,
  titleField,
  approvalField,
  onDefault,
  onTitle,
  onApproval,
  subagentModel,
  subagentField,
  onSubagent,
  memoryModel,
  memoryField,
  onMemory,
  embedModel,
  embedField,
  onEmbed,
  voiceModel,
  voiceField,
  onVoice,
  onSaveModels,
}: {
  models: ModelOption[];
  defaultModel: string;
  titleModel: string;
  approvalModel: string;
  defaultField?: ConfigField;
  titleField?: ConfigField;
  approvalField?: ConfigField;
  onDefault: (v: string) => void;
  onTitle: (v: string) => void;
  onApproval: (v: string) => void;
  subagentModel: string;
  subagentField?: ConfigField;
  onSubagent: (v: string) => void;
  memoryModel: string;
  memoryField?: ConfigField;
  onMemory: (v: string) => void;
  embedModel: string;
  embedField?: ConfigField;
  onEmbed: (v: string) => void;
  voiceModel: string;
  voiceField?: ConfigField;
  onVoice: (v: string) => void;
  onSaveModels: (list: string[]) => Promise<void>;
}) {
  const [provider, setProvider] = useState("openrouter");
  const [name, setName] = useState("");

  const allowed = models.map((m) => m.id);
  const lockedDefault = defaultField?.source === ConfigSource.ENV;
  const lockedTitle = titleField?.source === ConfigSource.ENV;
  const lockedApproval = approvalField?.source === ConfigSource.ENV;

  function add() {
    const id = `${provider}/${name.trim()}`.replace(/\/+$/, "");
    if (!name.trim() || allowed.includes(id)) return;
    const next = [...allowed, id];
    setName("");
    void onSaveModels(next);
  }

  function remove(id: string) {
    const next = allowed.filter((m) => m !== id);
    void onSaveModels(next);
  }

  const providerIDs = ["openrouter", "openai", "anthropic"];

  return (
    <Panel title="Models" note="Model ids are provider/model. The allowlist drives the chat model picker.">
      <div className="mb-5 grid gap-4 sm:grid-cols-2">
        <Field label="Default model">
          <Select
            value={defaultModel}
            onChange={onDefault}
            disabled={lockedDefault}
            placeholder={models.length ? "Select…" : "No models allowed yet"}
            emptyLabel="No models allowed yet"
            options={models.map((m) => ({ value: m.id, label: m.label || m.id, hint: m.label && m.label !== m.id ? m.id : undefined }))}
          />
        </Field>
        <Field label="Chat title model">
          <Select
            value={titleModel}
            onChange={onTitle}
            disabled={lockedTitle}
            placeholder="Same as default"
            emptyLabel="Same as default"
            options={[{ value: "", label: "Same as default" }, ...models.map((m) => ({ value: m.id, label: m.label || m.id }))]}
          />
        </Field>
        <Field label="Auto-approval model" hint="Decides rules set to Auto.">
          <Select
            value={approvalModel}
            onChange={onApproval}
            disabled={lockedApproval}
            placeholder="Same as title model"
            emptyLabel="Same as title model"
            options={[{ value: "", label: "Same as title model" }, ...models.map((m) => ({ value: m.id, label: m.label || m.id }))]}
          />
        </Field>
        <Field label="Subagent model" hint="Default for subagents a lead starts. A cheaper model keeps parallel work affordable; the lead can still pick another.">
          <Select
            value={subagentModel}
            onChange={onSubagent}
            disabled={subagentField?.source === ConfigSource.ENV}
            placeholder="Same as the lead"
            emptyLabel="Same as the lead"
            options={[{ value: "", label: "Same as the lead" }, ...models.map((m) => ({ value: m.id, label: m.label || m.id }))]}
          />
        </Field>
        <Field label="Memory save model" hint="Reads chats once they go quiet and saves the facts and lessons the Bot missed. A cheap model is enough.">
          <Select
            value={memoryModel}
            onChange={onMemory}
            disabled={memoryField?.source === ConfigSource.ENV}
            placeholder="Same as title model"
            emptyLabel="Same as title model"
            options={[{ value: "", label: "Same as title model" }, ...models.map((m) => ({ value: m.id, label: m.label || m.id }))]}
          />
        </Field>
        {/* Embedding models are not chat models, so this is free text, not the allowlist. */}
        <Field label="Embedding model" hint={HINTS.embedding_model} headerRight={embedField ? <SourceChips field={embedField} /> : undefined}>
          <input
            className={`${inputClass} font-mono text-[13px]`}
            autoComplete="off"
            placeholder={PLACEHOLDERS.embedding_model}
            value={embedModel}
            disabled={embedField?.source === ConfigSource.ENV}
            onChange={(e) => onEmbed(e.target.value)}
          />
        </Field>
        <Field label="Speech-to-text model" hint={HINTS.transcribe_model} headerRight={voiceField ? <SourceChips field={voiceField} /> : undefined}>
          <input
            className={`${inputClass} font-mono text-[13px]`}
            autoComplete="off"
            placeholder={PLACEHOLDERS.transcribe_model}
            value={voiceModel}
            disabled={voiceField?.source === ConfigSource.ENV}
            onChange={(e) => onVoice(e.target.value)}
          />
        </Field>
      </div>

      <div className="mb-1.5 text-[12px] font-medium text-ink-3">Allowed models</div>
      {models.length === 0 ? (
        <p className="mb-3 text-[13px] text-ink-2">No models allowed. Add one below.</p>
      ) : (
        <ul className="mb-3 divide-y divide-line-strong overflow-hidden rounded-card border border-line-strong">
          {models.map((m) => (
            <li key={m.id} className="flex items-center gap-2 px-3 py-2 text-[13px]">
              <span className="min-w-0 truncate font-mono text-ink">{m.id}</span>
              <span className="shrink-0 text-ink-3">{m.provider}</span>
              <span className="flex-1" />
              <button
                type="button"
                title="Remove"
                className="rounded-sm p-1 text-ink-2 transition-colors hover:bg-pressed hover:text-vermilion"
                onClick={() => remove(m.id)}
              >
                <Trash2 size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <div className="w-[150px]">
          <Select value={provider} onChange={setProvider} options={providerIDs.map((id) => ({ value: id, label: id }))} />
        </div>
        <span className="text-ink-3">/</span>
        <input
          className={`${inputClass} min-w-[180px] flex-1 font-mono text-[13px]`}
          placeholder="model name, e.g. gpt-5.6-luna"
          value={name}
          onChange={(e) => setName(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") {
              e.preventDefault();
              add();
            }
          }}
        />
        <Btn kind="primary" onClick={add}>
          <Plus size={14} /> Add
        </Btn>
      </div>
    </Panel>
  );
}
