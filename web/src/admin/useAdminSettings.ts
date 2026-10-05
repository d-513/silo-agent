import { useQuery } from "@connectrpc/connect-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { ConfigSource, UI, type AuditRow, type ConfigField, type ConnectorVar, type ModelOption, type Provider, type SearchEngine, type Settings } from "../gen/silo/v1/ui_pb";
import { put, reload } from "../query";

const noFields: ConfigField[] = [];
const noEngines: SearchEngine[] = [];
const noProviders: Provider[] = [];
const noModels: ModelOption[] = [];
const noVars: ConnectorVar[] = [];
const noAudit: AuditRow[] = [];

// useAdminSettings is the operator settings form. What the server holds is the
// cached getSettings answer; the form keeps only what the human changed on top
// of it (`edits`, and a silo.yaml draft), so the patch to save is exactly the
// edits that differ. There are four ways to save: the form, silo.yaml, the
// model allowlist, connector variables.
export function useAdminSettings() {
  const settingsQ = useQuery(UI.method.getSettings, {});
  const auditQ = useQuery(UI.method.listAudit, {});
  const server = settingsQ.data;
  const fields = server?.fields ?? noFields;
  const [edits, setEdits] = useState<Record<string, string>>({});
  // null until the YAML editor is typed in: it then shows the file as saved.
  const [yamlDraft, setYamlText] = useState<string | null>(null);
  const formSaver = useSave();
  const yamlSaver = useSave();
  const [actErr, setErr] = useState("");
  const failed = settingsQ.error ?? auditQ.error;

  const values = useMemo(() => ({ ...Object.fromEntries(fields.map((f) => [f.key, f.value])), ...edits }), [fields, edits]);

  // A save answers with the settings as they now are: every reader of the
  // cache gets them, and the form goes back to showing the server's values.
  const apply = (x: Settings) => {
    put(UI.method.getSettings, {}, x);
    setEdits({});
    setYamlText(null);
    void reload(UI.method.listAudit);
  };

  function setValue(key: string, v: string) {
    setEdits((prev) => ({ ...prev, [key]: v }));
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
    setEdits({});
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
      apply(await yamlSaver.run(() => ui.putSettings({ yaml: yamlDraft ?? server?.yaml ?? "" })));
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


  return {
    fields,
    values,
    setValue,
    engines: server?.searchEngines ?? noEngines,
    providers: server?.providers ?? noProviders,
    models: server?.models ?? noModels,
    connVars: server?.connectorVars ?? noVars,
    yamlText: yamlDraft ?? server?.yaml ?? "",
    setYamlText,
    yamlPath: server?.yamlPath ?? "",
    audit: auditQ.data?.rows ?? noAudit,
    // The effective tunnels.host (explicit or derived), shown where it is unset.
    tunnelsHost: server?.tunnelsHost ?? "",
    formSaver,
    yamlSaver,
    err: actErr || (failed ? fail(failed) : ""),
    dirty,
    discard,
    saveForm,
    saveYaml,
    saveModels,
    saveConnVars,
  };
}
