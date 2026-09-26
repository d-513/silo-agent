import { ChevronLeft, Play, Plus, Square, Timer } from "lucide-react";
import { useEffect, useState, type FormEvent } from "react";
import { Link, useNavigate } from "react-router-dom";
import { ui } from "./api";
import { Btn } from "./Btn";
import { ArmedButton, SaveButton, useSave } from "./Feedback";
import { Field, inputClass, Panel, SkeletonRows } from "./Field";
import { Lamp } from "./Lamp";
import { Select } from "./Select";
import { PromptWell } from "./Settings";
import { Switch } from "./Switch";
import { Thread } from "./Thread";
import type { Artifact } from "./Artifact";
import { clock, DAY_SHORT, describe, HOUR_STEPS, MINUTE_STEPS, parse, toCron, type Mode, type Sched } from "./schedule";
import { useRunStream } from "./useRunStream";
import type { Automation, Bot } from "./gen/silo/v1/ui_pb";

function fail(e: unknown) {
  const m = e instanceof Error ? e.message : "failed";
  return m.replace(/^\[[^\]]+\]\s*/, "");
}

function whenShort(iso: string) {
  if (!iso) return "";
  const d = new Date(iso);
  return `${d.toLocaleDateString([], { weekday: "short", month: "short", day: "numeric" })} ${d.toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })}`;
}

function nextLine(a: Automation) {
  if (a.running) return "Running now";
  if (!a.enabled) return "Paused";
  if (!a.schedule) return "No schedule — never runs on its own";
  return a.nextRunAt ? `Next ${whenShort(a.nextRunAt)}` : "";
}

const modeOptions: { value: Mode; label: string }[] = [
  { value: "none", label: "No schedule" },
  { value: "minutes", label: "Every few minutes" },
  { value: "hours", label: "Every few hours" },
  { value: "daily", label: "Every day" },
  { value: "weekly", label: "On chosen days" },
  { value: "monthly", label: "Every month" },
  { value: "custom", label: "Custom (cron)" },
];

