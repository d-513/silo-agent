import { Plus, RotateCcw, Search, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, Route, Routes, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { ConnectorFields, ConnectorMark, McpChip, CategoryChip, draftFrom, emptyDraft, specOf, type ConnectorDraft } from "./ConnectorForm";
import { inputClass } from "./Field";
import { Select } from "./Select";
import type { Connector } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function AdminConnectors() {
  return (
    <Routes>
      <Route index element={<CatalogList />} />
      <Route path="new" element={<LibraryForm />} />
      <Route path=":id" element={<LibraryForm />} />
    </Routes>
  );
}

function byName(a: Connector, b: Connector) {
  return a.name.localeCompare(b.name);
}

function categoryOf(c: Connector) {
  return c.category?.trim() || "General";
}

function RemoveBtn({ armed, onClick }: { armed: boolean; onClick: (e: React.MouseEvent) => void }) {
  return (
    <Btn
      kind="deny"
      type="button"
      className="shrink-0"
      icon={<Trash2 size={12} />}
      onClick={onClick}
      title={armed ? "Click again to remove from the library" : "Remove from the library"}
    >
      {armed ? "Remove?" : "Remove"}
    </Btn>
  );
}

function PresetCard({
  c,
  armed,
  onArm,
}: {
  c: Connector;
  armed: boolean;
  onArm: () => void;
}) {
  return (
    <div className="group flex items-start justify-between gap-3 rounded-[10px] border border-thread bg-folio p-4 transition-colors hover:border-hover">
      <Link to={`/admin/connectors/${c.id}`} className="flex min-w-0 flex-1 items-start gap-3">
        <div className="shrink-0 pt-0.5">
          <ConnectorMark id={c.id} hasImage={c.hasImage} size={40} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate font-medium text-iron">{c.name}</span>
            <McpChip />
            {c.category && <CategoryChip label={c.category} />}
            {c.autoAttach && (
              <span className="rounded-[6px] bg-bindery-pale px-1.5 py-0.5 text-[11px] font-medium text-bindery">Default</span>
            )}
          </div>
          <p className="mt-1 line-clamp-2 text-[12px] leading-relaxed text-stone">{c.description || c.httpUrl || c.stdioCommand}</p>
          <div className="mt-2 flex items-center gap-2 font-mono text-[11px] text-stone">
            <span className="uppercase">{c.transport}</span>
            <span>·</span>
            <span>{c.auth}</span>
            <span>·</span>
            <span>{c.defaultMode || "ask"}</span>
          </div>
        </div>
      </Link>
      <RemoveBtn armed={armed} onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        onArm();
      }} />
    </div>
  );
}

