import type { ReactNode } from "react";

// FilterChip narrows a list to one kind of thing. The open one is an ink fill
// at the same weight, so picking one moves nothing beside it.
export function FilterChip({ on, onClick, children }: { on: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      onClick={onClick}
      className={`h-6 rounded-full px-3 text-[11px] font-medium transition-colors duration-[160ms] ease-quiet ${
        on ? "bg-ink text-on-ink" : "bg-well text-ink-2 hover:bg-pressed hover:text-ink"
      }`}
    >
      {children}
    </button>
  );
}
