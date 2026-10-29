import { useQuery } from "@connectrpc/connect-query";
import { AudioLines, Binary, Check, ChevronRight, Ear, Plug, Plus, RefreshCw, Split, Type, Volume2, X } from "lucide-react";
import { useEffect, useState } from "react";
import { useSearch } from "@tanstack/react-router";
import { Btn } from "../Btn";
import { Segmented } from "../connectors/form/atoms";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell, Panel, SkeletonRows } from "../Field";
import { ConfigSource, UI, type Provider, type ProviderModel } from "../gen/silo/v1/ui_pb";
import { SearchBox } from "../SearchBox";
import { Tip, TipTitle } from "../Tip";
import { FieldRow } from "./FieldRow";
import { capOf, isReady, providersInUse, resolveJobs, type Cap } from "./providerMap";
import { ProviderMark, Status } from "./ProviderMap";
import { ModelsHead } from "./roles";
import { bareModel, filterModels, filterWord, fmtContext, kindOf, kindsIn, kindWord, removeTitle, type ModelKind } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";
import { LiveChip } from "./LiveChip";
import { locale, msg, t } from "../i18n.ts";

export function ProviderSettings() {
  const form = useSettingsForm();
  const { providers, values, fields, decisionModels, allowedOf } = form;
  // The map sends a provider to open (?provider=…).
  const { provider: asked } = useSearch({ from: "/_authed/admin/settings/models/$page" });
  const [open, setOpen] = useState(asked ?? "");
  const jobs = resolveJobs(values, decisionModels.map((m) => m.id));
  const inUse = providersInUse(jobs);
  const ready = (id: string) => isReady(id, fields, values, ConfigSource.ENV);
  // In use first (in the map's order), then the ones set up, then the rest.
  const rank = (p: Provider) => (inUse.includes(p.id) ? inUse.indexOf(p.id) : ready(p.id) ? 100 : 200);
  const sorted = [...providers].sort((a, b) => rank(a) - rank(b));
  const allowed = Object.fromEntries(CAP_KINDS.map((k) => [k.kind, allowedOf(k.kind).map((m) => m.id)]));
  const setUp = providers.filter((p) => ready(p.id)).length;

  useEffect(() => {
    if (!asked) return;
    setOpen(asked);
    requestAnimationFrame(() => document.getElementById(`provider-${asked}`)?.scrollIntoView({ block: "start", behavior: "smooth" }));
  }, [asked]);

  return (
    <>
      <ModelsHead icon={Plug} title={t("Providers")} note={t("Who serves your models. Each row says what a provider can do and how many of its models you allow; open one for its key and its own list of models.")}>
        <div className="flex shrink-0 flex-col items-end gap-1.5 pt-1 max-sm:hidden">
          <span className="text-label-caps text-ink-3 uppercase">{t("Set up")}</span>
          <span className="font-mono text-[20px] leading-6 text-ink tabular-nums">
            {setUp}
            <span className="text-ink-3">/{providers.length}</span>
          </span>
        </div>
      </ModelsHead>
      <Panel padded={false}>
        <div className="@container">
          <div className="hidden h-9 items-center gap-2 pr-4 pl-5 text-[11.5px] font-medium tracking-[0.02em] text-ink-3 uppercase @min-[640px]:grid @min-[640px]:grid-cols-[minmax(0,1fr)_repeat(5,3.25rem)_6.5rem]">
            <span>{t("Provider")}</span>
            {CAP_KINDS.map((k) => (
              <Tip key={k.kind} content={t(k.label)}>
                <span className="flex justify-center" aria-label={t(k.label)}>
                  <k.icon size={14} />
                </span>
              </Tip>
            ))}
            <span className="text-right">{t("Status")}</span>
          </div>
          <ul className="divide-y divide-line shadow-[inset_0_1px_0_var(--color-line)]">
            {sorted.map((p) => (
              <ProviderRow key={p.id} provider={p} open={open === p.id} ready={ready(p.id)} inUse={inUse.includes(p.id)} allowed={allowed} onToggle={() => setOpen(open === p.id ? "" : p.id)} />
            ))}
          </ul>
          <Legend />
        </div>
      </Panel>
    </>
  );
}

