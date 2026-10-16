import { TriangleAlert } from "lucide-react";
import { lazy, Suspense } from "react";
import { useParams } from "@tanstack/react-router";
import { SaveButton } from "../Feedback";
import { Panel } from "../Field";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { AutoenablePanel } from "./AutoenablePanel";
import { ConnectorVarsPanel } from "./ConnectorVarsPanel";
import { OperatorRulesPanel } from "./OperatorRulesPanel";
import { FieldGroup } from "./FieldGroup";
import { NetworkSettings } from "./NetworkSettings";
import { ModelSettings } from "./ModelSettings";
import { ProviderSettings } from "./ProviderSettings";
import { OidcSettings } from "./OidcSettings";
import { AUTH_NOTE, BOOTSTRAP_NOTE, CHANGES_NOTE, FILTER_NOTE, CONTAINERS_NOTE, CONTEXT_NOTE, KNOWLEDGE_NOTE, MAIL_NOTE, MEMORY_NOTE, OUTBOUND_NOTE, RULES_NOTE, RUNS_NOTE, TUNNELS_NOTE, groupOf } from "./fields";
import { isSection, type SectionId } from "./sections";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

const YamlEditor = lazy(() => import("../YamlEditor").then((m) => ({ default: m.YamlEditor })));

// SettingsSection is the open category of Admin → Settings. The form state
// lives in the layout above, so each of these only lays out its fields.
export function SettingsSection() {
  const { section } = useParams({ from: "/_authed/admin/settings/$section" });
  if (!isSection(section)) return null;
  return <div className="grid gap-5">{pages[section]()}</div>;
}

const pages: Record<SectionId, () => React.ReactNode> = {
  models: () => <ModelSettings />,
  providers: () => <ProviderSettings />,
  search: () => <SearchSettings />,
  memory: () => (
    <>
      <Group title={t("Memory")} note={t(MEMORY_NOTE)} id="memory" />
      <Group title={t("Knowledge")} note={t(KNOWLEDGE_NOTE)} id="knowledge" />
    </>
  ),
  runs: () => (
    <>
      <Group title={t("Context")} note={t(CONTEXT_NOTE)} id="context" />
      <Group title={t("Runs")} note={t(RUNS_NOTE)} id="runs" />
      <Group title={t("Workspace changes")} note={t(CHANGES_NOTE)} id="changes" />
      <Group title={t("Output filter")} note={t(FILTER_NOTE)} id="filter" />
    </>
  ),
  connectors: () => <ConnectorOptions />,
  rules: () => <OperatorRules />,
  tunnels: () => <Tunnels />,
  mail: () => <Group title={t("Mail")} note={t(MAIL_NOTE)} id="mail" />,
  network: () => <NetworkSettings />,
  signin: () => (
    <>
      <Group title={t("Password sign-in")} note={t(AUTH_NOTE)} id="auth" />
      <OidcSettings />
      <Group title={t("Bootstrap")} note={t(BOOTSTRAP_NOTE)} id="bootstrap" />
    </>
  ),
  server: () => (
    <>
      <Group title={t("Server")} id="server" />
      <Group title={t("Containers")} note={t(CONTAINERS_NOTE)} id="containers" />
    </>
  ),
  yaml: () => <Yaml />,
};

function Group({ title, note, id }: { title: string; note?: string; id: string }) {
  const { fields } = useSettingsForm();
  return <FieldGroup title={title} note={note} rows={fields.filter((f) => groupOf(f.key) === id)} />;
}

function Tunnels() {
  const { fields, tunnelsHost } = useSettingsForm();
  return (
    <FieldGroup
      title={t("Tunnels")}
      note={t(TUNNELS_NOTE)}
      rows={fields.filter((f) => groupOf(f.key) === "tunnels")}
      placeholders={{ "tunnels.host": tunnelsHost || "tunnels.example.com" }}
    />
  );
}

// The engine, then only the settings of the engine that is picked.
function SearchSettings() {
  const { fields, values, engines } = useSettingsForm();
  const engine = values["search.engine"] || engines[0]?.id || "";
  const rows = fields.filter((f) => f.key === "search.engine" || f.key.startsWith(`search.${engine}.`));
  return (
    <FieldGroup
      title={t("Search")}
      note={t("The engine behind the Bot's web search (silo_runtime.web_search).")}
      rows={rows}
      hints={{ "search.engine": engines.find((e) => e.id === engine)?.description || undefined }}
    />
  );
}

// Two lists that each save on their own, beside the form.
function ConnectorOptions() {
  const { connVars, saveConnVars, autoenable, saveAutoenable } = useSettingsForm();
  return (
    <>
      <Group title={t("Outbound connections")} note={t(OUTBOUND_NOTE)} id="outbound" />
      <ConnectorVarsPanel vars={connVars} onSave={saveConnVars} />
      <AutoenablePanel list={autoenable} onSave={saveAutoenable} />
    </>
  );
}

// The operator's layer: the rule rows (their own RPCs) and the policy text
// its models read before a Bot's own.
function OperatorRules() {
  return (
    <>
      <Group title={t("Rules")} note={t(RULES_NOTE)} id="rules" />
      <OperatorRulesPanel />
    </>
  );
}

// The file itself. Saving it replaces what the form shows.
function Yaml() {
  const { fields, yamlText, setYamlText, yamlPath, yamlSaver, saveYaml } = useSettingsForm();
  const env = fields.filter((f) => f.source === ConfigSource.ENV);
  return (
    <Panel
      title={t("silo.yaml")}
      note={t("Everything the other sections edit, as one file. Saving it drops unsaved changes made there.")}
      action={
        <SaveButton size="sm" state={yamlSaver.state} onClick={() => void saveYaml()}>
          {t("Save YAML")}
        </SaveButton>
      }
    >
      <p className="mb-3 font-mono text-[12px] break-all text-ink-3">{yamlPath || "silo.yaml"}</p>
      {env.length > 0 ? (
        <p className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
          <TriangleAlert className="mt-0.5 shrink-0" size={16} />
          <span>{t("Overridden by env and will not apply until unset: {{names}}", { names: env.map((f) => f.envName).join(", ") })}</span>
        </p>
      ) : null}
      <Suspense fallback={<div className="h-80 rounded-sm bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]" />}>
        <YamlEditor value={yamlText} onChange={(v) => setYamlText(v)} />
      </Suspense>
    </Panel>
  );
}
