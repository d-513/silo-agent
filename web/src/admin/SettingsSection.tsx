import { ArrowRight, TriangleAlert } from "lucide-react";
import { lazy, Suspense } from "react";
import { Link, useParams } from "@tanstack/react-router";
import { SaveButton } from "../Feedback";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { AutoenablePanel } from "./AutoenablePanel";
import { ConnectorVarsPanel } from "./ConnectorVarsPanel";
import { OperatorRulesPanel } from "./OperatorRulesPanel";
import { FieldGroup } from "./FieldGroup";
import { NetworkSettings } from "./NetworkSettings";
import { OidcSettings } from "./OidcSettings";
import { CaddyHealth } from "./CaddyHealth";
import { SearchSources, SearchTry } from "./SearchHealth";
import { AdminHead, Block, Group, Groups } from "./kit";
import {
  ARTIFACTS_NOTE,
  AUTH_NOTE,
  HTTPS_NOTE,
  BOOTSTRAP_NOTE,
  CHANGES_NOTE,
  CONTENT_NOTE,
  FILTER_NOTE,
  CONTAINERS_NOTE,
  CONTEXT_NOTE,
  KNOWLEDGE_NOTE,
  MAIL_NOTE,
  MEMORY_NOTE,
  OUTBOUND_NOTE,
  READER_NOTE,
  RULES_NOTE,
  RUNS_NOTE,
  TUNNELS_NOTE,
  VOICE_CALL_NOTE,
  VOICE_NETWORK_NOTE,
  groupOf,
} from "./fields";
import { SECTIONS, isSection, type SectionId } from "./sections";
import { useSettingsForm } from "./useAdminSettings";
import { t } from "../i18n.ts";

const YamlEditor = lazy(() => import("../YamlEditor").then((m) => ({ default: m.YamlEditor })));

// SettingsSection is the open category of Admin → Settings: its name and one
// sentence, then its groups. The form state lives in the layout above, so each
// of these only lays out its fields.
export function SettingsSection() {
  const { section } = useParams({ from: "/_authed/admin/settings/$section" });
  if (!isSection(section)) return null;
  const s = SECTIONS.find((x) => x.id === section)!;
  return (
    <>
      <AdminHead title={t(s.label)} note={t(s.note)} />
      <Groups>{pages[section]()}</Groups>
    </>
  );
}

const pages: Record<SectionId, () => React.ReactNode> = {
  // Models has routes of its own (ModelsPages.tsx).
  models: () => null,
  search: () => <SearchSettings />,
  memory: () => (
    <>
      <FormGroup title={t("Memory")} note={t(MEMORY_NOTE)} id="memory" />
      <FormGroup title={t("Knowledge")} note={t(KNOWLEDGE_NOTE)} id="knowledge" />
    </>
  ),
  runs: () => (
    <>
      <FormGroup title={t("Context")} note={t(CONTEXT_NOTE)} id="context" />
      <FormGroup title={t("Runs")} note={t(RUNS_NOTE)} id="runs" />
      <FormGroup title={t("Workspace changes")} note={t(CHANGES_NOTE)} id="changes" />
      <FormGroup title={t("Artifacts")} note={t(ARTIFACTS_NOTE)} id="artifacts" />
      <FormGroup title={t("Output filter")} note={t(FILTER_NOTE)} id="filter" />
    </>
  ),
  voice: () => <Voice />,
  connectors: () => <ConnectorOptions />,
  rules: () => (
    <>
      <FormGroup title={t("Policy")} note={t(RULES_NOTE)} id="rules" />
      <OperatorRulesPanel />
    </>
  ),
  tunnels: () => <Tunnels />,
  mail: () => <FormGroup title={t("Mail")} note={t(MAIL_NOTE)} id="mail" />,
  network: () => <NetworkSettings />,
  https: () => <HttpsSettings />,
  signin: () => (
    <>
      <FormGroup title={t("Password sign-in")} note={t(AUTH_NOTE)} id="auth" />
      <OidcSettings />
      <FormGroup title={t("Bootstrap")} note={t(BOOTSTRAP_NOTE)} id="bootstrap" />
    </>
  ),
  server: () => (
    <>
      <FormGroup title={t("Server")} id="server" />
      <FormGroup title={t("Containers")} note={t(CONTAINERS_NOTE)} id="containers" />
    </>
  ),
  yaml: () => <Yaml />,
};

function FormGroup({ title, note, id }: { title: string; note?: string; id: string }) {
  const { fields } = useSettingsForm();
  return <FieldGroup title={title} note={note} rows={fields.filter((f) => groupOf(f.key) === id)} />;
}

