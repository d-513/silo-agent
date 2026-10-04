import { useEffect, useRef, useState } from "react";
import { ui } from "../api";
import { useSave } from "../Feedback";
import { ConfigSource, type AuditRow, type ConfigField, type ConnectorVar, type ModelOption, type Provider, type SearchEngine, type Settings } from "../gen/silo/v1/ui_pb";
import { fail } from "../errors";
import { applySettings } from "./fields";

// useAdminSettings loads the operator settings and holds the form: the values
// being edited, what differs from the server (the patch), and the four ways to
// save (the form, silo.yaml, the model allowlist, connector variables).
export function useAdminSettings() {
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


  return {
    fields, values, setValue, engines, providers, models, connVars, yamlText, setYamlText, yamlPath, audit,
    formSaver, yamlSaver, err, dirty, discard, saveForm, saveYaml, saveModels, saveConnVars,
  };
}
