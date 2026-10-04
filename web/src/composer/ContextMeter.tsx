import { Tip, TipAction, TipTitle } from "../Tip";
import type { Usage } from "../useRunStream";
import { contextFill, fmtTokens } from "./model";

// ContextMeter is how full the model's context window was on the last turn;
// clicking it compacts the conversation into a summary.
export function ContextMeter({ usage, onCompact, disabled }: { usage: Usage; onCompact?: () => void; disabled: boolean }) {
  const { used, frac, pct, tone, barTone } = contextFill(usage);
  const r = 6;
  const circ = 2 * Math.PI * r;
  const can = !disabled && !!onCompact;
  return (
    <Tip
      content={
        <div className="w-[220px]">
          <TipTitle aside={`${pct}%`}>Context window</TipTitle>
          <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-well">
            <div className={`h-full rounded-full ${barTone}`} style={{ width: `${Math.max(2, pct)}%` }} />
          </div>
          <dl className="mt-2 grid grid-cols-[1fr_auto] gap-x-3 gap-y-0.5">
            <dt>Used</dt>
            <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(used)}</dd>
            <dt>Model limit</dt>
            <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(usage.window)}</dd>
            {usage.cacheRead > 0 ? (
              <>
                <dt>From cache</dt>
                <dd className="text-right font-mono text-[11px] text-ink">{fmtTokens(usage.cacheRead)}</dd>
              </>
            ) : null}
          </dl>
          <p className="mt-2">Near the limit the conversation is summarized automatically; the thread keeps everything.</p>
          {onCompact ? <TipAction>{can ? "Click to compact now" : "You can compact once the reply finishes"}</TipAction> : null}
        </div>
      }
    >
      <button
        type="button"
        aria-label={`Context ${pct}% full. Compact conversation`}
        aria-disabled={!can}
        onClick={can ? onCompact : undefined}
        className={`flex h-[34px] shrink-0 items-center gap-1.5 rounded-control px-2 font-mono text-[11px] transition-[background-color,color,transform] duration-[160ms] ease-quiet ${
          can ? "hover:bg-well hover:text-ink active:scale-[.96] active:duration-[70ms]" : "cursor-default"
        } ${tone}`}
      >
        <svg width="16" height="16" viewBox="0 0 16 16" aria-hidden className="shrink-0 -rotate-90">
          <circle cx="8" cy="8" r={r} fill="none" stroke="currentColor" strokeOpacity="0.18" strokeWidth="2" />
          <circle
            cx="8"
            cy="8"
            r={r}
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeDasharray={circ}
            strokeDashoffset={circ * (1 - frac)}
            className="transition-[stroke-dashoffset] duration-[400ms] ease-quiet"
          />
        </svg>
        <span className="hidden @min-[400px]:inline">{pct}%</span>
      </button>
    </Tip>
  );
}
