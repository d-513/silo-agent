import { Plus } from "lucide-react";
import { btnClass } from "../Btn";
import { TabPill, TabPills } from "../TabPills";
import { ToolbarSearch } from "../ToolbarSearch";
import { t } from "../i18n.ts";

export type ToolbarTab<M extends string> = { mode: M; label: string; count?: number; add?: boolean };

// The pills (what is in use, what else the Workspace has, the library, a
// custom server), the search box, and the Add connector shortcut on the first
// pill.
export function ConnectorToolbar<M extends string>({
  tabs,
  tab,
  setTab,
  search,
  setSearch,
  onAdd,
}: {
  tabs: ToolbarTab<M>[];
  tab: M;
  setTab: (t: M) => void;
  search: string;
  setSearch: (s: string) => void;
  onAdd?: () => void;
}) {
  const first = tab === tabs[0]?.mode;
  return (
    <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <TabPills>
        {tabs.map((x) => (
          <TabPill key={x.mode} on={tab === x.mode} onClick={() => setTab(x.mode)} count={x.count}>
            {x.add ? <Plus size={14} /> : null}
            <span>{x.label}</span>
          </TabPill>
        ))}
      </TabPills>

      <div className="flex items-center gap-2.5">
        <ToolbarSearch value={search} onChange={setSearch} placeholder={first ? t("Search installed...") : t("Search connectors...")} />

        {first && onAdd ? (
          <button type="button" className={btnClass("primary")} onClick={onAdd}>
            <Plus size={15} />
            <span>{t("Add connector")}</span>
          </button>
        ) : null}
      </div>
    </div>
  );
}
