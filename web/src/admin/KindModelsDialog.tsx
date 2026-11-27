import { Check, Plus, RefreshCw, X } from "lucide-react";
import { useState } from "react";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { locale, t } from "../i18n.ts";
import { Modal } from "../Modal";
import { SearchBox } from "../SearchBox";
import { FilterChip } from "../FilterChip";
import { LiveChip } from "./LiveChip";
import { ProviderMark } from "./ProviderMap";
import { filterModels, filterWord, fmtContext, kindWord, removeTitle, type ModelKind } from "./providerModels";
import { useProviderLists } from "./useProviderLists";
import { useSettingsForm } from "./useAdminSettings";

// KindModelsDialog lists one kind's models from every provider that is set
// up, each under its full provider/model id, so a model is found without
// knowing who serves it. Each provider is asked on its own: a slow or failing
// one never holds up the rest.
export function KindModelsDialog({ kind, onClose }: { kind: ModelKind; onClose: () => void }) {
  const form = useSettingsForm();
  const { allowedOf, setModel, busyModels } = form;
  const lists = useProviderLists([kind]);
  const { asked, pending, fetching, failed } = lists;
  const [query, setQuery] = useState("");
  // "" is every provider.
  const [only, setOnly] = useState("");

  const rows = lists.rows.filter((m) => !only || m.provider.id === only);
  const shown = filterModels(rows, query);
  const allowed = allowedOf(kind);
  const title = t("{{kind}} models from your providers", { kind: kindWord(kind) });

  return (
    <Modal onClose={onClose} size="wide" rise dismissOnBackdrop label={title}>
      <div className="mb-4 pr-8">
        <h2 className="truncate text-heading text-ink">{title}</h2>
        <p className="text-meta text-ink-3">{t("What the providers you have set up can serve, each under its full id. Add a model to allow it.")}</p>
      </div>
      {asked.length === 0 ? (
        <p className="text-small text-ink-2">{t("No provider that serves these models is set up yet. Give one its key under Providers.")}</p>
      ) : (
        <>
          <div className="mb-3 flex items-center gap-2">
            <div className="min-w-0 flex-1">
              <SearchBox value={query} onChange={setQuery} placeholder={filterWord(kind, rows.length)} searching={false} />
            </div>
            <Btn kind="ghost" size="sm" iconOnly title={t("Ask again")} aria-label={t("Ask again")} disabled={fetching} onClick={lists.refetch}>
              {fetching ? <Spinner size={13} /> : <RefreshCw size={14} />}
            </Btn>
          </div>
          {asked.length > 1 ? (
            <div className="mb-3 flex flex-wrap gap-1.5">
              <FilterChip on={only === ""} onClick={() => setOnly("")}>
                {t("All providers")}
              </FilterChip>
              {asked.map((p) => (
                <FilterChip key={p.id} on={only === p.id} onClick={() => setOnly(only === p.id ? "" : p.id)}>
                  {p.name}
                </FilterChip>
              ))}
            </div>
          ) : null}
          {form.err ? <ErrorWell className="mb-3">{form.err}</ErrorWell> : null}
          {failed.map((f) => (
            <ErrorWell key={f.provider.id} className="mb-3">
              {f.provider.name}: {fail(f.error)}
            </ErrorWell>
          ))}
          {shown.length === 0 ? (
            pending ? (
              <SkeletonRows rows={6} height={36} />
            ) : (
              <p className="py-2 text-small text-ink-2">{query.trim() ? t("No model matches “{{search}}”.", { search: query.trim() }) : t("These providers list no such models. Add one by id instead.")}</p>
            )
          ) : (
            <ul className="max-h-[min(60vh,560px)] divide-y divide-line overflow-y-auto overscroll-contain rounded-control bg-surface shadow-card">
              {shown.map((m) => {
                const on = allowed.some((a) => a.id === m.id);
                const context = kind === "speech" ? (m.voices.length ? t("{{count}} voices", { count: m.voices.length }) : "") : fmtContext(m.contextWindow);
                return (
                  <li key={m.id} className="flex min-h-11 items-center gap-3 py-1 pr-1.5 pl-3 text-small">
                    <ProviderMark id={m.provider.id} name={m.provider.name} small />
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-mono text-ink">{m.id}</span>
                      {m.fastOf || m.name ? <span className="block truncate text-label leading-4 text-ink-3">{m.fastOf ? t("Fast mode: faster output at the provider's premium price") : m.name}</span> : null}
                    </span>
                    <LiveChip kind={kind} live={m.live} words={m.words} endOfTurn={m.endOfTurn} />
                    {context ? (
                      <span className="shrink-0 font-mono text-label text-ink-3 tabular-nums max-sm:hidden" title={kind === "speech" ? undefined : t("{{tokens}} tokens of context", { tokens: m.contextWindow.toLocaleString(locale()) })}>
                        {context}
                      </span>
                    ) : null}
                    <span className="flex w-[98px] shrink-0 items-center justify-end">
                      {busyModels.has(m.id) ? (
                        <span className="flex h-8 w-8 items-center justify-center">
                          <Spinner size={13} />
                        </span>
                      ) : on ? (
                        <>
                          <span className="flex items-center gap-1 text-meta font-medium text-emerald">
                            <Check size={14} />
                            {t("Allowed")}
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
              {pending ? (
                <li className="flex h-11 items-center gap-2 pl-3 text-meta text-ink-3">
                  <Spinner size={13} /> {t("Still asking {{count}} providers", { count: pending })}
                </li>
              ) : null}
            </ul>
          )}
        </>
      )}
    </Modal>
  );
}
