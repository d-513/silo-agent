import {
  ArrowClockwise,
  Check,
  MagnifyingGlass,
  PencilSimple,
  Plugs,
  Plus,
  ShieldCheck,
  Trash,
  X,
} from "@phosphor-icons/react";
import { useEffect, useMemo, useRef, useState } from "react";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import {
  CategoryChip,
  ConnectorFields,
  ConnectorMark,
  CustomChip,
  McpChip,
  draftFrom,
  emptyDraft,
  specOf,
  type ConnectorDraft,
} from "./ConnectorForm";
import type { BotConnector, Connector } from "./gen/silo/v1/ui_pb";

export async function startConnectorAuth(botId: string, id: string) {
  const r = await ui.startConnectorAuth({ botId, id });
  if (r.authorizeUrl) window.open(r.authorizeUrl, "silo-oauth", "width=480,height=720");
}

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function statusLabel(s: string) {
  if (s === "authorized") return "Authorized";
  if (s === "needs_auth") return "Needs authorization";
  if (s === "initializing") return "Initializing";
  if (s === "error") return "Error";
  return "Ready";
}

function statusLamp(s: string) {
  if (s === "authorized") return "bg-pine";
  if (s === "needs_auth" || s === "error") return "bg-carmine";
  if (s === "initializing") return "bg-bindery animate-pulse";
  return "bg-stone";
}

function isCustom(c: Connector) {
  return c.kind === "custom" && !c.sourceId;
}

function nextCopyName(base: string, names: string[]) {
  const used = new Set(names.map((n) => n.trim().toLowerCase()));
  const stem = base.trim();
  if (!used.has(stem.toLowerCase())) return stem;
  for (let n = 2; n < 10000; n++) {
    const cand = `${stem} ${n}`;
    if (!used.has(cand.toLowerCase())) return cand;
  }
  return `${stem} ${names.length + 1}`;
}

type TabMode = "in_use" | "library" | "custom";

