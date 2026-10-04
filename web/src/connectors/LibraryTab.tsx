import { Plug, Plus, Sparkles } from "lucide-react";
import { btnClass } from "../Btn";
import type { Connector } from "../gen/silo/v1/ui_pb";
import { CatalogCard, FeaturedCard } from "./LibraryCards";

// The library: category pills, then featured presets and the rest by category
// (or the flat filtered list once a category or search narrows it), and a
// pointer to the custom tab for anything not in the library.
export function LibraryTab({
  catalog,
  filtered,
  categories,
  byCategory,
  featured,
  selectedCategory,
  setSelectedCategory,
  search,
  setSearch,
  countFor,
  onAdd,
  onCustom,
}: {
  catalog: Connector[];
  filtered: Connector[];
  categories: string[];
  byCategory: Map<string, Connector[]>;
  featured: Connector[];
  selectedCategory: string;
  setSelectedCategory: (c: string) => void;
  search: string;
  setSearch: (s: string) => void;
  countFor: (sourceIdOrName: string) => number;
  onAdd: (c: Connector) => void;
  onCustom: () => void;
}) {
  return (
    <div className="space-y-6">
      {/* Category Filter Pills */}
      <div className="flex flex-wrap items-center gap-1.5 border-b border-line pb-3">
        <button
          type="button"
          className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${ selectedCategory === "all"
              ? "bg-ink text-white font-semibold shadow-2xs"
              : "bg-well text-ink-2 hover:text-ink hover:bg-pressed"
          }`}
          onClick={() => setSelectedCategory("all")}
        >
          All ({catalog.length})
        </button>
        {categories.map((cat) => {
          const count = catalog.filter((c) => (c.category?.trim() || "General") === cat).length;
          const active = selectedCategory.toLowerCase() === cat.toLowerCase();
          return (
            <button
              key={cat}
              type="button"
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${ active ? "bg-ink text-white font-semibold shadow-2xs"
                  : "bg-well text-ink-2 hover:text-ink hover:bg-pressed"
              }`}
              onClick={() => setSelectedCategory(cat)}
            >
              {cat} ({count})
            </button>
          );
        })}
      </div>

      {filtered.length === 0 ? (
        <div className="rounded-card shadow-card bg-surface p-8 text-center text-ink-3">
          <p className="text-[13px]">No catalog connectors found matching your criteria.</p>
          <button
            type="button"
            className="mt-2 text-[12px] font-medium text-cobalt hover:underline"
            onClick={() => {
              setSearch("");
              setSelectedCategory("all");
            }}
          >
            Reset filters
          </button>
        </div>
      ) : selectedCategory === "all" && !search.trim() ? (
        /* Grouped category overview */
        <div className="space-y-7">
          {featured.length > 0 && (
            <section className="space-y-3">
              <div className="flex items-center gap-2 border-b border-line/70 pb-2">
                <Sparkles size={14} className="text-cobalt" />
                <h3 className="font-semibold text-sm text-ink">Featured</h3>
              </div>
              <div className={`grid grid-cols-1 gap-4 ${featured.length > 1 ? "md:grid-cols-2" : ""}`}>
                {featured.map((c) => (
                  <FeaturedCard key={c.id} c={c} count={countFor(c.id)} onAdd={() => onAdd(c)} />
                ))}
              </div>
            </section>
          )}
          {Array.from(byCategory.entries()).map(([categoryName, items]) => (
            <section key={categoryName} className="space-y-3">
              <div className="flex items-center justify-between border-b border-line/70 pb-2">
                <div className="flex items-center gap-2">
                  <h3 className="font-semibold text-sm text-ink">{categoryName}</h3>
                  <span className="rounded-full bg-well px-2 py-0.5 text-[10px] font-medium text-ink-3">{items.length}</span>
                </div>
              </div>

              <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
                {items.map((c) => (
                  <CatalogCard key={c.id} c={c} count={countFor(c.id)} onAdd={() => onAdd(c)} />
                ))}
              </div>
            </section>
          ))}
        </div>
      ) : (
        /* Flat filtered list */
        <div className="space-y-3">
          <div className="flex items-center justify-between text-xs text-ink-3">
            <span>
              Showing {filtered.length} connector{filtered.length === 1 ? "" : "s"}
            </span>
          </div>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
            {filtered.map((c) => (
              <CatalogCard key={c.id} c={c} count={countFor(c.id)} onAdd={() => onAdd(c)} />
            ))}
          </div>
        </div>
      )}

      {/* Footer Callout for Custom MCP */}
      <div className="rounded-card border border-dashed border-line bg-well p-5 flex flex-col sm:flex-row items-center justify-between gap-4">
        <div className="flex items-center gap-3.5">
          <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
            <Plug size={20} />
          </div>
          <div>
            <h4 className="text-sm font-semibold text-ink">Need a private or in-house connector?</h4>
            <p className="text-xs text-ink-2">Connect any standard Model Context Protocol (MCP) server over HTTP or STDIO (Docker sidecar).</p>
          </div>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <button type="button" className={btnClass("primary")} onClick={onCustom}>
            <Plus size={14} />
            Connect Custom MCP
          </button>
        </div>
      </div>
    </div>
  );
}
