import { X } from "lucide-react";
import { fmtClock, type Dictation } from "../voice";
import { levelBars } from "./model";

// DictationStrip sits above the toolbar while a take is live: a breathing dot,
// the clock, a five-bar level meter, and Cancel. While the CP transcribes it
// becomes a shimmering "Transcribing…".
export function DictationStrip({ dictation }: { dictation: Dictation }) {
  if (dictation.state === "transcribing") {
    return (
      <div className="flex h-8 items-center px-4 pt-2 text-[12px]" aria-live="polite">
        <span className="shimmer-text font-medium">Transcribing…</span>
      </div>
    );
  }
  return (
    <div className="rise flex h-8 items-center gap-2 px-4 pt-2 text-[12px] text-ink-2" aria-live="polite">
      <span className="breathe h-1.5 w-1.5 shrink-0 rounded-full bg-vermilion" />
      <span className="font-medium text-ink">Listening</span>
      <span className="font-mono text-ink-3 tabular-nums">{fmtClock(dictation.elapsed)}</span>
      <span className="flex h-3.5 items-center gap-[3px]" aria-hidden>
        {levelBars(dictation.level).map((h, i) => (
          <span key={i} className="w-[3px] rounded-full bg-ink-3 transition-[height] duration-[80ms]" style={{ height: `${h}px` }} />
        ))}
      </span>
      <span className="flex-1" />
      <span className="hidden text-ink-3 @min-[460px]:inline">Esc cancels</span>
      <button
        type="button"
        title="Cancel dictation"
        aria-label="Cancel dictation"
        className="flex h-6 w-6 items-center justify-center rounded-xs text-ink-2 transition-colors duration-[160ms] hover:bg-well hover:text-ink"
        onClick={dictation.cancel}
      >
        <X size={13} />
      </button>
    </div>
  );
}
