import { useRef, type KeyboardEvent, type ReactNode } from "react";

export type ChipOption<V extends string | boolean> = {
  value: V;
  label: ReactNode;
  icon?: ReactNode;
  // The selected chip's text colour (default ink).
  tone?: string;
  // This one choice is unavailable: it still explains itself (aria-disabled,
  // so the title shows), but cannot be picked.
  disabled?: boolean;
  title?: string;
  // Replaces the resting look of an unselected chip (e.g. an armed one).
  look?: string;
};

// ChipRadio is the one segmented choice: the options on a well, the current
// one a raised surface chip. A radio group to assistive tech, with arrow keys
// moving the choice and one tab stop (the selected chip). `chip` sets a chip's
// width and padding; `className` adds to the group (its width, wrapping).
export function ChipRadio<V extends string | boolean>({
  options,
  value,
  onChange,
  label,
  disabled,
  size = "md",
  chip = "px-3",
  className = "w-fit",
}: {
  options: ChipOption<V>[];
  value: V;
  onChange: (v: V) => void;
  label?: string;
  disabled?: boolean;
  size?: "sm" | "md";
  chip?: string;
  className?: string;
}) {
  const refs = useRef<(HTMLButtonElement | null)[]>([]);
  const at = options.findIndex((o) => o.value === value);
  const open = (o: ChipOption<V>) => !disabled && !o.disabled;

  const keys = (e: KeyboardEvent) => {
    const step = e.key === "ArrowRight" || e.key === "ArrowDown" ? 1 : e.key === "ArrowLeft" || e.key === "ArrowUp" ? -1 : 0;
    if (!step) return;
    e.preventDefault();
    const n = options.length;
    for (let i = 1; i < n; i++) {
      const j = (((at < 0 ? 0 : at) + step * i) % n + n) % n;
      if (!open(options[j])) continue;
      refs.current[j]?.focus();
      onChange(options[j].value);
      return;
    }
  };

  return (
    <div role="radiogroup" aria-label={label} onKeyDown={keys} className={`flex select-none items-center gap-0.5 rounded-control bg-well p-[3px] ${className}`}>
      {options.map((o, i) => {
        const active = i === at;
        const off = !open(o);
        return (
          <button
            key={String(o.value)}
            ref={(el) => {
              refs.current[i] = el;
            }}
            type="button"
            role="radio"
            aria-checked={active}
            tabIndex={active || (at < 0 && i === 0) ? 0 : -1}
            disabled={disabled}
            aria-disabled={o.disabled || undefined}
            title={o.title}
            onClick={(e) => {
              e.preventDefault();
              e.stopPropagation();
              if (off) return;
              onChange(o.value);
            }}
            className={`flex items-center justify-center rounded-sm font-medium transition-[transform,background-color,color,box-shadow] duration-[200ms] ease-quiet active:duration-[70ms] ${chip} ${
              size === "sm" ? "h-7 text-[11.5px]" : "h-8 text-[12.5px]"
            } ${active ? `bg-surface shadow-card ${o.tone ?? "text-ink"}` : (o.look ?? (off ? "text-ink-3" : "text-ink-3 hover:bg-pressed hover:text-ink"))} ${
              off ? "cursor-not-allowed opacity-50" : "cursor-pointer active:scale-[.97]"
            }`}
          >
            {o.icon}
            {o.label}
          </button>
        );
      })}
    </div>
  );
}
