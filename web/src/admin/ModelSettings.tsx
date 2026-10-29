import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { Btn } from "../Btn";
import { Field, Panel, inputClass } from "../Field";
import { Spinner } from "../Feedback";
import { Select } from "../Select";
import { ConfigSource } from "../gen/silo/v1/ui_pb";
import { modelOptions } from "../modelOptions";
import { SourceChips } from "./FieldRow";
import { HINTS, LABELS } from "./fields";
import { LiveChip } from "./LiveChip";
import { eitherKind, type ModelKind } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";
import { msg, t } from "../i18n.ts";
import { Trans } from "../Trans.tsx";

// The jobs a model can be given, and the kind of model each takes: a text
// model writes, a decision model answers typed questions with probabilities.
// Only auto-approval and the output filter take either. All but the two
// defaults may be left to follow another role.
const ROLES: { key: string; takes?: ModelKind | "either"; unset?: string; hint?: string }[] = [
  { key: "model" },
  { key: "model_title", unset: msg("Same as default") },
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
  { key: "model_subagent", unset: msg("Same as the lead"), hint: msg("Default for subagents a lead starts. A cheaper model keeps parallel work affordable; the lead can still pick another.") },
  { key: "model_memory", unset: msg("Same as title model"), hint: msg("Reads chats once they go quiet and saves the facts the Bot missed. A cheap model is enough.") },
  { key: "model_decision", takes: "decision", hint: msg("What the Bot's decide tool asks unless it names another allowed decision model.") },
];

export function ModelSettings() {
  const { fields, values, setValue, models, decisionModels, allowedOf } = useSettingsForm();
  const field = (key: string) => fields.find((f) => f.key === key);
  const embedding = field("embedding_model");

  // A role's picker offers the allowed models of the kind it takes.
  function picker(r: (typeof ROLES)[number]) {
    if (r.takes === "decision") {
      const none = t("No decision models allowed yet");
      // Left unset, the first allowed decision model is the default.
      return { options: modelOptions(decisionModels), placeholder: decisionModels.length ? t("First allowed ({{id}})", { id: decisionModels[0].id }) : none, emptyLabel: none };
    }
    const none = t("No text models allowed yet");
    const rows = r.takes === "either" ? eitherKind(models, decisionModels) : modelOptions(models);
    if (r.unset) return { options: [{ value: "", label: t(r.unset) }, ...rows], placeholder: t(r.unset), emptyLabel: none };
    return { options: rows, placeholder: models.length ? t("Select…") : none, emptyLabel: none };
  }

  return (
    <>
      <Panel
        title={t("Default models")}
        note={t("Which allowed model does which job. A Bot or a chat can pick another text model for itself; the decision model is what the Bot's decide tool uses.")}
      >
        <div className="grid gap-x-4 gap-y-5 sm:grid-cols-2">
          {ROLES.map((r) => {
            const f = field(r.key);
            return (
              <Field key={r.key} label={t(LABELS[r.key])} hint={r.hint ? t(r.hint) : undefined} headerRight={f ? <SourceChips field={f} /> : undefined}>
                <Select value={values[r.key] ?? ""} onChange={(v) => setValue(r.key, v)} disabled={f?.source === ConfigSource.ENV} {...picker(r)} />
              </Field>
            );
          })}
        </div>
      </Panel>

      <AllowedModels kind="text" />
      <AllowedModels kind="decision" />

      <Panel title={t("Embeddings")} note={t("Memories and indexed documents are searched by meaning with this model. Speech models live under Voice.")}>
        <Field label={t(LABELS.embedding_model)} hint={t(HINTS.embedding_model)} headerRight={embedding ? <SourceChips field={embedding} /> : undefined}>
          <Select
            value={values.embedding_model ?? ""}
            onChange={(v) => setValue("embedding_model", v)}
            disabled={embedding?.source === ConfigSource.ENV}
            options={modelOptions(allowedOf("embedding"))}
            placeholder={t("Select…")}
            emptyLabel={t("No embedding models allowed yet")}
          />
        </Field>
      </Panel>
      <AllowedModels kind="embedding" />
    </>
  );
}

