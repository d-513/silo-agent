import { AlertCircle, Check, MessageSquare, RotateCw, X } from "lucide-react";
import { type HTMLAttributes, type ReactNode, useEffect, useRef, useState } from "react";
import { AppFrame } from "../Artifact";
import { Btn } from "../Btn";
import { ArmedButton, Spinner } from "../Feedback";
import type { DashboardWidget } from "../gen/silo/v1/ui_pb";
import { clockTime, fullTime } from "../format";
import { t } from "../i18n.ts";
import { apart } from "../links";
import { Tip } from "../Tip";
import { Body, HandTip } from "./kinds";
import { type Action, type Cols, type Span, type WidgetHandlers, actions as actionsOf, age, checked, interval, items, obj, receipt as receiptOf, span, text } from "./widgets";

export type { WidgetHandlers };

// spanWord is a span of time as the widget's corner says it: "4 m", "2 h".
function spanWord(s: Span): string {
  if (s.unit === "m") return t("{{n}} m", { n: s.n, context: "minutes" });
  if (s.unit === "h") return t("{{n}} h", { n: s.n, context: "hours" });
  return t("{{n}} d", { n: s.n, context: "days" });
}

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
  const meta = [compact ? "" : w.source, stamp === "now" ? t("now") : stamp ? spanWord(stamp) : ""].filter(Boolean).join(" · ");
  const every = interval(w.every);
  const when = [w.dataAt ? fullTime(new Date(w.dataAt)) : "", every ? t("Refreshes every {{every}}", { every: spanWord(every) }) : ""].filter(Boolean);
  const tone = onHatch ? "text-on-hatch/75 hover:text-on-hatch hover:bg-on-hatch/10" : "text-ink-3 hover:bg-well hover:text-ink";
  const reveal = selected ? "opacity-100" : "opacity-0 group-hover/w:opacity-100 group-focus-within/w:opacity-100 [@media(hover:none)]:opacity-100";
  return (
    <div className="flex shrink-0 items-center gap-1">
      {busy ? (
        <span className={onHatch ? "text-[11.5px]" : "shimmer-text text-[11.5px]"}>{t("Refreshing")}</span>
      ) : extra ? (
        extra
      ) : meta ? (
        when.length ? (
          <Tip content={when.join(" · ")}>
            <span className={`font-mono text-[11.5px] ${onHatch ? "" : "text-ink-3"}`}>{meta}</span>
          </Tip>
        ) : (
          <span className={`font-mono text-[11.5px] ${onHatch ? "" : "text-ink-3"}`}>{meta}</span>
        )
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
              <HandTip key={a.id} hand={a.kind === "hand"}>
                <Btn kind={kind} size="sm" disabled={busy !== ""} onClick={(e) => run(a, apart(e))} icon={icon}>
                  {a.label}
                </Btn>
              </HandTip>
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
            {t("by you · {{time}}", { time: rc.at ? clockTime(new Date(rc.at)) : "" })}
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
