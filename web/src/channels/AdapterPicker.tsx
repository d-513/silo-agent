import { Check, Plug, Plus } from "lucide-react";
import { useMemo, useState } from "react";
import { useNavigate } from "react-router-dom";
import { btnClass } from "../Btn";
import type { ChannelAdapter } from "../gen/silo/v1/ui_pb";
import { PageHead, widePage } from "../PageHead";
import { ToolbarSearch } from "../ToolbarSearch";
import { AdapterLogo } from "./AdapterLogo";
import { adapterPoints, adapterTraits, filterAdapters } from "./model";

// Past this many adapters the picker grows a search box.
const searchable = 4;

function AdapterCard({ a, count, onAdd }: { a: ChannelAdapter; count: number; onAdd: () => void }) {
  const traits = adapterTraits(a);
  const points = adapterPoints(a.slug);
  return (
    <div className="relative flex flex-col overflow-hidden rounded-card bg-surface shadow-card transition-shadow duration-[160ms] ease-quiet hover:shadow-float">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(120%_70%_at_100%_0%,var(--color-cobalt-pale),transparent_60%)]"
      />
      <div className="relative flex flex-1 flex-col gap-4 p-6">
        <div className="flex items-start justify-between gap-3">
          <div className="rounded-card bg-surface p-2 shadow-card">
            <AdapterLogo adapter={a} size={56} />
          </div>
          {count > 0 ? (
            <span className="mt-1 inline-flex items-center gap-1 text-[11px] font-medium text-emerald">
              <Check size={11} />
              {count === 1 ? "In use" : `${count} in use`}
            </span>
          ) : null}
        </div>

        <div className="min-w-0">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-[19px] font-semibold tracking-tight text-ink">{a.name}</h3>
            {traits.map((t) => (
              <span key={t} className="rounded-sm bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">
                {t}
              </span>
            ))}
          </div>
          {a.description ? <p className="mt-1.5 text-[13px] leading-relaxed text-ink-2">{a.description}</p> : null}
        </div>

        {points.length > 0 ? (
          <ul className="space-y-1.5 text-[12.5px] text-ink-2">
            {points.map((p) => (
              <li key={p} className="flex items-start gap-1.5">
                <Check size={13} className="mt-[3px] shrink-0 text-emerald" />
                {p}
              </li>
            ))}
          </ul>
        ) : null}

        <div className="mt-auto pt-2">
          <button type="button" className={btnClass("primary", "w-full justify-center")} onClick={onAdd}>
            <Plus size={15} />
            {count > 0 ? "Add another" : `Add ${a.name}`}
          </button>
        </div>
      </div>
    </div>
  );
}

// /bots/:id/channels/new: the built-in adapters to start a channel from.
export function AdapterPicker({
  botId,
  adapters,
  counts,
  loaded,
  onBack,
}: {
  botId: string;
  adapters: ChannelAdapter[];
  counts: Record<string, number>;
  loaded: boolean;
  onBack: () => void;
}) {
  const navigate = useNavigate();
  const [search, setSearch] = useState("");
  const shown = useMemo(() => filterAdapters(adapters, search), [adapters, search]);
  return (
    <div className={widePage}>
      <PageHead title="Add a channel" subtitle="Pick the app you want to talk to this Bot from." onBack={onBack} />

      {adapters.length > searchable ? (
        <div className="mb-6">
          <ToolbarSearch value={search} onChange={setSearch} placeholder="Search channels..." className="sm:w-80" />
        </div>
      ) : null}

      {!loaded ? (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3" aria-busy="true" aria-label="Loading">
          {[0, 1, 2].map((i) => (
            <div key={i} className="skeleton h-[340px] rounded-card" />
          ))}
        </div>
      ) : adapters.length === 0 ? (
        <div className="rounded-card border border-dashed border-line bg-surface p-12 text-center text-ink-3">No channel adapters available on this system.</div>
      ) : shown.length === 0 ? (
        <div className="rounded-card bg-surface p-8 text-center text-ink-3 shadow-card">
          <p className="text-[13px]">No channels match “{search}”.</p>
          <button type="button" className="mt-2 text-[12px] font-medium text-cobalt hover:underline" onClick={() => setSearch("")}>
            Clear search
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
          {shown.map((a) => (
            <AdapterCard key={a.slug} a={a} count={counts[a.slug] ?? 0} onAdd={() => navigate(`/bots/${botId}/channels/new/${a.slug}`)} />
          ))}
        </div>
      )}

      <div className="mt-8 flex flex-col items-center justify-between gap-4 rounded-card border border-dashed border-line bg-well p-5 sm:flex-row">
        <div className="flex items-center gap-3.5">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-surface text-ink-2 shadow-card">
            <Plug size={20} />
          </div>
          <div>
            <h4 className="text-sm font-semibold text-ink">Looking for email, a calendar, or another service?</h4>
            <p className="text-xs text-ink-2">Those are Connectors: the Bot calls them as tools instead of chatting through them.</p>
          </div>
        </div>
        <button type="button" className={btnClass("secondary")} onClick={() => navigate(`/bots/${botId}/connectors`)}>
          Browse Connectors
        </button>
      </div>
    </div>
  );
}
