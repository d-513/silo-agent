import { Boxes, ChevronRight, HardDrive, Monitor, Network, Plug, Power, Search, Shield, X } from "lucide-react";
import { useQuery } from "@connectrpc/connect-query";
import { Link } from "@tanstack/react-router";
import { useState, type ReactNode } from "react";
import { ui } from "./api";
import { setFold, useFolds } from "./adminContainerFolds";
import { Btn } from "./Btn";
import { fail } from "./errors";
import { ArmedButton } from "./Feedback";
import { ErrorWell, inputClass, Panel, SkeletonRows } from "./Field";
import { UI, type AdminBotGroup, type AdminContainer, type AdminNetwork, type AdminUserGroup } from "./gen/silo/v1/ui_pb";
import { fmtBytes } from "./format";
import { Lamp, Status } from "./Lamp";
import { msg, t } from "./i18n.ts";
import { recheck } from "./query";

// AdminContainers is the admin's view of the container engine: every user's
// Bots with their machines and sidecars, the egress gateway, and anything
// Silo left behind that the database no longer claims — with the client
// networks and the containers that sit on them. A container only appears
// here if Silo created it; the engine's other tenants are never listed and
// never touched.
//
// A large instance reads long, so everything folds: each user group and each
// Bot collapses to a header with its counts, the folds are the reader's
// (localStorage over the whole page), and the search box narrows the tree to
// one user or one Bot.

type Action = "start" | "stop" | "remove";

function n64(v: bigint | number | undefined) {
  if (typeof v === "bigint") return Number(v);
  return v ?? 0;
}

function Meter({ value, max }: { value: number; max: number }) {
  const pct = max > 0 ? Math.min(100, (value / max) * 100) : 0;
  return (
    <div className="h-1.5 overflow-hidden rounded-full bg-pressed">
      <div className="h-full rounded-full bg-ink transition-[width] duration-[280ms] ease-quiet" style={{ width: `${pct}%` }} />
    </div>
  );
}

function Stat({ label, value, meter }: { label: string; value: string; meter?: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-label-caps uppercase leading-4 text-ink-3">{label}</div>
      <div className="mt-0.5 truncate font-mono text-[15px] font-medium tabular-nums tracking-tight text-ink">{value}</div>
      {meter ? <div className="mt-1.5">{meter}</div> : null}
    </div>
  );
}

const KIND: Record<string, { icon: typeof Monitor; chip: keyof typeof kindChip }> = {
  bot: { icon: Monitor, chip: "bot" },
  drive: { icon: HardDrive, chip: "drive" },
  mcp: { icon: Plug, chip: "mcp" },
  egress: { icon: Shield, chip: "egress" },
  stray: { icon: Boxes, chip: "stray" },
};

const kindChip = {
  bot: msg("Machine"),
  drive: msg("Drive sidecar"),
  mcp: msg("MCP sidecar"),
  egress: msg("System"),
  stray: msg("Leftover"),
};

function uptime(iso: string, now: number) {
  if (!iso) return "";
  let s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  const d = Math.floor(s / 86400);
  s -= d * 86400;
  const h = Math.floor(s / 3600);
  const m = Math.floor((s - h * 3600) / 60);
  if (d > 0) return t("Up {{d}}d {{h}}h", { d, h });
  if (h > 0) return t("Up {{h}}h {{m}}m", { h, m });
  if (m > 0) return t("Up {{m}}m", { m });
  return t("Up just now");
}

function runningOf(containers: AdminContainer[]) {
  const any = containers.filter((c) => c.state !== "absent");
  return { up: any.filter((c) => c.state === "running").length, total: any.length };
}

function CountChip({ up, total, plural }: { up: number; total: number; plural?: boolean }) {
  if (total === 0) return null;
  return (
    <span className={`shrink-0 rounded-sm px-1.5 py-0.5 text-[11px] font-medium tabular-nums ${up > 0 ? "bg-well text-ink-2" : "bg-well text-ink-3"}`}>{plural ? t("{{count}} Bots", { count: total }) : `${up}/${total}`}</span>
  );
}

/** A fold: the header toggles it, the reader's choice is kept in localStorage. */
function Fold({
  id,
  fallback,
  force,
  header,
  children,
  rest,
}: {
  id: string;
  fallback: boolean;
  /** Overrides the reader's fold while set (the search shows its hits). */
  force?: boolean;
  header: ReactNode;
  children: ReactNode;
  rest?: ReactNode;
}) {
  const folds = useFolds();
  const open = force || (folds[id] ?? fallback);
  return (
    <details
      className="group overflow-hidden rounded-card bg-surface shadow-card"
      open={open}
      onToggle={(e) => {
        const want = (e.currentTarget as HTMLDetailsElement).open;
        if (want !== open) setFold(id, want);
      }}
    >
      <summary className="flex cursor-pointer list-none items-center gap-2.5 px-4 py-3 transition-colors duration-[160ms] ease-quiet hover:bg-well [&::-webkit-details-marker]:hidden">
        <ChevronRight size={14} className={`shrink-0 text-ink-3 transition-transform duration-[200ms] ease-quiet group-open:rotate-90 ${open ? "rotate-90" : ""}`} />
        <span className="flex min-w-0 flex-1 items-center gap-2.5">{header}</span>
        {rest}
      </summary>
      {children}
    </details>
  );
}

