import { Binary, Cpu, Split, Type } from "lucide-react";
import { useNavigate, useParams } from "@tanstack/react-router";
import { Panel } from "../Field";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { AllowedModels } from "./AllowedModels";
import { ProviderSettings } from "./ProviderSettings";
import { ProviderMap } from "./ProviderMap";
import { isReady, providerOf, providersInUse, resolveJobs } from "./providerMap";
import { ModelsHead, RolePanel, type Role } from "./roles";
import { isModelPage, type ModelPage } from "./sections";
import { useSettingsForm } from "./useAdminSettings";
import { VoiceSettings } from "./VoiceSettings";
import { msg, t } from "../i18n.ts";

// The Models part of Admin → Settings: the map on its own page, then a page
// per kind of job, each holding its pickers and the allowlists they pick from.

export function ModelMapPage() {
  const { providers, values, fields, decisionModels } = useSettingsForm();
  const navigate = useNavigate();
  const jobs = resolveJobs(values, decisionModels.map((m) => m.id));
  const ready = (id: string) => isReady(id, fields, values, ConfigSource.ENV);
  const used = providersInUse(jobs);
  const missing = used.filter((p) => !ready(p)).length;
  const idle = jobs.filter((j) => !providerOf(j.model)).length;

  return (
    <div className="grid gap-5">
      <ModelsHead icon={Cpu} title={t("Models")} note={t("Which provider does each job. Press a job to change its model, or a provider for its settings.")}>
        <dl className="flex shrink-0 gap-6 pt-1 max-sm:hidden">
          <Fact label={t("Providers")} value={used.length} />
          <Fact label={t("Need a key")} value={missing} warn={missing > 0} />
          <Fact label={t("Not done", { context: "job" })} value={idle} />
        </dl>
      </ModelsHead>
      <Panel>
        <ProviderMap
          jobs={jobs}
          providers={providers}
          ready={ready}
          onJob={(j) => void navigate({ to: "/admin/settings/models/$page", params: { page: j.page } })}
          onPick={(id) => void navigate({ to: "/admin/settings/models/$page", params: { page: "providers" }, search: { provider: id } })}
        />
        <div className="mt-5 flex flex-wrap items-center gap-x-5 gap-y-1.5 pt-4 text-[12px] text-ink-3 shadow-[inset_0_1px_0_var(--color-line)]">
          <span className="flex items-center gap-2">
            <span aria-hidden className="h-px w-5 bg-ink-3" />
            {t("Set for this job")}
          </span>
          <span className="flex items-center gap-2">
            <span aria-hidden className="w-5 border-t border-dashed border-ink-3" />
            {t("Follows another setting")}
          </span>
          <span className="flex items-center gap-2">
            <span aria-hidden className="h-[6px] w-[6px] rounded-full bg-canvas shadow-[inset_0_0_0_1px_var(--color-line-strong)]" />
            {t("Nobody does it")}
          </span>
        </div>
      </Panel>
    </div>
  );
}

function Fact({ label, value, warn }: { label: string; value: number; warn?: boolean }) {
  return (
    <div className="flex flex-col items-end gap-1.5">
      <dt className="text-label-caps text-ink-3 uppercase">{label}</dt>
      <dd className={`font-mono text-[20px] leading-6 tabular-nums ${warn ? "text-vermilion" : "text-ink"}`}>{value}</dd>
    </div>
  );
}

export function ModelsPage() {
  const { page } = useParams({ from: "/_authed/admin/settings/models/$page" });
  if (!isModelPage(page)) return null;
  return <div className="grid gap-5">{pages[page]()}</div>;
}

const pages: Record<ModelPage, () => React.ReactNode> = {
  providers: () => <ProviderSettings />,
  text: () => <TextModels />,
  decision: () => <DecisionModels />,
  embedding: () => <EmbeddingModels />,
  voice: () => <VoiceSettings />,
};

// The text jobs. Every one but the default may follow another.
const TEXT_ROLES: Role[] = [
  { key: "model", takes: "text", hint: msg("What a chat writes with, unless its Bot or the chat picks another allowed text model.") },
  { key: "model_title", takes: "text", unset: msg("Same as default"), hint: msg("Names a chat from its first message. A small, fast model is enough.") },
  { key: "model_subagent", takes: "text", unset: msg("Same as the lead"), hint: msg("Default for subagents a lead starts. A cheaper model keeps parallel work affordable; the lead can still pick another.") },
  { key: "model_memory", takes: "text", unset: msg("Same as title model"), hint: msg("Reads chats once they go quiet and saves the facts the Bot missed. A cheap model is enough.") },
];

function TextModels() {
  return (
    <>
      <ModelsHead icon={Type} title={t("Text")} note={t("Models that write: every chat, the jobs around it, and the talker on a call. A Bot and a chat can each pick another from the allowed list.")} kinds={["text"]} />
      <RolePanel title={t("Jobs")} roles={TEXT_ROLES} />
      <AllowedModels kind="text" />
    </>
  );
}

// The decide tool's model, and the two judges that take either kind.
const DECIDE_ROLES: Role[] = [{ key: "model_decision", takes: "decision", hint: msg("What the Bot's decide tool asks unless it names another allowed decision model.") }];
const JUDGE_ROLES: Role[] = [
  {
    key: "model_approval",
    takes: "either",
    unset: msg("Same as title model"),
    hint: msg("Decides rules set to Auto. A decision model answers in a fraction of a second and only a sure yes approves; a text model is asked for one word."),
  },
  {
    key: "model_filter",
    takes: "either",
    unset: msg("Same as auto-approval model"),
    hint: msg("Judges tool results for rules set to Check, once per result. A decision model is the fast, cheap choice; a text model is asked for one word."),
  },
];

function DecisionModels() {
  return (
    <>
      <ModelsHead icon={Split} title={t("Decisions")} note={t("Models that answer typed questions (yes or no, a choice, a score) with probabilities instead of text. They judge, they do not chat.")} kinds={["decision"]} />
      <RolePanel title={t("The decide tool")} roles={DECIDE_ROLES} />
      <RolePanel title={t("Judges")} note={t("They rule on what a Bot may do and what it may read. Each takes a decision model or a text model.")} roles={JUDGE_ROLES} />
      <AllowedModels kind="decision" />
    </>
  );
}

const EMBED_ROLES: Role[] = [{ key: "embedding_model", takes: "embedding" }];

function EmbeddingModels() {
  return (
    <>
      <ModelsHead icon={Binary} title={t("Embeddings")} note={t("Models that turn text into vectors, so memories and indexed documents can be searched by meaning.")} kinds={["embedding"]} />
      <RolePanel title={t("Jobs")} roles={EMBED_ROLES} />
      <AllowedModels kind="embedding" />
    </>
  );
}