// What a call needs that is not a model. The models themselves (what hears,
// what speaks, the talker) are jobs like any other, under Models.
function Voice() {
  const { fields } = useSettingsForm();
  return (
    <>
      <FieldGroup
        title={t("Voice")}
        note={t(VOICE_CALL_NOTE)}
        rows={fields.filter((f) => groupOf(f.key) === "voice")}
        action={
          <Link to="/admin/settings/models" className="flex items-center gap-1 text-small font-medium text-cobalt hover:text-cobalt-deep">
            {t("Voice models")}
            <ArrowRight size={13} />
          </Link>
        }
      />
      <FieldGroup title={t("Call network")} note={t(VOICE_NETWORK_NOTE)} rows={fields.filter((f) => groupOf(f.key) === "voice_network")} />
    </>
  );
}

function Tunnels() {
  const { fields, contentHost } = useSettingsForm();
  return (
    <>
      <FieldGroup
        title={t("Content host")}
        note={t(CONTENT_NOTE)}
        rows={fields.filter((f) => groupOf(f.key) === "content")}
        placeholders={{ "content.host": contentHost || "content.example.com" }}
      />
      <FieldGroup title={t("Tunnels")} note={t(TUNNELS_NOTE)} rows={fields.filter((f) => groupOf(f.key) === "tunnels")} />
    </>
  );
}

// Caddy's mode, then only what that mode uses: the email and the DNS
// provider for Let's Encrypt, and only the picked provider's settings.
function HttpsSettings() {
  const { fields, values, dnsProviders } = useSettingsForm();
  const mode = values["caddy.mode"] || "auto";
  const acme = mode === "auto" || mode === "acme";
  const provider = values["caddy.dns_provider"] || "";
  const rows = fields.filter(
    (f) =>
      f.key === "caddy.admin" ||
      f.key === "caddy.upstream" ||
      f.key === "caddy.mode" ||
      (acme && (f.key === "caddy.email" || f.key === "caddy.dns_provider" || (provider !== "" && f.key.startsWith(`caddy.dns.${provider}.`)))),
  );
  return (
    <>
      <FieldGroup
        title={t("Certificates")}
        note={t(HTTPS_NOTE)}
        rows={rows}
        hints={{ "caddy.dns_provider": dnsProviders.find((d) => d.id === provider)?.description || t("With a DNS provider, Let's Encrypt needs no inbound connection, and the content domain gets one wildcard certificate.") }}
      />
      <CaddyHealth />
    </>
  );
}

// The engine and only the settings of the one that is picked, a search to try
// it with, and its sources where it has any.
function SearchSettings() {
  const { fields, values, engines } = useSettingsForm();
  const engine = values["search.engine"] || engines[0]?.id || "";
  const rows = fields.filter((f) => f.key === "search.engine" || f.key.startsWith(`search.${engine}.`));
  return (
    <>
      <FieldGroup
        title={t("Engine")}
        note={t("The engine behind the Bot's web search (silo_runtime.web_search).")}
        rows={rows}
        hints={{ "search.engine": engines.find((e) => e.id === engine)?.description || undefined }}
      >
        <SearchTry />
      </FieldGroup>
      <SearchSources />
      <FormGroup title={t("Reading pages")} note={t(READER_NOTE)} id="reader" />
    </>
  );
}

// Two lists that each save on their own, beside the form.
function ConnectorOptions() {
  const { connVars, saveConnVars, autoenable, saveAutoenable } = useSettingsForm();
  return (
    <>
      <FormGroup title={t("Outbound connections")} note={t(OUTBOUND_NOTE)} id="outbound" />
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
    <Group
      title={t("Contents")}
      note={
        <>
          <span className="font-mono break-all">{yamlPath || "silo.yaml"}</span>
          {" · "}
          {t("Saving it drops unsaved changes made on the other pages.")}
        </>
      }
      action={
        <SaveButton size="sm" state={yamlSaver.state} onClick={() => void saveYaml()}>
          {t("Save YAML")}
        </SaveButton>
      }
    >
      {env.length > 0 ? (
        <Block className="flex items-start gap-2 text-small leading-5 text-vermilion">
          <TriangleAlert className="mt-0.5 shrink-0" size={15} />
          <span className="min-w-0 break-words">{t("Overridden by env and will not apply until unset: {{names}}", { names: env.map((f) => f.envName).join(", ") })}</span>
        </Block>
      ) : null}
      <Block>
        <Suspense fallback={<div className="h-80 rounded-sm bg-surface shadow-[inset_0_0_0_1px_var(--color-line-strong)]" />}>
          <YamlEditor value={yamlText} onChange={(v) => setYamlText(v)} />
        </Suspense>
      </Block>
    </Group>
  );
}
