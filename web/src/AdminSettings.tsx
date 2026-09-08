import { Warning } from "@phosphor-icons/react";
import { lazy, Suspense, useEffect, useState } from "react";
import { ui } from "./api";
import { Btn } from "./Btn";
import { Crest } from "./Crest";
import { ConfigSource, type AuditRow, type ConfigField, type SearchEngine, type Settings } from "./gen/silo/v1/ui_pb";

const YamlEditor = lazy(() => import("./YamlEditor").then((m) => ({ default: m.YamlEditor })));

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

const GROUPS: { id: string; title: string; note?: string }[] = [
  { id: "model", title: "Model" },
  { id: "openrouter", title: "OpenRouter" },
  { id: "search", title: "Search" },
  { id: "server", title: "Server" },
  { id: "bootstrap", title: "Bootstrap", note: "First admin only. Ignored after a user exists. Restart required." },
];

const LABELS: Record<string, string> = {
  model: "Model",
  "openrouter.api_key": "API key",
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
  if (key === "model") return "model";
  if (key.startsWith("openrouter.")) return "openrouter";
  if (key.startsWith("search.")) return "search";
  if (key.startsWith("bootstrap.")) return "bootstrap";
  return "server";
}

function labelOf(key: string, engines: SearchEngine[]) {
  if (LABELS[key]) return LABELS[key];
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
) {
  setFields(x.fields);
  setValues(Object.fromEntries(x.fields.map((f) => [f.key, f.value])));
  setYaml(x.yaml);
  setYamlPath(x.yamlPath);
  setEngines(x.searchEngines);
}

export function AdminSettings() {
  const [fields, setFields] = useState<ConfigField[]>([]);
  const [values, setValues] = useState<Record<string, string>>({});
  const [engines, setEngines] = useState<SearchEngine[]>([]);
  const [yamlText, setYamlText] = useState("");
  const [yamlPath, setYamlPath] = useState("");
  const [audit, setAudit] = useState<AuditRow[]>([]);
  const [saved, setSaved] = useState("");
  const [err, setErr] = useState("");

  useEffect(() => {
    ui.getSettings({})
      .then((x) => applySettings(x, setFields, setValues, setYamlText, setYamlPath, setEngines))
      .catch((e) => setErr(fail(e)));
    ui.listAudit({})
      .then((x) => setAudit(x.rows))
      .catch((e) => setErr(fail(e)));
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
      const x = await ui.putSettings({ fields: patch });
      applySettings(x, setFields, setValues, setYamlText, setYamlPath, setEngines);
      setSaved("form");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveYaml() {
    setErr("");
    try {
      const x = await ui.putSettings({ yaml: yamlText });
      applySettings(x, setFields, setValues, setYamlText, setYamlPath, setEngines);
      setSaved("yaml");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  const envFields = fields.filter((f) => f.source === ConfigSource.ENV);

  return (
    <div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {GROUPS.map((g) => {
        const rows = fields.filter((f) => groupOf(f.key) === g.id);
        if (rows.length === 0) return null;
        return (
          <section key={g.id} className="mb-8">
            <h2 className="mb-3 text-[22px] font-medium">{g.title}</h2>
            {g.note ? <p className="mb-3 text-[13px] text-stone">{g.note}</p> : null}
            {rows.map((f) => (
              <FieldRow
                key={f.key}
                field={f}
                value={values[f.key] ?? ""}
                engines={engines}
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
          <span>
            Overridden by env and will not apply until unset: {envFields.map((f) => f.envName).join(", ")}
          </span>
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

function FieldRow({
  field,
  value,
  engines,
  onChange,
}: {
  field: ConfigField;
  value: string;
  engines: SearchEngine[];
  onChange: (v: string) => void;
}) {
  const locked = field.source === ConfigSource.ENV;
  const inputClass =
    "h-9 w-full rounded border border-thread bg-folio px-3 disabled:bg-cloth disabled:text-stone";
  return (
    <div className="mb-4">
      <div className="mb-1 flex items-center gap-2">
        <div className="text-[11px] font-medium tracking-wide text-stone">{labelOf(field.key, engines)}</div>
        <span className="font-mono text-[11px] text-stone">{sourceWord(field.source)}</span>
        {locked ? (
          <span className="inline-flex items-center gap-1 text-[11px] text-carmine" title={field.envName}>
            <Warning size={14} weight="fill" />
            {field.envName}
          </span>
        ) : null}
        {field.restartRequired ? <span className="text-[11px] text-stone">restart</span> : null}
      </div>
      {field.key === "search.engine" ? (
        <select className={inputClass} value={value} disabled={locked} onChange={(e) => onChange(e.target.value)}>
          {engines.map((e) => (
            <option key={e.id} value={e.id}>
              {e.name}
            </option>
          ))}
        </select>
      ) : (
        <input
          className={inputClass}
          type={field.secret ? "password" : "text"}
          autoComplete="off"
          value={value}
          disabled={locked}
          onChange={(e) => onChange(e.target.value)}
        />
      )}
    </div>
  );
}
