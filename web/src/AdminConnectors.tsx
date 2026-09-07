import { ArrowCounterClockwise, Plus, Trash } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { Link, Route, Routes, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { ConnectorFields, ConnectorMark, McpChip, draftFrom, emptyDraft, specOf, type ConnectorDraft } from "./ConnectorForm";
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

function CatalogList() {
  const [rows, setRows] = useState<Connector[] | null>(null);
  const [err, setErr] = useState("");
  const [arm, setArm] = useState("");
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
  return (
    <div>
      <div className="mb-4 flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-[22px] font-medium">Connectors Library</h2>
        <div className="flex flex-wrap gap-2">
          <Btn kind="secondary" type="button" onClick={() => void seed()} icon={<ArrowCounterClockwise size={12} />}>
            Re-add defaults
          </Btn>
          <Link to="/admin/connectors/new" className={btnClass("primary")}>
            <Plus size={16} />
            Add connector
          </Link>
        </div>
      </div>
      <p className="mb-4 text-stone">Presets every Bot can pick. Attaching copies the preset onto that Bot.</p>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {rows === null ? (
        <p className="text-stone">Loading…</p>
      ) : rows.length === 0 ? (
        <p className="text-stone">No presets yet. HTTP MCP servers added here show up when a Bot adds from the library.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((c) => (
            <div key={c.id} className="flex flex-wrap items-center gap-2 rounded-[10px] border border-thread bg-folio px-3 py-3 hover:border-[#B9B3A6]">
              <Link to={`/admin/connectors/${c.id}`} className="flex min-w-0 flex-1 items-center gap-3">
                <ConnectorMark id={c.id} hasImage={c.hasImage} />
                <div className="min-w-0 flex-1">
                  <div className="flex items-center gap-2">
                    <span className="font-medium">{c.name}</span>
                    <McpChip />
                  </div>
                  <p className="truncate text-stone">{c.description || c.httpUrl}</p>
                </div>
                <span className="hidden font-mono text-stone wide:inline">{c.transport} · {c.auth} · {c.defaultMode || "ask"}</span>
              </Link>
              <Btn
                kind="deny"
                type="button"
                className="shrink-0"
                icon={<Trash size={12} />}
                onClick={(e) => {
                  e.preventDefault();
                  e.stopPropagation();
                  if (arm !== c.id) {
                    setArm(c.id);
                    return;
                  }
                  setArm("");
                  void remove(c.id);
                }}
              >
                {arm === c.id ? "Remove?" : "Remove"}
              </Btn>
            </div>
          ))}
        </div>
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
      <ConnectorFields value={draft} onChange={setDraft} existing={!!id} />
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
