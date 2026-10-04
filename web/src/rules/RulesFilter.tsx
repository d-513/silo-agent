import { Search, X } from "lucide-react";

export function RulesFilter({ value, onChange }: { value: string; onChange: (v: string) => void }) {
  return (
    <div className="relative w-full sm:w-[220px]">
      <Search size={14} className="pointer-events-none absolute top-1/2 left-2.5 -translate-y-1/2 text-ink-3" />
      <input
        type="text"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="Filter rules..."
        className="h-8 w-full rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface pr-7 pl-8 text-[13px] text-ink placeholder:text-ink-3 focus:outline-none"
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange("")}
          className="absolute top-1/2 right-2 -translate-y-1/2 text-ink-2 hover:text-ink"
          title="Clear filter"
        >
          <X size={12} />
        </button>
      )}
    </div>
  );
}
