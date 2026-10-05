import { TriangleAlert } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState, type ReactNode } from "react";
import { Link, Outlet, type LinkProps } from "@tanstack/react-router";
import { ui } from "./api";
import { SaveButton, useSave } from "./Feedback";
import { Field, Panel } from "./Field";
import { Select } from "./Select";
import { ConfigSource, UI, type ConfigField, type SearchEngine } from "./gen/silo/v1/ui_pb";
import { fail } from "./errors";
import { put } from "./query";

function AdminTab({ to, children }: { to: LinkProps["to"]; children: ReactNode }) {
  return (
    <Link
      to={to}
      className="shrink-0 whitespace-nowrap border-b-2 px-3 py-2.5 text-[13px] font-medium transition-colors duration-[160ms] ease-quiet"
      activeProps={{ className: "border-cobalt text-ink" }}
      inactiveProps={{ className: "border-transparent text-ink-3 hover:text-ink" }}
    >
      {children}
    </Link>
  );
}

export function AdminLayout() {
  const debug = useQuery(UI.method.getSettings, {}).data?.fields.some((f) => f.key === "debug" && f.value === "true") ?? false;
  return (
    <div className="silo-page">
      <h1 className="text-title">Admin</h1>
      <nav className="silo-scroll-x mb-6 mt-4 flex gap-1 shadow-[inset_0_-1px_0_var(--color-line)]">
        <AdminTab to="/admin/settings">Settings</AdminTab>
        <AdminTab to="/admin/connectors">Connectors Library</AdminTab>
        <AdminTab to="/admin/skills">Skills Library</AdminTab>
        <AdminTab to="/admin/drives">Drives</AdminTab>
        <AdminTab to="/admin/search-extract">Search & Extract</AdminTab>
        {debug && <AdminTab to="/admin/debug">Debug</AdminTab>}
      </nav>
      <Outlet />
    </div>
  );
}

const noEngines: SearchEngine[] = [];

function fieldOf(fields: ConfigField[], key: string) {
  return fields.find((f) => f.key === key);
}

export function AdminSearchExtract() {
  const q = useQuery(UI.method.getSettings, {});
  const engines = q.data?.searchEngines ?? noEngines;
  const engineField = q.data ? fieldOf(q.data.fields, "search.engine") : undefined;
  // null until the human picks: the box then shows the saved engine.
  const [picked, setEngine] = useState<string | null>(null);
  const engine = picked ?? (engineField?.value || engines[0]?.id || "");
  const saver = useSave();
  const [actErr, setErr] = useState("");
  const err = actErr || (q.error ? fail(q.error) : "");
  async function save() {
    setErr("");
    try {
      put(UI.method.getSettings, {}, await saver.run(() => ui.putSettings({ fields: { "search.engine": engine } })));
      setEngine(null);
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
      <h2 className="mb-3 text-title">Extract</h2>
      <p className="text-ink-2">Page extract is not available yet.</p>
    </div>
  );
}
