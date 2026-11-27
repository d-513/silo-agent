import { Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { Btn } from "../Btn";
import { inputClass } from "../Field";
import { Spinner } from "../Feedback";
import { Select } from "../Select";
import { Tip } from "../Tip";
import { KindModelsDialog } from "./KindModelsDialog";
import { Band, Block, Empty, Group } from "./kit";
import { LiveChip } from "./LiveChip";
import { ProviderMark } from "./ProviderMap";
import { jobsUsing, providerOf } from "./providerJobs";
import { type ModelKind } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";
import { msg, t } from "../i18n.ts";

// What each allowlist is for, in the panel's own words.
export const ALLOWLISTS: Record<ModelKind, { title: string; note: string; none: string; example: string }> = {
  text: {
    title: msg("Allowed text models"),
    note: msg("Models that write. A chat's model menu and a Bot's default offer these, and a Bot may switch to any of them."),
    none: msg("No text models allowed yet."),
    example: msg("model name, e.g. gpt-5.6-luna"),
  },
  decision: {
    title: msg("Allowed decision models"),
    note: msg("Models that answer typed questions (yes or no, a choice, a score) with probabilities instead of text, in a fraction of a second. The Bot's decide tool may name any of these; a chat cannot use them."),
    none: msg("No decision models allowed yet. Without one the Bot has no decide tool."),
    example: msg("model name, e.g. jev-latest"),
  },
  transcription: {
    title: msg("Allowed speech-to-text models"),
    note: msg("Models that turn speech into text: dictation, the transcribe tool and what you say on a call."),
    none: msg("No speech-to-text models allowed yet."),
    example: msg("model name, e.g. openai/whisper-1"),
  },
  speech: {
    title: msg("Allowed speech models"),
    note: msg("Models that read text aloud: Read aloud and a Bot's voice on a call. Each Bot picks one of these and a voice."),
    none: msg("No speech models allowed yet."),
    example: msg("model name, e.g. hexgrad/kokoro-82m"),
  },
  embedding: {
    title: msg("Allowed embedding models"),
    note: msg("Models that turn text into vectors for search by meaning. Only the Embeddings job uses one."),
    none: msg("No embedding models allowed yet."),
    example: msg("model name, e.g. openai/text-embedding-3-small"),
  },
  image: {
    title: msg("Allowed image models"),
    note: msg("Models that make and edit pictures. A Bot's generate_image tool can name any of these."),
    none: msg("No image models allowed yet. Without one the Bot has no generate_image tool."),
    example: msg("model name, e.g. flux-3-image"),
  },
};

// One kind's allowlist (the text models every chat's model menu offers, the
// decision models, …). A change here saves at once, on its own. A model a
// job names is on its list even when silo.yaml does not list it; it shows
// as in use, and leaves the list only when the job names another.
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
  // The models every provider that is set up lists for this kind.
  const [listing, setListing] = useState(false);

  function add() {
    if (!pick || !name.trim() || known) return;
    setName("");
    void setModel(id, true, kind);
  }

  return (
    <Group
      title={t(copy.title)}
      note={t(copy.note)}
      action={
        <Btn size="sm" kind="primary" aria-haspopup="dialog" onClick={() => setListing(true)}>
          <Plus size={14} /> {t("Add models")}
        </Btn>
      }
    >
      {form.err ? (
        <Block>
          <p className="text-small leading-5 text-vermilion">{form.err}</p>
        </Block>
      ) : null}
      {models.length === 0 ? <Empty>{t(copy.none)}</Empty> : null}
      {models.map((m) => {
        // The jobs that name this model, so a removal is never a surprise.
        const jobs = jobsUsing(form.values, m.id);
        const provider = providerOf(m.id);
        return (
          <div key={m.id} className="flex min-h-12 items-center gap-3 py-1.5 pr-3 pl-5 text-small">
            <ProviderMark id={provider} name={form.providers.find((p) => p.id === provider)?.name ?? provider} small />
            <span className="min-w-0 flex-1">
              <span className="block truncate font-mono leading-5 text-ink">{m.id}</span>
              {jobs.length ? <span className="block truncate text-label leading-4 text-ink-3">{t("Used for {{jobs}}", { jobs: jobs.map((j) => t(j.label)).join(", ") })}</span> : null}
            </span>
            <LiveChip kind={kind} live={m.live} words={m.words} endOfTurn={m.endOfTurn} />
            {busyModels.has(m.id) ? (
              <span className="flex w-8 justify-center">
                <Spinner size={13} />
              </span>
            ) : kind !== "text" && kind !== "decision" && !listed.includes(m.id) ? (
              <Tip content={t("A job uses this model, which keeps it on the list. Give the job another model to take it off.")}>
                <span className="pr-2 text-label text-ink-3">{t("In use", { context: "model" })}</span>
              </Tip>
            ) : (
              <Btn kind="ghost" size="sm" iconOnly title={t("Remove {{id}}", { id: m.id })} aria-label={t("Remove {{id}}", { id: m.id })} className="hover:text-vermilion" onClick={() => void setModel(m.id, false, kind)}>
                <Trash2 size={14} />
              </Btn>
            )}
          </div>
        );
      })}
      <Band className="flex flex-wrap items-center gap-2">
        <span className="mr-1 text-meta text-ink-2">{t("Or add one by its id")}</span>
        <div className="w-[150px]">
          <Select value={pick} onChange={setProvider} ariaLabel={t("Provider")} options={providers.map((p) => ({ value: p.id, label: p.id }))} />
        </div>
        <span className="text-ink-3">/</span>
        <input
          className={`${inputClass} min-w-[180px] flex-1 font-mono text-small`}
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
        <Btn onClick={add} disabled={!name.trim() || known}>
          {t("Add")}
        </Btn>
      </Band>
      {listing ? <KindModelsDialog kind={kind} onClose={() => setListing(false)} /> : null}
    </Group>
  );
}
