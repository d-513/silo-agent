import { CornerDownRight, type LucideIcon } from "lucide-react";
import type { ReactNode } from "react";
import { Panel } from "../Field";
import { ConfigSource, type ModelOption } from "../gen/silo/v1/ui_pb";
import { modelOptions } from "../modelOptions";
import { Select } from "../Select";
import { SourceChips } from "./FieldRow";
import { HINTS, LABELS } from "./fields";
import { ProviderMark } from "./ProviderMap";
import { effective, providerOf } from "./providerMap";
import { eitherKind, type ModelKind } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";
import { msg, t } from "../i18n.ts";

// The short name of each job a model setting gives, as the map and an
// allowlist's "used by" line say it.
export const ROLE_NAMES: Record<string, string> = {
  model: msg("Chat"),
  model_title: msg("Titles"),
  model_subagent: msg("Subagents"),
  model_memory: msg("Memory"),
  model_approval: msg("Auto-approval"),
  model_filter: msg("Output filter"),
  model_decision: msg("Decisions"),
  embedding_model: msg("Embeddings"),
  transcribe_model: msg("Dictation"),
  "voice.transcribe_model": msg("Hears calls"),
  "voice.speech_model": msg("Speaks"),
  model_voice: msg("Talker"),
};

// A model setting as a row: what it is for, and a picker over the allowed
// models of the kind it takes ("either" is a text or a decision model).
export type Role = {
  key: string;
  takes: ModelKind | "either";
  label?: string;
  hint?: string;
  // The picker's empty choice, and its value when that is not "".
  unset?: string;
  unsetValue?: string;
  // A line under the picker about the chosen model (live or not, …).
  describe?: (chosen: ModelOption | undefined) => ReactNode;
};

const NONE: Record<ModelKind, string> = {
  text: msg("No text models allowed yet"),
  decision: msg("No decision models allowed yet"),
  transcription: msg("No speech-to-text models allowed yet"),
  speech: msg("No speech models allowed yet"),
  embedding: msg("No embedding models allowed yet"),
};

// RolePanel is a card of model settings, one row each: the job on the left,
// its picker on the right, and under an empty picker the model it falls
// back to and where that comes from.
export function RolePanel({ title, note, roles }: { title: string; note?: string; roles: Role[] }) {
  return (
    <Panel title={title} note={note} padded={false}>
      <div className="@container divide-y divide-line">
        {roles.map((r) => (
          <RoleRow key={r.key} role={r} />
        ))}
      </div>
    </Panel>
  );
}

function RoleRow({ role: r }: { role: Role }) {
  const { fields, values, setValue, models, decisionModels, allowedOf } = useSettingsForm();
  const f = fields.find((x) => x.key === r.key);
  const kind: ModelKind = r.takes === "either" ? "text" : r.takes;
  const list = r.takes === "either" ? [...decisionModels, ...models] : r.takes === "text" ? models : r.takes === "decision" ? decisionModels : allowedOf(r.takes);
  const live = r.takes === "transcription" || r.takes === "speech" ? t("live", { context: "model" }) : undefined;
  const rows = r.takes === "either" ? eitherKind(models, decisionModels) : modelOptions(list, live);
  const none = t(NONE[kind]);
  const own = values[r.key] ?? "";
  const use = effective(values, r.key, decisionModels.map((m) => m.id));
  const chosen = list.find((m) => m.id === use.model);
  // Left unset, the decision model is the first allowed one.
  const placeholder = r.unset ? t(r.unset) : r.takes === "decision" && list.length ? t("First allowed ({{id}})", { id: list[0].id }) : list.length ? t("Select…") : none;
  const follows = !own && use.model && use.from && use.from !== "decision_models";

  return (
    <div className="grid gap-x-8 gap-y-2.5 px-5 py-4 @min-[640px]:grid-cols-[minmax(0,1fr)_minmax(0,300px)] @min-[640px]:items-start">
      <div className="min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-[13.5px] leading-5 font-medium text-ink">{t(r.label ?? LABELS[r.key] ?? r.key)}</span>
          {f ? <span className="ml-auto flex items-center gap-2 @min-[640px]:ml-0">{<SourceChips field={f} />}</span> : null}
        </div>
        {r.hint || HINTS[r.key] ? <p className="mt-0.5 max-w-[56ch] text-[12.5px] leading-[18px] text-ink-2">{t(r.hint ?? HINTS[r.key])}</p> : null}
      </div>
      <div className="min-w-0">
        <Select
          value={own}
          onChange={(v) => setValue(r.key, v)}
          disabled={f?.source === ConfigSource.ENV}
          options={r.unset ? [{ value: r.unsetValue ?? "", label: t(r.unset) }, ...rows] : rows}
          placeholder={placeholder}
          emptyLabel={none}
        />
        {follows ? (
          <p className="mt-1.5 flex min-w-0 items-center gap-1.5 text-[12px] leading-[17px] text-ink-3">
            <CornerDownRight size={12} className="shrink-0" />
            <span className="min-w-0 truncate font-mono">{use.model}</span>
            <span className="shrink-0">· {t("from {{job}}", { job: t(ROLE_NAMES[use.from] ?? use.from) })}</span>
          </p>
        ) : null}
        {r.describe?.(chosen)}
      </div>
    </div>
  );
}

// ModelsHead opens a page under Models: its kind's mark, what it is for,
// and on the right how many models of it are allowed and from whom.
export function ModelsHead({ icon: Icon, title, note, kinds = [], children }: { icon: LucideIcon; title: string; note: string; kinds?: ModelKind[]; children?: ReactNode }) {
  const { allowedOf, providers } = useSettingsForm();
  const ids = kinds.flatMap((k) => allowedOf(k).map((m) => m.id));
  const from = [...new Set(ids.map(providerOf))].filter(Boolean);
  return (
    <header className="flex items-start gap-4 pt-1 pb-2">
      <span className="flex h-10 w-10 shrink-0 items-center justify-center rounded-card bg-surface text-ink-2 shadow-card">
        <Icon size={18} />
      </span>
      <div className="min-w-0 flex-1">
        <h2 className="text-title text-ink">{title}</h2>
        <p className="mt-1 max-w-[64ch] text-[13.5px] leading-5 text-ink-2">{note}</p>
      </div>
      {children ??
        (kinds.length ? (
          <div className="flex shrink-0 flex-col items-end gap-1.5 pt-1 max-sm:hidden">
            <span className="text-label-caps text-ink-3 uppercase">{t("Allowed", { context: "count" })}</span>
            <span className="flex items-center gap-2">
              <span className="font-mono text-[20px] leading-6 text-ink tabular-nums">{ids.length}</span>
              {from.length ? (
                <span className="flex items-center gap-1">
                  {from.slice(0, 4).map((p) => (
                    <ProviderMark key={p} name={providers.find((x) => x.id === p)?.name ?? p} small />
                  ))}
                </span>
              ) : null}
            </span>
          </div>
        ) : null)}
    </header>
  );
}
