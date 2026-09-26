import { Plus, RotateCcw, Search, Trash2 } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { Link, Route, Routes, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { ArmedButton, SaveButton, useSave } from "./Feedback";
import { Btn, btnClass } from "./Btn";
import { ConnectorFields, ConnectorMark, McpChip, CategoryChip, draftFrom, emptyDraft, specOf, type ConnectorDraft } from "./ConnectorForm";
import { inputClass, SkeletonRows } from "./Field";
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

function PresetCard({ c, onRemove }: { c: Connector; onRemove: () => void }) {
  return (
    <div className="group flex items-start justify-between gap-3 rounded-card shadow-card bg-surface p-4 transition-colors hover:shadow-float">
      <Link to={`/admin/connectors/${c.id}`} className="flex min-w-0 flex-1 items-start gap-3">
        <div className="shrink-0 pt-0.5">
          <ConnectorMark id={c.id} hasImage={c.hasImage} size={40} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate font-medium text-ink">{c.name}</span>
            <McpChip />
            {c.category && <CategoryChip label={c.category} />}
            {c.autoAttach && (
              <span className="rounded-sm bg-cobalt-pale px-1.5 py-0.5 text-[11px] font-medium text-cobalt">Default</span>
            )}
          </div>
          <p className="mt-1 line-clamp-2 text-[12px] leading-relaxed text-ink-3">{c.description || c.httpUrl || c.stdioCommand}</p>
          <div className="mt-2 flex items-center gap-2 font-mono text-[11px] text-ink-3">
            <span className="uppercase">{c.transport}</span>
            <span>·</span>
            <span>{c.auth}</span>
            <span>·</span>
            <span>{c.defaultMode || "ask"}</span>
          </div>
        </div>
      </Link>
      <ArmedButton
        kind="ghost"
        size="sm"
        className="shrink-0"
        title="Remove from the library"
        armedLabel="Click again to remove"
        icon={<Trash2 size={13} />}
        onConfirm={onRemove}
      >
        Remove
      </ArmedButton>
    </div>
  );
}

function CatalogList() {
  const [rows, setRows] = useState<Connector[] | null>(null);
  const [err, setErr] = useState("");
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

  const grid = (items: Connector[]) => (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
      {items.map((c) => (
        <PresetCard key={c.id} c={c} onRemove={() => void remove(c.id)} />
      ))}
    </div>
  );

  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Connectors Library</h2>
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
      <p className="mb-4 text-ink-2">Presets every Bot can pick. Attaching copies the preset onto that Bot. Removing one keeps it gone; <span className="text-ink">Add new presets</span> only pulls in newly published ones.</p>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      {rows === null ? (
        <SkeletonRows rows={4} height={96} />
      ) : rows.length === 0 ? (
        <p className="text-ink-2">No presets yet. HTTP or STDIO MCP servers added here show up when a Bot adds from the library.</p>
      ) : (
        <>
          <div className="mb-5 flex flex-wrap items-center gap-2">
            <div className="relative min-w-[200px] flex-1 sm:max-w-sm">
              <Search size={14} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
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
            <p className="text-ink-2">No presets match “{search}”.</p>
          ) : groups ? (
            <div className="space-y-7">
              {Array.from(groups.entries()).map(([categoryName, items]) => (
                <section key={categoryName} className="space-y-3">
                  <div className="flex items-center gap-2 border-b border-line/70 pb-2">
                    <h3 className="text-sm font-semibold text-ink">{categoryName}</h3>
                    <span className="rounded-full bg-well px-2 py-0.5 text-[10px] font-medium text-ink-3">{items.length}</span>
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
  const saver = useSave();

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
      await saver.run(async () => {
        if (id) await ui.updateConnector({ id, ...spec });
        else await ui.createConnector(spec);
      });
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

  if (!loaded) return err ? <p className="text-vermilion">{err}</p> : <SkeletonRows rows={4} height={60} className="max-w-[560px]" />;

  return (
    <form onSubmit={onSubmit} className="max-w-[560px]">
      <h2 className="mb-4 text-[22px] leading-7 font-medium tracking-[-0.015em]">{id ? "Edit preset" : "Add preset"}</h2>
      {err && <p className="mb-3 text-vermilion">{err}</p>}
      <ConnectorFields value={draft} onChange={setDraft} existing={!!id} allowStdioImage allowAutoAttach />
      <div className="flex gap-2">
        <SaveButton type="submit" state={saver.state}>
          {id ? "Save" : "Add to library"}
        </SaveButton>
        <Btn kind="ghost" type="button" onClick={() => nav("/admin/connectors")}>
          Cancel
        </Btn>
        {id && (
          <ArmedButton kind="ghost" icon={<Trash2 size={14} />} onConfirm={() => void remove()}>
            Delete
          </ArmedButton>
        )}
      </div>
    </form>
  );
}
