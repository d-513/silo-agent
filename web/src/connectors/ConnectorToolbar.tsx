import { Plus } from "lucide-react";
import { btnClass } from "../Btn";
import { TabPill, TabPills } from "../TabPills";
import { ToolbarSearch } from "../ToolbarSearch";
import type { TabMode } from "./model";

// Tabs (in use / library / custom), the search box with its ⌘K hint, and the
// Add connector shortcut on the in-use tab.
export function ConnectorToolbar({
  tab,
  setTab,
  attachedCount,
  catalogCount,
  search,
  setSearch,
}: {
  tab: TabMode;
  setTab: (t: TabMode) => void;
  attachedCount: number;
  catalogCount: number;
  search: string;
  setSearch: (s: string) => void;
}) {
  return (
    <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <TabPills>
        <TabPill on={tab === "in_use"} onClick={() => setTab("in_use")} count={attachedCount}>
          <span>In Use</span>
        </TabPill>
        <TabPill on={tab === "library"} onClick={() => setTab("library")} count={catalogCount}>
          <span>Library</span>
        </TabPill>
        <TabPill on={tab === "custom"} onClick={() => setTab("custom")}>
          <Plus size={14} />
          <span>Custom</span>
        </TabPill>
      </TabPills>

      <div className="flex items-center gap-2.5">
        <ToolbarSearch value={search} onChange={setSearch} placeholder={tab === "in_use" ? "Search installed..." : "Search connectors..."} />

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
