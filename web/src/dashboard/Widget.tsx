import { IdeaList, ideasOf } from "./Ideas";
import { AlertCircle, Check, MessageSquare, RotateCw, X } from "lucide-react";
import { type HTMLAttributes, type ReactNode, useEffect, useRef, useState } from "react";
import { AppFrame } from "../Artifact";
import { Btn } from "../Btn";
import { ArmedButton, Spinner } from "../Feedback";
import type { DashboardWidget } from "../gen/silo/v1/ui_pb";
import { clockTime, fullTime } from "../format";
import { locale, t } from "../i18n.ts";
import { Md } from "../Md";
import { Tip } from "../Tip";
import {
  type Action,
  type Cols,
  actions as actionsOf,
  age,
  bars,
  checked,
  interval,
  items,
  nowSplit,
  num,
  numeric,
  obj,
  receipt as receiptOf,
  safeUrl,
  span,
  sparkPoints,
  text,
  toggle,
  tone,
} from "./widgets";

export type WidgetHandlers = {
  onSelect: () => void;
  onRefresh: () => Promise<void>;
  onState: (stateJson: string) => void;
  // apart: a hand action opens a chat of its own instead of the main one
  // (Ctrl/⌘-click, like a link in a new tab).
  onAction: (a: Action, args: Record<string, unknown>, apart?: boolean) => Promise<void>;
};

// Widget is one view on the Dashboard, drawn by Silo whatever the Bot wrote:
// a surface card with its title, where the data came from and how old it is,
// the kind's body, then its buttons and the last action's receipt. An app is
// the exception: a live artifact app in the hatch, the dark window that says
// "this is the Bot's machine".
export function Widget({
  botId,
  w,
  cols,
  fresh,
  selected,
  arranging,
  arrange,
  drag,
  handlers,
}: {
  botId: string;
  w: DashboardWidget;
  cols: Cols;
  fresh: boolean;
  selected: boolean;
  arranging: boolean;
  arrange?: ReactNode;
  drag?: HTMLAttributes<HTMLElement>;
  handlers: WidgetHandlers;
}) {
  const style = {
    gridColumn: `span ${span(w.size, cols)}`,
    gridRow: w.rows === 2 && cols > 1 ? "span 2" : undefined,
  };
  const ring = selected ? "shadow-[0_0_0_1px_var(--color-cobalt),0_0_0_4px_var(--color-cobalt-pale)]" : "shadow-card";
  if (w.kind === "app") {
    return (
      <article
        {...drag}
        style={style}
        className={`rise group/w flex min-w-0 flex-col rounded-card bg-hatch px-2 pb-2 ${selected ? ring : ""} ${drag ? "cursor-grab" : ""}`}
      >
        <div className="flex h-[34px] items-center gap-2 px-1.5 font-mono text-[11.5px] text-on-hatch/75">
          <span className="min-w-0 flex-1 truncate">{w.title}</span>
          {arranging ? arrange : <HeadTools w={w} selected={selected} handlers={handlers} onHatch />}
        </div>
        <LazyApp botId={botId} w={w} />
      </article>
    );
  }
  const acts = actionsOf(w.specJson);
  const data = obj(w.dataJson);
  return (
    <article
      {...drag}
      style={style}
      className={`rise group/w @container relative flex min-w-0 flex-col rounded-card bg-surface px-3.5 pt-2.5 pb-3 transition-shadow duration-[160ms] ${ring} ${drag ? "cursor-grab" : ""}`}
    >
      <header className="mb-2 flex h-6 min-w-0 items-center gap-2">
        {fresh ? <span aria-label={t("Changed")} className="h-1.5 w-1.5 shrink-0 rounded-full bg-cobalt" /> : null}
        <h3 className="min-w-0 flex-1 truncate text-[13px] leading-[18px] font-medium text-ink">{w.title}</h3>
        {arranging ? (
          arrange
        ) : (
          <HeadTools
            w={w}
            selected={selected}
            handlers={handlers}
            compact={span(w.size, cols) === 1 && cols > 1}
            extra={w.kind === "checklist" ? <ChecklistCount w={w} /> : null}
          />
        )}
      </header>
      <div className="min-w-0 flex-1">
        <Body w={w} data={data} acts={acts.filter((a) => a.row)} handlers={handlers} />
      </div>
      <Foot w={w} acts={acts.filter((a) => !a.row)} handlers={handlers} />
    </article>
  );
}

