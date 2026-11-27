import { createQueryOptions, useTransport } from "@connectrpc/connect-query";
import { useQueries } from "@tanstack/react-query";
import { ConfigSource, UI, type Provider, type ProviderModel } from "../gen/silo/v1/ui_pb";
import { accountsOf, isReady } from "./providerJobs";
import { kindOf, withFast, type ModelKind } from "./providerModels";
import { useSettingsForm } from "./useAdminSettings";

/** The providers that serve one of these kinds and have what they need to be asked, by their saved settings. */
export function useAskable(kinds: ModelKind[]): Provider[] {
  const { providers, fields } = useSettingsForm();
  // The list is asked with the saved settings, so an unsaved key does not count.
  const saved = Object.fromEntries(fields.map((f) => [f.key, f.value]));
  return providers.filter((p) => kinds.some((k) => p.kinds.includes(k)) && isReady(p.id, fields, saved, ConfigSource.ENV, accountsOf(providers)));
}

/** A listed model with who lists it, and for a fast-mode row the model it is the fast mode of. */
export type ListedModel = ProviderModel & { provider: Provider; fastOf?: string };

/**
 * What the providers that are set up serve of these kinds. Each provider is
 * asked on its own and kept for the visit, so a slow or failing one never
 * holds up the rest and opening a second list costs nothing.
 */
export function useProviderLists(kinds: ModelKind[]) {
  const asked = useAskable(kinds);
  const transport = useTransport();
  const lists = useQueries({
    queries: asked.map((p) => ({ ...createQueryOptions(UI.method.listProviderModels, { provider: p.id }, { transport }), staleTime: Infinity, retry: false })),
  });
  const rows: ListedModel[] = [];
  asked.forEach((p, i) => {
    for (const m of withFast((lists[i].data?.models ?? []).filter((m) => kinds.includes(kindOf(m))))) rows.push({ ...m, provider: p });
  });
  return {
    asked,
    rows,
    // How many providers have not answered yet.
    pending: lists.filter((l) => l.isPending).length,
    fetching: lists.some((l) => l.isFetching),
    failed: asked.flatMap((p, i) => (lists[i].error ? [{ provider: p, error: lists[i].error }] : [])),
    refetch: () => lists.forEach((l) => void l.refetch()),
  };
}
