import { useQuery } from "@connectrpc/connect-query";
import { createContext, useContext, useEffect, useMemo, useRef, useState } from "react";
import { ui } from "../api";
import { fail } from "../errors";
import { useSave } from "../Feedback";
import { ConfigSource, UI, type AuditRow, type ConfigField, type ConnectorVar, type ModelOption, type Provider, type SearchEngine, type Settings } from "../gen/silo/v1/ui_pb";
import { peek, put, reload } from "../query";
import { withModel } from "./providerModels";

const noFields: ConfigField[] = [];
const noEngines: SearchEngine[] = [];
const noProviders: Provider[] = [];
const noModels: ModelOption[] = [];
const noVars: ConnectorVar[] = [];
const noAudit: AuditRow[] = [];

// useAdminSettings is the operator settings form. What the server holds is the
// cached getSettings answer; the form keeps only what the human changed on top
// of it (`edits`, and a silo.yaml draft), so the patch to save is exactly the
// edits that differ. There are five ways to save: the form, silo.yaml, the
// model allowlist, connector variables, and the connectors every new Bot
// gets. The settings layout calls it once and
// every section reads it through useSettingsForm, so an edit made in one
// section is still there after a look at another.
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
  // Models whose add or remove is on its way to the server.
  const [busyModels, setBusy] = useState<ReadonlySet<string>>(new Set());
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

  // A save beside the form (the allowlist, the connector options) leaves the
  // form's edits alone. The YAML draft goes: the file changed under it.
  const applyAside = (x: Settings) => {
    put(UI.method.getSettings, {}, x);
    setYamlText(null);
  };

  // Allowlist changes run one after another, each on top of what the server
  // last answered, so two quick presses cannot drop each other's model.
  const modelQueue = useRef<Promise<void>>(Promise.resolve());
  function setModel(id: string, on: boolean) {
    setErr("");
    setBusy((prev) => new Set(prev).add(id));
    modelQueue.current = modelQueue.current.then(async () => {
      try {
        const cur = (peek(UI.method.getSettings, {})?.models ?? []).map((m) => m.id);
        const next = withModel(cur, id, on);
        if (next !== cur) applyAside(await ui.setModels({ models: next }));
      } catch (ex) {
        setErr(fail(ex));
      } finally {
        setBusy((prev) => {
          const left = new Set(prev);
          left.delete(id);
          return left;
        });
      }
    });
    return modelQueue.current;
  }

  async function saveConnVars(list: { name: string; value: string }[]) {
    setErr("");
    try {
      applyAside(await ui.setConnectorVars({ connectorVars: list.map((v) => ({ name: v.name, value: v.value, source: ConfigSource.DEFAULT, envName: "" })) }));
    } catch (ex) {
      setErr(fail(ex));
      throw ex;
    }
  }

  async function saveAutoenable(identifiers: string[]) {
    setErr("");
    try {
      applyAside(await ui.setAutoenableConnectors({ identifiers }));
      // Each library row says whether the list names it.
      void reload(UI.method.listConnectors);
    } catch (ex) {
      setErr(fail(ex));
      throw ex;
    }
  }

  return {
    loading: settingsQ.isPending,
    fields,
    values,
    setValue,
    engines: server?.searchEngines ?? noEngines,
    providers: server?.providers ?? noProviders,
    models: server?.models ?? noModels,
    connVars: server?.connectorVars ?? noVars,
    autoenable: server?.autoenableConnectors,
    yamlText: yamlDraft ?? server?.yaml ?? "",
    setYamlText,
    yamlPath: server?.yamlPath ?? "",
    audit: auditQ.data?.rows ?? noAudit,
    // The effective tunnels.host (explicit or derived), shown where it is unset.
    tunnelsHost: server?.tunnelsHost ?? "",
    // The callback address to register at the OIDC provider.
    oidcRedirectUrl: server?.oidcRedirectUrl ?? "",
    formSaver,
    yamlSaver,
    err: actErr || (failed ? fail(failed) : ""),
    dirty,
    discard,
    saveForm,
    saveYaml,
    setModel,
    busyModels,
    saveConnVars,
    saveAutoenable,
  };
}

export type SettingsForm = ReturnType<typeof useAdminSettings>;

export const SettingsFormContext = createContext<SettingsForm | null>(null);

/** The settings form of the page a section sits in. */
export function useSettingsForm(): SettingsForm {
  const form = useContext(SettingsFormContext);
  if (!form) throw new Error("useSettingsForm outside the settings layout");
  return form;
}
