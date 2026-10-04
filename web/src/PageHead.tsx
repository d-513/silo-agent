import { ArrowLeft } from "lucide-react";
import type { ReactNode } from "react";

// The title row of a Bot sub-page that has a way back: back button, an optional
// mark (a logo tile), the title, and a quiet subtitle.
export function PageHead({ title, subtitle, mark, onBack }: { title: string; subtitle?: string; mark?: ReactNode; onBack: () => void }) {
  return (
    <div className="mb-6 flex items-center gap-3.5">
      <button
        type="button"
        className="flex h-9 w-9 shrink-0 items-center justify-center rounded-sm bg-surface text-ink-2 shadow-card transition-colors hover:text-ink hover:shadow-float"
        onClick={onBack}
        aria-label="Back"
      >
        <ArrowLeft size={16} />
      </button>
      {mark}
      <div className="min-w-0">
        <h2 className="truncate text-title text-ink">{title}</h2>
        {subtitle ? <div className="mt-0.5 text-[12.5px] text-ink-3">{subtitle}</div> : null}
      </div>
    </div>
  );
}