function CatalogList() {
  const [rows, setRows] = useState<Connector[] | null>(null);
  const [err, setErr] = useState("");
  const [arm, setArm] = useState("");
  const [search, setSearch] = useState("");
  const [sort, setSort] = useState("category");
  async function load() {
    const r = await ui.listConnectors({});
    setRows(r.connectors);
  }
  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
  }, []);
  async function seed() {
    setErr("");
    try {
      await ui.seedConnectors({});
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }
  async function remove(id: string) {
    setErr("");
    try {
      await ui.deleteConnector({ id });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    const list = (rows ?? []).filter(
      (c) =>
        !q ||
        c.name.toLowerCase().includes(q) ||
        (c.description || "").toLowerCase().includes(q) ||
        categoryOf(c).toLowerCase().includes(q) ||
        (c.transport || "").toLowerCase().includes(q),
    );
    if (sort === "name") return [...list].sort(byName);
    if (sort === "transport")
      return [...list].sort((a, b) => (a.transport || "").localeCompare(b.transport || "") || byName(a, b));
    return [...list].sort((a, b) => categoryOf(a).localeCompare(categoryOf(b)) || byName(a, b));
  }, [rows, search, sort]);

  const groups = useMemo(() => {
    if (sort !== "category") return null;
    const map = new Map<string, Connector[]>();
    for (const c of filtered) {
      const cat = categoryOf(c);
      const list = map.get(cat);
      if (list) list.push(c);
      else map.set(cat, [c]);
    }
    return map;
  }, [filtered, sort]);

  function removeClick(id: string) {
    if (arm !== id) {
      setArm(id);
      return;
    }
    setArm("");
    void remove(id);
  }

  const grid = (items: Connector[]) => (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
      {items.map((c) => (
        <PresetCard key={c.id} c={c} armed={arm === c.id} onArm={() => removeClick(c.id)} />
      ))}
    </div>
  );

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[22px] font-medium">Connectors Library</h2>
        <div className="flex flex-wrap gap-2">
          <Btn kind="secondary" type="button" onClick={() => void seed()} icon={<RotateCcw size={12} />}>
            Add new presets
          </Btn>
          <Link to="/admin/connectors/new" className={btnClass("primary")}>
            <Plus size={16} />
            Add connector
          </Link>
        </div>
      </div>
      <p className="mb-4 text-stone">Presets every Bot can pick. Attaching copies the preset onto that Bot. Removing one keeps it gone; <span className="text-iron">Add new presets</span> only pulls in newly published ones.</p>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {rows === null ? (
        <p className="text-stone">Loading…</p>
      ) : rows.length === 0 ? (
        <p className="text-stone">No presets yet. HTTP or STDIO MCP servers added here show up when a Bot adds from the library.</p>
      ) : (
        <>
          <div className="mb-5 flex flex-wrap items-center gap-2">
            <div className="relative min-w-[200px] flex-1 sm:max-w-sm">
              <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-stone" />
              <input
                className={`${inputClass} pl-9`}
                placeholder="Search presets…"
                value={search}
                onChange={(e) => setSearch(e.target.value)}
              />
            </div>
            <Select
              value={sort}
              onChange={setSort}
              ariaLabel="Sort presets"
              className="w-[200px] shrink-0"
              options={[
                { value: "category", label: "Group by category" },
                { value: "name", label: "Sort by name" },
                { value: "transport", label: "Sort by transport" },
              ]}
            />
          </div>
          {filtered.length === 0 ? (
            <p className="text-stone">No presets match “{search}”.</p>
          ) : groups ? (
            <div className="space-y-7">
              {Array.from(groups.entries()).map(([categoryName, items]) => (
                <section key={categoryName} className="space-y-3">
                  <div className="flex items-center gap-2 border-b border-thread/70 pb-2">
                    <h3 className="text-sm font-semibold text-iron">{categoryName}</h3>
                    <span className="rounded-full bg-cloth px-2 py-0.5 text-[10px] font-medium text-stone">{items.length}</span>
                  </div>
                  {grid(items)}
                </section>
              ))}
            </div>
          ) : (
            grid(filtered)
          )}
        </>
      )}
    </div>
  );
}

function LibraryForm() {
  const { id } = useParams();
  const nav = useNavigate();
  const [draft, setDraft] = useState<ConnectorDraft>(emptyDraft());
  const [err, setErr] = useState("");
  const [loaded, setLoaded] = useState(!id);

  useEffect(() => {
    if (!id) return;
    ui.listConnectors({})
      .then((r) => {
        const c = r.connectors.find((x) => x.id === id);
        if (!c) {
          setErr("not found");
          return;
        }
        setDraft(draftFrom(c));
        setLoaded(true);
      })
      .catch((e) => setErr(fail(e)));
  }, [id]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    const spec = specOf(draft);
    try {
      if (id) {
        await ui.updateConnector({ id, ...spec });
      } else {
        await ui.createConnector(spec);
      }
      nav("/admin/connectors");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  async function remove() {
    if (!id) return;
    setErr("");
    try {
      await ui.deleteConnector({ id });
      nav("/admin/connectors");
    } catch (ex) {
      setErr(fail(ex));
    }
  }

  if (!loaded) return <p className="text-stone">Loading…</p>;

  return (
    <form onSubmit={onSubmit} className="max-w-[560px]">
      <h2 className="mb-4 text-[22px] font-medium">{id ? "Edit preset" : "Add preset"}</h2>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      <ConnectorFields value={draft} onChange={setDraft} existing={!!id} allowStdioImage allowAutoAttach />
      <div className="flex gap-2">
        <Btn kind="primary" type="submit">
          {id ? "Save" : "Add to library"}
        </Btn>
        <Btn kind="ghost" type="button" onClick={() => nav("/admin/connectors")}>
          Cancel
        </Btn>
        {id && (
          <Btn kind="deny" type="button" onClick={() => void remove()}>
            Delete
          </Btn>
        )}
      </div>
    </form>
  );
}
