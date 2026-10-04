import { Plus, Search, X } from "lucide-react";
import type { RefObject } from "react";
import { btnClass } from "../Btn";
import type { TabMode } from "./model";

function TabButton({ on, onClick, count, children }: { on: boolean; onClick: () => void; count?: number; children: React.ReactNode }) {
  return (
    <button
      type="button"
      className={`flex items-center gap-1.5 rounded-md px-3.5 py-1.5 transition-[background-color,color,box-shadow] ${ on
          ? "bg-surface font-semibold text-ink shadow-2xs"
          : "hover:text-ink"
      }`}
      onClick={onClick}
    >
      {children}
      {count !== undefined ? (
        <span
          className={`rounded-full px-1.5 py-0.2 text-[10px] font-semibold ${ on
              ? "bg-cobalt-pale text-cobalt"
              : "bg-pressed text-ink-3"
          }`}
        >
          {count}
        </span>
      ) : null}
    </button>
  );
}

// Tabs (in use / library / custom), the search box with its ⌘K hint, and the
// Add connector shortcut on the in-use tab.
export function ConnectorToolbar({
  tab,
  setTab,
  attachedCount,
  catalogCount,
  search,
  setSearch,
  searchRef,
}: {
  tab: TabMode;
  setTab: (t: TabMode) => void;
  attachedCount: number;
  catalogCount: number;
  search: string;
  setSearch: (s: string) => void;
  searchRef: RefObject<HTMLInputElement | null>;
}) {
  return (
    <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex items-center rounded-sm bg-well p-1 text-[13px] font-medium text-ink-3">
        <TabButton on={tab === "in_use"} onClick={() => setTab("in_use")} count={attachedCount}>
          <span>In Use</span>
        </TabButton>
        <TabButton on={tab === "library"} onClick={() => setTab("library")} count={catalogCount}>
          <span>Library</span>
        </TabButton>
        <TabButton on={tab === "custom"} onClick={() => setTab("custom")}>
          <Plus size={14} />
          <span>Custom</span>
        </TabButton>
      </div>

      <div className="flex items-center gap-2.5">
        <div className="relative w-full sm:w-72">
          <span className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-ink-3">
            <Search size={15} />
          </span>
          <input
            ref={searchRef}
            type="text"
            className="h-9 w-full rounded-sm shadow-card bg-surface pl-9 pr-14 text-[13px] text-ink placeholder:text-ink-3 focus:outline-none focus:ring-1 focus:ring-cobalt transition-colors"
            placeholder={tab === "in_use" ? "Search installed..." : "Search connectors..."}
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          {search ? (
            <button type="button" className="absolute inset-y-0 right-0 flex items-center pr-2.5 text-ink-2 hover:text-ink" onClick={() => setSearch("")} title="Clear search">
              <X size={14} />
            </button>
          ) : (
            <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2.5">
              <kbd className="rounded bg-well px-1.5 py-0.5 font-mono text-[10px] text-ink-3">⌘K</kbd>
            </span>
          )}
        </div>

        {tab === "in_use" && (
          <button type="button" className={btnClass("primary")} onClick={() => setTab("library")}>
            <Plus size={15} />
            <span>Add connector</span>
          </button>
        )}
      </div>
    </div>
  );
}
