import { Check, Plus, ShieldCheck } from "lucide-react";
import { btnClass } from "../Btn";
import { CategoryChip, ConnectorMark, McpChip } from "./form";
import type { Connector } from "../gen/silo/v1/ui_pb";
import { featuredPoints } from "./model";

export function CatalogCard({ c, count, onAdd }: { c: Connector; count: number; onAdd: () => void }) {
  return (
    <div className="group flex items-start justify-between gap-4 rounded-card shadow-card bg-surface p-5 transition-[background-color,color,box-shadow] hover:shadow-float hover:shadow-xs min-h-[130px]">
      <div className="flex min-w-0 flex-1 items-start gap-4">
        <div className="shrink-0 pt-0.5">
          <ConnectorMark id={c.id} hasImage={c.hasImage} size={48} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h4 className="font-semibold text-[15px] text-ink tracking-tight">{c.name}</h4>
            <ShieldCheck size={15} className="text-emerald shrink-0" />
            <McpChip transport={c.transport} />
            {c.category && <CategoryChip label={c.category} />}
          </div>
          <p className="mt-1.5 line-clamp-2 text-xs text-ink-2 leading-relaxed">{c.description}</p>
          <div className="mt-2.5 flex items-center gap-2 text-[11px] text-ink-3">
            <span className="font-mono uppercase">{c.transport}</span>
            <span>·</span>
            <span className="capitalize">{c.auth}</span>
            {count > 0 && (
              <>
                <span>·</span>
                <span className="inline-flex items-center gap-1 font-medium text-emerald">
                  <Check size={11} />
                  {count === 1 ? "In use" : `${count} in use`}
                </span>
              </>
            )}
          </div>
        </div>
      </div>

      <button
        type="button"
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm shadow-card bg-surface text-ink-2 transition-[background-color,color,box-shadow] hover:shadow-float hover:bg-well hover:text-ink"
        onClick={onAdd}
        title={`Add ${c.name}`}
      >
        <Plus size={16} />
      </button>
    </div>
  );
}

export function FeaturedCard({ c, count, onAdd }: { c: Connector; count: number; onAdd: () => void }) {
  const points = featuredPoints[c.builtin] ?? [];
  return (
    <div className="relative overflow-hidden rounded-card shadow-card bg-surface transition-shadow hover:shadow-float">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 bg-[radial-gradient(120%_90%_at_100%_0%,var(--color-cobalt-pale),transparent_60%)]"
      />
      <div className="relative flex flex-col gap-5 p-6 sm:flex-row sm:items-start sm:gap-6">
        <div className="shrink-0 rounded-card bg-surface p-2 shadow-card">
          <ConnectorMark id={c.id} hasImage={c.hasImage} size={64} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h4 className="text-[19px] font-semibold tracking-tight text-ink">{c.name}</h4>
            <span className="rounded-full bg-cobalt-pale px-2 py-0.5 text-[10.5px] font-semibold uppercase tracking-wide text-cobalt">
              Built in
            </span>
            {c.category && <CategoryChip label={c.category} />}
          </div>
          <p className="mt-1.5 max-w-2xl text-[13px] leading-relaxed text-ink-2">{c.description}</p>
          {points.length > 0 && (
            <ul className="mt-3 flex flex-wrap gap-x-4 gap-y-1.5 text-[12px] text-ink-2">
              {points.map((p) => (
                <li key={p} className="inline-flex items-center gap-1.5">
                  <Check size={12} className="text-emerald" />
                  {p}
                </li>
              ))}
            </ul>
          )}
        </div>
        <div className="flex shrink-0 flex-col items-start gap-1.5 sm:items-end">
          <button type="button" className={btnClass("primary")} onClick={onAdd}>
            <Plus size={15} />
            {count > 0 ? "Add another" : `Add ${c.name}`}
          </button>
          {count > 0 && (
            <span className="inline-flex items-center gap-1 text-[11px] font-medium text-emerald">
              <Check size={11} />
              {count === 1 ? "In use" : `${count} in use`}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}
