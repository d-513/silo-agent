import { HardDrive, MessageCircle, Monitor, Plug, Power } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { ui } from "../api";
import { Btn } from "../Btn";
import { fail } from "../errors";
import { ArmedButton } from "../Feedback";
import { Panel, SkeletonRows } from "../Field";
import type { Bot, BotContainer } from "../gen/silo/v1/ui_pb";
import { fmtBytes } from "../format";
import { Lamp } from "../Lamp";

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

function uptime(iso: string, now: number) {
  if (!iso) return "";
  let s = Math.max(0, Math.floor((now - Date.parse(iso)) / 1000));
  const d = Math.floor(s / 86400);
  s -= d * 86400;
  const h = Math.floor(s / 3600);
  const m = Math.floor((s - h * 3600) / 60);
  if (d > 0) return `Up ${d}d ${h}h`;
  if (h > 0) return `Up ${h}h ${m}m`;
  if (m > 0) return `Up ${m}m`;
  return "Up just now";
}

const BOX_KIND: Record<string, { icon: typeof MessageCircle; chip: string }> = {
  bot: { icon: Monitor, chip: "Machine" },
  drive: { icon: HardDrive, chip: "Drive sidecar" },
  mcp: { icon: Plug, chip: "MCP sidecar" },
};

function BoxStat({ label, value, meter }: { label: string; value: string; meter?: ReactNode }) {
  return (
    <div className="min-w-0">
      <div className="text-[11px] font-medium uppercase leading-4 tracking-[0.08em] text-ink-3">{label}</div>
      <div className="mt-0.5 truncate font-mono text-[15px] font-medium tabular-nums tracking-tight text-ink">{value}</div>
      {meter ? <div className="mt-1.5">{meter}</div> : null}
    </div>
  );
}

// ContainersPane is everything a Bot runs on: its machine, the drive sidecar,
// and one sidecar per STDIO connector, with live usage from the engine.
export function ContainersPane({ bot, onStart, onStop, onChanged }: { bot: Bot; onStart: () => void; onStop: () => void; onChanged: (b: Bot) => void }) {
  const [boxes, setBoxes] = useState<BotContainer[] | null>(null);
  const [err, setErr] = useState("");
  const [now, setNow] = useState(() => Date.now());
  const [removed, setRemoved] = useState(false);
  useEffect(() => {
    let dead = false;
    const load = () => {
      ui.listBotContainers({ id: bot.id })
        .then((r) => {
          if (dead) return;
          setBoxes(r.containers);
          setNow(Date.now());
          setErr("");
        })
        .catch((e) => {
          if (!dead) setErr(fail(e));
        });
    };
    load();
    const t = setInterval(load, 2500);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [bot.id]);

  const running = (boxes ?? []).filter((b) => b.state === "running");
  const cpu = running.reduce((n, b) => n + b.cpuPercent, 0);
  const mem = running.reduce((n, b) => n + n64(b.memUsed), 0);
  const anyUp = (boxes ?? []).some((b) => b.state !== "absent");

  return (
    <div className="silo-page">
      <h2 className="text-title">Containers</h2>
      <p className="mb-5 mt-1 text-[13.5px] leading-[21px] text-ink-2">Everything this Bot runs on. Usage comes from the container engine and refreshes every few seconds.</p>
      {err ? <p className="mb-4 text-[13px] text-vermilion">{err}</p> : null}

      <div className="mb-5 grid grid-cols-3 gap-4 rounded-card bg-well px-5 py-4">
        <BoxStat label="Running" value={boxes ? `${running.length} of ${boxes.length}` : "—"} />
        <BoxStat label="CPU" value={boxes ? `${cpu.toFixed(1)}%` : "—"} />
        <BoxStat label="RAM" value={boxes ? fmtBytes(mem) : "—"} />
      </div>

      {boxes === null ? (
        <SkeletonRows rows={2} height={120} />
      ) : (
        <ul className="space-y-2.5">
          {boxes.map((b) => {
            const kind = BOX_KIND[b.kind] ?? BOX_KIND.mcp;
            const Icon = kind.icon;
            const up = b.state === "running";
            const lamp = up ? "online" : "stopped";
            const word = up ? "Running" : b.state === "stopped" ? "Stopped" : "Not created";
            const memUsed = n64(b.memUsed);
            const memCap = n64(b.memLimit);
            return (
              <li key={b.name} className="rounded-card bg-surface p-4 shadow-card">
                <div className="flex items-start gap-3.5">
                  <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-sm bg-well text-ink-2">
                    <Icon size={18} />
                  </div>
                  <div className="min-w-0 flex-1">
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-[15px] font-semibold tracking-[-0.01em] text-ink">{b.label}</span>
                      <span className="rounded-sm bg-well px-1.5 py-0.5 text-[11px] font-medium text-ink-3">{kind.chip}</span>
                    </div>
                    <div className="mt-0.5 truncate text-[12.5px] text-ink-2">{b.detail}</div>
                    <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-[12.5px]">
                      <span className="flex items-center gap-1.5">
                        <Lamp status={lamp} />
                        <span className={`font-medium ${up ? "text-emerald" : "text-ink-3"}`}>{word}</span>
                      </span>
                      {up && b.startedAt ? <span className="text-ink-3">{uptime(b.startedAt, now)}</span> : null}
                      <code className="min-w-0 truncate font-mono text-[12px] text-ink-3">{b.name}</code>
                    </div>
                  </div>
                  {b.kind === "bot" ? (
                    bot.workerConnected ? (
                      <Btn kind="secondary" size="sm" onClick={onStop} icon={<Power size={12} />}>
                        Stop
                      </Btn>
                    ) : (
                      <Btn kind="primary" size="sm" onClick={onStart} disabled={bot.status === "starting"} icon={<Power size={12} />}>
                        {bot.status === "starting" ? "Starting…" : "Start"}
                      </Btn>
                    )
                  ) : null}
                </div>
                {up ? (
                  <div className="mt-4 grid grid-cols-2 gap-5 pl-[54px] max-wide:pl-0">
                    <BoxStat label="CPU" value={`${b.cpuPercent.toFixed(1)}%`} meter={<Meter value={b.cpuPercent} max={100} />} />
                    <BoxStat label="RAM" value={`${fmtBytes(memUsed)}${memCap ? ` / ${fmtBytes(memCap)}` : ""}`} meter={<Meter value={memUsed} max={memCap} />} />
                  </div>
                ) : null}
                {b.image ? <div className="mt-3 truncate pl-[54px] font-mono text-[11.5px] text-ink-3 max-wide:pl-0">{b.image}</div> : null}
              </li>
            );
          })}
        </ul>
      )}

      <Panel title="Remove all containers" tone="danger" className="mt-6" note="Stops and removes every container above. The workspace, drives, connectors, and caches stay; each container is made again the next time it is needed.">
        <div className="flex flex-wrap items-center gap-3">
          <ArmedButton
            kind="secondary"
            armedLabel="Click again to remove all"
            disabled={!anyUp}
            onConfirm={async () => {
              try {
                onChanged(await ui.removeBotContainers({ id: bot.id }));
                setRemoved(true);
              } catch (e) {
                setErr(fail(e));
              }
            }}
          >
            Remove all containers
          </ArmedButton>
          {removed && !anyUp ? <span className="text-[12.5px] text-ink-3">Removed. Start the Bot to bring them back.</span> : null}
        </div>
      </Panel>
    </div>
  );
}