function NetworkRow({ n }: { n: AdminNetwork }) {
  return (
    <div className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 py-1.5 text-[12.5px]">
      <Network size={13} className="shrink-0 text-ink-3" />
      <code className="font-mono text-ink">{n.name}</code>
      {n.subnet ? <code className="font-mono text-ink-3">{n.subnet}</code> : null}
      <span className="flex min-w-0 flex-1 flex-wrap gap-1.5">
        {n.containers?.length ? (
          n.containers.map((c) => (
            <span key={c} className="truncate rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{c}</span>
          ))
        ) : (
          <span className="text-ink-3">{t("nothing attached")}</span>
        )}
      </span>
    </div>
  );
}

function ContainerRow({ c, busy, now, onAct }: { c: AdminContainer; busy: string | null; now: number; onAct: (name: string, action: Action) => void }) {
  const kind = KIND[c.kind] ?? KIND.stray;
  const Icon = kind.icon;
  const up = c.state === "running";
  const word = up ? t("Running") : c.state === "stopped" ? t("Stopped") : t("Not created");
  const mem = n64(c.memUsed);
  const cap = n64(c.memLimit);
  const busyAct = busy !== null;
  return (
    <li className="border-t border-line px-4 py-3 first:border-t-0">
      <div className="flex items-start gap-3.5">
        <div className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
          <Icon size={16} />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <span className="min-w-0 truncate text-[14px] font-medium text-ink">{c.label}</span>
            <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{t(kindChip[kind.chip])}</span>
          </div>
          {c.detail ? <div className="mt-0.5 truncate text-[12.5px] text-ink-2">{c.detail}</div> : null}
          <div className="mt-1 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px]">
            <span className="flex items-center gap-1.5">
              <Lamp status={up ? "online" : "stopped"} />
              <span className={`font-medium ${up ? "text-emerald" : "text-ink-3"}`}>{word}</span>
            </span>
            {up && c.startedAt ? <span className="text-ink-3">{uptime(c.startedAt, now)}</span> : null}
            {c.networks?.length ? (
              <span className="min-w-0 truncate text-ink-3" title={c.networks.join(", ")}>
                <Network size={12} className="mr-1 inline-block align-[-2px]" />
                {c.networks.join(", ")}
              </span>
            ) : null}
            <code className="min-w-0 truncate font-mono text-[12px] text-ink-3">{c.name}</code>
            {c.image ? <span className="hidden min-w-0 truncate font-mono text-[11.5px] text-ink-3 max-wide:hidden">{c.image}</span> : null}
          </div>
        </div>
        <span className="flex shrink-0 items-center gap-1.5">
          {up ? (
            <Btn kind="secondary" size="sm" disabled={busyAct} onClick={() => onAct(c.name, "stop")} icon={<Power size={12} />}>
              {t("Stop")}
            </Btn>
          ) : null}
          {!up ? (
            <Btn kind="secondary" size="sm" disabled={busyAct} onClick={() => onAct(c.name, "start")} icon={<Power size={12} />}>
              {t("Start")}
            </Btn>
          ) : null}
          {c.state !== "absent" ? (
            <ArmedButton kind="ghost" size="sm" disabled={busyAct} armedLabel={t("Click again to remove")} onConfirm={() => onAct(c.name, "remove")}>
              {t("Remove")}
            </ArmedButton>
          ) : null}
        </span>
      </div>
      {up ? (
        <div className="mt-3 grid grid-cols-2 gap-5 pl-[48px] max-wide:pl-0">
          <Stat label={t("CPU")} value={`${c.cpuPercent.toFixed(1)}%`} meter={<Meter value={c.cpuPercent} max={100} />} />
          <Stat label={t("RAM")} value={`${fmtBytes(mem)}${cap ? ` / ${fmtBytes(cap)}` : ""}`} meter={<Meter value={mem} max={cap} />} />
        </div>
      ) : null}
    </li>
  );
}

