import { TriangleAlert } from "lucide-react";
import { useEffect, useState } from "react";
import { NavLink, Outlet } from "react-router-dom";
import { ui } from "./api";
import { SaveButton, useSave } from "./Feedback";
import { Btn } from "./Btn";
import { Field, Panel } from "./Field";
import { Select } from "./Select";
import { ConfigSource, type ConfigField, type SearchEngine } from "./gen/silo/v1/ui_pb";

export { AdminSettings } from "./AdminSettings";
export { AdminDebug } from "./AdminDebug";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function AdminLayout() {
  const [debug, setDebug] = useState(false);
  useEffect(() => {
    ui.getSettings({})
      .then((x) => setDebug(x.fields.some((f) => f.key === "debug" && f.value === "true")))
      .catch(() => {});
  }, []);
  return (
    <div className="silo-page">
      <h1 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Admin</h1>
      <nav className="silo-scroll-x mb-6 mt-4 flex gap-1 shadow-[inset_0_-1px_0_var(--color-line)]">
        <NavLink
          to="/admin/settings"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet ${isActive ? "border-cobalt text-ink" : "border-transparent text-ink-3 hover:text-ink"}`
          }
        >
          Settings
        </NavLink>
        <NavLink
          to="/admin/connectors"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet ${isActive ? "border-cobalt text-ink" : "border-transparent text-ink-3 hover:text-ink"}`
          }
        >
          Connectors Library
        </NavLink>
        <NavLink
          to="/admin/skills"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet ${isActive ? "border-cobalt text-ink" : "border-transparent text-ink-3 hover:text-ink"}`
          }
        >
          Skills Library
        </NavLink>
        <NavLink
          to="/admin/search-extract"
          className={({ isActive }) =>
            `shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet ${isActive ? "border-cobalt text-ink" : "border-transparent text-ink-3 hover:text-ink"}`
          }
        >
          Search & Extract
        </NavLink>
        {debug && (
          <NavLink
            to="/admin/debug"
            className={({ isActive }) =>
              `shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet ${isActive ? "border-cobalt text-ink" : "border-transparent text-ink-3 hover:text-ink"}`
            }
          >
            Debug
          </NavLink>
        )}
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
  const saver = useSave();
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
    try {
      const x = await saver.run(() => ui.putSettings({ fields: { "search.engine": engine } }));
      const f = fieldOf(x.fields, "search.engine");
      setEngineField(f);
      setEngine(f?.value || engine);
      setEngines(x.searchEngines);
    } catch (ex) {
      setErr(fail(ex));
    }
  }
  const current = engines.find((e) => e.id === engine);
  const locked = engineField?.source === ConfigSource.ENV;
  return (
    <div>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      <Panel title="Search" note="The engine behind the web_search tool. Extract is wired later." className="mb-6">
        <Field
          label="Engine"
          headerRight={
            <>
              {engineField ? (
                <span className="font-mono text-[11px] text-ink-3">
                  {engineField.source === ConfigSource.ENV ? "env" : engineField.source === ConfigSource.YAML ? "yaml" : "default"}
                </span>
              ) : null}
              {locked ? (
                <span className="inline-flex items-center gap-1 text-[11px] text-vermilion" title={engineField?.envName}>
                  <TriangleAlert size={14} />
                  {engineField?.envName}
                </span>
              ) : null}
            </>
          }
        >
          <Select
            value={engine}
            disabled={locked}
            emptyLabel="No engines"
            onChange={(v) => setEngine(v)}
            options={engines.map((e) => ({ value: e.id, label: e.name }))}
          />
        </Field>
        {current?.description ? <p className="mt-3 text-[13px] text-ink-2">{current.description}</p> : null}
        {current && current.fields.length > 0 ? (
          <div className="mt-3 space-y-2 border-t border-line-strong pt-3">
            {current.fields.map((f) => (
              <div key={f.key}>
                <div className="text-[13px]">
                  <span className="font-medium">{f.label}</span>
                  <span className="ml-2 font-mono text-ink-3">{f.key}</span>
                </div>
                {f.description ? <p className="text-[12px] text-ink-3">{f.description}</p> : null}
              </div>
            ))}
          </div>
        ) : (
          <p className="mt-3 text-[13px] text-ink-2">No settings for this engine.</p>
        )}
        <div className="mt-4 flex items-center gap-3">
          <SaveButton state={saver.state} onClick={() => void save()} disabled={locked} />
        </div>
      </Panel>
      <h2 className="mb-3 text-[22px] leading-7 font-medium tracking-[-0.015em]">Extract</h2>
      <p className="text-ink-2">Page extract is not available yet.</p>
    </div>
  );
}

export function AccountPage({ email }: { email: string }) {
  return (
    <div className="silo-page silo-page-sm">
      <h1 className="mb-2 text-[22px] leading-7 font-medium tracking-[-0.015em]">Account</h1>
      <p className="mb-6 text-ink-2">Your sign-in. More settings later.</p>
      <div className="mb-1 text-[12px] font-medium text-ink-3">Email</div>
      <div className="rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3 py-2">{email}</div>
    </div>
  );
}
