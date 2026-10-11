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
import { t } from "../i18n.ts";

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
      const tn = await ui.createTunnel({ botId, port: p.port, public: pub });
      change((rows) => [...rows, tn]);
      setPort("");
      setPub(false);
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(false);
    }
  }

  async function setPublic(tn: Tunnel, on: boolean) {
    setErr("");
    change((rows) => rows.map((r) => (r.id === tn.id ? ({ ...r, public: on } as Tunnel) : r)));
    try {
      const u = await ui.updateTunnel({ botId, id: tn.id, public: on });
      change((rows) => rows.map((r) => (r.id === tn.id ? u : r)));
    } catch (e) {
      change((rows) => rows.map((r) => (r.id === tn.id ? tn : r)));
      setErr(fail(e));
    }
  }

  async function remove(tn: Tunnel) {
    setErr("");
    try {
      await ui.deleteTunnel({ botId, id: tn.id });
      change((rows) => rows.filter((r) => r.id !== tn.id));
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
      <h2 className="text-title">{t("Tunnels")}</h2>
      <p className="mb-5 mt-1 text-[13.5px] leading-[21px] text-ink-2">
        {t("Addresses for services running on this Bot's machine. A private tunnel opens for you, signed in to Silo; a public one opens for anyone with the link. The Bot can open tunnels too.")}
      </p>
      {err ? <p className="mb-4 text-[13px] text-vermilion">{err}</p> : null}
      {notice ? (
        <div className="mb-5 rounded-card bg-well px-5 py-4 text-[13.5px] leading-[21px] text-ink-2">
          {notice.text}{" "}
          {notice.admin ? (
            <Link to="/admin/settings/$section" params={{ section: "tunnels" }} className="font-medium text-cobalt hover:underline">
              {t("Open settings")}
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
          <Field label={t("Port")} error={portErr} className="w-36">
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
          <Field label={t("Who can open it")} className="w-fit">
            <TunnelAccess isPublic={pub} onChange={setPub} />
          </Field>
          <div className="pt-[22px]">
            <Btn kind="primary" type="submit" disabled={busy || full} icon={<Plus size={13} />}>
              {t("Open tunnel")}
            </Btn>
          </div>
          <p className={`basis-full text-[12.5px] leading-[18px] ${pub ? "text-vermilion" : "text-ink-3"}`}>{accessOf(pub).caption}</p>
          {full ? <p className="basis-full text-[12.5px] text-ink-3">{t("This Bot has {{count}} tunnels, the most it can have. Delete one to add another.", { count: data?.max })}</p> : null}
        </form>
      ) : null}

      {data === null ? (
        <SkeletonRows rows={2} height={84} />
      ) : rows.length === 0 ? (
        <div className="flex flex-col items-center gap-2 rounded-card bg-well px-6 py-12 text-center">
          <Globe size={20} className="text-ink-3" />
          <p className="text-ink-2">{t("No tunnels yet.")}</p>
          <p className="max-w-sm text-[12.5px] text-ink-3">{t("Start a service on the Bot's machine, then add its port here, or ask the Bot to open one.")}</p>
        </div>
      ) : (
        <ul className="space-y-2.5">
          {rows.map((tn) => (
            <li key={tn.id} className="flex flex-wrap items-center gap-x-4 gap-y-3 rounded-card bg-surface p-4 shadow-card">
              <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">{tn.public ? <Globe size={18} /> : <Lock size={18} />}</div>
              <div className="min-w-0 flex-1 basis-60">
                {tn.url ? (
                  <a href={tn.url} target="_blank" rel="noopener noreferrer" className="block truncate font-mono text-[13.5px] font-medium text-ink hover:underline">
                    {tn.url.replace(/^https?:\/\//, "")}
                  </a>
                ) : (
                  <span className="block truncate font-mono text-[13.5px] font-medium text-ink">{tn.name}</span>
                )}
                <p className={`mt-0.5 text-[12.5px] ${tn.public ? "text-vermilion" : "text-ink-2"}`}>{accessOf(tn.public).caption}</p>
                <p className="mt-0.5 flex flex-wrap gap-x-1.5 text-[12.5px] text-ink-3">
                  <span>{t("Port {{port}}", { port: tn.port })}</span>
                  <span aria-hidden>·</span>
                  <span>{tn.createdBy === "bot" ? t("Opened by the Bot") : t("Opened by you")}</span>
                  <span aria-hidden>·</span>
                  <span>{tn.lastUsedAt ? t("Used {{when}}", { when: ago(tn.lastUsedAt) }) : t("Not used yet")}</span>
                </p>
              </div>
              <div className="flex shrink-0 items-center gap-1.5">
                <TunnelAccess isPublic={tn.public} confirm onChange={(on) => void setPublic(tn, on)} />
                {tn.url ? <CopyButton text={tn.url} title={t("Copy address")} /> : null}
                {tn.url ? (
                  <a
                    href={tn.url}
                    target="_blank"
                    rel="noopener noreferrer"
                    title={t("Open in a new tab")}
                    aria-label={t("Open in a new tab")}
                    className="flex h-7 w-7 items-center justify-center rounded-sm text-ink-2 transition-[background-color,color] duration-[160ms] ease-quiet hover:bg-well hover:text-ink"
                  >
                    <ExternalLink size={14} />
                  </a>
                ) : null}
                <ArmedButton kind="ghost" size="sm" iconOnly title={t("Delete tunnel")} armedLabel={t("Click again to delete")} icon={<Trash2 size={13} />} onConfirm={() => void remove(tn)}>
                  {t("Delete")}
                </ArmedButton>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