// The kinds a provider is read for, as the grid's columns.
const CAP_KINDS: { kind: ModelKind; label: string; icon: typeof Type }[] = [
  { kind: "text", label: msg("Text: chats, the talker and the text roles"), icon: Type },
  { kind: "decision", label: msg("Decisions: typed answers with probabilities"), icon: Split },
  { kind: "transcription", label: msg("Speech-to-text: dictation and what you say on a call"), icon: Ear },
  { kind: "speech", label: msg("Speech: Read aloud and a Bot's voice on a call"), icon: Volume2 },
  { kind: "embedding", label: msg("Embeddings: memories and documents searched by meaning"), icon: Binary },
];

// One provider: a row of what it can do, that opens into its settings.
function ProviderRow({ provider, open, ready, inUse, allowed, onToggle }: { provider: Provider; open: boolean; ready: boolean; inUse: boolean; allowed: Record<string, string[]>; onToggle: () => void }) {
  const { values } = useSettingsForm();
  const caps = CAP_KINDS.map((k) => ({ k, cap: capOf(provider, k.kind, allowed[k.kind] ?? [], values) }));
  return (
    <li id={`provider-${provider.id}`} className="scroll-mt-4">
      <button
        type="button"
        aria-expanded={open}
        onClick={onToggle}
        className={`relative grid w-full grid-cols-[minmax(0,1fr)_auto] items-center gap-x-2 gap-y-2 py-3 pr-4 pl-5 text-left transition-colors duration-[160ms] ease-quiet hover:bg-well @min-[640px]:grid-cols-[minmax(0,1fr)_repeat(5,3.25rem)_6.5rem] ${open ? "bg-well" : ""}`}
      >
        {open ? <span aria-hidden className="absolute top-3 bottom-3 left-0 w-[2px] rounded-full bg-cobalt" /> : null}
        <span className="flex min-w-0 items-center gap-3">
          <ChevronRight size={14} className={`shrink-0 text-ink-3 transition-transform duration-[200ms] ease-quiet ${open ? "rotate-90" : ""}`} />
          <ProviderMark name={provider.name} />
          <span className="min-w-0">
            <span className="flex items-center gap-2 text-[13.5px] leading-[18px] font-medium text-ink">
              {provider.name}
              {inUse ? <span className="text-[11.5px] font-normal text-ink-3">{t("in use", { context: "provider" })}</span> : null}
            </span>
            <span className="block truncate text-[12px] leading-[17px] text-ink-3">{provider.description}</span>
          </span>
        </span>
        {/* Wide: one column per kind. Narrow: the same cells in a strip. */}
        <span className="col-span-2 flex flex-wrap gap-x-4 gap-y-1 pl-[3.25rem] @min-[640px]:contents">
          {caps.map(({ k, cap }) => (
            <CapCell key={k.kind} provider={provider.name} kind={k} cap={cap} />
          ))}
        </span>
        <span className="row-start-1 flex justify-end @min-[640px]:col-start-7">
          <Status ok={ready} />
        </span>
      </button>
      <div className={`grid transition-[grid-template-rows] duration-[320ms] ease-quiet ${open ? "grid-rows-[1fr]" : "grid-rows-[0fr]"}`}>
        <div className="min-h-0 overflow-hidden">{open ? <ProviderDetail provider={provider} /> : null}</div>
      </div>
    </li>
  );
}

// CapCell is one kind of one provider: nothing (a hairline), available (an
// open square), allowed (a filled square and how many), live (a waveform).
function CapCell({ provider, kind, cap }: { provider: string; kind: (typeof CAP_KINDS)[number]; cap: Cap }) {
  const narrowLabel = <span className="text-[11.5px] text-ink-3 @min-[640px]:hidden">{kindWord(kind.kind)}</span>;
  if (!cap.served) {
    return (
      <span className="hidden items-center justify-center @min-[640px]:flex" aria-label={t("{{provider}} has no models of this kind", { provider })}>
        <span className="h-px w-2.5 bg-line-strong" />
      </span>
    );
  }
  const words = [cap.live ? t("Some models work live.") : t("One request at a time."), cap.allowed ? t("{{count}} allowed", { count: cap.allowed }) : t("None allowed yet.")].join(" ");
  return (
    <Tip
      content={
        <>
          <TipTitle>{`${provider} · ${kindWord(kind.kind)}`}</TipTitle>
          <p className="mt-1">{words}</p>
        </>
      }
    >
      <span className="flex items-center gap-1.5 @min-[640px]:justify-center">
        {cap.live ? (
          <AudioLines size={13} className={cap.allowed ? "text-ink" : "text-ink-3"} aria-label={t("Live")} />
        ) : (
          <span aria-hidden className={`h-[7px] w-[7px] rounded-[2px] ${cap.allowed ? "bg-ink" : "shadow-[inset_0_0_0_1.25px_var(--color-ink-3)]"}`} />
        )}
        {cap.allowed ? <span className="font-mono text-[11.5px] text-ink-2 tabular-nums">{cap.allowed}</span> : null}
        {narrowLabel}
      </span>
    </Tip>
  );
}

