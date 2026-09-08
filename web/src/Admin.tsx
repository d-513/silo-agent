import { Warning } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { ui } from "./api";
import { Btn } from "./Btn";
import { ConfigSource, type ConfigField, type SearchEngine } from "./gen/silo/v1/ui_pb";

export { AdminSettings } from "./AdminSettings";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function AdminLayout() {
  return (
    <div className="silo-page">
      <h1 className="text-[22px] font-medium tracking-tight">Admin</h1>
      <nav className="silo-scroll-x mb-6 mt-4 flex gap-1 border-b border-thread-2">
        <NavLink
          to="/admin/settings"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2 ${isActive ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`
          }
        >
          Settings
        </NavLink>
        <NavLink
          to="/admin/connectors"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2 ${isActive ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`
          }
        >
          Connectors Library
        </NavLink>
        <NavLink
          to="/admin/skills"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2 ${isActive ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`
          }
        >
          Skills Library
        </NavLink>
        <NavLink
          to="/admin/search-extract"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2 ${isActive ? "border-bindery text-iron" : "border-transparent text-stone hover:text-iron"}`
          }
        >
          Search & Extract
        </NavLink>
      </nav>
      <Outlet />
    </div>
  );
}

function fieldOf(fields: ConfigField[], key: string) {
  return fields.find((f) => f.key === key);
}

export function AdminSearchExtract() {
  const [engine, setEngine] = useState("");
  const [engines, setEngines] = useState<SearchEngine[]>([]);
  const [engineField, setEngineField] = useState<ConfigField | undefined>();
  const [saved, setSaved] = useState(false);
  const [err, setErr] = useState("");
  useEffect(() => {
    ui.getSettings({})
      .then((x) => {
        const f = fieldOf(x.fields, "search.engine");
        setEngineField(f);
        setEngine(f?.value || x.searchEngines[0]?.id || "");
        setEngines(x.searchEngines);
      })
      .catch((e) => setErr(fail(e)));
  }, []);
  async function save() {
    setErr("");
    setSaved(false);
    try {
      const x = await ui.putSettings({ fields: { "search.engine": engine } });
      const f = fieldOf(x.fields, "search.engine");
      setEngineField(f);
      setEngine(f?.value || engine);
      setEngines(x.searchEngines);
      setSaved(true);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  const current = engines.find((e) => e.id === engine);
  const locked = engineField?.source === ConfigSource.ENV;
  return (
    <div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      <h2 className="mb-3 text-[22px] font-medium">Search</h2>
      <div className="mb-1 flex items-center gap-2">
        <div className="text-[11px] font-medium tracking-wide text-stone">Engine</div>
        {engineField ? (
          <span className="font-mono text-[11px] text-stone">
            {engineField.source === ConfigSource.ENV ? "env" : engineField.source === ConfigSource.YAML ? "yaml" : "default"}
          </span>
        ) : null}
        {locked ? (
          <span className="inline-flex items-center gap-1 text-[11px] text-carmine" title={engineField?.envName}>
            <Warning size={14} weight="fill" />
            {engineField?.envName}
          </span>
        ) : null}
      </div>
      <select
        className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3 disabled:bg-cloth disabled:text-stone"
        value={engine}
        disabled={locked}
        onChange={(e) => {
          setEngine(e.target.value);
          setSaved(false);
        }}
      >
        {engines.map((e) => (
          <option key={e.id} value={e.id}>
            {e.name}
          </option>
        ))}
      </select>
      {current?.description ? <p className="mb-4 text-[13px] text-stone">{current.description}</p> : null}
      {current && current.fields.length > 0 ? (
        <div className="mb-4 space-y-2">
          {current.fields.map((f) => (
            <div key={f.key}>
              <div className="text-[13px]">
                <span className="font-medium">{f.label}</span>
                <span className="ml-2 font-mono text-stone">{f.key}</span>
              </div>
              {f.description ? <p className="text-[12px] text-stone">{f.description}</p> : null}
            </div>
          ))}
        </div>
      ) : (
        <p className="mb-4 text-[13px] text-stone">No settings for this engine.</p>
      )}
      <Btn kind="primary" onClick={() => void save()} disabled={locked}>
        Save
      </Btn>
      {saved && <span className="ml-3 text-stone">Saved</span>}
      <h2 className="mt-10 mb-3 text-[22px] font-medium">Extract</h2>
      <p className="text-stone">Page extract is not available yet.</p>
    </div>
  );
}

export function AccountPage({ email }: { email: string }) {
  return (
    <div className="silo-page silo-page-sm">
      <h1 className="mb-2 text-[22px] font-medium tracking-tight">Account</h1>
      <p className="mb-6 text-stone">Your sign-in. More settings later.</p>
      <div className="mb-1 text-[12px] font-medium text-stone">Email</div>
      <div className="rounded border border-thread bg-folio px-3 py-2">{email}</div>
    </div>
  );
}
