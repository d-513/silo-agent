import { Search, X } from "lucide-react";
import { useEffect, useRef } from "react";

// The search field of a list page's toolbar: a magnifier, a clear button once
// there is text, and a ⌘K / Ctrl+K hint (and shortcut) while it is empty.
export function ToolbarSearch({ value, onChange, placeholder, className = "sm:w-72" }: { value: string; onChange: (v: string) => void; placeholder: string; className?: string }) {
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    function onKeyDown(e: KeyboardEvent) {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === "k") {
        e.preventDefault();
        ref.current?.focus();
      }
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);
  return (
    <div className={`relative w-full ${className}`}>
      <span className="pointer-events-none absolute inset-y-0 left-0 flex items-center pl-3 text-ink-3">
        <Search size={15} />
      </span>
      <input
        ref={ref}
        type="text"
        aria-label={placeholder}
        className="h-9 w-full rounded-sm bg-surface pl-9 pr-14 text-[13px] text-ink shadow-card transition-colors placeholder:text-ink-3 focus:outline-none focus:ring-1 focus:ring-cobalt"
        placeholder={placeholder}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === "Escape") onChange("");
        }}
      />
      {value ? (
        <button type="button" className="absolute inset-y-0 right-0 flex items-center pr-2.5 text-ink-2 hover:text-ink" onClick={() => onChange("")} title="Clear search">
          <X size={14} />
        </button>
      ) : (
        <span className="pointer-events-none absolute inset-y-0 right-0 flex items-center pr-2.5">
          <kbd className="rounded bg-well px-1.5 py-0.5 font-mono text-[10px] text-ink-3">⌘K</kbd>
        </span>
      )}
    </div>
  );
}
