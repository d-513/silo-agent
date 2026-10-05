import { ExternalLink, Globe, Lock, Plus, Trash2 } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { useState } from "react";
import { Link } from "@tanstack/react-router";
import { ui } from "../api";
import { useAuth } from "../auth";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { ArmedButton, CopyButton } from "../Feedback";
import { Field, inputClass, SkeletonRows } from "../Field";
import { ago } from "../format";
import { UI, type Tunnel } from "../gen/silo/v1/ui_pb";
import { patch } from "../query";
import { accessOf, parsePort, tunnelNotice } from "../tunnels";
import { TunnelAccess } from "./TunnelAccess";

// TunnelsPane lists the addresses that reach services on this Bot's machine.
// A tunnel is a generated name and a port; private ones open for the owner
// (signed in to Silo), public ones for anyone with the link. The Bot can open
// them too (open_tunnel), so the list refreshes while the tab is open.
export function TunnelsPane({ botId }: { botId: string }) {
  const { admin } = useAuth();
  const q = useQuery(UI.method.listTunnels, { botId }, { refetchInterval: 5000 });
  const data = q.data ?? null;
  const [actErr, setErr] = useState("");
  const err = actErr || (q.error ? fail(q.error) : "");
  const [port, setPort] = useState("");
  const [portErr, setPortErr] = useState("");
  const [pub, setPub] = useState(false);
  const [busy, setBusy] = useState(false);

  const change = (fn: (rows: Tunnel[]) => Tunnel[]) => patch(UI.method.listTunnels, { botId }, (d) => ({ ...d, tunnels: fn(d.tunnels) }));

  async function add() {
    const p = parsePort(port);
    if ("error" in p) {
      setPortErr(p.error);
      return;
    }
    setPortErr("");
    setErr("");
    setBusy(true);
    try {
      const t = await ui.createTunnel({ botId, port: p.port, public: pub });
      change((rows) => [...rows, t]);
      setPort("");
      setPub(false);
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  async function setPublic(t: Tunnel, on: boolean) {
    setErr("");
    change((rows) => rows.map((r) => (r.id === t.id ? ({ ...r, public: on } as Tunnel) : r)));
    try {
      const u = await ui.updateTunnel({ botId, id: t.id, public: on });
      change((rows) => rows.map((r) => (r.id === t.id ? u : r)));
    } catch (e) {
      change((rows) => rows.map((r) => (r.id === t.id ? t : r)));
      setErr(fail(e));
    }
  }

  async function remove(t: Tunnel) {
    setErr("");
    try {
      await ui.deleteTunnel({ botId, id: t.id });
      change((rows) => rows.filter((r) => r.id !== t.id));
    } catch (e) {
      setErr(fail(e));
    }
  }

  const notice = data ? tunnelNotice(data.state, admin) : null;
  const usable = data?.state === "ok";
  const rows = data?.tunnels ?? [];
  const full = data ? rows.length >= data.max : false;

  return (
    <div className="silo-page pb-12">
      <h2 className="text-title">Tunnels</h2>
      <p className="mb-5 mt-1 text-[13.5px] leading-[21px] text-ink-2">
        Addresses for services running on this Bot's machine. A private tunnel opens for you, signed in to Silo; a public one opens for anyone with the link. The Bot can open tunnels too.
      </p>
      {err ? <p className="mb-4 text-[13px] text-vermilion">{err}</p> : null}
      {notice ? (
        <div className="mb-5 rounded-card bg-well px-5 py-4 text-[13.5px] leading-[21px] text-ink-2">
          {notice.text}{" "}
          {notice.admin ? (
            <Link to="/admin/settings" className="font-medium text-cobalt hover:underline">
              Open settings
            </Link>
          ) : null}
        </div>
      ) : null}

      {usable ? (
        <form
          className="mb-5 flex flex-wrap items-start gap-x-4 gap-y-3 rounded-card bg-surface px-5 py-4 shadow-card"
          onSubmit={(e) => {
            e.preventDefault();
            void add();
          }}
        >
          <Field label="Port" error={portErr} className="w-36">
            <input
              className={inputClass}
              inputMode="numeric"
              placeholder="8000"
              value={port}
              aria-invalid={portErr ? true : undefined}
              onChange={(e) => {
                setPort(e.target.value);
                setPortErr("");
              }}
            />
          </Field>
          <Field label="Who can open it" className="w-fit">
            <TunnelAccess isPublic={pub} onChange={setPub} />
          </Field>
          <div className="pt-[22px]">
            <Btn kind="primary" type="submit" disabled={busy || full} icon={<Plus size={13} />}>
              Open tunnel
            </Btn>
          </div>
          <p className={`basis-full text-[12.5px] leading-[18px] ${pub ? "text-vermilion" : "text-ink-3"}`}>{accessOf(pub).caption}</p>
          {full ? <p className="basis-full text-[12.5px] text-ink-3">This Bot has {data?.max} tunnels, the most it can have. Delete one to add another.</p> : null}
        </form>
      ) : null}

      {data === null ? (
        <SkeletonRows rows={2} height={84} />
      ) : rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <Globe size={20} className="text-ink-3" />
          <p className="text-ink-2">No tunnels yet.</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">Start a service on the Bot's machine, then add its port here, or ask the Bot to open one.</p>
        </div>
      ) : (
        <ul className="space-y-2.5">
          {rows.map((t) => (
            <li key={t.id} className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-card bg-surface p-4 shadow-card">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">{t.public ? <Globe size={18} /> : <Lock size={18} />}</div>
              <div className="min-w-0 flex-1 basis-60">
                {t.url ? (
                  <a href={t.url} target="_blank" rel="noopener noreferrer" className="block truncate font-mono text-[13.5px] font-medium text-ink hover:underline">
                    {t.url.replace(/^https?:\/\//, "")}
                  </a>
                ) : (
                  <span className="block truncate font-mono text-[13.5px] font-medium text-ink">{t.name}</span>
                )}
                <p className={`mt-0.5 text-[12.5px] ${t.public ? "text-vermilion" : "text-ink-2"}`}>{accessOf(t.public).caption}</p>
                <p className="mt-0.5 flex flex-wrap gap-x-1.5 text-[12.5px] text-ink-3">
                  <span>Port {t.port}</span>
                  <span aria-hidden>·</span>
                  <span>{t.createdBy === "bot" ? "Opened by the Bot" : "Opened by you"}</span>
                  <span aria-hidden>·</span>
                  <span>{t.lastUsedAt ? `Used ${ago(t.lastUsedAt)}` : "Not used yet"}</span>
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                <TunnelAccess isPublic={t.public} confirm onChange={(on) => void setPublic(t, on)} />
                {t.url ? <CopyButton text={t.url} title="Copy address" /> : null}
                {t.url ? (
                  <a
                    href={t.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    title="Open in a new tab"
                    aria-label="Open in a new tab"
                    className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-[background-color,color] duration-[160ms] ease-quiet hover:bg-well hover:text-ink"
                  >
                    <ExternalLink size={14} />
                  </a>
                ) : null}
                <ArmedButton kind="ghost" size="sm" iconOnly title="Delete tunnel" armedLabel="Click again to delete" icon={<Trash2 size={13} />} onConfirm={() => void remove(t)}>
                  Delete
                </ArmedButton>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