// What each allowlist is for, in the panel's own words.
export const ALLOWLISTS: Record<ModelKind, { title: string; note: string; none: string; example: string }> = {
  text: {
    title: msg("Allowed text models"),
    note: msg("Models that write: the ones a chat, a Bot and the pickers above can use. A change here is saved at once."),
    none: msg("No text models allowed yet."),
    example: msg("model name, e.g. gpt-5.6-luna"),
  },
  decision: {
    title: msg("Allowed decision models"),
    note: msg("Models that answer typed questions (yes or no, a choice, a score) with probabilities instead of text, in a fraction of a second. The Bot's decide tool and auto-approval can use them; a chat cannot. Saved at once."),
    none: msg("No decision models allowed yet. Without one the Bot has no decide tool."),
    example: msg("model name, e.g. jev-latest"),
  },
  transcription: {
    title: msg("Allowed speech-to-text models"),
    note: msg("Models that turn speech into text: dictation, the transcribe tool and what you say on a call. Saved at once."),
    none: msg("No speech-to-text models allowed yet."),
    example: msg("model name, e.g. openai/whisper-1"),
  },
  speech: {
    title: msg("Allowed speech models"),
    note: msg("Models that read text aloud: Read aloud and a Bot's voice on a call. Each Bot picks one of these and a voice. Saved at once."),
    none: msg("No speech models allowed yet."),
    example: msg("model name, e.g. hexgrad/kokoro-82m"),
  },
  embedding: {
    title: msg("Allowed embedding models"),
    note: msg("Models that turn text into vectors for search by meaning. Saved at once."),
    none: msg("No embedding models allowed yet."),
    example: msg("model name, e.g. openai/text-embedding-3-small"),
  },
};

// One kind's allowlist (the text models every chat's model menu offers, the
// decision models, …). A change here saves at once, on its own. A model a
// role names is on its list even when silo.yaml does not list it; it shows
// as in use, and leaves the list only when the role names another.
export function AllowedModels({ kind }: { kind: ModelKind }) {
  const form = useSettingsForm();
  const { setModel, busyModels } = form;
  const models = form.allowedOf(kind);
  const listed = form.listedOf(kind);
  // Only the providers that have models of this kind can be typed in.
  const providers = form.providers.filter((p) => p.kinds.includes(kind));
  const copy = ALLOWLISTS[kind];
  const [provider, setProvider] = useState("");
  const [name, setName] = useState("");
  const pick = provider || providers[0]?.id || "";
  const id = `${pick}/${name.trim()}`.replace(/\/+$/, "");
  const known = models.some((m) => m.id === id);

  function add() {
    if (!pick || !name.trim() || known) return;
    setName("");
    void setModel(id, true, kind);
  }

  return (
    <Panel title={t(copy.title)} note={t(copy.note)} padded={false}>
      {models.length === 0 ? (
        <p className="px-5 py-4 text-[13px] text-ink-2">{t(copy.none)}</p>
      ) : (
        <ul className="divide-y divide-line">
          {models.map((m) => (
            <li key={m.id} className="flex h-11 items-center gap-3 pr-3 pl-5 text-[13px]">
              <span className="min-w-0 flex-1 truncate font-mono text-ink">{m.id}</span>
              <LiveChip kind={kind} live={m.live} words={m.words} endOfTurn={m.endOfTurn} />
              {busyModels.has(m.id) ? (
                <span className="flex w-8 justify-center">
                  <Spinner size={13} />
                </span>
              ) : kind !== "text" && kind !== "decision" && !listed.includes(m.id) ? (
                <span className="pr-2 text-[12px] text-ink-3">{t("In use", { context: "model" })}</span>
              ) : (
                <Btn kind="ghost" size="sm" iconOnly title={t("Remove {{id}}", { id: m.id })} aria-label={t("Remove {{id}}", { id: m.id })} className="hover:text-vermilion" onClick={() => void setModel(m.id, false, kind)}>
                  <Trash2 size={14} />
                </Btn>
              )}
            </li>
          ))}
        </ul>
      )}
      <div className="bg-well px-5 py-4 shadow-[inset_0_1px_0_var(--color-line)]">
        <p className="mb-3 text-[12.5px] leading-[18px] text-ink-2">
          <Trans i18nKey="Pick from a provider's own list under <link>Providers</link>, or add a model by id." components={{ link: <Link to="/admin/settings/$section" params={{ section: "providers" }} className="text-cobalt hover:text-cobalt-deep" /> }} />
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <div className="w-[150px]">
            <Select value={pick} onChange={setProvider} ariaLabel={t("Provider")} options={providers.map((p) => ({ value: p.id, label: p.id }))} />
          </div>
          <span className="text-ink-3">/</span>
          <input
            className={`${inputClass} min-w-[180px] flex-1 font-mono text-[13px]`}
            placeholder={t(copy.example)}
            aria-label={t("Model name")}
            autoComplete="off"
            spellCheck={false}
            value={name}
            onChange={(e) => setName(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                add();
              }
            }}
          />
          <Btn kind="primary" onClick={add} disabled={!name.trim() || known}>
            <Plus size={14} /> {t("Add")}
          </Btn>
        </div>
      </div>
    </Panel>
  );
}
