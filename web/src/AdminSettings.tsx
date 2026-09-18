import { Plus, Trash, Warning } from "@phosphor-icons/react";
import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import { Field, Panel, inputClass } from "./Field";
import { Select } from "./Select";
import { ToggleRow } from "./Switch";
import {
  ConfigSource,
  type AuditRow,
  type ConfigField,
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

const BOOTSTRAP_NOTE = "First admin only. Ignored after a user exists. Restart required.";

function groupOf(key: string) {
  if (key === "model" || key === "model_title") return "models";
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
) {
  setFields(x.fields);
  setValues(Object.fromEntries(x.fields.map((f) => [f.key, f.value])));
  setYaml(x.yaml);
  setYamlPath(x.yamlPath);
  setEngines(x.searchEngines);
  setProviders(x.providers);
  setModels(x.models);
}

export function AdminSettings() {
  const [fields, setFields] = useState<ConfigField[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [engines, setEngines] = useState<SearchEngine[]>([]);
  const [providers, setProviders] = useState<Provider[]>([]);
  const [models, setModels] = useState<ModelOption[]>([]);
  const [yamlText, setYamlText] = useState("");
  const [yamlPath, setYamlPath] = useState("");
  const [audit, setAudit] = useState<AuditRow[]>([]);
  const [saved, setSaved] = useState("");
  const [err, setErr] = useState("");

  const apply = (x: Settings) => applySettings(x, setFields, setValues, setYamlText, setYamlPath, setEngines, setProviders, setModels);

  useEffect(() => {
    ui.getSettings({}).then(apply).catch((e) => setErr(fail(e)));
    ui.listAudit({})
      .then((x) => setAudit(x.rows))
      .catch((e) => setErr(fail(e)));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  function setValue(key: string, v: string) {
    setValues((prev) => ({ ...prev, [key]: v }));
    setSaved("");
  }

  async function saveForm() {
    setErr("");
    const patch: Record<string, string> = {};
    for (const f of fields) {
      if (f.source === ConfigSource.ENV) continue;
      const v = values[f.key] ?? "";
      if (v !== f.value) patch[f.key] = v;
    }
    try {
      apply(await ui.putSettings({ fields: patch }));
      setSaved("form");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveYaml() {
    setErr("");
    try {
      apply(await ui.putSettings({ yaml: yamlText }));
      setSaved("yaml");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveModels(list: string[]) {
    setErr("");
    try {
      apply(await ui.setModels({ models: list }));
      setSaved("models");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  const envFields = fields.filter((f) => f.source === ConfigSource.ENV);
  const rowsIn = (id: string) => fields.filter((f) => groupOf(f.key) === id);
  const defaultModel = values["model"] ?? "";
  const titleModel = values["model_title"] ?? "";

  return (
    <div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      <div className="grid gap-5">
        <ModelSettings
          models={models}
          defaultModel={defaultModel}
          titleModel={titleModel}
          defaultField={fields.find((f) => f.key === "model")}
          titleField={fields.find((f) => f.key === "model_title")}
          onDefault={(v) => setValue("model", v)}
          onTitle={(v) => setValue("model_title", v)}
          onSaveModels={saveModels}
        />
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
        <FieldGroup title="Search" rows={rowsIn("search")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Server" rows={rowsIn("server")} values={values} engines={engines} providers={providers} onChange={setValue} />
        <FieldGroup title="Bootstrap" note={BOOTSTRAP_NOTE} rows={rowsIn("bootstrap")} values={values} engines={engines} providers={providers} onChange={setValue} />
      </div>

      <div className="mt-6 flex items-center gap-3">
        <Btn kind="primary" onClick={() => void saveForm()}>
          Save changes
        </Btn>
        {saved === "form" && <span className="text-stone">Saved</span>}
        {saved === "models" && <span className="text-stone">Saved</span>}
      </div>

      <h2 className="mt-10 mb-3 text-[22px] font-medium">silo.yaml</h2>
      <p className="mb-2 font-mono text-[12px] text-stone">{yamlPath || "silo.yaml"}</p>
      {envFields.length > 0 ? (
        <p className="mb-3 flex items-start gap-2 text-[13px] text-carmine">
          <Warning className="mt-0.5 shrink-0" size={16} weight="fill" />
          <span>Overridden by env and will not apply until unset: {envFields.map((f) => f.envName).join(", ")}</span>
        </p>
      ) : null}
      <Suspense fallback={<div className="h-80 rounded-[6px] border border-thread bg-folio" />}>
        <YamlEditor
          value={yamlText}
          onChange={(v) => {
            setYamlText(v);
            setSaved("");
          }}
        />
      </Suspense>
      <div className="mt-3">
        <Btn kind="primary" onClick={() => void saveYaml()}>
          Save YAML
        </Btn>
        {saved === "yaml" && <span className="ml-3 text-stone">Saved</span>}
      </div>

      <h2 className="mt-10 mb-3 text-[22px] font-medium">Audit</h2>
      {audit.length === 0 ? (
        <p className="text-stone">No decisions yet.</p>
      ) : (
        <div className="silo-scroll-x">
          <table className="w-full min-w-[36rem] text-left text-[13px]">
            <thead className="bg-cloth text-stone">
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
                <tr key={r.id} className="border-b border-thread-2">
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
      <div className="divide-y divide-thread-2">
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
      <div className="divide-y divide-thread-2">
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
  defaultField,
  titleField,
  onDefault,
  onTitle,
  onSaveModels,
}: {
  models: ModelOption[];
  defaultModel: string;
  titleModel: string;
  defaultField?: ConfigField;
  titleField?: ConfigField;
  onDefault: (v: string) => void;
  onTitle: (v: string) => void;
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
      </div>

      <div className="mb-1.5 text-[12px] font-medium text-stone">Allowed models</div>
      {models.length === 0 ? (
        <p className="mb-3 text-[13px] text-stone">No models allowed. Add one below.</p>
      ) : (
        <ul className="mb-3 divide-y divide-thread-2 overflow-hidden rounded-[10px] border border-thread-2">
          {models.map((m) => (
            <li key={m.id} className="flex items-center gap-2 px-3 py-2 text-[13px]">
              <span className="min-w-0 truncate font-mono text-iron">{m.id}</span>
              <span className="shrink-0 text-stone">{m.provider}</span>
              <span className="flex-1" />
              <button
                type="button"
                title="Remove"
                className="rounded-[6px] p-1 text-stone transition-colors hover:bg-linen hover:text-carmine"
                onClick={() => remove(m.id)}
              >
                <Trash size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <div className="w-[150px]">
          <Select value={provider} onChange={setProvider} options={providerIDs.map((id) => ({ value: id, label: id }))} />
        </div>
        <span className="text-stone">/</span>
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
          <Plus size={14} weight="bold" /> Add
        </Btn>
      </div>
    </Panel>
  );
}

function SourceChips({ field }: { field: ConfigField }) {
  return (
    <>
      <span className="font-mono text-[11px] text-stone">{sourceWord(field.source)}</span>
      {field.restartRequired ? <span className="text-[11px] text-stone">restart</span> : null}
      {field.source === ConfigSource.ENV ? (
        <span className="inline-flex items-center gap-1 text-[11px] text-carmine" title={field.envName}>
          <Warning size={14} weight="fill" />
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
        value={value}
        disabled={locked}
        onChange={(e) => onChange(e.target.value)}
      />
    );
  }

  return (
    <Field label={label} headerRight={<SourceChips field={field} />}>
      {control}
    </Field>
  );
}