// HeadTools sit at the end of the head: where the data came from and how old
// it is at rest; Refresh and Ask about this fade in over it on hover.
function HeadTools({
  w,
  selected,
  handlers,
  extra,
  onHatch,
  compact,
}: {
  w: DashboardWidget;
  selected: boolean;
  handlers: WidgetHandlers;
  extra?: ReactNode;
  onHatch?: boolean;
  compact?: boolean;
}) {
  const [refreshing, setRefreshing] = useState(false);
  const busy = w.busy || refreshing;
  const stamp = age(w.dataAt);
  const meta = [compact ? "" : w.source, stamp === "now" ? t("now") : stamp].filter(Boolean).join(" · ");
  const tone = onHatch ? "text-on-hatch/75 hover:text-on-hatch hover:bg-on-hatch/10" : "text-ink-3 hover:bg-well hover:text-ink";
  const reveal = selected ? "opacity-100" : "opacity-0 group-hover/w:opacity-100 group-focus-within/w:opacity-100 [@media(hover:none)]:opacity-100";
  return (
    <div className="flex shrink-0 items-center gap-1">
      {busy ? (
        <span className={onHatch ? "text-[11.5px]" : "shimmer-text text-[11.5px]"}>{t("Refreshing")}</span>
      ) : extra ? (
        extra
      ) : meta ? (
        <span
          title={[w.dataAt ? fullTime(new Date(w.dataAt)) : "", w.every ? t("Refreshes every {{every}}", { every: interval(w.every) }) : ""]
            .filter(Boolean)
            .join(" · ")}
          className={`font-mono text-[11.5px] ${onHatch ? "" : "text-ink-3"}`}
        >
          {meta}
        </span>
      ) : null}
      <span
        className={`flex items-center gap-0.5 transition-opacity duration-[160ms] ${reveal} ${compact && !selected ? "absolute right-3 rounded-sm bg-surface pl-1 [@media(hover:none)]:static" : ""}`}
      >
        {w.hasProbe ? (
          <button
            type="button"
            disabled={busy}
            aria-label={t("Refresh")}
            title={t("Refresh")}
            onClick={() => {
              setRefreshing(true);
              void handlers.onRefresh().finally(() => setRefreshing(false));
            }}
            className={`grid h-6 w-6 place-items-center rounded-sm transition-colors duration-[160ms] disabled:opacity-50 ${tone}`}
          >
            <RotateCw size={12.5} className={busy ? "animate-spin motion-reduce:animate-none" : ""} />
          </button>
        ) : null}
        <button
          type="button"
          aria-pressed={selected}
          aria-label={t("Ask about this")}
          title={t("Ask about this")}
          onClick={handlers.onSelect}
          className={`grid h-6 w-6 place-items-center rounded-sm transition-colors duration-[160ms] ${selected ? "text-cobalt" : tone}`}
        >
          <MessageSquare size={12.5} />
        </button>
      </span>
    </div>
  );
}

function ChecklistCount({ w }: { w: DashboardWidget }) {
  const all = items(obj(w.dataJson).items);
  if (!all.length) return null;
  const on = checked(w.stateJson).filter((id) => all.some((x) => text(x.id) === id)).length;
  return <span className="font-mono text-[11.5px] text-ink-3">{`${on}/${all.length}`}</span>;
}

// WidgetSnapshot is a widget as it stood when a chat quoted it: the same
// body drawn read-only, with no actions and nothing to press.
export function WidgetSnapshot({ w }: { w: DashboardWidget }) {
  return (
    <div className="@container min-w-0 rounded-card bg-surface px-3.5 pt-2.5 pb-3 shadow-card">
      <header className="mb-2 flex h-6 min-w-0 items-center gap-2">
        <h3 className="min-w-0 flex-1 truncate text-[13px] leading-[18px] font-medium text-ink">{w.title}</h3>
        {w.source ? <span className="shrink-0 truncate font-mono text-[11.5px] text-ink-3">{w.source}</span> : null}
      </header>
      {w.kind === "app" ? (
        <p className="text-[12.5px] text-ink-3">{t("A live app on the Dashboard.")}</p>
      ) : (
        <Body w={w} data={obj(w.dataJson)} acts={[]} />
      )}
    </div>
  );
}