// Legend is the grid's key, on the well band under it.
function Legend() {
  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-1.5 bg-well px-5 py-3 text-[12px] text-ink-3 shadow-[inset_0_1px_0_var(--color-line)]">
      <span className="flex items-center gap-1.5">
        <span aria-hidden className="h-[7px] w-[7px] rounded-[2px] bg-ink" />
        {t("Allowed, with how many")}
      </span>
      <span className="flex items-center gap-1.5">
        <span aria-hidden className="h-[7px] w-[7px] rounded-[2px] shadow-[inset_0_0_0_1.25px_var(--color-ink-3)]" />
        {t("Available")}
      </span>
      <span className="flex items-center gap-1.5">
        <AudioLines size={13} />
        {t("Live: streams while you talk")}
      </span>
      <span className="flex items-center gap-1.5">
        <span aria-hidden className="h-px w-2.5 bg-line-strong" />
        {t("Not offered")}
      </span>
    </div>
  );
}

// ProviderDetail is an open provider: its settings (saved with the form),
// and on request the models its saved key can call, each one press from the
// allowlist of its kind.
function ProviderDetail({ provider }: { provider: Provider }) {
  const { fields, values, engines, providers, setValue } = useSettingsForm();
  const rows = fields.filter((f) => f.key.startsWith(`providers.${provider.id}.`));
  // The list is asked with what is saved, so an edited key must be saved first.
  const unsaved = rows.some((f) => f.source !== ConfigSource.ENV && (values[f.key] ?? "") !== f.value);
  const [open, setOpen] = useState(false);
  // Asked only once opened; kept for the visit so reopening is instant.
  const list = useQuery(UI.method.listProviderModels, { provider: provider.id }, { enabled: open, staleTime: Infinity, retry: false });

  const button = (
    <Btn
      size="sm"
      aria-expanded={open}
      aria-disabled={unsaved && !open ? true : undefined}
      className={unsaved && !open ? "cursor-not-allowed opacity-50" : ""}
      onClick={() => {
        if (unsaved && !open) return;
        setOpen(!open);
      }}
    >
      {open ? t("Hide models") : t("List models")}
    </Btn>
  );

  return (
    <div className="bg-surface shadow-[inset_0_1px_0_var(--color-line)]">
      <div className="flex items-start gap-3 px-5 pt-4 pb-1">
        <p className="min-w-0 flex-1 text-[13px] leading-5 text-ink-2">{provider.description}</p>
        {unsaved && !open ? <Tip content={t("Save your changes first. The list is asked with {{name}}'s saved settings.", { name: provider.name })}>{button}</Tip> : button}
      </div>
      <div className="divide-y divide-line-strong">
        {rows.map((f) => (
          <div key={f.key} className="px-5 py-3.5">
            <FieldRow field={f} value={values[f.key] ?? ""} engines={engines} providers={providers} onChange={(v) => setValue(f.key, v)} />
          </div>
        ))}
      </div>
      {open ? (
        <div className="bg-well px-5 py-4 shadow-[inset_0_1px_0_var(--color-line)]">
          {list.isPending ? (
            <SkeletonRows rows={4} height={36} />
          ) : list.error ? (
            <div className="flex items-start gap-3">
              <ErrorWell className="min-w-0 flex-1">{fail(list.error)}</ErrorWell>
              <Btn size="sm" onClick={() => void list.refetch()}>
                {t("Try again")}
              </Btn>
            </div>
          ) : (
            <ModelList provider={provider} models={list.data.models} refreshing={list.isFetching} onRefresh={() => void list.refetch()} />
          )}
        </div>
      ) : null}
    </div>
  );
}