export function BotConnectors({
  botId,
  onNeedAuth,
}: {
  botId: string;
  onNeedAuth: (row: BotConnector | null) => void;
}) {
  const [attached, setAttached] = useState<BotConnector[]>([]);
  const [catalog, setCatalog] = useState<Connector[]>([]);
  const [tab, setTab] = useState<TabMode>("in_use");
  const [search, setSearch] = useState("");
  const [selectedCategory, setSelectedCategory] = useState("all");

  // Library modal popup state
  const [libraryModal, setLibraryModal] = useState<Connector | null>(null);
  const [libraryDraft, setLibraryDraft] = useState<ConnectorDraft>(emptyDraft());

  // Custom tab draft state
  const [customDraft, setCustomDraft] = useState<ConnectorDraft>(emptyDraft());

  // Edit attached connector state
  const [edit, setEdit] = useState<BotConnector | null>(null);
  const [editDraft, setEditDraft] = useState<ConnectorDraft>(emptyDraft());

  // Action states
  const [armDetach, setArmDetach] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState("");

  const searchInputRef = useRef<HTMLInputElement>(null);
  const hasInit = attached.some((r) => r.authStatus === "initializing");

  async function load() {
    const [a, c] = await Promise.all([ui.listBotConnectors({ botId }), ui.listConnectors({})]);
    setAttached(a.connectors);
    setCatalog(c.connectors);
  }

  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
    const t = setInterval(() => load().catch(() => {}), hasInit ? 1000 : 3000);
    return () => clearInterval(t);
  }, [botId, hasInit]);

  // Keyboard shortcut ⌘K / Ctrl+K for search
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        searchInputRef.current?.focus();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  // Distinct categories in the library
  const categories = useMemo(() => {
    const set = new Set<string>();
    for (const c of catalog) {
      const cat = c.category?.trim();
      if (cat) set.add(cat);
      else set.add("General");
    }
    return Array.from(set).sort();
  }, [catalog]);

  // Filtered library connectors
  const filteredCatalog = useMemo(() => {
    const q = search.trim().toLowerCase();
    return catalog.filter((c) => {
      const cat = (c.category?.trim() || "General");
      const matchCat = selectedCategory === "all" || cat.toLowerCase() === selectedCategory.toLowerCase();
      if (!matchCat) return false;
      if (!q) return true;
      return (
        c.name.toLowerCase().includes(q) ||
        c.description.toLowerCase().includes(q) ||
        cat.toLowerCase().includes(q) ||
        c.transport.toLowerCase().includes(q)
      );
    });
  }, [catalog, search, selectedCategory]);

  // Catalog grouped by category for overview
  const catalogByCategory = useMemo(() => {
    const map = new Map<string, Connector[]>();
    for (const c of filteredCatalog) {
      const cat = c.category?.trim() || "General";
      const list = map.get(cat) ?? [];
      list.push(c);
      map.set(cat, list);
    }
    return map;
  }, [filteredCatalog]);

  // Filtered in-use connectors
  const filteredAttached = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return attached;
    return attached.filter((r) => {
      const c = r.connector;
      if (!c) return false;
      return (
        c.name.toLowerCase().includes(q) ||
        c.description.toLowerCase().includes(q) ||
        (c.category && c.category.toLowerCase().includes(q)) ||
        r.authStatus.toLowerCase().includes(q)
      );
    });
  }, [attached, search]);

  // How many copies of a catalog connector are currently attached?
  function attachedCountFor(sourceIdOrName: string) {
    return attached.filter((r) => {
      const c = r.connector;
      if (!c) return false;
      return c.sourceId === sourceIdOrName || c.name.toLowerCase() === sourceIdOrName.toLowerCase();
    }).length;
  }

  function openLibraryModal(c: Connector) {
    setErr("");
    setLibraryModal(c);
    setLibraryDraft({
      ...draftFrom(c),
      name: nextCopyName(c.name, attached.map((x) => x.connector?.name ?? "")),
    });
  }

  function closeLibraryModal() {
    setLibraryModal(null);
    setLibraryDraft(emptyDraft());
  }

  async function submitAddLibrary(e: React.FormEvent) {
    e.preventDefault();
    if (!libraryModal) return;
    setErr("");
    try {
      const row = await ui.createBotConnector({
        botId,
        ...specOf(libraryDraft),
        sourceId: libraryModal.id,
      });
      closeLibraryModal();
      await load();
      setTab("in_use");
      if (row.authStatus === "needs_auth") onNeedAuth(row);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function submitAddCustom(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      const row = await ui.createBotConnector({
        botId,
        ...specOf(customDraft),
        sourceId: "",
      });
      setCustomDraft(emptyDraft());
      await load();
      setTab("in_use");
      if (row.authStatus === "needs_auth") onNeedAuth(row);
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function saveEdit(e: React.FormEvent) {
    e.preventDefault();
    const c = edit?.connector;
    if (!c) return;
    setErr("");
    try {
      await ui.updateConnector({ id: c.id, ...specOf(editDraft) });
      setEdit(null);
      await load();
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function authorize(row: BotConnector) {
    setErr("");
    try {
      await startConnectorAuth(botId, row.id);
      onNeedAuth(null);
    } catch (e) {
      setErr(fail(e));
    }
  }

  async function refresh(id: string) {
    setErr("");
    setBusy(id);
    try {
      await ui.refreshBotConnector({ botId, id });
      await load();
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy("");
    }
  }

  async function handleDetach(id: string) {
    if (armDetach !== id) {
      setArmDetach(id);
      return;
    }
    setArmDetach("");
    setErr("");
    try {
      await ui.detachConnector({ botId, id });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }

  return (
    <div className="silo-page pb-16">
      {/* Header & Page Description */}
      <div className="mb-6 space-y-1.5">
        <h1 className="text-[22px] font-medium tracking-tight text-iron">Connectors</h1>
        <p className="max-w-3xl text-[13px] leading-relaxed text-stone">
          Connectors let this Bot work with outside services like GitHub, Slack, Notion, or your custom MCP servers. Search the library to add verified integrations or link bespoke tools directly into your bot runtime.
        </p>
      </div>

      {err && (
        <div className="mb-4 flex items-center justify-between rounded-lg border border-carmine/20 bg-carmine/10 px-3.5 py-2.5 text-[13px] text-carmine">
          <span>{err}</span>
          <button type="button" onClick={() => setErr("")} className="text-carmine hover:opacity-80">
            <X size={14} />
          </button>
        </div>
      )}

      {/* Primary Toolbar: Tabs + Search & Action Controls */}
      <div className="mb-6 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        {/* Navigation Tabs (In Use / Library / Custom) */}
        <div className="flex items-center rounded-lg border border-thread bg-cloth p-1 text-[13px] font-medium text-stone">
          <button
            type="button"
            className={`flex items-center gap-1.5 rounded-md px-3.5 py-1.5 transition-all ${
              tab === "in_use"
                ? "bg-folio font-semibold text-iron shadow-2xs"
                : "hover:text-iron"
            }`}
            onClick={() => setTab("in_use")}
          >
            <span>In Use</span>
            <span
              className={`rounded-full px-1.5 py-0.2 text-[10px] font-semibold ${
                tab === "in_use"
                  ? "bg-bindery-pale text-bindery"
                  : "bg-linen text-stone"
              }`}
            >
              {attached.length}
            </span>
          </button>

          <button
            type="button"
            className={`flex items-center gap-1.5 rounded-md px-3.5 py-1.5 transition-all ${
              tab === "library"
                ? "bg-folio font-semibold text-iron shadow-2xs"
                : "hover:text-iron"
            }`}
            onClick={() => setTab("library")}
          >
            <span>Library</span>
            <span
              className={`rounded-full px-1.5 py-0.2 text-[10px] font-semibold ${
                tab === "library"
                  ? "bg-bindery-pale text-bindery"
                  : "bg-linen text-stone"
              }`}
            >
              {catalog.length}
            </span>
          </button>

          <button
            type="button"
            className={`flex items-center gap-1.5 rounded-md px-3.5 py-1.5 transition-all ${
              tab === "custom"
                ? "bg-folio font-semibold text-iron shadow-2xs"
                : "hover:text-iron"
            }`}
            onClick={() => setTab("custom")}
          >
            <Plus size={14} weight="bold" />
            <span>Custom</span>
          </button>
        </div>

        {/* Search Input & Quick Actions */}
        <div className="flex items-center gap-2.5">
          <div className="relative w-full sm:w-72">
            <span className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-stone">
              <MagnifyingGlass size={15} />
            </span>
            <input
              ref={searchInputRef}
              type="text"
              className="h-9 w-full rounded-lg border border-thread bg-folio pl-9 pr-14 text-[13px] text-iron placeholder:text-stone/70 focus:border-bindery focus:outline-none focus:ring-1 focus:ring-bindery shadow-2xs transition-colors"
              placeholder={tab === "in_use" ? "Search installed..." : "Search connectors..."}
              value={search}
              onChange={(e) => setSearch(e.target.value)}
            />
            {search ? (
              <button
                type="button"
                className="absolute inset-y-0 right-0 flex items-center pr-2.5 text-stone hover:text-iron"
                onClick={() => setSearch("")}
                title="Clear search"
              >
                <X size={14} />
              </button>
            ) : (
              <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2.5">
                <kbd className="rounded border border-thread bg-cloth px-1.5 py-0.5 font-mono text-[10px] text-stone">
                  ⌘K
                </kbd>
              </span>
            )}
          </div>

          {tab === "in_use" && (
            <button
              type="button"
              className={btnClass("primary")}
              onClick={() => setTab("library")}
            >
              <Plus size={15} weight="bold" />
              <span>Add connector</span>
            </button>
          )}
        </div>
      </div>

      {/* TAB 1: IN USE */}
      {tab === "in_use" && (
        <div className="space-y-4">
          {attached.length === 0 ? (
            <div className="rounded-xl border border-dashed border-thread bg-folio p-12 text-center shadow-2xs">
              <div className="mx-auto mb-4 flex h-14 w-14 items-center justify-center rounded-2xl bg-cloth text-stone">
                <Plugs size={28} />
              </div>
              <h3 className="text-base font-semibold text-iron">No connectors in use yet</h3>
              <p className="mx-auto mt-1 max-w-md text-[13px] text-stone">
                Add verified integrations from the library to grant this Bot external tools, or connect your own local or remote MCP servers.
              </p>
              <div className="mt-6 flex items-center justify-center gap-3">
                <button
                  type="button"
                  className={btnClass("primary")}
                  onClick={() => setTab("library")}
                >
                  <Plus size={15} weight="bold" />
                  Browse Library
                </button>
                <button
                  type="button"
                  className={btnClass("secondary")}
                  onClick={() => setTab("custom")}
                >
                  Add Custom Connector
                </button>
              </div>
            </div>
          ) : filteredAttached.length === 0 ? (
            <div className="rounded-xl border border-thread bg-folio p-8 text-center text-stone shadow-2xs">
              <p className="text-[13px]">No installed connectors match "{search}".</p>
              <button
                type="button"
                className="mt-2 text-[12px] font-medium text-bindery hover:underline"
                onClick={() => setSearch("")}
              >
                Clear search filter
              </button>
            </div>
          ) : (
            <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
              {filteredAttached.map((row) => {
                const c = row.connector;
                if (!c) return null;
                const isBusy = busy === row.id || row.authStatus === "initializing";
                return (
                  <div
                    key={row.id}
                    className="flex flex-col justify-between rounded-xl border border-thread bg-folio p-4 shadow-2xs transition-all hover:border-hover"
                  >
                    <div>
                      {/* Card Top: Mark + Title/Badges + Status */}
                      <div className="flex items-start gap-3.5">
                        <ConnectorMark id={c.id} hasImage={c.hasImage} size={42} />
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <span className="font-semibold text-sm text-iron">{c.name}</span>
                            <McpChip />
                            {isCustom(c) && <CustomChip />}
                            {c.category && <CategoryChip label={c.category} />}
                          </div>

                          <p className="mt-1 line-clamp-2 text-xs text-stone">
                            {c.description || (c.transport === "http" ? c.httpUrl : c.stdioCommand)}
                          </p>

                          {/* Status line */}
                          <div className="mt-2 flex items-center gap-1.5 text-[11px] font-medium text-stone">
                            {row.authStatus === "initializing" ? (
                              <span className="inline-block h-2.5 w-2.5 animate-spin rounded-full border border-thread border-t-bindery" />
                            ) : (
                              <span className={`inline-block h-2 w-2 rounded-full ${statusLamp(row.authStatus)}`} />
                            )}
                            <span className={row.authStatus === "needs_auth" || row.authStatus === "error" ? "text-carmine" : "text-stone"}>
                              {statusLabel(row.authStatus)}
                            </span>
                            {row.statusDetail && (
                              <span className="truncate text-stone">· {row.statusDetail}</span>
                            )}
                            {row.lastError && (
                              <span className="truncate text-carmine">· {row.lastError}</span>
                            )}
                          </div>
                        </div>
                      </div>
                    </div>

                    {/* Card Bottom: Actions */}
                    <div className="mt-4 flex items-center justify-between border-t border-thread/80 pt-3">
                      <div className="text-[11px] font-mono text-stone uppercase tracking-wider">
                        {c.transport} · {c.auth}
                      </div>

                      <div className="flex items-center gap-1.5">
                        {row.authStatus === "needs_auth" && (
                          <Btn kind="primary" onClick={() => authorize(row)}>
                            Authorize
                          </Btn>
                        )}
                        <Btn
                          kind="ghost"
                          onClick={() => {
                            setEdit(row);
                            setEditDraft(draftFrom(c));
                          }}
                        >
                          <PencilSimple size={13} />
                          <span>Edit</span>
                        </Btn>
                        <Btn
                          kind="ghost"
                          disabled={isBusy}
                          onClick={() => refresh(row.id)}
                          title="Refresh status"
                        >
                          <ArrowClockwise size={13} className={isBusy ? "animate-spin" : ""} />
                          <span>Refresh</span>
                        </Btn>
                        <Btn
                          kind="ghost"
                          className="text-carmine hover:text-carmine"
                          onClick={() => handleDetach(row.id)}
                        >
                          <Trash size={13} />
                          <span>{armDetach === row.id ? "Remove?" : "Remove"}</span>
                        </Btn>
                      </div>
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      )}

      {/* TAB 2: LIBRARY */}
      {tab === "library" && (
        <div className="space-y-6">
          {/* Category Filter Pills */}
          <div className="flex flex-wrap items-center gap-1.5 border-b border-thread pb-3">
            <button
              type="button"
              className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                selectedCategory === "all"
                  ? "bg-iron text-white font-semibold shadow-2xs"
                  : "bg-cloth text-stone hover:text-iron hover:bg-linen"
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
                  className={`rounded-full px-3 py-1 text-xs font-medium transition-colors ${
                    active
                      ? "bg-iron text-white font-semibold shadow-2xs"
                      : "bg-cloth text-stone hover:text-iron hover:bg-linen"
                  }`}
                  onClick={() => setSelectedCategory(cat)}
                >
                  {cat} ({count})
                </button>
              );
            })}
          </div>

          {filteredCatalog.length === 0 ? (
            <div className="rounded-xl border border-thread bg-folio p-8 text-center text-stone shadow-2xs">
              <p className="text-[13px]">No catalog connectors found matching your criteria.</p>
              <button
                type="button"
                className="mt-2 text-[12px] font-medium text-bindery hover:underline"
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
              {Array.from(catalogByCategory.entries()).map(([categoryName, items]) => (
                <section key={categoryName} className="space-y-3">
                  <div className="flex items-center justify-between border-b border-thread/70 pb-2">
                    <div className="flex items-center gap-2">
                      <h3 className="font-semibold text-sm text-iron">{categoryName}</h3>
                      <span className="rounded-full bg-cloth px-2 py-0.5 text-[10px] font-medium text-stone">
                        {items.length}
                      </span>
                    </div>
                  </div>

                  <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
                    {items.map((c) => {
                      const count = attachedCountFor(c.id);
                      return (
                        <div
                          key={c.id}
                          className="group flex items-start justify-between gap-3.5 rounded-xl border border-thread bg-folio p-4 shadow-2xs transition-all hover:border-hover hover:shadow-sm"
                        >
                          <div className="flex min-w-0 flex-1 items-start gap-3.5">
                            <div className="shrink-0 p-0.5">
                              <ConnectorMark id={c.id} hasImage={c.hasImage} size={44} />
                            </div>
                            <div className="min-w-0 flex-1">
                              <div className="flex flex-wrap items-center gap-1.5">
                                <h4 className="font-semibold text-sm text-iron">{c.name}</h4>
                                <ShieldCheck size={14} className="text-bindery" weight="fill" />
                                <McpChip />
                                {c.category && <CategoryChip label={c.category} />}
                              </div>
                              <p className="mt-1 line-clamp-2 text-xs text-stone">{c.description}</p>
                              <div className="mt-2 flex items-center gap-2 text-[11px] text-stone">
                                <span className="font-mono uppercase">{c.transport}</span>
                                <span>·</span>
                                <span className="capitalize">{c.auth}</span>
                                {count > 0 && (
                                  <>
                                    <span>·</span>
                                    <span className="inline-flex items-center gap-1 font-medium text-pine">
                                      <Check size={11} weight="bold" />
                                      {count === 1 ? "In use" : `${count} in use`}
                                    </span>
                                  </>
                                )}
                              </div>
                            </div>
                          </div>

                          <button
                            type="button"
                            className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-thread bg-folio text-stone shadow-2xs transition-colors hover:border-bindery hover:bg-bindery-pale hover:text-bindery"
                            onClick={() => openLibraryModal(c)}
                            title={`Add ${c.name}`}
                          >
                            <Plus size={15} weight="bold" />
                          </button>
                        </div>
                      );
                    })}
                  </div>
                </section>
              ))}
            </div>
          ) : (
            /* Flat filtered list */
            <div className="space-y-3">
              <div className="flex items-center justify-between text-xs text-stone">
                <span>Showing {filteredCatalog.length} connector{filteredCatalog.length === 1 ? "" : "s"}</span>
              </div>
              <div className="grid grid-cols-1 gap-3.5 md:grid-cols-2">
                {filteredCatalog.map((c) => {
                  const count = attachedCountFor(c.id);
                  return (
                    <div
                      key={c.id}
                      className="group flex items-start justify-between gap-3.5 rounded-xl border border-thread bg-folio p-4 shadow-2xs transition-all hover:border-hover hover:shadow-sm"
                    >
                      <div className="flex min-w-0 flex-1 items-start gap-3.5">
                        <div className="shrink-0 p-0.5">
                          <ConnectorMark id={c.id} hasImage={c.hasImage} size={44} />
                        </div>
                        <div className="min-w-0 flex-1">
                          <div className="flex flex-wrap items-center gap-1.5">
                            <h4 className="font-semibold text-sm text-iron">{c.name}</h4>
                            <ShieldCheck size={14} className="text-bindery" weight="fill" />
                            <McpChip />
                            {c.category && <CategoryChip label={c.category} />}
                          </div>
                          <p className="mt-1 line-clamp-2 text-xs text-stone">{c.description}</p>
                          <div className="mt-2 flex items-center gap-2 text-[11px] text-stone">
                            <span className="font-mono uppercase">{c.transport}</span>
                            <span>·</span>
                            <span className="capitalize">{c.auth}</span>
                            {count > 0 && (
                              <>
                                <span>·</span>
                                <span className="inline-flex items-center gap-1 font-medium text-pine">
                                  <Check size={11} weight="bold" />
                                  {count === 1 ? "In use" : `${count} in use`}
                                </span>
                              </>
                            )}
                          </div>
                        </div>
                      </div>

                      <button
                        type="button"
                        className="flex h-8 w-8 shrink-0 items-center justify-center rounded-lg border border-thread bg-folio text-stone shadow-2xs transition-colors hover:border-bindery hover:bg-bindery-pale hover:text-bindery"
                        onClick={() => openLibraryModal(c)}
                        title={`Add ${c.name}`}
                      >
                        <Plus size={15} weight="bold" />
                      </button>
                    </div>
                  );
                })}
              </div>
            </div>
          )}

          {/* Footer Callout for Custom MCP */}
          <div className="rounded-xl border border-dashed border-thread bg-cloth p-5 flex flex-col sm:flex-row items-center justify-between gap-4">
            <div className="flex items-center gap-3.5">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg bg-bindery-pale text-bindery">
                <Plugs size={20} />
              </div>
              <div>
                <h4 className="text-sm font-semibold text-iron">Need a private or in-house connector?</h4>
                <p className="text-xs text-stone">
                  Connect any standard Model Context Protocol (MCP) server over HTTP or STDIO (Docker sidecar).
                </p>
              </div>
            </div>
            <div className="flex items-center gap-2 shrink-0">
              <button
                type="button"
                className={btnClass("primary")}
                onClick={() => setTab("custom")}
              >
                <Plus size={14} weight="bold" />
                Connect Custom MCP
              </button>
            </div>
          </div>
        </div>
      )}

      {/* TAB 3: CUSTOM CONNECTOR */}
      {tab === "custom" && (
        <div className="max-w-2xl space-y-6">
          <div className="rounded-xl border border-thread bg-folio p-6 shadow-2xs">
            <div className="mb-5 border-b border-thread pb-4">
              <h2 className="text-base font-semibold text-iron">Connect Custom MCP Server</h2>
              <p className="mt-1 text-xs text-stone">
                Attach your own MCP integration. Choose HTTP for remote servers or STDIO for an isolated Docker sidecar container.
              </p>
            </div>

            <form onSubmit={submitAddCustom}>
              <ConnectorFields
                value={customDraft}
                onChange={setCustomDraft}
              />
              <div className="mt-6 flex items-center gap-2.5">
                <Btn kind="primary" type="submit">
                  Add connector
                </Btn>
                <Btn
                  kind="ghost"
                  type="button"
                  onClick={() => setCustomDraft(emptyDraft())}
                >
                  Reset
                </Btn>
                <Btn
                  kind="ghost"
                  type="button"
                  onClick={() => setTab("library")}
                >
                  Cancel
                </Btn>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* POPUP: Add From Library Modal */}
      {libraryModal && (
        <div className="fixed inset-0 z-30 flex items-start justify-center overflow-y-auto bg-iron/30 backdrop-blur-xs py-[6vh] px-4">
          <div className="w-full max-w-[560px] rounded-xl border border-thread bg-folio p-6 shadow-xl relative animate-in fade-in zoom-in-95 duration-150">
            <button
              type="button"
              className="absolute top-4 right-4 text-stone hover:text-iron p-1 rounded-md transition-colors"
              onClick={closeLibraryModal}
              title="Close"
            >
              <X size={18} />
            </button>

            <form onSubmit={submitAddLibrary}>
              <ConnectorFields
                value={libraryDraft}
                onChange={setLibraryDraft}
                fromCatalog={true}
                catalogGuide={libraryModal.catalogGuide}
              />
              <div className="mt-6 flex items-center gap-2">
                <Btn kind="primary" type="submit">
                  Add to Bot
                </Btn>
                <Btn kind="ghost" type="button" onClick={closeLibraryModal}>
                  Cancel
                </Btn>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* POPUP: Edit Attached Connector Modal */}
      {edit && (
        <div className="fixed inset-0 z-30 flex items-start justify-center overflow-y-auto bg-iron/30 backdrop-blur-xs py-[6vh] px-4">
          <div className="w-full max-w-[560px] rounded-xl border border-thread bg-folio p-6 shadow-xl relative">
            <button
              type="button"
              className="absolute top-4 right-4 text-stone hover:text-iron p-1 rounded-md transition-colors"
              onClick={() => setEdit(null)}
              title="Close"
            >
              <X size={18} />
            </button>

            <h3 className="mb-4 text-base font-semibold text-iron">Edit connector</h3>
            <form onSubmit={saveEdit}>
              <ConnectorFields value={editDraft} onChange={setEditDraft} existing />
              <div className="mt-6 flex items-center gap-2">
                <Btn kind="primary" type="submit">
                  Save changes
                </Btn>
                <Btn kind="ghost" type="button" onClick={() => setEdit(null)}>
                  Cancel
                </Btn>
              </div>
            </form>
          </div>
        </div>
      )}
    </div>
  );
}
