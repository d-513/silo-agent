import { Plus, Trash2, TriangleAlert } from "lucide-react";
import { lazy, Suspense, useEffect, useRef, useState, type ReactNode } from "react";
import { ui } from "./api";
import { SaveButton, useSave, type SaveState } from "./Feedback";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import { Field, Panel, inputClass } from "./Field";
import { Select } from "./Select";
import { ToggleRow } from "./Switch";
import {
  ConfigSource,
  type AuditRow,
  type ConfigField,
  type ConnectorVar,
  type ModelOption,
  type Provider,
  type SearchEngine,
  type Settings,
} from "./gen/silo/v1/ui_pb";

const YamlEditor = lazy(() => import("./YamlEditor").then((m) => ({ default: m.YamlEditor })));

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

const LABELS: Record<string, string> = {
  model: "Default model",
  model_title: "Chat title model",
  model_approval: "Auto-approval model",
  embedding_model: "Embedding model",
  "memory.auto_recall": "Auto-recall",
  debug: "Debug logging",
  "search.engine": "Engine",
  http_addr: "Listen address",
  public_url: "Public URL",
  cp_url: "Control plane URL",
  data_dir: "Data directory",
  docker_host: "Docker host",
  bot_image: "Bot image",
  mcp_stdio_image: "STDIO MCP image",
  "bootstrap.email": "Email",
  "bootstrap.password": "Password",
};

const HINTS: Record<string, string> = {
  embedding_model: "For long-term memories. OpenAI-compatible, 1536-wide; switching models makes old memories match poorly.",
};

const PLACEHOLDERS: Record<string, string> = {
  embedding_model: "openrouter/openai/text-embedding-3-small",
};

const MEMORY_NOTE = "Auto-recall puts up to 3 long-term memories close to the opening message into each run. The embedding model is under Models.";

const BOOTSTRAP_NOTE = "First admin only. Ignored after a user exists. Restart required.";

function groupOf(key: string) {
  if (key === "model" || key === "model_title" || key === "model_approval" || key === "embedding_model") return "models";
  if (key.startsWith("memory.")) return "memory";
  if (key.startsWith("providers.")) return "providers";
  if (key.startsWith("search.")) return "search";
  if (key.startsWith("bootstrap.")) return "bootstrap";
  return "server";
}

function labelOf(key: string, engines: SearchEngine[], providers: Provider[]) {
  if (LABELS[key]) return LABELS[key];
  const p = /^providers\.(.+)\.(.+)$/.exec(key);
  if (p) {
    const prov = providers.find((x) => x.id === p[1]);
    const f = prov?.fields.find((x) => x.key === p[2]);
    if (f?.label) return f.label;
  }
  const m = /^search\.(.+)\.(.+)$/.exec(key);
  if (m) {
    const eng = engines.find((e) => e.id === m[1]);
    const f = eng?.fields.find((x) => x.key === m[2]);
    if (f?.label) return f.label;
  }
  return key;
}

function sourceWord(s: ConfigSource) {
  if (s === ConfigSource.ENV) return "env";
  if (s === ConfigSource.YAML) return "yaml";
  return "default";
}

function applySettings(
  x: Settings,
  setFields: (f: ConfigField[]) => void,
  setValues: (v: Record<string, string>) => void,
  setYaml: (s: string) => void,
  setYamlPath: (s: string) => void,
  setEngines: (e: SearchEngine[]) => void,
  setProviders: (p: Provider[]) => void,
  setModels: (m: ModelOption[]) => void,
  setConnVars: (v: ConnectorVar[]) => void,
) {
  setFields(x.fields);
  setValues(Object.fromEntries(x.fields.map((f) => [f.key, f.value])));
  setYaml(x.yaml);
  setYamlPath(x.yamlPath);
  setEngines(x.searchEngines);
  setProviders(x.providers);
  setModels(x.models);
  setConnVars(x.connectorVars);
}

