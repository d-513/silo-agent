import { ArrowClockwise, Plus } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { ui } from "./api";
import { Btn, btnClass } from "./Btn";
import { ConnectorMark, McpChip } from "./AdminConnectors";
import type { BotConnector, Connector } from "./gen/silo/v1/ui_pb";

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

export function BotConnectors({ botId }: { botId: string }) {
  const [attached, setAttached] = useState<BotConnector[]>([]);
  const [catalog, setCatalog] = useState<Connector[]>([]);
  const [pick, setPick] = useState(false);
  const [err, setErr] = useState("");
  const [prompt, setPrompt] = useState<BotConnector | null>(null);

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

  async function attach(id: string) {
    setErr("");
    try {
      const row = await ui.attachConnector({ botId, connectorId: id });
      setPick(false);
      await load();
      if (row.authStatus === "needs_auth") setPrompt(row);
    } catch (e) {
      setErr(fail(e));
    }
  }

  async function authorize(row: BotConnector) {
    setErr("");
    try {
      const r = await ui.startConnectorAuth({ botId, id: row.id });
      if (r.authorizeUrl) window.open(r.authorizeUrl, "silo-oauth", "width=480,height=720");
      setPrompt(null);
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

  const taken = new Set(attached.map((x) => x.connector?.id));
  const available = catalog.filter((c) => !taken.has(c.id));

  return (
    <div className="mx-auto w-[760px] p-7">
      <div className="mb-4 flex items-center justify-between">
        <h2 className="text-[22px] font-medium">Connectors</h2>
        <button className={btnClass("primary")} onClick={() => setPick(true)} disabled={available.length === 0}>
          <Plus size={16} />
          Add connector
        </button>
      </div>
      <p className="mb-4 text-stone">Pick from the site catalog. The Control Plane talks to the server; this Bot never sees tokens.</p>
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
              </div>
              <p className="truncate text-stone">{c.description}</p>
              <p className="text-stone">{statusLabel(row.authStatus)}{row.lastError ? ` · ${row.lastError}` : ""}</p>
            </div>
            {row.authStatus === "needs_auth" && (
              <Btn kind="primary" onClick={() => authorize(row)}>
                Authorize
              </Btn>
            )}
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
        <div className="fixed inset-0 z-20 flex items-start justify-center bg-iron/20 pt-[12vh]">
          <div className="w-[480px] rounded-[10px] border border-thread bg-folio p-5">
            <h3 className="mb-3 text-[16px] font-medium">Add connector</h3>
            {available.length === 0 && <p className="text-stone">Nothing left in the catalog.</p>}
            {available.map((c) => (
              <button
                key={c.id}
                className="mb-2 flex w-full items-center gap-3 rounded border border-thread px-3 py-2 text-left hover:border-[#B9B3A6]"
                onClick={() => attach(c.id)}
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
            <Btn kind="ghost" className="mt-2" onClick={() => setPick(false)}>
              Cancel
            </Btn>
          </div>
        </div>
      )}
      {prompt?.connector && (
        <div className="fixed inset-y-0 right-0 z-30 w-[400px] border-l border-thread bg-folio p-5">
          <div className="mb-1 text-[12px] font-medium text-carmine">Needs you</div>
          <h3 className="mb-2 text-[16px] font-medium">Authorize {prompt.connector.name}</h3>
          <p className="mb-4 text-stone">This connector uses OAuth. A browser window will ask you to allow Silo to call it.</p>
          <div className="flex gap-2">
            <Btn kind="primary" onClick={() => authorize(prompt)}>
              Authorize
            </Btn>
            <Btn kind="ghost" onClick={() => setPrompt(null)}>
              Later
            </Btn>
          </div>
        </div>
      )}
    </div>
  );
}
