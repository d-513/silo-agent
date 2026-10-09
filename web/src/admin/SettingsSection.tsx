import { TriangleAlert } from "lucide-react";
import { lazy, Suspense } from "react";
import { useParams } from "@tanstack/react-router";
import { SaveButton } from "../Feedback";
import { Panel } from "../Field";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { AuditTable } from "./AuditTable";
import { AutoenablePanel } from "./AutoenablePanel";
import { ConnectorVarsPanel } from "./ConnectorVarsPanel";
import { FieldGroup } from "./FieldGroup";
import { ModelSettings } from "./ModelSettings";
import { ProviderSettings } from "./ProviderSettings";
import { OidcSettings } from "./OidcSettings";
import { AUTH_NOTE, BOOTSTRAP_NOTE, CONTAINERS_NOTE, CONTEXT_NOTE, KNOWLEDGE_NOTE, MEMORY_NOTE, RUNS_NOTE, TUNNELS_NOTE, groupOf } from "./fields";
import { isSection, type SectionId } from "./sections";
import { useSettingsForm } from "./useAdminSettings";

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
      <Group title="Memory" note={MEMORY_NOTE} id="memory" />
      <Group title="Knowledge" note={KNOWLEDGE_NOTE} id="knowledge" />
    </>
  ),
  runs: () => (
    <>
      <Group title="Context" note={CONTEXT_NOTE} id="context" />
      <Group title="Runs" note={RUNS_NOTE} id="runs" />
    </>
  ),
  connectors: () => <ConnectorOptions />,
  tunnels: () => <Tunnels />,
  signin: () => (
    <>
      <Group title="Password sign-in" note={AUTH_NOTE} id="auth" />
      <OidcSettings />
      <Group title="Bootstrap" note={BOOTSTRAP_NOTE} id="bootstrap" />
    </>
  ),
  server: () => (
    <>
      <Group title="Server" id="server" />
      <Group title="Containers" note={CONTAINERS_NOTE} id="containers" />
    </>
  ),
  yaml: () => <Yaml />,
  audit: () => <Audit />,
};

function Group({ title, note, id }: { title: string; note?: string; id: string }) {
  const { fields } = useSettingsForm();
  return <FieldGroup title={title} note={note} rows={fields.filter((f) => groupOf(f.key) === id)} />;
}

function Tunnels() {
  const { fields, tunnelsHost } = useSettingsForm();
  return (
    <FieldGroup
      title="Tunnels"
      note={TUNNELS_NOTE}
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
    <>
      <FieldGroup
        title="Search"
        note="The engine behind the Bot's web_search tool."
        rows={rows}
        hints={{ "search.engine": engines.find((e) => e.id === engine)?.description || undefined }}
      />
      <Panel title="Extract" note="Turning a page into text for the Bot.">
        <p className="text-[13px] text-ink-2">Page extract is not available yet.</p>
      </Panel>
    </>
  );
}

// Two lists that each save on their own, beside the form.
function ConnectorOptions() {
  const { connVars, saveConnVars, autoenable, saveAutoenable } = useSettingsForm();
  return (
    <>
      <ConnectorVarsPanel vars={connVars} onSave={saveConnVars} />
      <AutoenablePanel list={autoenable} onSave={saveAutoenable} />
    </>
  );
}

// The file itself. Saving it replaces what the form shows.
function Yaml() {
  const { fields, yamlText, setYamlText, yamlPath, yamlSaver, saveYaml } = useSettingsForm();
  const env = fields.filter((f) => f.source === ConfigSource.ENV);
  return (
    <Panel
      title="silo.yaml"
      note="Everything the other sections edit, as one file. Saving it drops unsaved changes made there."
      action={
        <SaveButton size="sm" state={yamlSaver.state} onClick={() => void saveYaml()}>
          Save YAML
        </SaveButton>
      }
    >
      <p className="mb-3 font-mono text-[12px] break-all text-ink-3">{yamlPath || "silo.yaml"}</p>
      {env.length > 0 ? (
        <p className="mb-3 flex items-start gap-2 text-[13px] text-vermilion">
          <TriangleAlert className="mt-0.5 shrink-0" size={16} />
          <span>Overridden by env and will not apply until unset: {env.map((f) => f.envName).join(", ")}</span>
        </p>
      ) : null}
      <Suspense fallback={<div className="h-80 rounded-sm bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]" />}>
        <YamlEditor value={yamlText} onChange={(v) => setYamlText(v)} />
      </Suspense>
    </Panel>
  );
}

function Audit() {
  const { audit } = useSettingsForm();
  return (
    <Panel title="Audit" note="The last 100 approval decisions on every Bot, newest first.">
      <AuditTable audit={audit} />
    </Panel>
  );
}
