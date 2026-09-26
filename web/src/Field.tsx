import type { ReactNode } from "react";

// Inputs: 36px surface with an inset line-strong ring. Focus is a 1px cobalt
// ring plus the global 2px cobalt outline. Inside a Field with an error the
// ring turns vermilion on a vermilion-pale ground.
const fieldBase =
  "w-full rounded-control bg-surface text-[14px] text-ink outline-none shadow-[inset_0_0_0_1px_var(--color-line-strong)] transition-[box-shadow,background-color] duration-[160ms] ease-quiet placeholder:text-ink-3 focus:shadow-[inset_0_0_0_1px_var(--color-cobalt)] disabled:cursor-not-allowed disabled:bg-well disabled:text-ink-3 group-data-[invalid]/field:bg-vermilion-pale group-data-[invalid]/field:shadow-[inset_0_0_0_1px_var(--color-vermilion)] aria-[invalid=true]:bg-vermilion-pale aria-[invalid=true]:shadow-[inset_0_0_0_1px_var(--color-vermilion)]";

export const inputClass = `h-9 px-3 ${fieldBase}`;

export const textareaClass = `resize-y px-3 py-2 leading-[22px] ${fieldBase}`;

export const labelClass = "text-[12px] font-medium leading-4 text-ink-3";

export function Field({
  label,
  hint,
  error,
  required,
  headerRight,
  children,
  className = "",
}: {
  label?: string;
  hint?: string;
  error?: string;
  required?: boolean;
  headerRight?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={`group/field ${className}`} data-invalid={error ? "" : undefined}>
      {label ? (
        <div className="mb-1.5 flex items-center gap-2">
          <label className={labelClass}>
            {label}
            {required ? " *" : ""}
          </label>
          {headerRight ? <span className="ml-auto flex items-center gap-2">{headerRight}</span> : null}
        </div>
      ) : null}
      {children}
      {error ? (
        <p role="alert" className="mt-1.5 text-[12px] leading-4 text-vermilion">
          {error}
        </p>
      ) : hint ? (
        <p className="mt-1.5 text-[12.5px] leading-[18px] text-ink-3">{hint}</p>
      ) : null}
    </div>
  );
}

export function Panel({
  title,
  note,
  action,
  tone = "default",
  padded = true,
  className = "",
  children,
}: {
  title?: string;
  note?: string;
  action?: ReactNode;
  // danger panels hold armed destructive buttons; the panel itself stays calm.
  tone?: "default" | "danger";
  padded?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      data-tone={tone}
      className={`overflow-hidden rounded-card bg-surface shadow-card ${className}`}
    >
      {title ? (
        <header className="flex items-start justify-between gap-3 px-5 py-4 shadow-[inset_0_-1px_0_var(--color-line)]">
          <div className="min-w-0">
            <h3 className="text-[15px] leading-5 font-semibold tracking-[-0.01em] text-ink">{title}</h3>
            {note ? <p className="mt-1 text-[12.5px] leading-[18px] text-ink-2">{note}</p> : null}
          </div>
          {action}
        </header>
      ) : null}
      <div className={padded ? "px-5 py-5" : ""}>{children}</div>
    </section>
  );
}

// Skeleton rows for list loads: reserve the row height so nothing shifts.
export function SkeletonRows({ rows = 3, height = 56, className = "" }: { rows?: number; height?: number; className?: string }) {
  return (
    <div className={`space-y-2 ${className}`} aria-busy="true" aria-label="Loading">
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="skeleton rounded-control" style={{ height }} />
      ))}
    </div>
  );
}
