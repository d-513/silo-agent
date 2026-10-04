import { ChevronRight, Folder } from "lucide-react";
import { ErrorWell, SkeletonRows } from "./Field";
import { crumbs } from "./fs";

export type FolderEntry = { name: string; path: string };

// A folder browser that walks one level at a time: the trail to here, then the
// folders here. A remote or a big tree is a network round trip per level, so
// nothing is expanded in place. dirs is null while a level loads.
export function FolderNav({
  rootLabel,
  at,
  dirs,
  err,
  onGo,
  empty = "No folders here.",
}: {
  rootLabel: string;
  at: string;
  dirs: FolderEntry[] | null;
  err: string;
  onGo: (path: string) => void;
  empty?: string;
}) {
  const trail = crumbs(at, rootLabel);
  return (
    <>
      <nav className="silo-scroll-x flex items-center gap-1 px-2 py-2 text-[12.5px] shadow-[inset_0_-1px_0_var(--color-line)]" aria-label="Folder path">
        {trail.map((c, i) => {
          const last = i === trail.length - 1;
          const button = (
            <button
              key={i === 0 ? "root" : undefined}
              type="button"
              className={`${i === 0 ? "shrink-0 " : ""}rounded-sm px-2 py-1 ${last ? "font-medium text-ink" : "text-ink-2 hover:bg-well hover:text-ink"}`}
              onClick={() => onGo(c.path)}
            >
              {c.label}
            </button>
          );
          return i === 0 ? (
            button
          ) : (
            <span key={c.path} className="flex shrink-0 items-center gap-1">
              <ChevronRight size={12} className="text-ink-3" />
              {button}
            </span>
          );
        })}
      </nav>
      <div className="max-h-[260px] overflow-auto py-1">
        {err ? (
          <div className="p-2">
            <ErrorWell>{err}</ErrorWell>
          </div>
        ) : dirs === null ? (
          <SkeletonRows rows={3} height={32} className="p-2" />
        ) : dirs.length === 0 ? (
          <p className="px-4 py-3 text-[12.5px] text-ink-3">{empty}</p>
        ) : (
          dirs.map((d) => (
            <button
              key={d.path}
              type="button"
              className="flex h-9 w-full items-center gap-2.5 px-3 text-left text-[13.5px] text-ink transition-colors duration-[160ms] ease-quiet hover:bg-well"
              onClick={() => onGo(d.path)}
            >
              <Folder size={15} className="shrink-0 text-ink-3" />
              <span className="min-w-0 flex-1 truncate">{d.name}</span>
              <ChevronRight size={14} className="shrink-0 text-ink-3" />
            </button>
          ))
        )}
      </div>
    </>
  );
}
