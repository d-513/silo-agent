import { Check } from "lucide-react";
import type { ReactNode } from "react";

export function Step({ n, title, note, done, locked, children }: { n: number; title: string; note?: string; done?: boolean; locked?: boolean; children: ReactNode }) {
  return (
    <section className={`rounded-card bg-surface p-5 shadow-card transition-opacity duration-[200ms] ease-quiet ${locked ? "opacity-55" : ""}`} aria-disabled={locked || undefined}>
      <header className="mb-4 flex items-start gap-3">
        <span
          className={`flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[12px] font-semibold tabular-nums transition-colors duration-[240ms] ease-quiet ${
            done ? "bg-ink text-white" : "bg-well text-ink-2"
          }`}
        >
          {done ? <Check size={13} strokeWidth={2.5} /> : n}
        </span>
        <div className="min-w-0">
          <h3 className="text-card-title leading-6 text-ink">{title}</h3>
          {note ? <p className="text-[12.5px] leading-[18px] text-ink-2">{note}</p> : null}
        </div>
      </header>
      <fieldset disabled={locked} className="space-y-4">
        {children}
      </fieldset>
    </section>
  );
}
