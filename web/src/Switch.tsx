import type { ReactNode } from "react";

// 44×26 track; the 20px knob stretches to 26px while pressed, growing toward
// where it is about to go, then slides over 260ms.
export function Switch({
  on,
  onChange,
  disabled,
  title,
}: {
  on: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  title?: string;
}) {
  return (
    <button
      type="button"
      role="switch"
      aria-checked={on}
      disabled={disabled}
      title={title}
      onClick={(e) => {
        e.stopPropagation();
        onChange(!on);
      }}
      className={`group/switch relative inline-block h-[26px] w-[44px] shrink-0 rounded-full transition-[background-color,box-shadow] duration-[260ms] ease-quiet disabled:cursor-not-allowed disabled:opacity-40 ${
        on ? "bg-ink" : "bg-pressed shadow-[inset_0_0_0_1px_var(--color-line)]"
      }`}
    >
      <span
        aria-hidden
        className={`absolute top-[3px] h-5 w-5 rounded-full bg-white shadow-[0_0_0_1px_rgb(26_25_23/0.08),0_1px_2px_rgb(20_18_14/0.18)] transition-[left,width] duration-[260ms] ease-quiet group-enabled/switch:group-active/switch:w-[26px] ${
          on ? "left-[21px] group-enabled/switch:group-active/switch:left-[15px]" : "left-[3px]"
        }`}
      />
    </button>
  );
}

export function ToggleRow({
  label,
  hint,
  meta,
  on,
  onChange,
  disabled,
  className = "",
}: {
  label: ReactNode;
  hint?: ReactNode;
  meta?: ReactNode;
  on: boolean;
  onChange: (v: boolean) => void;
  disabled?: boolean;
  className?: string;
}) {
  return (
    <div className={`flex items-center justify-between gap-4 ${className}`}>
      <div className="min-w-0">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-[14px] font-medium text-ink">{label}</span>
          {meta}
        </div>
        {hint ? <p className="mt-0.5 text-[12.5px] leading-[18px] text-ink-2">{hint}</p> : null}
      </div>
      <Switch on={on} onChange={onChange} disabled={disabled} />
    </div>
  );
}