function BotCard({ g, busy, now, onAct, onRemoveAll }: { g: AdminBotGroup; busy: string | null; now: number; onAct: (name: string, action: Action) => void; onRemoveAll: (botId: string) => void }) {
  const containers = g.containers ?? [];
  const { up } = runningOf(containers);
  const anyUp = up > 0 || containers.some((c) => c.state === "stopped");
  return (
    <Fold
      id={`b:${g.botId}`}
      fallback
      header={
        <>
          <Link to="/bots/$botId/container" params={{ botId: g.botId }} className="min-w-0 truncate text-[15px] font-semibold text-ink transition-colors duration-[160ms] ease-quiet hover:text-cobalt" onClick={(e) => e.stopPropagation()}>
            {g.botName}
          </Link>
          <Status status={g.status} />
          <CountChip up={up} total={containers.filter((c) => c.state !== "absent").length} />
        </>
      }
      rest={
        <ArmedButton
          kind="ghost"
          size="sm"
          disabled={!anyUp || busy !== null}
          armedLabel={t("Click again to remove all")}
          onConfirm={() => onRemoveAll(g.botId)}
        >
          {t("Remove all containers")}
        </ArmedButton>
      }
    >
      <ul className="border-t border-line">
        {containers.map((c) => (
          <ContainerRow key={c.name} c={c} busy={busy} now={now} onAct={onAct} />
        ))}
      </ul>
      {g.networks?.length ? (
        <div className="border-t border-line bg-well px-4 py-2">
          {g.networks.map((n) => (
            <NetworkRow key={n.name} n={n} />
          ))}
        </div>
      ) : null}
    </Fold>
  );
}

function UserGroup({ u, busy, now, onAct, onRemoveAll, open }: { u: AdminUserGroup; busy: string | null; now: number; onAct: (name: string, action: Action) => void; onRemoveAll: (botId: string) => void; open: boolean }) {
  const all = u.bots.flatMap((b) => b.containers ?? []);
  const { up } = runningOf(all);
  return (
    <Fold
      id={`u:${u.userId}`}
      fallback={open}
      force={open}
      header={
        <>
          <span className="min-w-0 truncate text-[14.5px] font-semibold text-ink">{u.email || t("Unknown account")}</span>
          <CountChip up={up} total={u.bots.length} plural />
        </>
      }
    >
      <div className="space-y-2.5 border-t border-line p-3">{u.bots.map((g) => <BotCard key={g.botId} g={g} busy={busy} now={now} onAct={onAct} onRemoveAll={onRemoveAll} />)}</div>
    </Fold>
  );
}

function foldAll(keys: string[], open: boolean) {
  for (const k of keys) setFold(k, open);
}