// inert stands in for a snapshot's handlers: with no actions, nothing calls it.
const inert: WidgetHandlers = { onSelect: () => {}, onRefresh: async () => {}, onState: () => {}, onAction: async () => {} };

// Body draws a widget's kind. With no handlers it is read-only.
function Body({ w, data, acts, handlers }: { w: DashboardWidget; data: Record<string, unknown>; acts: Action[]; handlers?: WidgetHandlers }) {
  if (!w.dataAt && Object.keys(data).length === 0) {
    return <p className="text-[12.5px] text-ink-3">{w.hasProbe ? t("Waiting for the first refresh.") : t("Nothing here yet.")}</p>;
  }
  switch (w.kind) {
    case "note":
      return text(data.text) ? (
        <div className="silo-reply text-[15.5px] leading-[26px]">
          <Md text={text(data.text)} />
        </div>
      ) : null;
    case "stat":
      return <Stat data={data} />;
    case "chart":
      return <Chart data={data} type={text(obj(w.specJson).type) === "bar" ? "bar" : "line"} />;
    case "list":
      return <List data={data} acts={handlers ? acts : []} handlers={handlers ?? inert} />;
    case "table":
      return <Table data={data} acts={handlers ? acts : []} handlers={handlers ?? inert} />;
    case "timeline":
      return <Timeline data={data} />;
    case "status":
      return <StatusList data={data} />;
    case "checklist":
      return <Checklist w={w} data={data} onState={handlers?.onState} />;
    case "progress":
      return <Progress data={data} />;
    case "ideas":
      return <IdeaList ideas={ideasOf(data)} />;
  }
  return null;
}

function fmtNum(v: unknown): string {
  const n = num(v);
  if (n === null || typeof v === "string") return text(v);
  return n.toLocaleString(locale(), { maximumFractionDigits: 2 });
}

function Stat({ data }: { data: Record<string, unknown> }) {
  const series = Array.isArray(data.series) ? data.series.map(num).filter((x): x is number => x !== null) : [];
  return (
    <div className="flex h-full flex-col">
      <div className="flex min-w-0 flex-wrap items-baseline gap-x-1.5">
        <span className="min-w-0 text-[clamp(18px,14cqi,34px)] leading-[1.12] font-medium tracking-[-0.025em] [overflow-wrap:anywhere] text-ink tabular-nums">
          {fmtNum(data.value)}
        </span>
        {text(data.unit) ? <span className="font-mono text-[13px] text-ink-3">{text(data.unit)}</span> : null}
      </div>
      {text(data.caption) ? <p className="mt-1 text-[12.5px] leading-[18px] text-ink-2">{text(data.caption)}</p> : null}
      {series.length > 1 ? <Line series={series} height={36} className="mt-auto pt-3" /> : null}
    </div>
  );
}

// Line is a hairline in ink with its last point marked: no fill, no grid.
function Line({ series, height, className = "" }: { series: number[]; height: number; className?: string }) {
  const W = 200;
  const pts = sparkPoints(series, W, height);
  if (!pts.length) return null;
  const [lx, ly] = pts[pts.length - 1];
  return (
    <div className={className}>
      <div className="relative">
        <svg viewBox={`0 0 ${W} ${height}`} preserveAspectRatio="none" className="block w-full" style={{ height }} aria-hidden>
          <line x1="0" y1={height - 0.5} x2={W} y2={height - 0.5} stroke="var(--color-line)" vectorEffect="non-scaling-stroke" />
          <polyline
            points={pts.map((p) => p.join(",")).join(" ")}
            fill="none"
            stroke="var(--color-ink)"
            strokeWidth="1.3"
            strokeLinejoin="round"
            vectorEffect="non-scaling-stroke"
          />
        </svg>
        <span
          aria-hidden
          className="absolute h-[5px] w-[5px] -translate-x-1/2 -translate-y-1/2 rounded-full bg-ink"
          style={{ left: `${(lx / W) * 100}%`, top: `${(ly / height) * 100}%` }}
        />
      </div>
    </div>
  );
}

