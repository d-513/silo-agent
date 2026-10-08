import { useQuery } from "@connectrpc/connect-query";
import { Check, Plus, RefreshCw, X } from "lucide-react";
import { useState } from "react";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { Spinner } from "../Feedback";
import { ErrorWell, SkeletonRows } from "../Field";
import { ConfigSource, UI, type Provider, type ProviderModel } from "../gen/silo/v1/ui_pb";
import { SearchBox } from "../SearchBox";
import { Tip } from "../Tip";
import { FieldGroup } from "./FieldGroup";
import { bareModel, filterModels, fmtContext } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";

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
// its saved key can call, each one press from the allowlist.
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
      {open ? "Hide models" : "List models"}
    </Btn>
  );

  return (
    <FieldGroup
      title={provider.name}
      note={provider.description || undefined}
      rows={rows}
      action={unsaved && !open ? <Tip content={`Save your changes first. The list is asked with ${provider.name}'s saved settings.`}>{button}</Tip> : button}
    >
      {open ? (
        <div className="bg-well px-5 py-4 shadow-[inset_0_1px_0_var(--color-line)]">
          {list.isPending ? (
            <SkeletonRows rows={4} height={36} />
          ) : list.error ? (
            <div className="flex items-start gap-3">
              <ErrorWell className="min-w-0 flex-1">{fail(list.error)}</ErrorWell>
              <Btn size="sm" onClick={() => void list.refetch()}>
                Try again
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
  const { models: allowed, setModel, busyModels } = useSettingsForm();
  const [query, setQuery] = useState("");
  const shown = filterModels(models, query);
  const isAllowed = (id: string) => allowed.some((m) => m.id === id);

  if (models.length === 0) {
    return <p className="text-[13px] text-ink-2">{provider.name} lists no models for this key. Add one by id under Models.</p>;
  }
  return (
    <>
      <div className="mb-3 flex items-center gap-2">
        <div className="min-w-0 flex-1">
          <SearchBox value={query} onChange={setQuery} placeholder={`Filter ${models.length} models`} searching={false} />
        </div>
        <Btn kind="ghost" size="sm" iconOnly title="Ask again" aria-label="Ask again" disabled={refreshing} onClick={onRefresh}>
          {refreshing ? <Spinner size={13} /> : <RefreshCw size={14} />}
        </Btn>
      </div>
      {shown.length === 0 ? (
        <p className="py-2 text-[13px] text-ink-2">No model matches “{query.trim()}”.</p>
      ) : (
        <ul className="max-h-[396px] divide-y divide-line overflow-y-auto overscroll-contain rounded-control bg-surface shadow-card">
          {shown.map((m) => {
            const on = isAllowed(m.id);
            const busy = busyModels.has(m.id);
            const context = fmtContext(m.contextWindow);
            return (
              <li key={m.id} className="flex h-11 items-center gap-3 pr-1.5 pl-3 text-[13px]">
                <span className="max-w-full shrink-0 truncate font-mono text-ink max-sm:min-w-0 max-sm:flex-1 max-sm:shrink">{bareModel(m.id)}</span>
                <span className="min-w-0 flex-1 truncate text-ink-3 max-sm:hidden">{m.name}</span>
                {context ? (
                  <span className="shrink-0 font-mono text-[12px] text-ink-3 tabular-nums" title={`${m.contextWindow.toLocaleString()} tokens of context`}>
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
                        <Check size={14} />
                        Allowed
                      </span>
                      <Btn kind="ghost" size="sm" iconOnly title="Remove from the allowed models" aria-label={`Remove ${m.id}`} onClick={() => void setModel(m.id, false)}>
                        <X size={14} />
                      </Btn>
                    </>
                  ) : (
                    <Btn size="sm" aria-label={`Add ${m.id}`} onClick={() => void setModel(m.id, true)}>
                      <Plus size={14} /> Add
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
