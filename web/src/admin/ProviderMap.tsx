import { ArrowRight } from "lucide-react";
import { useLayoutEffect, useRef, useState } from "react";
import type { Provider } from "../gen/silo/v1/ui_pb";
import { t } from "../i18n.ts";
import { markOf, modelOf, providerOf, providersInUse, type Job } from "./providerMap";

// ProviderMap draws which provider does each job: the jobs down the left,
// the providers they use down the right, a hairline from each job to its
// provider (dashed where a job follows another setting). Hovering a job or a
// provider brings its lines forward; pressing a job opens the page that sets
// it, pressing a provider opens its settings. A narrow frame reads as a plain
// list instead.

type Path = { job: string; provider: string; d: string; dashed: boolean };

// Below this width the two columns and the lines between them do not fit.
const NARROW = 560;

export function ProviderMap({ jobs, providers, ready, onJob, onPick }: { jobs: Job[]; providers: Provider[]; ready: (id: string) => boolean; onJob: (job: Job) => void; onPick: (id: string) => void }) {
  const box = useRef<HTMLDivElement>(null);
  const jobEls = useRef(new Map<string, HTMLElement>());
  const provEls = useRef(new Map<string, HTMLElement>());
  const [paths, setPaths] = useState<Path[]>([]);
  const [size, setSize] = useState({ w: 0, h: 0 });
  const [hover, setHover] = useState<{ job?: string; provider?: string }>({});
  const used = providersInUse(jobs);
  const byID = new Map(providers.map((p) => [p.id, p]));
  const narrow = size.w > 0 && size.w < NARROW;
  const sig = jobs.map((j) => j.key + "=" + j.model).join("|");

  useLayoutEffect(() => {
    const el = box.current;
    if (!el) return;
    const measure = () => {
      const r = el.getBoundingClientRect();
      setSize({ w: r.width, h: r.height });
      const out: Path[] = [];
      for (const j of jobs) {
        const p = providerOf(j.model);
        const a = jobEls.current.get(j.key)?.getBoundingClientRect();
        const b = provEls.current.get(p)?.getBoundingClientRect();
        if (!p || !a || !b) continue;
        const x1 = a.right - r.left;
        const y1 = a.top + a.height / 2 - r.top;
        const x2 = b.left - r.left;
        const y2 = b.top + b.height / 2 - r.top;
        const dx = Math.max(24, (x2 - x1) / 2);
        out.push({ job: j.key, provider: p, d: `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`, dashed: !!j.inherited });
      }
      setPaths(out);
    };
    measure();
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    return () => ro.disconnect();
    // The lines follow what each job is done with.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [sig, narrow]);

  const lit = (j: string, p: string) => (hover.job ? hover.job === j : hover.provider ? hover.provider === p : false);
  const anyHover = !!(hover.job || hover.provider);

  if (narrow) {
    return (
      <div ref={box} className="divide-y divide-line">
        {jobs.map((j) => {
          const p = byID.get(providerOf(j.model));
          return (
            <div key={j.key} className="flex items-center gap-3 py-2.5 text-[13px]">
              <button type="button" className="w-28 shrink-0 text-left font-medium text-ink hover:text-cobalt" onClick={() => onJob(j)}>
                {t(j.label)}
              </button>
              {p ? (
                <button type="button" className="flex min-w-0 flex-1 items-center gap-2 text-left" onClick={() => onPick(p.id)}>
                  <ArrowRight size={13} className="shrink-0 text-ink-3" />
                  <span className="shrink-0 font-medium text-ink">{p.name}</span>
                  <span className="min-w-0 truncate font-mono text-[11.5px] text-ink-3">{modelOf(j.model)}</span>
                </button>
              ) : (
                <span className="min-w-0 truncate text-ink-3">{t(j.idle)}</span>
              )}
            </div>
          );
        })}
      </div>
    );
  }

  return (
    <div ref={box} className="relative grid grid-cols-[minmax(0,15rem)_minmax(3.5rem,1fr)_minmax(0,15rem)] items-center gap-y-1.5" onMouseLeave={() => setHover({})} onBlur={() => setHover({})}>
      <svg className="pointer-events-none absolute inset-0" width={size.w} height={size.h} aria-hidden>
        {paths.map((p) => {
          const on = lit(p.job, p.provider);
          return (
            <path
              key={p.job}
              d={p.d}
              fill="none"
              strokeWidth={on ? 1.5 : 1.15}
              strokeDasharray={p.dashed ? "3 3" : undefined}
              className={`transition-[stroke,stroke-width] duration-[160ms] ease-quiet ${on ? "stroke-ink" : anyHover ? "stroke-line" : "stroke-line-strong"}`}
            />
          );
        })}
      </svg>

      {/* Jobs, one row each, down the left. */}
      <div className="col-start-1 row-start-1 grid gap-1.5" style={{ gridRow: `1 / span 1` }}>
        {jobs.map((j) => {
          const p = providerOf(j.model);
          const missing = !!p && !ready(p);
          return (
            <button
              type="button"
              key={j.key}
              ref={(el) => {
                if (el) jobEls.current.set(j.key, el);
                else jobEls.current.delete(j.key);
              }}
              onMouseEnter={() => setHover({ job: j.key })}
              onFocus={() => setHover({ job: j.key })}
              onClick={() => onJob(j)}
              className={`relative flex h-11 min-w-0 flex-col justify-center rounded-control px-3 text-left transition-colors duration-[160ms] ease-quiet ${hover.job === j.key ? "bg-well" : ""}`}
            >
              <span className="flex items-center gap-1.5 text-[13px] leading-[18px] font-medium text-ink">
                {t(j.label)}
                {missing ? <span className="text-[11.5px] font-normal text-vermilion">{t("needs a key")}</span> : null}
              </span>
              <span className="truncate font-mono text-[11.5px] leading-4 text-ink-3">{j.model ? modelOf(j.model) : t(j.idle)}</span>
              {/* Where a line leaves; a job nobody does has an open end. */}
              <span aria-hidden className={`absolute top-1/2 -right-[3px] h-[6px] w-[6px] -translate-y-1/2 rounded-full ${p ? "bg-ink-3" : "shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-canvas"}`} />
            </button>
          );
        })}
      </div>

      {/* The providers in use, down the right. */}
      <div className="col-start-3 row-start-1 grid content-center gap-2.5">
        {used.map((id) => {
          const p = byID.get(id);
          if (!p) return null;
          const on = hover.provider === id || (!!hover.job && providerOf(jobs.find((j) => j.key === hover.job)?.model ?? "") === id);
          const ok = ready(id);
          const count = jobs.filter((j) => providerOf(j.model) === id).length;
          return (
            <button
              key={id}
              type="button"
              ref={(el) => {
                if (el) provEls.current.set(id, el);
                else provEls.current.delete(id);
              }}
              onMouseEnter={() => setHover({ provider: id })}
              onFocus={() => setHover({ provider: id })}
              onClick={() => onPick(id)}
              className={`relative flex h-14 min-w-0 items-center gap-2.5 rounded-card bg-surface pr-3 pl-2.5 text-left transition-[box-shadow] duration-[160ms] ease-quiet ${on ? "shadow-float" : "shadow-card"}`}
            >
              <span aria-hidden className="absolute top-1/2 -left-[3px] h-[6px] w-[6px] -translate-y-1/2 rounded-full bg-ink-3" />
              <ProviderMark name={p.name} />
              <span className="min-w-0 flex-1">
                <span className="block truncate text-[13.5px] leading-[18px] font-medium text-ink">{p.name}</span>
                <span className="block truncate text-[11.5px] leading-4 text-ink-3">{t("{{count}} jobs", { count })}</span>
              </span>
              <Status ok={ok} />
            </button>
          );
        })}
      </div>
    </div>
  );
}

// ProviderMark is a provider's monogram on a well tile: the crest is a Bot's
// character, a provider gets only its initials.
export function ProviderMark({ name, small }: { name: string; small?: boolean }) {
  return (
    <span
      title={small ? name : undefined}
      className={`flex shrink-0 items-center justify-center rounded-sm bg-well font-mono font-semibold tracking-wide text-ink-2 ${small ? "h-6 w-6 text-[9.5px]" : "h-8 w-8 text-[11px]"}`}
    >
      {markOf(name)}
    </span>
  );
}

// Status is a provider's lamp and word: ready to be asked, or waiting for a key.
export function Status({ ok, compact }: { ok: boolean; compact?: boolean }) {
  return (
    <span className="flex shrink-0 items-center gap-1.5 text-[12px]">
      <span aria-hidden className={`h-[7px] w-[7px] rounded-full ${ok ? "bg-lamp" : "bg-canvas shadow-[inset_0_0_0_1.5px_var(--color-line-strong)]"}`} />
      {compact ? null : <span className={ok ? "text-emerald" : "text-ink-3"}>{ok ? t("Ready") : t("Not set up")}</span>}
    </span>
  );
}