function Chart({ data, type }: { data: Record<string, unknown>; type: "line" | "bar" }) {
  const pts = items(data.points).map((p) => ({
    label: text(p.label),
    value: num(p.value) ?? 0,
  }));
  if (!pts.length) return null;
  const unit = text(data.unit);
  const last = pts[pts.length - 1];
  return (
    <div className="flex h-full flex-col">
      <div className="flex items-baseline gap-1.5">
        <span className="text-[20px] leading-6 font-medium tracking-[-0.015em] tabular-nums">{fmtNum(last.value)}</span>
        {unit ? <span className="font-mono text-[12px] text-ink-3">{unit}</span> : null}
        {last.label ? <span className="ml-auto font-mono text-[11.5px] text-ink-3">{last.label}</span> : null}
      </div>
      {text(data.caption) ? <p className="mt-0.5 text-[12.5px] leading-[18px] text-ink-2">{text(data.caption)}</p> : null}
      {type === "line" ? (
        <Line series={pts.map((p) => p.value)} height={88} className="mt-auto pt-3" />
      ) : (
        <div className="mt-auto flex h-[96px] items-end gap-1 pt-3">
          {bars(pts.map((p) => p.value)).map((h, i) => (
            <div
              key={i}
              title={`${pts[i].label}: ${fmtNum(pts[i].value)}${unit ? " " + unit : ""}`}
              className="flex h-full min-w-0 flex-1 flex-col justify-end"
            >
              <div className={`rounded-t-[3px] ${i === pts.length - 1 ? "bg-ink" : "bg-ink-3/45"}`} style={{ height: `${Math.max(2, h * 100)}%` }} />
            </div>
          ))}
        </div>
      )}
      {pts.length > 1 ? (
        <div className="mt-1.5 flex justify-between font-mono text-[11px] text-ink-3">
          <span>{pts[0].label}</span>
          <span>{last.label}</span>
        </div>
      ) : null}
    </div>
  );
}

// RowActions are the buttons a list or table row carries: hidden at rest on a
// pointer device and faded in over the row, like rename on a chat row.
function RowActions({ acts, row, handlers }: { acts: Action[]; row: Record<string, unknown>; handlers: WidgetHandlers }) {
  const [busy, setBusy] = useState("");
  if (!acts.length) return null;
  return (
    <span className="flex shrink-0 items-center gap-0.5 opacity-0 transition-opacity duration-[160ms] group-hover/row:opacity-100 group-focus-within/row:opacity-100 [@media(hover:none)]:opacity-100">
      {acts.map((a) => {
        const run = (apart = false) => {
          setBusy(a.id);
          void handlers.onAction(a, row, apart).finally(() => setBusy(""));
        };
        const cls =
          "h-6 rounded-sm px-1.5 text-[12px] font-medium text-ink-2 transition-colors duration-[160ms] hover:bg-pressed hover:text-ink disabled:opacity-50";
        return a.confirm ? (
          <ArmedButton
            key={a.id}
            kind="ghost"
            size="sm"
            className="!h-6 !px-1.5 !text-[12px]"
            armedLabel={t("Click again")}
            onConfirm={() => run()}
            disabled={busy !== ""}
          >
            {a.label}
          </ArmedButton>
        ) : (
          <button
            key={a.id}
            type="button"
            disabled={busy !== ""}
            title={a.kind === "hand" ? t("Opens in the main chat. Ctrl/⌘-click for a new chat.") : undefined}
            onClick={(e) => run(e.metaKey || e.ctrlKey)}
            className={cls}
          >
            {busy === a.id ? <Spinner size={11} /> : a.label}
          </button>
        );
      })}
    </span>
  );
}