function ModelList({ provider, models, refreshing, onRefresh }: { provider: Provider; models: ProviderModel[]; refreshing: boolean; onRefresh: () => void }) {
  const { allowedOf, setModel, busyModels } = useSettingsForm();
  const [query, setQuery] = useState("");
  // A provider with several kinds shows one at a time; text is what most are after.
  const kinds = kindsIn(models);
  const [picked, setKind] = useState<ModelKind>("text");
  const kind = kinds.includes(picked) ? picked : (kinds[0] ?? "text");
  const ofKind = models.filter((m) => kindOf(m) === kind);
  const shown = filterModels(ofKind, query);
  const allowed = allowedOf(kind);
  const isAllowed = (id: string) => allowed.some((m) => m.id === id);

  if (models.length === 0) {
    return <p className="text-[13px] text-ink-2">{t("{{name}} lists no models for this key. Add one by id on its kind's page under Models.", { name: provider.name })}</p>;
  }
  return (
    <>
      {kinds.length > 1 ? (
        <div className="mb-3 max-w-full overflow-x-auto text-[13px]">
          <Segmented value={kind} onChange={(v) => setKind(v as ModelKind)} options={kinds.map((k) => ({ id: k, label: kindWord(k) }))} />
        </div>
      ) : null}
      <div className="mb-3 flex items-center gap-2">
        <div className="min-w-0 flex-1">
          <SearchBox value={query} onChange={setQuery} placeholder={filterWord(kind, ofKind.length)} searching={false} />
        </div>
        <Btn kind="ghost" size="sm" iconOnly title={t("Ask again")} aria-label={t("Ask again")} disabled={refreshing} onClick={onRefresh}>
          {refreshing ? <Spinner size={13} /> : <RefreshCw size={14} />}
        </Btn>
      </div>
      {shown.length === 0 ? (
        <p className="py-2 text-[13px] text-ink-2">{t("No model matches “{{search}}”.", { search: query.trim() })}</p>
      ) : (
        <ul className="max-h-[396px] divide-y divide-line overflow-y-auto overscroll-contain rounded-control bg-surface shadow-card">
          {shown.map((m) => {
            const on = isAllowed(m.id);
            const busy = busyModels.has(m.id);
            // A speech model says how many voices it has; the rest their context.
            const context = kind === "speech" ? (m.voices.length ? t("{{count}} voices", { count: m.voices.length }) : "") : fmtContext(m.contextWindow);
            return (
              <li key={m.id} className="flex h-11 items-center gap-3 pr-1.5 pl-3 text-[13px]">
                <span className="max-w-full shrink-0 truncate font-mono text-ink max-sm:min-w-0 max-sm:flex-1 max-sm:shrink">{bareModel(m.id)}</span>
                <LiveChip kind={kind} live={m.live} words={m.words} endOfTurn={m.endOfTurn} />
                <span className="min-w-0 flex-1 truncate text-ink-3 max-sm:hidden">{m.name}</span>
                {context ? (
                  <span className="shrink-0 font-mono text-[12px] text-ink-3 tabular-nums" title={kind === "speech" ? undefined : t("{{tokens}} tokens of context", { tokens: m.contextWindow.toLocaleString(locale()) })}>
                    {context}
                  </span>
                ) : null}
                {/* One slot, one width: Add becomes Allowed in place. */}
                <span className="flex w-[98px] shrink-0 items-center justify-end">
                  {busy ? (
                    <span className="flex h-8 w-8 items-center justify-center">
                      <Spinner size={13} />
                    </span>
                  ) : on ? (
                    <>
                      <span className="flex items-center gap-1 text-[12.5px] font-medium text-emerald">
                        <Check size={14} />{t("Allowed")}
                      </span>
                      <Btn kind="ghost" size="sm" iconOnly title={removeTitle(kind)} aria-label={t("Remove {{id}}", { id: m.id })} onClick={() => void setModel(m.id, false, kind)}>
                        <X size={14} />
                      </Btn>
                    </>
                  ) : (
                    <Btn size="sm" aria-label={t("Add {{id}}", { id: m.id })} onClick={() => void setModel(m.id, true, kind)}>
                      <Plus size={14} /> {t("Add")}
                    </Btn>
                  )}
                </span>
              </li>
            );
          })}
        </ul>
      )}
    </>
  );
}
