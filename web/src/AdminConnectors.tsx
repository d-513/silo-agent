import { Plus, Plugs } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { Link, Route, Routes, useNavigate, useParams } from "react-router-dom";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import type { Connector } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

export function ConnectorMark({ id, hasImage, size = 40 }: { id: string; hasImage: boolean; size?: number }) {
  if (hasImage) {
    return (
      <img
        src={`/connectors/${id}/image`}
        alt=""
        className="shrink-0 rounded object-cover"
        style={{ width: size, height: size }}
      />
    );
  }
  return (
    <span
      className="flex shrink-0 items-center justify-center rounded bg-cloth text-stone"
      style={{ width: size, height: size }}
    >
      <Plugs size={Math.round(size * 0.45)} />
    </span>
  );
}

export function McpChip() {
  return <span className="rounded bg-slate px-1.5 py-0.5 text-[11px] font-medium text-plaster">MCP</span>;
}

export function AdminConnectors() {
  return (
    <Routes>
      <Route index element={<CatalogList />} />
      <Route path="new" element={<ConnectorForm />} />
      <Route path=":id" element={<ConnectorForm />} />
    </Routes>
  );
}

function CatalogList() {
  const [rows, setRows] = useState<Connector[] | null>(null);
  const [err, setErr] = useState("");
  useEffect(() => {
    ui.listConnectors({})
      .then((r) => setRows(r.connectors))
      .catch((e) => setErr(fail(e)));
  }, []);
  return (
    <div>
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-[22px] font-medium">Connectors</h2>
        <Link to="/admin/connectors/new" className={btnClass("primary")}>
          <Plus size={16} />
          Add connector
        </Link>
      </div>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {rows === null ? (
        <p className="text-stone">Loading…</p>
      ) : rows.length === 0 ? (
        <p className="text-stone">No connectors yet. HTTP MCP servers live here for every Bot to pick from.</p>
      ) : (
        <div className="flex flex-col gap-2">
          {rows.map((c) => (
            <Link
              key={c.id}
              to={`/admin/connectors/${c.id}`}
              className="flex items-center gap-3 rounded-[10px] border border-thread bg-folio px-3 py-3 hover:border-[#B9B3A6]"
            >
              <ConnectorMark id={c.id} hasImage={c.hasImage} />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2">
                  <span className="font-medium">{c.name}</span>
                  <McpChip />
                </div>
                <p className="truncate text-stone">{c.description || c.httpUrl}</p>
              </div>
              <span className="font-mono text-stone">{c.transport} · {c.auth} · {c.defaultMode || "ask"}</span>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}

function ConnectorForm() {
  const { id } = useParams();
  const nav = useNavigate();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [httpUrl, setHttpUrl] = useState("");
  const [auth, setAuth] = useState("none");
  const [defaultMode, setDefaultMode] = useState("ask");
  const [transport, setTransport] = useState("http");
  const [headers, setHeaders] = useState<{ name: string; value: string }[]>([{ name: "", value: "" }]);
  const [image, setImage] = useState<Uint8Array | undefined>();
  const [imageType, setImageType] = useState("");
  const [hasImage, setHasImage] = useState(false);
  const [clearImage, setClearImage] = useState(false);
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
        setName(c.name);
        setDescription(c.description);
        setHttpUrl(c.httpUrl);
        setAuth(c.auth || "none");
        setDefaultMode(c.defaultMode || "ask");
        setTransport(c.transport || "http");
        setHasImage(c.hasImage);
        setHeaders(c.headerKeys.length ? c.headerKeys.map((h) => ({ name: h.name, value: "" })) : [{ name: "", value: "" }]);
        setLoaded(true);
      })
      .catch((e) => setErr(fail(e)));
  }, [id]);

  async function onFile(f: File | undefined) {
    if (!f) return;
    const buf = new Uint8Array(await f.arrayBuffer());
    setImage(buf);
    setImageType(f.type);
    setClearImage(false);
    setHasImage(true);
  }

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    const hdrs = headers.filter((h) => h.name.trim());
    try {
      if (id) {
        await ui.updateConnector({
          id,
          name,
          description,
          httpUrl,
          auth,
          defaultMode,
          transport,
          headers: hdrs,
          image,
          imageType,
          clearImage,
        });
      } else {
        await ui.createConnector({
          name,
          description,
          httpUrl,
          auth,
          defaultMode,
          transport,
          headers: hdrs,
          image,
          imageType,
        });
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
      <h2 className="mb-4 text-[22px] font-medium">{id ? "Edit connector" : "Add connector"}</h2>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      <label className="mb-1 block text-[12px] font-medium text-stone">Name</label>
      <input className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3" value={name} onChange={(e) => setName(e.target.value)} required />
      <label className="mb-1 block text-[12px] font-medium text-stone">Description</label>
      <input className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3" value={description} onChange={(e) => setDescription(e.target.value)} />
      <label className="mb-1 block text-[12px] font-medium text-stone">Image</label>
      <div className="mb-3 flex items-center gap-3">
        {id ? <ConnectorMark id={id} hasImage={hasImage && !clearImage} /> : hasImage ? <span className="text-stone">chosen</span> : <ConnectorMark id="" hasImage={false} />}
        <input type="file" accept="image/*" onChange={(e) => onFile(e.target.files?.[0])} />
        {id && hasImage && !clearImage && (
          <button type="button" className="text-stone hover:text-carmine" onClick={() => { setClearImage(true); setImage(undefined); setHasImage(false); }}>
            Remove
          </button>
        )}
      </div>
      <div className="mb-1 text-[12px] font-medium text-stone">Type</div>
      <p className="mb-3 text-stone">MCP</p>
      <div className="mb-1 text-[12px] font-medium text-stone">Transport</div>
      <div className="mb-3 flex overflow-hidden rounded border border-thread">
        <button type="button" className={`h-9 flex-1 ${transport === "http" ? "bg-bindery-pale" : "bg-folio text-stone"}`} onClick={() => setTransport("http")}>
          HTTP
        </button>
        <button type="button" disabled className="h-9 flex-1 bg-linen text-stone" title="Coming later">
          STDIO
        </button>
      </div>
      <label className="mb-1 block text-[12px] font-medium text-stone">URL</label>
      <input className="mb-3 h-9 w-full rounded border border-thread bg-folio px-3 font-mono" value={httpUrl} onChange={(e) => setHttpUrl(e.target.value)} placeholder="https://…" required />
      <div className="mb-1 text-[12px] font-medium text-stone">Auth</div>
      <div className="mb-3 flex overflow-hidden rounded border border-thread">
        <button type="button" className={`h-9 flex-1 ${auth === "none" ? "bg-bindery-pale" : "bg-folio text-stone"}`} onClick={() => setAuth("none")}>
          None
        </button>
        <button type="button" className={`h-9 flex-1 ${auth === "oauth" ? "bg-bindery-pale" : "bg-folio text-stone"}`} onClick={() => setAuth("oauth")}>
          OAuth
        </button>
      </div>
      <div className="mb-1 text-[12px] font-medium text-stone">Default for tools</div>
      <p className="mb-2 text-stone">When a Bot has no rule for an action. Bots can still override on Rules.</p>
      <div className="mb-3 flex overflow-hidden rounded border border-thread">
        {(["allow", "ask", "deny"] as const).map((m) => (
          <button key={m} type="button" className={`h-9 flex-1 capitalize ${defaultMode === m ? "bg-bindery-pale" : "bg-folio text-stone"}`} onClick={() => setDefaultMode(m)}>
            {m === "allow" ? "Allow" : m === "deny" ? "Deny" : "Ask"}
          </button>
        ))}
      </div>
      <div className="mb-1 text-[12px] font-medium text-stone">Extra headers</div>
      <p className="mb-2 text-stone">Values are stored on the Control Plane and never shown again.</p>
      {headers.map((h, i) => (
        <div key={i} className="mb-2 flex gap-2">
          <input className="h-9 w-40 rounded border border-thread bg-folio px-2 font-mono" placeholder="Name" value={h.name} onChange={(e) => setHeaders((xs) => xs.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
          <input className="h-9 min-w-0 flex-1 rounded border border-thread bg-folio px-2 font-mono" placeholder={id ? "unchanged" : "Value"} type="password" value={h.value} onChange={(e) => setHeaders((xs) => xs.map((x, j) => (j === i ? { ...x, value: e.target.value } : x)))} />
        </div>
      ))}
      <button type="button" className="mb-6 text-bindery" onClick={() => setHeaders((xs) => [...xs, { name: "", value: "" }])}>
        Add header
      </button>
      <div className="flex gap-2">
        <Btn kind="primary" type="submit">
          {id ? "Save" : "Add connector"}
        </Btn>
        <Btn kind="ghost" type="button" onClick={() => nav("/admin/connectors")}>
          Cancel
        </Btn>
        {id && (
          <Btn kind="deny" type="button" onClick={remove}>
            Delete
          </Btn>
        )}
      </div>
    </form>
  );
}
