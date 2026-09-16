import { Plus, Trash, Warning } from "@phosphor-icons/react";
import { lazy, Suspense, useEffect, useState, type ReactNode } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
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

const GROUPS: { id: string; title: string; note?: string }[] = [
  { id: "models", title: "Models" },
  { id: "providers", title: "Providers" },
  { id: "search", title: "Search" },
  { id: "server", title: "Server" },
  { id: "bootstrap", title: "Bootstrap", note: "First admin only. Ignored after a user exists. Restart required." },
];

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
  const defaultModel = values["model"] ?? "";
  const titleModel = values["model_title"] ?? "";

  return (
    <div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {GROUPS.map((g) => {
        if (g.id === "models") {
          return (
            <section key={g.id} className="mb-8">
              <h2 className="mb-3 text-[22px] font-medium">{g.title}</h2>
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
              {saved === "models" && <span className="ml-3 text-stone">Saved</span>}
            </section>
          );
        }
        const rows = fields.filter((f) => groupOf(f.key) === g.id);
        if (rows.length === 0) return null;
        return (
          <section key={g.id} className="mb-8">
            <h2 className="mb-3 text-[22px] font-medium">{g.title}</h2>
            {g.note ? <p className="mb-3 text-[13px] text-stone">{g.note}</p> : null}
            {g.id === "providers"
              ? providers.map((p) => (
                  <ProviderBlock
                    key={p.id}
                    provider={p}
                    rows={rows.filter((f) => f.key.startsWith(`providers.${p.id}.`))}
                    values={values}
                    engines={engines}
                    providers={providers}
                    onChange={setValue}
                  />
                ))
              : rows.map((f) => (
                  <FieldRow
                    key={f.key}
                    field={f}
                    value={values[f.key] ?? ""}
                    engines={engines}
                    providers={providers}
                    onChange={(v) => setValue(f.key, v)}
                  />
                ))}
          </section>
        );
      })}
      <Btn kind="primary" onClick={() => void saveForm()}>
        Save
      </Btn>
      {saved === "form" && <span className="ml-3 text-stone">Saved</span>}

      <h2 className="mt-10 mb-3 text-[22px] font-medium">silo.yaml</h2>
      <p className="mb-2 font-mono text-[12px] text-stone">{yamlPath || "silo.yaml"}</p>
      {envFields.length > 0 ? (
        <p className="mb-3 flex items-start gap-2 text-[13px] text-carmine">
          <Warning className="mt-0.5 shrink-0" size={16} weight="fill" />
          <span>Overridden by env and will not apply until unset: {envFields.map((f) => f.envName).join(", ")}</span>
        </p>
      ) : null}
      <Suspense fallback={<div className="h-80 rounded border border-thread bg-folio" />}>
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
  return (
    <div className="mb-6 rounded-xl border border-thread-2 bg-folio p-4">
      <h3 className="mb-3 text-[15px] font-medium text-iron">{provider.name}</h3>
      {provider.description ? <p className="mb-3 text-[12px] text-stone">{provider.description}</p> : null}
      {rows.map((f) => (
        <FieldRow
          key={f.key}
          field={f}
          value={values[f.key] ?? ""}
          engines={engines}
          providers={providers}
          onChange={(v) => onChange(f.key, v)}
        />
      ))}
    </div>
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
    <div>
      <div className="mb-4 grid gap-4 sm:grid-cols-2">
        <label className="block">
          <div className="mb-1 text-[11px] font-medium tracking-wide text-stone">Default model</div>
          <select
            className="h-9 w-full rounded border border-thread bg-folio px-3 disabled:bg-cloth disabled:text-stone"
            value={defaultModel}
            disabled={lockedDefault}
            onChange={(e) => onDefault(e.target.value)}
          >
            <option value="">{models.length ? "Select…" : "No models allowed yet"}</option>
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label || m.id}
              </option>
            ))}
          </select>
        </label>
        <label className="block">
          <div className="mb-1 text-[11px] font-medium tracking-wide text-stone">Chat title model</div>
          <select
            className="h-9 w-full rounded border border-thread bg-folio px-3 disabled:bg-cloth disabled:text-stone"
            value={titleModel}
            disabled={lockedTitle}
            onChange={(e) => onTitle(e.target.value)}
          >
            <option value="">Same as default</option>
            {models.map((m) => (
              <option key={m.id} value={m.id}>
                {m.label || m.id}
              </option>
            ))}
          </select>
        </label>
      </div>

      <div className="mb-2 text-[11px] font-medium tracking-wide text-stone">Allowed models</div>
      {models.length === 0 ? (
        <p className="mb-3 text-[13px] text-stone">No models allowed. Add one below.</p>
      ) : (
        <ul className="mb-3 divide-y divide-thread-2 rounded-lg border border-thread-2">
          {models.map((m) => (
            <li key={m.id} className="flex items-center gap-2 px-3 py-2 text-[13px]">
              <span className="font-mono text-iron">{m.id}</span>
              <span className="text-stone">{m.provider}</span>
              <span className="flex-1" />
              <button
                type="button"
                title="Remove"
                className="rounded p-1 text-stone transition-colors hover:bg-linen hover:text-carmine"
                onClick={() => remove(m.id)}
              >
                <Trash size={14} />
              </button>
            </li>
          ))}
        </ul>
      )}

      <div className="flex flex-wrap items-center gap-2">
        <select
          className="h-9 rounded border border-thread bg-folio px-2 text-[13px]"
          value={provider}
          onChange={(e) => setProvider(e.target.value)}
        >
          {providerIDs.map((id) => (
            <option key={id} value={id}>
              {id}
            </option>
          ))}
        </select>
        <span className="text-stone">/</span>
        <input
          className="h-9 flex-1 rounded border border-thread bg-folio px-3 font-mono text-[13px]"
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
      <p className="mt-2 text-[12px] text-stone">
        Model ids are <span className="font-mono">provider/model</span>. The allowlist drives the chat model picker.
      </p>
    </div>
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
  const inputClass =
    "h-9 w-full rounded border border-thread bg-folio px-3 disabled:bg-cloth disabled:text-stone";

  let control: ReactNode;
  if (field.key === "search.engine") {
    control = (
      <select className={inputClass} value={value} disabled={locked} onChange={(e) => onChange(e.target.value)}>
        {engines.map((e) => (
          <option key={e.id} value={e.id}>
            {e.name}
          </option>
        ))}
      </select>
    );
  } else if (field.type === "bool") {
    control = (
      <label className="inline-flex cursor-pointer items-center gap-2 text-[13px]">
        <input
          type="checkbox"
          className="h-4 w-4 accent-bindery"
          checked={value === "true"}
          disabled={locked}
          onChange={(e) => onChange(e.target.checked ? "true" : "false")}
        />
        <span className="text-stone">{value === "true" ? "Enabled" : "Disabled"}</span>
      </label>
    );
  } else if (field.type === "select") {
    const m = /^providers\.(.+)\.cache_ttl$/.exec(field.key);
    const ttls = (m && providers.find((p) => p.id === m[1])?.cacheTtls) || [];
    control = (
      <select className={inputClass} value={value} disabled={locked} onChange={(e) => onChange(e.target.value)}>
        {ttls.map((t) => (
          <option key={t} value={t}>
            {t}
          </option>
        ))}
      </select>
    );
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
    <div className="mb-4">
      <div className="mb-1 flex items-center gap-2">
        <div className="text-[11px] font-medium tracking-wide text-stone">{labelOf(field.key, engines, providers)}</div>
        <span className="font-mono text-[11px] text-stone">{sourceWord(field.source)}</span>
        {locked ? (
          <span className="inline-flex items-center gap-1 text-[11px] text-carmine" title={field.envName}>
            <Warning size={14} weight="fill" />
            {field.envName}
          </span>
        ) : null}
        {field.restartRequired ? <span className="text-[11px] text-stone">restart</span> : null}
      </div>
      {control}
    </div>
  );
}
