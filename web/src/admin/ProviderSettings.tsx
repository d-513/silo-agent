import { useQuery } from "@connectrpc/connect-query";
import { Check, Plus, RefreshCw, X } from "lucide-react";
import { useState } from "react";
import { Btn } from "../Btn";
import { Segmented } from "../connectors/form/atoms";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { ConfigSource, UI, type Provider, type ProviderModel } from "../gen/silo/v1/ui_pb";
import { SearchBox } from "../SearchBox";
import { Tip } from "../Tip";
import { FieldGroup } from "./FieldGroup";
import { bareModel, filterModels, filterWord, fmtContext, kindOf, kindsIn, kindWord, removeTitle, type ModelKind } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";
import { locale, t } from "../i18n.ts";

export function ProviderSettings() {
  const { providers } = useSettingsForm();
  return (
    <>
      {providers.map((p) => (
        <ProviderPanel key={p.id} provider={p} />
      ))}
    </>
  );
}

// One provider: its settings (saved with the form), and on request the models
// its saved key can call, sorted into text and decision models, each one press
// from the allowlist of its kind.
function ProviderPanel({ provider }: { provider: Provider }) {
  const { fields, values } = useSettingsForm();
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
    <FieldGroup
      title={provider.name}
      note={provider.description || undefined}
      rows={rows}
      action={unsaved && !open ? <Tip content={t("Save your changes first. The list is asked with {{name}}'s saved settings.", { name: provider.name })}>{button}</Tip> : button}
    >
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
    </FieldGroup>
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
    return <p className="text-[13px] text-ink-2">{t("{{name}} lists no models for this key. Add one by id under Models.", { name: provider.name })}</p>;
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