export function AdminContainers() {
  const q = useQuery(UI.method.listAdminContainers, {}, { refetchInterval: 5000 });
  const data = q.data ?? null;
  // Uptimes are counted from the moment the engine last answered.
  const now = q.dataUpdatedAt || Date.now();
  const [busy, setBusy] = useState<string | null>(null);
  const [actErr, setErr] = useState("");
  const [needle, setNeedle] = useState("");
  const err = actErr || (q.error ? fail(q.error) : "");

  const users = data?.users ?? [];
  const system = data?.system;
  const systemContainers = system?.containers ?? [];
  const strays = systemContainers.filter((c) => c.kind === "stray");
  const totals = [...systemContainers, ...users.flatMap((u) => u.bots.flatMap((b) => b.containers ?? []))];

  // The search narrows the tree: a user matches by address, a Bot by its
  // name; matching groups open so the hit is visible at once.
  const s = needle.trim().toLowerCase();
  const hit = (u: AdminUserGroup) => u.email.toLowerCase().includes(s) || u.bots.some((b) => b.botName.toLowerCase().includes(s));
  const visible = s ? users.filter(hit) : users;

  const keys = [...users.map((u) => `u:${u.userId}`), ...users.flatMap((u) => u.bots.map((b) => `b:${b.botId}`)), "system"];

  async function act(name: string, action: Action) {
    setErr("");
    setBusy(name);
    try {
      await ui.adminContainerAction({ name, action });
      await recheck(UI.method.listAdminContainers, {});
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(null);
    }
  }

  async function removeAll(botId: string) {
    setErr("");
    setBusy(botId);
    try {
      await ui.removeAdminBotContainers({ id: botId });
      await recheck(UI.method.listAdminContainers, {});
    } catch (e) {
      setErr(fail(e));
    } finally {
      setBusy(null);
    }
  }

  async function sweepStrays() {
    setErr("");
    for (const c of strays) {
      setBusy(c.name);
      try {
        await ui.adminContainerAction({ name: c.name, action: "remove" });
      } catch (e) {
        setErr(fail(e));
        break;
      } finally {
        setBusy(null);
      }
    }
    await recheck(UI.method.listAdminContainers, {});
  }

  const running = totals.filter((c) => c.state === "running");
  const cpu = running.reduce((n, c) => n + c.cpuPercent, 0);
  const mem = running.reduce((n, c) => n + n64(c.memUsed), 0);
  const netCount = (system?.networks?.length ?? 0) + users.reduce((n, u) => n + u.bots.reduce((m, b) => m + (b.networks?.length ?? 0), 0), 0);
  const showSystem = !s && (systemContainers.length > 0 || (system?.networks?.length ?? 0) > 0);

  return (
    <div>
      <p className="mb-4 max-w-[640px] text-[13.5px] leading-[21px] text-ink-2">
        {t("Everything Silo runs on the container engine — every Bot's machine, drive and MCP sidecars, the egress gateway, and anything left over. Only containers Silo created are listed and acted on.")}
      </p>
      {err ? <ErrorWell className="mb-4">{err}</ErrorWell> : null}

      <div className="mb-4 grid grid-cols-5 gap-4 rounded-card bg-well px-5 py-4">
        <Stat label={t("Engine")} value={data ? data.runtime || "—" : "—"} />
        <Stat label={t("Running")} value={data ? `${running.length} of ${totals.length}` : "—"} />
        <Stat label={t("CPU")} value={data ? `${cpu.toFixed(1)}%` : "—"} />
        <Stat label={t("RAM")} value={data ? fmtBytes(mem) : "—"} />
        <Stat label={t("Networks")} value={data ? String(netCount) : "—"} />
      </div>

      {!data ? (
        <SkeletonRows rows={3} height={120} />
      ) : totals.length === 0 && netCount === 0 ? (
        <Panel title={t("Nothing on the engine")} note={t("No containers and no networks belong to Silo yet.")} className="mt-2">
          <p className="text-[13px] text-ink-2">{t("The first Bot creates its machine when it is started.")}</p>
        </Panel>
      ) : (
        <>
          {totals.length > 6 ? (
            <div className="mb-3 flex items-center gap-2">
              <div className="relative min-w-0 max-w-[320px] flex-1">
                <Search size={15} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-ink-3" />
                <input className={`${inputClass} pl-9`} placeholder={t("Find a user or Bot")} value={needle} onChange={(e) => setNeedle(e.target.value)} aria-label={t("Find a user or Bot")} />
                {needle ? (
                  <button type="button" className="absolute right-2 top-1/2 -translate-y-1/2 rounded-sm p-1 text-ink-3 hover:text-ink" aria-label={t("Clear")} onClick={() => setNeedle("")}>
                    <X size={13} />
                  </button>
                ) : null}
              </div>
              <Btn kind="ghost" size="sm" onClick={() => foldAll(keys, false)}>
                {t("Collapse all")}
              </Btn>
              <Btn kind="ghost" size="sm" onClick={() => foldAll(keys, true)}>
                {t("Expand all")}
              </Btn>
              {visible.length !== users.length ? <span className="text-[12.5px] text-ink-3">{t("{{count}} of {{total}} users", { count: visible.length, total: users.length })}</span> : null}
            </div>
          ) : null}

          {users.length > 0 && visible.length === 0 ? <p className="text-[13.5px] text-ink-2">{t("No user or Bot matches.")}</p> : null}

          <div className="space-y-2.5">
            {visible.map((u) => (
              <UserGroup key={u.userId} u={u} busy={busy} now={now} onAct={act} onRemoveAll={removeAll} open={!!s} />
            ))}
          </div>

          {showSystem ? (
            <div className="mt-5">
              <Fold
                id="system"
                fallback
                header={
                  <>
                    <span className="text-[14.5px] font-semibold text-ink">{t("System & leftovers")}</span>
                    <CountChip up={systemContainers.filter((c) => c.state === "running").length} total={systemContainers.length} />
                  </>
                }
              >
                <ul className="border-t border-line">
                  {systemContainers.map((c) => (
                    <ContainerRow key={c.name} c={c} busy={busy} now={now} onAct={act} />
                  ))}
                </ul>
                {system?.networks?.length ? (
                  <div className="border-t border-line bg-well px-4 py-2">
                    <p className="mb-0.5 text-[12.5px] text-ink-3">{t("A network with no Bot left is marked orphan; removing it is safe.")}</p>
                    {system.networks.map((n) => (
                      <NetworkRow key={n.name} n={n} />
                    ))}
                  </div>
                ) : null}
                {strays.length > 0 ? (
                  <div className="flex flex-wrap items-center gap-3 border-t border-line px-4 py-3">
                    <ArmedButton kind="ghost" size="sm" disabled={busy !== null} armedLabel={t("Click again to remove all")} onConfirm={sweepStrays}>
                      {t("Remove {{count}} leftovers", { count: strays.length })}
                    </ArmedButton>
                    <span className="text-[12.5px] text-ink-3">{t("Leftover sidecars are made again only if something asks for them.")}</span>
                  </div>
                ) : null}
              </Fold>
            </div>
          ) : null}
        </>
      )}
    </div>
  );
}
