import type { ReactNode } from "react";

// The segmented pill tabs of a list page (Connectors, Skills): a `well` tray
// with the open tab lifted onto `surface`, each with an optional count.
export function TabPills({ children }: { children: ReactNode }) {
  return <div className="flex items-center rounded-sm bg-well p-1 text-[13px] font-medium text-ink-3">{children}</div>;
}

export function TabPill({ on, onClick, count, children }: { on: boolean; onClick: () => void; count?: number; children: ReactNode }) {
  return (
    <button
      type="button"
      aria-pressed={on}
      className={`flex items-center gap-1.5 rounded-md px-3.5 py-1.5 transition-[background-color,color,box-shadow] ${on ? "bg-surface font-semibold text-ink shadow-2xs" : "hover:text-ink"}`}
      onClick={onClick}
    >
      {children}
      {count !== undefined ? (
        <span className={`rounded-full px-1.5 py-0.2 text-[10px] font-semibold ${on ? "bg-cobalt-pale text-cobalt" : "bg-pressed text-ink-3"}`}>{count}</span>
      ) : null}
    </button>
  );
}