export function AdminSettings() {
  const [fields, setFields] = useState<ConfigField[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [engines, setEngines] = useState<SearchEngine[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [connVars, setConnVars] = useState<ConnectorVar[]>([]);
  const [yamlText, setYamlText] = useState("");
  const [yamlPath, setYamlPath] = useState("");
  const [audit, setAudit] = useState<AuditRow[]>([]);
  const formSaver = useSave();
  const yamlSaver = useSave();
  const [err, setErr] = useState("");

  const apply = (x: Settings) => applySettings(x, setFields, setValues, setYamlText, setYamlPath, setEngines, setProviders, setModels, setConnVars);

  useEffect(() => {
    ui.getSettings({}).then(apply).catch((e) => setErr(fail(e)));
    ui.listAudit({})
      .then((x) => setAudit(x.rows))
      .catch((e) => setErr(fail(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function setValue(key: string, v: string) {
    setValues((prev) => ({ ...prev, [key]: v }));
  }

  // patch is every form field that differs from what the server holds.
  const patch: Record<string, string> = {};
  for (const f of fields) {
    if (f.source === ConfigSource.ENV) continue;
    const v = values[f.key] ?? "";
    if (v !== f.value) patch[f.key] = v;
  }
  const dirty = Object.keys(patch).length;

  function discard() {
    setErr("");
    setValues(Object.fromEntries(fields.map((f) => [f.key, f.value])));
  }

  async function saveForm() {
    setErr("");
    if (dirty === 0) return;
    try {
      apply(await formSaver.run(() => ui.putSettings({ fields: patch })));
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  // ⌘/Ctrl+S saves the form; the ref keeps the listener on the latest patch.
  const saveRef = useRef(saveForm);
  saveRef.current = saveForm;
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "s") {
        e.preventDefault();
        void saveRef.current();
      }
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  async function saveYaml() {
    setErr("");
    try {
      apply(await yamlSaver.run(() => ui.putSettings({ yaml: yamlText })));
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveModels(list: string[]) {
    setErr("");
    try {
      apply(await ui.setModels({ models: list }));
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveConnVars(list: { name: string; value: string }[]) {
    setErr("");
    try {
      apply(await ui.setConnectorVars({ connectorVars: list.map((v) => ({ name: v.name, value: v.value, source: ConfigSource.DEFAULT, envName: "" })) }));
    } catch (ex) {
      setErr(fail(ex));
      throw ex;
    }
  }

  const envFields = fields.filter((f) => f.source === ConfigSource.ENV);
  const rowsIn = (id: string) => fields.filter((f) => groupOf(f.key) === id);
  const defaultModel = values["model"] ?? "";
  const titleModel = values["model_title"] ?? "";
  const approvalModel = values["model_approval"] ?? "";
  const embedModel = values["embedding_model"] ?? "";

  return (
    <div>
      <div className="grid gap-5">
        <ModelSettings
          models={models}
          defaultModel={defaultModel}
          titleModel={titleModel}
          approvalModel={approvalModel}
          defaultField={fields.find((f) => f.key === "model")}
          titleField={fields.find((f) => f.key === "model_title")}
          approvalField={fields.find((f) => f.key === "model_approval")}
          embedModel={embedModel}
          embedField={fields.find((f) => f.key === "embedding_model")}
          onEmbed={(v) => setValue("embedding_model", v)}
          onDefault={(v) => setValue("model", v)}
          onTitle={(v) => setValue("model_title", v)}
          onApproval={(v) => setValue("model_approval", v)}
          onSaveModels={saveModels}
        />
        <ConnectorVarsPanel vars={connVars} onSave={saveConnVars} />
        {providers.map((p) => (
          <ProviderBlock
            key={p.id}
            provider={p}
            rows={rowsIn("providers").filter((f) => f.key.startsWith(`providers.${p.id}.`))}
            values={values}
            engines={engines}
            providers={providers}
            onChange={setValue}
          />
        ))}
        <FieldGroup title="Memory" note={MEMORY_NOTE} rows={rowsIn("memory")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Search" rows={rowsIn("search")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Server" rows={rowsIn("server")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Bootstrap" note={BOOTSTRAP_NOTE} rows={rowsIn("bootstrap")} values={values} engines={engines} providers={providers} onChange={setValue} />
      </div>

      <SaveBar dirty={dirty} state={formSaver.state} error={err} onSave={() => void saveForm()} onDiscard={discard} />

      <h2 className="mt-10 mb-3 text-[22px] leading-7 font-medium tracking-[-0.015em]">silo.yaml</h2>
      <p className="mb-2 font-mono text-[12px] text-ink-3">{yamlPath || "silo.yaml"}</p>
      {envFields.length > 0 ? (
        <p className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
          <TriangleAlert className="mt-0.5 shrink-0" size={16} />
          <span>Overridden by env and will not apply until unset: {envFields.map((f) => f.envName).join(", ")}</span>
        </p>
      ) : null}
      <Suspense fallback={<div className="h-80 rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface" />}>
        <YamlEditor
          value={yamlText}
          onChange={(v) => setYamlText(v)}
        />
      </Suspense>
      <div className="mt-3">
        <SaveButton state={yamlSaver.state} onClick={() => void saveYaml()}>
          Save YAML
        </SaveButton>
      </div>

      <h2 className="mt-10 mb-3 text-[22px] leading-7 font-medium tracking-[-0.015em]">Audit</h2>
      {audit.length === 0 ? (
        <p className="text-ink-2">No decisions yet.</p>
      ) : (
        <div className="silo-scroll-x">
          <table className="w-full min-w-[36rem] text-left text-[13px]">
            <thead className="bg-well text-ink-3">
              <tr>
                <th className="p-2">When</th>
                <th className="p-2">Bot</th>
                <th className="p-2">Actor</th>
                <th className="p-2">Action</th>
                <th className="p-2">Decision</th>
              </tr>
            </thead>
            <tbody>
              {audit.map((r) => (
                <tr key={r.id} className="border-b border-line-strong">
                  <td className="p-2">{r.at}</td>
                  <td className="flex items-center gap-2 p-2">
                    <Crest index={r.crest} size={20} />
                    {r.botName}
                  </td>
                  <td className="p-2">{r.actor}</td>
                  <td className="p-2 font-mono">{r.action}</td>
                  <td className="p-2">{r.decision}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

const SAVE_KEY = typeof navigator !== "undefined" && /Mac|iP(hone|ad)/.test(navigator.platform) ? "⌘S" : "Ctrl+S";

// SaveBar pins the form's Save to the bottom of the viewport while the form is
// in view, and settles under the last panel once the page scrolls past it. It
// is the only save for the panels above; the model list and connector
// variables save on their own.
function SaveBar({
  dirty,
  state,
  error,
  onSave,
  onDiscard,
}: {
  dirty: number;
  state: SaveState;
  error: string;
  onSave: () => void;
  onDiscard: () => void;
}) {
  const live = dirty > 0;
  return (
    <div className="sticky bottom-3 z-20 mt-5">
      <div
        className={`flex items-center gap-3 rounded-card bg-surface px-4 py-2.5 transition-shadow duration-[160ms] ease-quiet max-wide:flex-wrap ${live ? "shadow-float-focus" : "shadow-float"}`}
      >
        <span className={`size-2 shrink-0 rounded-full ${error ? "bg-vermilion" : live ? "bg-cobalt" : "bg-line-strong"}`} />
        <p className={`min-w-0 flex-1 text-[13px] ${error ? "text-vermilion" : live ? "text-ink" : "text-ink-3"}`}>
          {error
            ? error
            : live
              ? `${dirty} unsaved ${dirty === 1 ? "change" : "changes"}`
              : state === "saved"
                ? "Saved to silo.yaml"
                : "No unsaved changes"}
        </p>
        <span className="font-mono text-[11px] text-ink-3 max-wide:hidden">{SAVE_KEY}</span>
        {live ? (
          <Btn kind="ghost" size="sm" onClick={onDiscard} disabled={state === "saving"}>
            Discard
          </Btn>
        ) : null}
        <SaveButton size="sm" state={state} disabled={!live && state !== "saved"} onClick={onSave}>
          Save changes
        </SaveButton>
      </div>
    </div>
  );
}

function FieldGroup({
  title,
  note,
  rows,
  values,
  engines,
  providers,
  onChange,
}: {
  title: string;
  note?: string;
  rows: ConfigField[];
  values: Record<string, string>;
  engines: SearchEngine[];
  providers: Provider[];
  onChange: (key: string, v: string) => void;
}) {
  if (rows.length === 0) return null;
  return (
    <Panel title={title} note={note} padded={false}>
      <div className="divide-y divide-line-strong">
        {rows.map((f) => (
          <div key={f.key} className="px-4 py-3">
            <FieldRow
              field={f}
              value={values[f.key] ?? ""}
              engines={engines}
              providers={providers}
              onChange={(v) => onChange(f.key, v)}
            />
          </div>
        ))}
      </div>
    </Panel>
  );
}

function ProviderBlock({
  provider,
  rows,
  values,
  engines,
  providers,
  onChange,
}: {
  provider: Provider;
  rows: ConfigField[];
  values: Record<string, string>;
  engines: SearchEngine[];
  providers: Provider[];
  onChange: (key: string, v: string) => void;
}) {
  if (rows.length === 0) return null;
  return (
    <Panel title={provider.name} note={provider.description || undefined} padded={false}>
      <div className="divide-y divide-line-strong">
        {rows.map((f) => (
          <div key={f.key} className="px-4 py-3">
            <FieldRow
              field={f}
              value={values[f.key] ?? ""}
              engines={engines}
              providers={providers}
              onChange={(v) => onChange(f.key, v)}
            />
          </div>
        ))}
      </div>
    </Panel>
  );
}

function ModelSettings({
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
  embedModel,
  embedField,
  onEmbed,
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
  embedModel: string;
  embedField?: ConfigField;
  onEmbed: (v: string) => void;
  onSaveModels: (list: string[]) => Promise<void>;
}) {
  const [draft, setDraft] = useState(models.map((m) => m.id).join("\n"));
  const [provider, setProvider] = useState("openrouter");
  const [name, setName] = useState("");
  useEffect(() => {
    setDraft(models.map((m) => m.id).join("\n"));
  }, [models]);

  const allowed = models.map((m) => m.id);
  const lockedDefault = defaultField?.source === ConfigSource.ENV;
  const lockedTitle = titleField?.source === ConfigSource.ENV;
  const lockedApproval = approvalField?.source === ConfigSource.ENV;

  function add() {
    const id = `${provider}/${name.trim()}`.replace(/\/+$/, "");
    if (!name.trim() || allowed.includes(id)) return;
    const next = [...allowed, id];
    setName("");
    setDraft(next.join("\n"));
    void onSaveModels(next);
  }

  function remove(id: string) {
    const next = allowed.filter((m) => m !== id);
    setDraft(next.join("\n"));
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

type VarDraft = { name: string; value: string; source: ConfigSource; envName: string };

function ConnectorVarsPanel({
  vars,
  onSave,
}: {
  vars: ConnectorVar[];
  onSave: (list: { name: string; value: string }[]) => Promise<void>;
}) {
  const [draft, setDraft] = useState<VarDraft[]>([]);
  const varSaver = useSave();
  useEffect(() => {
    setDraft(vars.map((v) => ({ name: v.name, value: v.value, source: v.source, envName: v.envName })));
  }, [vars]);

  function update(i: number, patch: Partial<VarDraft>) {
    setDraft((prev) => prev.map((r, j) => (j === i ? { ...r, ...patch } : r)));
  }

  function save() {
    const out = draft
      .filter((r) => r.source !== ConfigSource.ENV)
      .map((r) => ({ name: r.name.trim(), value: r.value }))
      .filter((r) => r.name);
    void varSaver.run(() => onSave(out)).catch(() => {});
  }

  return (
    <Panel
      title="Connector variables"
      note="Use these in a connector URL, headers, OAuth fields, command, arguments, or env values as ${NAME}."
    >
      <p className="mb-4 flex items-start gap-2 rounded-sm border-l-4 border-vermilion bg-well px-3 py-2 text-[13px] text-ink">
        <TriangleAlert className="mt-0.5 shrink-0 text-vermilion" size={16} />
        <span>
          These are variables, not secrets. Values are stored in plain text in <span className="font-mono">silo.yaml</span> and are not
          protected — anyone who can read the config can see them. Use a Bot Secret for credentials.
        </span>
      </p>
      {draft.length === 0 ? (
        <p className="mb-3 text-[13px] text-ink-2">No variables yet.</p>
      ) : (
        <div className="mb-3 grid gap-2">
          {draft.map((r, i) => {
            const locked = r.source === ConfigSource.ENV;
            return (
              <div key={i} className="flex flex-wrap items-center gap-2">
                <input
                  className={`${inputClass} w-48 font-mono text-[13px]`}
                  placeholder="NAME"
                  value={r.name}
                  disabled={locked}
                  onChange={(e) => update(i, { name: e.target.value })}
                />
                <span className="text-ink-3">=</span>
                <input
                  className={`${inputClass} min-w-[160px] flex-1 font-mono text-[13px]`}
                  placeholder="value"
                  value={r.value}
                  disabled={locked}
                  onChange={(e) => update(i, { value: e.target.value })}
                />
                {locked ? (
                  <span className="inline-flex items-center gap-1 text-[11px] text-vermilion" title={r.envName}>
                    <TriangleAlert size={14} />
                    {r.envName}
                  </span>
                ) : (
                  <button
                    type="button"
                    title="Remove"
                    className="rounded-sm p-1 text-ink-2 transition-colors hover:bg-pressed hover:text-vermilion"
                    onClick={() => setDraft((prev) => prev.filter((_, j) => j !== i))}
                  >
                    <Trash2 size={14} />
                  </button>
                )}
              </div>
            );
          })}
        </div>
      )}
      <div className="flex items-center gap-3">
        <Btn onClick={() => setDraft((prev) => [...prev, { name: "", value: "", source: ConfigSource.DEFAULT, envName: "" }])}>
          <Plus size={14} /> Add variable
        </Btn>
        <SaveButton state={varSaver.state} onClick={save}>
          Save variables
        </SaveButton>
      </div>
    </Panel>
  );
}

function SourceChips({ field }: { field: ConfigField }) {
  return (
    <>
      <span className="font-mono text-[11px] text-ink-3">{sourceWord(field.source)}</span>
      {field.restartRequired ? <span className="text-[11px] text-ink-3">restart</span> : null}
      {field.source === ConfigSource.ENV ? (
        <span className="inline-flex items-center gap-1 text-[11px] text-vermilion" title={field.envName}>
          <TriangleAlert size={14} />
          {field.envName}
        </span>
      ) : null}
    </>
  );
}

function FieldRow({
  field,
  value,
  engines,
  providers,
  onChange,
}: {
  field: ConfigField;
  value: string;
  engines: SearchEngine[];
  providers: Provider[];
  onChange: (v: string) => void;
}) {
  const locked = field.source === ConfigSource.ENV;
  const label = labelOf(field.key, engines, providers);

  if (field.type === "bool") {
    return (
      <ToggleRow
        label={label}
        meta={<SourceChips field={field} />}
        on={value === "true"}
        disabled={locked}
        onChange={(v) => onChange(v ? "true" : "false")}
      />
    );
  }

  let control: ReactNode;
  if (field.key === "search.engine") {
    control = (
      <Select
        value={value}
        onChange={onChange}
        disabled={locked}
        emptyLabel="No engines"
        options={engines.map((e) => ({ value: e.id, label: e.name }))}
      />
    );
  } else if (field.type === "select") {
    const m = /^providers\.(.+)\.cache_ttl$/.exec(field.key);
    const ttls = (m && providers.find((p) => p.id === m[1])?.cacheTtls) || [];
    control = <Select value={value} onChange={onChange} disabled={locked} options={ttls.map((t) => ({ value: t, label: t }))} />;
  } else {
    control = (
      <input
        className={inputClass}
        type={field.secret ? "password" : "text"}
        autoComplete="off"
        placeholder={PLACEHOLDERS[field.key]}
        value={value}
        disabled={locked}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <Field label={label} hint={HINTS[field.key]} headerRight={<SourceChips field={field} />}>
      {control}
    </Field>
  );
}