function ScheduleField({ value, onChange }: { value: string; onChange: (cron: string) => void }) {
  const [s, setS] = useState<Sched>(() => parse(value));
  useEffect(() => {
    if (toCron(s) !== value) setS(parse(value));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [value]);
  const set = (patch: Partial<Sched>) => {
    const next = { ...s, ...patch };
    if (patch.mode === "custom" && !next.cron) next.cron = toCron(s) || "0 9 * * *";
    setS(next);
    onChange(toCron(next));
  };
  const time = (
    <input
      type="time"
      className={`${inputClass} w-32`}
      value={clock(s.hour, s.minute)}
      onChange={(e) => {
        const [h, m] = e.target.value.split(":").map(Number);
        if (Number.isFinite(h) && Number.isFinite(m)) set({ hour: h, minute: m });
      }}
    />
  );
  return (
    <Field label="Schedule" hint={`${describe(toCron(s))} · the machine's clock`}>
      <div className="flex flex-wrap items-center gap-2">
        <Select className="w-52" value={s.mode} options={modeOptions} onChange={(v) => set({ mode: v as Mode, every: v === "minutes" ? 30 : v === "hours" ? 1 : s.every })} />
        {s.mode === "minutes" && (
          <Select className="w-40" value={String(s.every)} options={MINUTE_STEPS.map((n) => ({ value: String(n), label: `every ${n} min` }))} onChange={(v) => set({ every: Number(v) })} />
        )}
        {s.mode === "hours" && (
          <>
            <Select className="w-40" value={String(s.every)} options={HOUR_STEPS.map((n) => ({ value: String(n), label: n === 1 ? "every hour" : `every ${n} hours` }))} onChange={(v) => set({ every: Number(v) })} />
            <span className="text-[13px] text-ink-2">at minute</span>
            <input type="number" min={0} max={59} className={`${inputClass} w-20`} value={s.minute} onChange={(e) => set({ minute: Math.max(0, Math.min(59, Number(e.target.value) || 0)) })} />
          </>
        )}
        {(s.mode === "daily" || s.mode === "weekly" || s.mode === "monthly") && (
          <>
            {s.mode === "monthly" && (
              <>
                <span className="text-[13px] text-ink-2">on day</span>
                <input type="number" min={1} max={31} className={`${inputClass} w-20`} value={s.dom} onChange={(e) => set({ dom: Math.max(1, Math.min(31, Number(e.target.value) || 1)) })} />
              </>
            )}
            <span className="text-[13px] text-ink-2">at</span>
            {time}
          </>
        )}
        {s.mode === "custom" && (
          <input className={`${inputClass} w-56 font-mono`} placeholder="0 9 * * 1-5" value={s.cron} onChange={(e) => set({ cron: e.target.value })} />
        )}
      </div>
      {s.mode === "weekly" && (
        <div className="mt-2 flex flex-wrap gap-1">
          {[1, 2, 3, 4, 5, 6, 0].map((d) => {
            const on = s.days.includes(d);
            return (
              <button
                key={d}
                type="button"
                aria-pressed={on}
                onClick={() => set({ days: on ? s.days.filter((x) => x !== d) : [...s.days, d] })}
                className={`h-8 w-11 rounded-sm text-[12.5px] font-medium transition-colors duration-[160ms] ${
                  on ? "bg-cobalt-pale text-ink shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "text-ink-2 hover:bg-well"
                }`}
              >
                {DAY_SHORT[d]}
              </button>
            );
          })}
        </div>
      )}
    </Field>
  );
}

function Editor({ bot, auto, onSaved, onDeleted, onError }: { bot: Bot; auto?: Automation; onSaved: (a: Automation) => void; onDeleted?: () => void; onError: (s: string) => void }) {
  const [name, setName] = useState(auto?.name ?? "");
  const [prompt, setPrompt] = useState(auto?.prompt ?? "");
  const [schedule, setSchedule] = useState(auto?.schedule ?? "0 9 * * *");
  const [enabled, setEnabled] = useState(auto?.enabled ?? true);
  const saver = useSave();
  const heartbeat = auto?.kind === "heartbeat";
  async function save(e: FormEvent) {
    e.preventDefault();
    onError("");
    try {
      const next = await saver.run(() =>
        auto
          ? ui.updateAutomation({ botId: bot.id, id: auto.id, name, prompt, schedule, enabled })
          : ui.createAutomation({ botId: bot.id, name, prompt, schedule, enabled }),
      );
      onSaved(next);
    } catch (ex) {
      onError(fail(ex));
    }
  }
  return (
    <form onSubmit={save} className="grid gap-4">
      <Field label="Name">
        <input className={inputClass} value={name} disabled={heartbeat} placeholder="Morning digest" onChange={(e) => setName(e.target.value)} />
      </Field>
      <ScheduleField value={schedule} onChange={setSchedule} />
      <PromptWell label="Prompt" hint="Sent as the message of every run. Each run starts fresh, so say what to do and where to keep state." value={prompt} onChange={setPrompt} />
      <div className="flex items-center gap-3">
        <Switch on={enabled} onChange={setEnabled} title="Active" />
        <span className="text-[13px] text-ink-2">{enabled ? "Active — runs on its schedule" : "Paused — keeps its schedule, does not run"}</span>
      </div>
      <div className="flex flex-wrap items-center gap-3">
        <SaveButton type="submit" state={saver.state} disabled={!name.trim() || !prompt.trim()}>
          {auto ? "Save" : "Create automation"}
        </SaveButton>
        {auto && !heartbeat && onDeleted && (
          <ArmedButton
            kind="ghost"
            onConfirm={async () => {
              try {
                await ui.deleteAutomation({ botId: bot.id, id: auto.id });
                onDeleted();
              } catch (ex) {
                onError(fail(ex));
              }
            }}
          >
            Delete
          </ArmedButton>
        )}
      </div>
    </form>
  );
}

function Row({ bot, a, onToggle }: { bot: Bot; a: Automation; onToggle: (on: boolean) => void }) {
  // The Heartbeat leads: the only row on a `well` band, its mark lifted off it.
  const lead = a.kind === "heartbeat";
  return (
    <div className={`flex items-center gap-3 px-5 py-3.5 shadow-[inset_0_-1px_0_var(--color-line)] transition-colors duration-[160ms] last:shadow-none ${lead ? "bg-well" : "hover:bg-well"}`}>
      {/* The switch is a sibling, not a child, of the Link: a control nested in
          an anchor would navigate on click. */}
      <Link to={`/bots/${bot.id}/automations/${a.id}`} className="flex min-w-0 flex-1 items-center gap-3">
        <span className={`flex h-8 w-8 shrink-0 items-center justify-center rounded-sm text-ink-2 ${lead ? "bg-surface shadow-card" : "bg-well"}`}>
          <Timer size={15} />
        </span>
        <span className="min-w-0 flex-1">
          <span className="flex items-center gap-2">
            <span className="truncate text-[13.5px] font-medium text-ink">{a.name}</span>
            {a.running ? <Lamp status="working" /> : null}
            {a.lastStatus === "error" ? <span className="text-[12px] text-vermilion">Last run failed</span> : null}
          </span>
          <span className="block truncate text-[12.5px] text-ink-3">
            {describe(a.schedule)} · {nextLine(a)}
          </span>
        </span>
      </Link>
      <Switch on={a.enabled} onChange={onToggle} title={a.enabled ? "Pause" : "Resume"} />
    </div>
  );
}

export function AutomationsPane({
  bot,
  sub,
  onError,
  onApprovals,
  onInspectArtifact,
  onSaveSkill,
}: {
  bot: Bot;
  sub: string[];
  onError: (s: string) => void;
  onApprovals: () => void;
  onInspectArtifact: (a: Artifact) => void;
  onSaveSkill: (a: Artifact) => void;
}) {
  const nav = useNavigate();
  const [list, setList] = useState<Automation[] | null>(null);
  const [editing, setEditing] = useState(false);
  const selId = sub[0] && sub[0] !== "new" ? sub[0] : "";
  const sel = list?.find((a) => a.id === selId);
  const stream = useRunStream(bot.id, sel?.chatId, { onApproval: onApprovals });

  useEffect(() => {
    let dead = false;
    const load = () =>
      ui
        .listAutomations({ botId: bot.id })
        .then((r) => !dead && setList(r.automations))
        .catch((e) => !dead && onError(fail(e)));
    load();
    const t = setInterval(load, 5000);
    return () => {
      dead = true;
      clearInterval(t);
    };
  }, [bot.id, onError]);
  useEffect(() => setEditing(false), [selId]);

  const upsert = (a: Automation) => setList((xs) => (xs?.some((x) => x.id === a.id) ? xs.map((x) => (x.id === a.id ? a : x)) : [...(xs ?? []), a]));

  async function toggle(a: Automation, enabled: boolean) {
    try {
      upsert(await ui.updateAutomation({ botId: bot.id, id: a.id, name: a.name, prompt: a.prompt, schedule: a.schedule, enabled }));
    } catch (e) {
      onError(fail(e));
    }
  }

  if (sub[0] === "new") {
    return (
      <div className="min-h-0 flex-1 overflow-auto">
        <div className="silo-page">
          <Link to={`/bots/${bot.id}/automations`} className="mb-3 inline-flex items-center gap-1 text-[13px] text-ink-2 hover:text-ink">
            <ChevronLeft size={14} /> Automations
          </Link>
          <h2 className="mb-6 text-[22px] leading-7 font-medium tracking-[-0.015em]">New automation</h2>
          <Panel>
            <Editor
              bot={bot}
              onError={onError}
              onSaved={(a) => {
                upsert(a);
                nav(`/bots/${bot.id}/automations/${a.id}`, { replace: true });
              }}
            />
          </Panel>
        </div>
      </div>
    );
  }

  if (selId) {
    if (!sel) {
      return <div className="p-7">{list === null ? <SkeletonRows rows={3} /> : <p className="text-ink-2">This automation is gone.</p>}</div>;
    }
    const busy = stream.sending || sel.running;
    return (
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        <div className="shrink-0 px-4 pt-4 wide:px-7">
          <Link to={`/bots/${bot.id}/automations`} className="mb-2 inline-flex items-center gap-1 text-[13px] text-ink-2 hover:text-ink">
            <ChevronLeft size={14} /> Automations
          </Link>
          <div className="flex flex-wrap items-center gap-3">
            <h2 className="text-[20px] leading-7 font-medium tracking-[-0.015em]">{sel.name}</h2>
            {busy ? <Lamp status="working" /> : null}
            <span className="text-[12.5px] text-ink-3">
              {describe(sel.schedule)} · {nextLine(sel)}
            </span>
            <span className="flex-1" />
            <Btn kind="ghost" size="sm" onClick={() => setEditing((v) => !v)}>
              {editing ? "Close" : "Edit"}
            </Btn>
            {busy ? (
              <Btn kind="secondary" size="sm" icon={<Square size={11} />} onClick={() => ui.stopRun({ botId: bot.id, chatId: sel.chatId }).catch((e) => onError(fail(e)))}>
                Stop
              </Btn>
            ) : (
              <Btn
                kind="primary"
                size="sm"
                icon={<Play size={12} />}
                onClick={() =>
                  ui
                    .runAutomation({ botId: bot.id, id: sel.id })
                    .then(() => upsert({ ...sel, running: true } as Automation))
                    .catch((e) => onError(fail(e)))
                }
              >
                Run now
              </Btn>
            )}
          </div>
          {editing && (
            <Panel className="mt-4 mb-2">
              <Editor
                key={sel.id}
                bot={bot}
                auto={sel}
                onError={onError}
                onSaved={(a) => {
                  upsert(a);
                  setEditing(false);
                }}
                onDeleted={() => {
                  setList((xs) => (xs ?? []).filter((x) => x.id !== sel.id));
                  nav(`/bots/${bot.id}/automations`, { replace: true });
                }}
              />
            </Panel>
          )}
        </div>
        <Thread
          botId={bot.id}
          botName={bot.name}
          botCrest={bot.crest}
          chatId={sel.chatId}
          events={stream.events}
          sending={stream.sending}
          userAs="run"
          onInspectArtifact={onInspectArtifact}
          onSaveSkill={onSaveSkill}
          emptyState={<p className="my-auto py-14 text-center text-[13px] text-ink-3">No runs yet. Run it now, or wait for its schedule.</p>}
        />
      </div>
    );
  }

  const heartbeat = list?.filter((a) => a.kind === "heartbeat") ?? [];
  const custom = list?.filter((a) => a.kind !== "heartbeat") ?? [];
  return (
    <div className="min-h-0 flex-1 overflow-auto">
      <div className="silo-page">
        <div className="mb-6 flex items-start justify-between gap-3">
          <div>
            <h2 className="text-[22px] leading-7 font-medium tracking-[-0.015em]">Automations</h2>
            <p className="text-ink-2">Prompts {bot.name} runs on its own, on a schedule. Each run starts fresh and lands in its log.</p>
          </div>
          <Btn kind="primary" icon={<Plus size={13} />} onClick={() => nav(`/bots/${bot.id}/automations/new`)}>
            New
          </Btn>
        </div>
        {list === null ? (
          <SkeletonRows rows={3} />
        ) : (
          <>
            <Panel padded={false}>
              {heartbeat.map((a) => (
                <Row key={a.id} bot={bot} a={a} onToggle={(on) => void toggle(a, on)} />
              ))}
              <p className="px-5 py-3 text-[12.5px] leading-[18px] text-ink-3">Every Bot has one, and it cannot be deleted. It does not run until you give it a schedule.</p>
            </Panel>
            <Panel padded={false} title="Scheduled" className="mt-4">
              {custom.length === 0 ? (
                <p className="px-5 py-4 text-[13px] text-ink-3">None yet. Ask {bot.name} in a chat — “every weekday at 9, summarize my inbox” — or add one here.</p>
              ) : (
                custom.map((a) => <Row key={a.id} bot={bot} a={a} onToggle={(on) => void toggle(a, on)} />)
              )}
            </Panel>
          </>
        )}
      </div>
    </div>
  );
}