function List({ data, acts, handlers }: { data: Record<string, unknown>; acts: Action[]; handlers: WidgetHandlers }) {
  const rows = items(data.items);
  if (!rows.length) return <p className="text-[12.5px] text-ink-3">{t("Nothing here.")}</p>;
  return (
    <ul className="-mx-2">
      {rows.map((r, i) => {
        const url = safeUrl(r.url);
        const title = text(r.title);
        return (
          <li
            key={i}
            className="group/row flex min-w-0 items-center gap-2 rounded-sm px-2 py-1.5 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none hover:bg-well"
          >
            <div className="min-w-0 flex-1">
              {url ? (
                <a
                  href={url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="block truncate text-[13px] leading-[18px] text-ink hover:text-cobalt hover:underline"
                >
                  {title}
                </a>
              ) : (
                <div className="truncate text-[13px] leading-[18px] text-ink">{title}</div>
              )}
              {text(r.detail) ? <div className="truncate text-[12px] leading-4 text-ink-3">{text(r.detail)}</div> : null}
            </div>
            <RowActions acts={acts} row={r} handlers={handlers} />
            {text(r.meta) ? <span className="shrink-0 font-mono text-[11.5px] text-ink-3">{text(r.meta)}</span> : null}
          </li>
        );
      })}
    </ul>
  );
}

function Table({ data, acts, handlers }: { data: Record<string, unknown>; acts: Action[]; handlers: WidgetHandlers }) {
  const columns = Array.isArray(data.columns) ? data.columns.map(text) : [];
  const rows = Array.isArray(data.rows) ? data.rows.filter(Array.isArray) : [];
  if (!rows.length) return <p className="text-[12.5px] text-ink-3">{t("Nothing here.")}</p>;
  const asObject = (r: unknown[]) => Object.fromEntries(r.map((v, i) => [columns[i] || String(i), v]));
  return (
    <div className="-mx-1 max-h-[420px] overflow-auto">
      <table className="w-full border-collapse text-[12.5px]">
        {columns.length ? (
          <thead>
            <tr>
              {columns.map((c, i) => (
                <th
                  key={i}
                  className={`sticky top-0 bg-surface px-1 pb-1.5 text-label-caps leading-4 font-medium text-ink-3 uppercase ${numeric(rows[0]?.[i]) ? "text-right" : "text-left"}`}
                >
                  {c}
                </th>
              ))}
              {acts.length ? <th className="bg-surface" /> : null}
            </tr>
          </thead>
        ) : null}
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="group/row shadow-[inset_0_1px_0_var(--color-line)] hover:bg-well">
              {r.map((v, j) => (
                <td
                  key={j}
                  className={`h-8 max-w-[240px] truncate px-1 ${numeric(v) ? "text-right font-mono text-[12px] text-ink-2 tabular-nums" : "text-ink"}`}
                >
                  {text(v)}
                </td>
              ))}
              {acts.length ? (
                <td className="px-1 text-right">
                  <RowActions acts={acts} row={asObject(r)} handlers={handlers} />
                </td>
              ) : null}
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function when(s: string): string {
  const ms = Date.parse(s);
  return Number.isFinite(ms) ? clockTime(new Date(ms)) : s;
}

// Timeline is the day's events on a rail of times. The "now" rule is cobalt:
// it is the one place on the board that says "you are here".
function Timeline({ data }: { data: Record<string, unknown> }) {
  const rows = items(data.items);
  if (!rows.length) return <p className="text-[12.5px] text-ink-3">{t("Nothing here.")}</p>;
  const split = nowSplit(rows.map((r) => text(r.time)));
  const now = (
    <li key="now" aria-label={t("Now")} className="relative my-1 h-0 border-t border-cobalt">
      <span className="absolute top-[-4px] left-[-4px] h-[7px] w-[7px] rounded-full bg-cobalt" />
      <span className="absolute top-[-9px] left-[-68px] w-16 font-mono text-[12px] font-medium whitespace-nowrap text-cobalt">{clockTime(new Date())}</span>
    </li>
  );
  const out: ReactNode[] = [];
  rows.forEach((r, i) => {
    if (i === split) out.push(now);
    const past = split >= 0 && i < split;
    out.push(
      <li key={i} className="relative py-1.5">
        <span className="absolute top-1.5 left-[-68px] w-16 font-mono text-[12px] whitespace-nowrap text-ink-3">{when(text(r.time))}</span>
        <div className={`text-[13px] leading-[18px] font-medium ${past ? "text-ink-3" : "text-ink"}`}>{text(r.title)}</div>
        {text(r.detail) ? <div className="text-[12px] leading-4 text-ink-3">{text(r.detail)}</div> : null}
      </li>,
    );
  });
  if (split === rows.length) out.push(now);
  return <ol className="pl-[68px]">{out}</ol>;
}

const statusWords = {
  ok: () => t("Up"),
  warn: () => t("Degraded"),
  down: () => t("Down"),
  off: () => t("Off"),
};

// StatusList is lamp + name + word: green is alive, vermilion needs you, and
// nothing else gets a colour.
function StatusList({ data }: { data: Record<string, unknown> }) {
  const rows = items(data.items);
  if (!rows.length) return <p className="text-[12.5px] text-ink-3">{t("Nothing here.")}</p>;
  return (
    <ul>
      {rows.map((r, i) => {
        const tn = tone(r.state);
        const dot = tn === "ok" ? "bg-lamp" : tn === "down" ? "bg-vermilion" : tn === "warn" ? "bg-ink-2" : "shadow-[inset_0_0_0_1.5px_var(--color-ink-3)]";
        const word = tn === "ok" ? "text-emerald" : tn === "down" ? "text-vermilion" : tn === "warn" ? "text-ink-2" : "text-ink-3";
        return (
          <li key={i} className="flex h-[30px] min-w-0 items-center gap-2.5 shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
            <span className={`h-[7px] w-[7px] shrink-0 rounded-full ${dot}`} />
            <span className="min-w-0 flex-1 truncate font-mono text-[12.5px] text-ink">{text(r.label)}</span>
            <span className={`shrink-0 text-[12px] font-medium ${word}`}>{text(r.word) || statusWords[tn]()}</span>
            {text(r.meta) ? <span className="min-w-14 shrink-0 text-right font-mono text-[11.5px] whitespace-nowrap text-ink-3">{text(r.meta)}</span> : null}
          </li>
        );
      })}
    </ul>
  );
}

function Checklist({ w, data, onState }: { w: DashboardWidget; data: Record<string, unknown>; onState?: (s: string) => void }) {
  const rows = items(data.items);
  const on = new Set(checked(w.stateJson));
  if (!rows.length) return <p className="text-[12.5px] text-ink-3">{t("Nothing here.")}</p>;
  return (
    <ul>
      {rows.map((r, i) => {
        const id = text(r.id) || String(i);
        const done = on.has(id);
        return (
          <li key={id} className="shadow-[inset_0_-1px_0_var(--color-line)] last:shadow-none">
            <button
              type="button"
              role="checkbox"
              aria-checked={done}
              disabled={!onState}
              onClick={() => onState?.(toggle(w.stateJson, id))}
              className="flex min-h-[30px] w-full items-center gap-2.5 py-1 text-left"
            >
              <span
                className={`grid h-[15px] w-[15px] shrink-0 place-items-center rounded-[4px] transition-colors duration-[200ms] ease-quiet ${
                  done ? "bg-emerald text-on-accent" : "shadow-[inset_0_0_0_1px_var(--color-line-strong)]"
                }`}
              >
                {done ? <Check size={11} strokeWidth={3} /> : null}
              </span>
              <span className={`min-w-0 flex-1 text-[13px] leading-[18px] transition-colors duration-[200ms] ${done ? "text-ink-3 line-through" : "text-ink"}`}>
                {text(r.text)}
              </span>
            </button>
          </li>
        );
      })}
    </ul>
  );
}

function Progress({ data }: { data: Record<string, unknown> }) {
  const v = num(data.value) ?? 0;
  const max = num(data.max) ?? 100;
  const pct = max > 0 ? Math.max(0, Math.min(1, v / max)) : 0;
  return (
    <div className="flex h-full flex-col">
      <div className="flex items-baseline gap-1.5">
        <span className="text-[34px] leading-[38px] font-medium tracking-[-0.025em] tabular-nums">{Math.round(pct * 100)}</span>
        <span className="font-mono text-[13px] text-ink-3">%</span>
      </div>
      <p className="mt-1 text-[12.5px] leading-[18px] text-ink-2">{text(data.caption) || `${fmtNum(v)} / ${fmtNum(max)}`}</p>
      <div className="mt-auto pt-3">
        <div className="h-[3px] overflow-hidden rounded-full bg-pressed">
          <div className="h-full rounded-full bg-ink transition-[width] duration-[420ms] ease-settle" style={{ width: `${pct * 100}%` }} />
        </div>
      </div>
    </div>
  );
}

// Foot holds the widget's own buttons, why the last refresh failed, and the
// receipt of the last thing done with it.
function Foot({ w, acts, handlers }: { w: DashboardWidget; acts: Action[]; handlers: WidgetHandlers }) {
  const [busy, setBusy] = useState("");
  const rc = receiptOf(w.receiptJson);
  if (!acts.length && !rc && !w.error) return null;
  const run = (a: Action, apart = false) => {
    setBusy(a.id);
    void handlers.onAction(a, {}, apart).finally(() => setBusy(""));
  };
  return (
    <div className="mt-3 space-y-2">
      {acts.length ? (
        <div className="flex flex-wrap items-center gap-1.5">
          {acts.map((a, i) => {
            const kind = a.kind === "hand" ? "ghost" : i === 0 ? "secondary" : "ghost";
            const icon = busy === a.id ? <Spinner size={12} /> : a.kind === "hand" ? <MessageSquare size={13} /> : undefined;
            return a.confirm ? (
              <ArmedButton key={a.id} kind={kind} size="sm" armedLabel={t("Click again to run")} onConfirm={() => run(a)} disabled={busy !== ""} icon={icon}>
                {a.label}
              </ArmedButton>
            ) : (
              <Btn
                key={a.id}
                kind={kind}
                size="sm"
                disabled={busy !== ""}
                title={a.kind === "hand" ? t("Opens in the main chat. Ctrl/⌘-click for a new chat.") : undefined}
                onClick={(e) => run(a, e.metaKey || e.ctrlKey)}
                icon={icon}
              >
                {a.label}
              </Btn>
            );
          })}
        </div>
      ) : null}
      {w.error ? (
        <Tip content={<span className="block max-w-[360px] font-mono text-[11.5px] leading-[17px] whitespace-pre-wrap">{w.error}</span>}>
          <p className="flex items-center gap-1.5 text-[12px] text-ink-3">
            <AlertCircle size={12.5} className="shrink-0" />
            <span className="truncate">{t("Couldn't refresh")}</span>
          </p>
        </Tip>
      ) : null}
      {rc ? (
        <p className="flex min-w-0 items-center gap-2 text-[12px] text-ink-3">
          {rc.ok ? <Check size={13} strokeWidth={2.4} className="shrink-0 text-emerald" /> : <X size={13} strokeWidth={2.4} className="shrink-0" />}
          <span className={`min-w-0 truncate ${rc.ok ? "font-medium text-ink" : ""}`}>{rc.text}</span>
          <span aria-hidden className="h-px min-w-4 flex-1 bg-line" />
          <span className="shrink-0">
            {t("by you · {{time}}", {
              time: rc.at ? clockTime(new Date(rc.at)) : "",
            })}
          </span>
        </p>
      ) : null}
    </div>
  );
}

// LazyApp asks for the app's address only once the widget scrolls into view,
// so a board with several apps stays light.
function LazyApp({ botId, w }: { botId: string; w: DashboardWidget }) {
  const ref = useRef<HTMLDivElement>(null);
  const [seen, setSeen] = useState(false);
  useEffect(() => {
    const el = ref.current;
    if (!el || seen) return;
    if (typeof IntersectionObserver === "undefined") {
      setSeen(true);
      return;
    }
    const io = new IntersectionObserver((es) => es.some((e) => e.isIntersecting) && setSeen(true), { rootMargin: "200px" });
    io.observe(el);
    return () => io.disconnect();
  }, [seen]);
  const h = w.rows === 2 ? "h-[420px]" : "h-[188px]";
  return (
    <div ref={ref} className={`relative overflow-hidden rounded-sm bg-matte ${h}`}>
      {seen && w.artifactId ? <AppFrame botId={botId} artifactId={w.artifactId} n={0} title={w.title} className={h} /> : null}
    </div>
  );
}
