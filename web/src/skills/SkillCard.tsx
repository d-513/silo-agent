import { Eye, ScrollText } from "lucide-react";
import type { ReactNode } from "react";
import { sourceLabel } from "./model";

// One skill: a scroll mark, its name and description, where it came from, and
// what you can do with it. The whole card opens the inspector; `aside` (a
// switch) and `actions` (Remove) sit inside it and keep their own clicks.
export function SkillCard({
  name,
  description,
  source,
  catalog,
  onOpen,
  aside,
  actions,
}: {
  name: string;
  description: string;
  source?: string;
  catalog?: boolean;
  onOpen: () => void;
  aside?: ReactNode;
  actions?: ReactNode;
}) {
  const from = source ? sourceLabel(source) : "";
  return (
    <div
      role="button"
      tabIndex={0}
      aria-label={`Inspect ${name}`}
      className="group flex min-h-[140px] cursor-pointer flex-col justify-between rounded-card bg-surface p-5 shadow-card transition-[box-shadow] duration-[160ms] ease-quiet hover:shadow-float"
      onClick={onOpen}
      onKeyDown={(e) => {
        if (e.target !== e.currentTarget) return;
        if (e.key === "Enter" || e.key === " ") {
          e.preventDefault();
          onOpen();
        }
      }}
    >
      <div className="flex items-start gap-4">
        <span className="flex h-12 w-12 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
          <ScrollText size={22} />
        </span>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="break-all text-[15px] font-semibold tracking-tight text-ink">{name}</span>
            {catalog ? <span className="rounded-sm bg-well px-2 py-0.5 text-[11px] font-medium text-ink-3">Catalog</span> : null}
          </div>
          {description ? (
            <p className="mt-1.5 line-clamp-2 text-xs leading-relaxed text-ink-2">{description}</p>
          ) : (
            <p className="mt-1.5 text-xs leading-relaxed text-ink-3">No description.</p>
          )}
        </div>
        {aside ? <div className="shrink-0 pt-0.5">{aside}</div> : null}
      </div>

      <div className="mt-4 flex items-center justify-between gap-3 border-t border-line/80 pt-3.5">
        <span className="min-w-0 truncate font-mono text-[11px] text-ink-3" title={source || undefined}>
          {from || "Local"}
        </span>
        <div className="flex shrink-0 items-center gap-1.5">
          <span className="inline-flex items-center gap-1.5 px-1 text-[12.5px] font-medium text-ink-3 transition-colors duration-[160ms] ease-quiet group-hover:text-ink">
            <Eye size={14} />
            Inspect
          </span>
          {actions}
        </div>
      </div>
    </div>
  );
}
