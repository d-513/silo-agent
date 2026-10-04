import { Plug, Plus } from "lucide-react";
import { btnClass } from "../Btn";
import type { BotConnector } from "../gen/silo/v1/ui_pb";
import { AttachedCard } from "./AttachedCard";
import type { TabMode } from "./model";

// The connectors attached to this Bot, or why there are none to show.
export function InUseTab({
  attached,
  filtered,
  search,
  setSearch,
  setTab,
  busy,
  onEdit,
  onAuthorize,
  onRefresh,
  onDetach,
  onViewError,
}: {
  attached: BotConnector[];
  filtered: BotConnector[];
  search: string;
  setSearch: (s: string) => void;
  setTab: (t: TabMode) => void;
  busy: string;
  onEdit: (row: BotConnector) => void;
  onAuthorize: (row: BotConnector) => void;
  onRefresh: (id: string) => void;
  onDetach: (id: string) => void;
  onViewError: (row: BotConnector) => void;
}) {
  return (
    <div className="space-y-4">
      {attached.length === 0 ? (
        <div className="rounded-card border border-dashed border-line bg-surface p-12 text-center">
          <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-well text-ink-3">
            <Plug size={28} />
          </div>
          <h3 className="text-base font-semibold text-ink">No connectors in use yet</h3>
          <p className="mx-auto mt-1 max-w-md text-[13px] text-ink-2">
            Add verified integrations from the library to grant this Bot external tools, or connect your own local or remote MCP servers.
          </p>
          <div className="mt-6 flex items-center justify-center gap-3">
            <button type="button" className={btnClass("primary")} onClick={() => setTab("library")}>
              <Plus size={15} />
              Browse Library
            </button>
            <button type="button" className={btnClass("secondary")} onClick={() => setTab("custom")}>
              Add Custom Connector
            </button>
          </div>
        </div>
      ) : filtered.length === 0 ? (
        <div className="rounded-card shadow-card bg-surface p-8 text-center text-ink-3">
          <p className="text-[13px]">No installed connectors match "{search}".</p>
          <button type="button" className="mt-2 text-[12px] font-medium text-cobalt hover:underline" onClick={() => setSearch("")}>
            Clear search filter
          </button>
        </div>
      ) : (
        <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
          {filtered.map((row) => (
            <AttachedCard
              key={row.id}
              row={row}
              busy={busy === row.id}
              onEdit={() => onEdit(row)}
              onAuthorize={() => onAuthorize(row)}
              onRefresh={() => onRefresh(row.id)}
              onDetach={() => onDetach(row.id)}
              onViewError={() => onViewError(row)}
            />
          ))}
        </div>
      )}
    </div>
  );
}
