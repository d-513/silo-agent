import { ArrowClockwise, Plus } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import {
  ConnectorFields,
  ConnectorMark,
  CustomChip,
  McpChip,
  Segmented,
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
  if (s === "error") return "Error";
  return "Ready";
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

export function BotConnectors({
  botId,
  onNeedAuth,
}: {
  botId: string;
  onNeedAuth: (row: BotConnector | null) => void;
}) {
  const [attached, setAttached] = useState<BotConnector[]>([]);
  const [catalog, setCatalog] = useState<Connector[]>([]);
  const [pick, setPick] = useState(false);
  const [mode, setMode] = useState<"library" | "custom">("library");
  const [picked, setPicked] = useState<Connector | null>(null);
  const [draft, setDraft] = useState<ConnectorDraft>(emptyDraft());
  const [edit, setEdit] = useState<BotConnector | null>(null);
  const [editDraft, setEditDraft] = useState<ConnectorDraft>(emptyDraft());
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState("");

  async function load() {
    const [a, c] = await Promise.all([ui.listBotConnectors({ botId }), ui.listConnectors({})]);
    setAttached(a.connectors);
    setCatalog(c.connectors);
  }

  useEffect(() => {
    load().catch((e) => setErr(fail(e)));
    const t = setInterval(() => load().catch(() => {}), 3000);
    return () => clearInterval(t);
  }, [botId]);

  function chooseLibrary(c: Connector) {
    setErr("");
    setPicked(c);
    setDraft({ ...draftFrom(c), name: nextCopyName(c.name, attached.map((x) => x.connector?.name ?? "")) });
  }

  async function addConnector(e: React.FormEvent) {
    e.preventDefault();
    setErr("");
    try {
      const row = await ui.createBotConnector({
        botId,
        ...specOf(draft),
        sourceId: picked?.id ?? "",
      });
      closePick();
      await load();
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

  async function detach(id: string) {
    setErr("");
    try {
      await ui.detachConnector({ botId, id });
      await load();
    } catch (e) {
      setErr(fail(e));
    }
  }

  function closePick() {
    setPick(false);
    setMode("library");
    setPicked(null);
    setDraft(emptyDraft());
  }

  return (
    <div className="mx-auto w-[760px] p-7">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-[22px] font-medium">Connectors</h2>
        <button className={btnClass("primary")} onClick={() => setPick(true)}>
          <Plus size={16} />
          Add connector
        </button>
      </div>
      <p className="mb-4 text-stone">Pick a library preset or add a custom MCP. Add the same preset again for another account (GitHub 2). The Control Plane talks to the server; this Bot never sees tokens.</p>
      {err && <p className="mb-3 text-carmine">{err}</p>}
      {attached.length === 0 && <p className="mb-4 text-stone">None on this Bot yet.</p>}
      {attached.map((row) => {
        const c = row.connector;
        if (!c) return null;
        return (
          <div key={row.id} className="mb-2 flex items-center gap-3 rounded-[10px] border border-thread bg-folio px-3 py-3">
            <ConnectorMark id={c.id} hasImage={c.hasImage} />
            <div className="min-w-0 flex-1">
              <div className="flex items-center gap-2">
                <span className="font-medium">{c.name}</span>
                <McpChip />
                {isCustom(c) && <CustomChip />}
              </div>
              <p className="truncate text-stone">{c.description}</p>
              <p className="text-stone">{statusLabel(row.authStatus)}{row.lastError ? ` · ${row.lastError}` : ""}</p>
            </div>
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
              Edit
            </Btn>
            <Btn kind="ghost" disabled={busy === row.id} onClick={() => refresh(row.id)}>
              <ArrowClockwise size={16} />
              Refresh
            </Btn>
            <Btn kind="ghost" className="text-carmine hover:text-carmine" onClick={() => detach(row.id)}>
              Remove
            </Btn>
          </div>
        );
      })}
      {pick && (
        <div className="fixed inset-0 z-20 flex items-start justify-center overflow-y-auto bg-iron/20 py-[8vh]">
          <div className="w-[560px] rounded-[10px] border border-thread bg-folio p-5">
            <h3 className="mb-3 text-[16px] font-medium">Add connector</h3>
            <div className="mb-4">
              <Segmented
                value={mode}
                onChange={(v) => {
                  setMode(v as "library" | "custom");
                  setPicked(null);
                  setDraft(emptyDraft());
                  setErr("");
                }}
                options={[
                  { id: "library", label: "Library" },
                  { id: "custom", label: "Custom" },
                ]}
              />
            </div>
            {mode === "library" && !picked ? (
              <>
                {catalog.length === 0 && <p className="text-stone">Nothing in the library.</p>}
                {catalog.map((c) => (
                  <button
                    key={c.id}
                    className="mb-2 flex w-full items-center gap-3 rounded border border-thread px-3 py-2 text-left hover:border-[#B9B3A6]"
                    onClick={() => chooseLibrary(c)}
                  >
                    <ConnectorMark id={c.id} hasImage={c.hasImage} size={32} />
                    <span className="min-w-0 flex-1">
                      <span className="flex items-center gap-2">
                        {c.name}
                        <McpChip />
                      </span>
                      <span className="block truncate text-stone">{c.description}</span>
                    </span>
                  </button>
                ))}
                <Btn kind="ghost" className="mt-2" onClick={closePick}>
                  Cancel
                </Btn>
              </>
            ) : (
              <form onSubmit={addConnector}>
                <ConnectorFields
                  value={draft}
                  onChange={setDraft}
                  fromCatalog={!!picked}
                  catalogGuide={picked?.catalogGuide}
                />
                <div className="flex gap-2">
                  <Btn kind="primary" type="submit">
                    Add connector
                  </Btn>
                  {picked && (
                    <Btn
                      kind="ghost"
                      type="button"
                      onClick={() => {
                        setPicked(null);
                        setDraft(emptyDraft());
                      }}
                    >
                      Back
                    </Btn>
                  )}
                  <Btn kind="ghost" type="button" onClick={closePick}>
                    Cancel
                  </Btn>
                </div>
              </form>
            )}
          </div>
        </div>
      )}
      {edit && (
        <div className="fixed inset-0 z-20 flex items-start justify-center overflow-y-auto bg-iron/20 py-[8vh]">
          <form onSubmit={saveEdit} className="w-[560px] rounded-[10px] border border-thread bg-folio p-5">
            <h3 className="mb-3 text-[16px] font-medium">Edit connector</h3>
            <ConnectorFields value={editDraft} onChange={setEditDraft} existing />
            <div className="flex gap-2">
              <Btn kind="primary" type="submit">
                Save
              </Btn>
              <Btn kind="ghost" type="button" onClick={() => setEdit(null)}>
                Cancel
              </Btn>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}
