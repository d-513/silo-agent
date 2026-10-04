import { Search, X } from "lucide-react";
import { inputClass } from "./Field";
import { Spinner } from "./Feedback";

// The search input of a list panel: a magnifier, a spinner while a search runs
// or a clear button once there is text, and Escape to clear.
export function SearchBox({ value, onChange, placeholder, searching }: { value: string; onChange: (v: string) => void; placeholder: string; searching: boolean }) {
  return (
    <label className="relative block">
      <Search size={14} className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-ink-3" />
      <input
        className={`${inputClass} w-full pr-9 pl-8`}
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") onChange("");
        }}
      />
      <span className="absolute top-1/2 right-2.5 -translate-y-1/2">
        {searching ? (
          <Spinner size={13} />
        ) : value ? (
          <button type="button" className="text-ink-3 hover:text-ink" title="Clear search" onClick={() => onChange("")}>
            <X size={14} />
          </button>
        ) : null}
      </span>
    </label>
  );
}
