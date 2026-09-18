import type { ReactNode } from "react";

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
      title={title ?? (on ? "On" : "Off")}
      onClick={(e) => {
        e.stopPropagation();
        onChange(!on);
      }}
      className={`relative inline-flex h-[22px] w-[40px] shrink-0 items-center rounded-full transition-colors duration-200 ease-quiet disabled:cursor-not-allowed disabled:opacity-40 ${
        on ? "bg-bindery" : "bg-thread"
      }`}
    >
      <span
        className={`inline-block h-[18px] w-[18px] transform rounded-full bg-folio transition-transform duration-200 ease-quiet ${
          on ? "translate-x-[20px]" : "translate-x-[2px]"
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
          <span className="text-[14px] font-medium text-iron">{label}</span>
          {meta}
        </div>
        {hint ? <p className="mt-0.5 text-[12px] text-stone">{hint}</p> : null}
      </div>
      <Switch on={on} onChange={onChange} disabled={disabled} />
    </div>
  );
}
