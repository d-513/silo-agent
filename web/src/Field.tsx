import type { ReactNode } from "react";

export const inputClass =
  "h-9 w-full rounded-[6px] border border-thread bg-folio px-3 text-[14px] text-iron outline-none transition-colors duration-150 placeholder:text-stone/60 hover:border-hover focus:border-bindery focus-visible:border-bindery disabled:cursor-not-allowed disabled:bg-cloth disabled:text-stone";

export const textareaClass =
  "w-full resize-y rounded-[6px] border border-thread bg-folio px-3 py-2 text-[14px] leading-relaxed text-iron outline-none transition-colors duration-150 placeholder:text-stone/60 hover:border-hover focus:border-bindery focus-visible:border-bindery disabled:cursor-not-allowed disabled:bg-cloth disabled:text-stone";

export function Field({
  label,
  hint,
  required,
  headerRight,
  children,
  className = "",
}: {
  label?: string;
  hint?: string;
  required?: boolean;
  headerRight?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={className}>
      {label ? (
        <div className="mb-1.5 flex items-center gap-2">
          <label className="text-[12px] font-medium text-stone">
            {label}
            {required ? " *" : ""}
          </label>
          {headerRight ? <span className="ml-auto flex items-center gap-2">{headerRight}</span> : null}
        </div>
      ) : null}
      {children}
      {hint ? <p className="mt-1.5 text-[12px] text-stone">{hint}</p> : null}
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
  tone?: "default" | "danger";
  padded?: boolean;
  className?: string;
  children: ReactNode;
}) {
  return (
    <section
      className={`overflow-hidden rounded-[10px] border bg-folio ${
        tone === "danger" ? "border-carmine/40" : "border-thread"
      } ${className}`}
    >
      {title ? (
        <header className="flex items-start justify-between gap-3 border-b border-thread-2 px-4 py-3">
          <div className="min-w-0">
            <h3 className={`text-[14px] font-medium ${tone === "danger" ? "text-carmine" : "text-iron"}`}>{title}</h3>
            {note ? <p className="mt-0.5 text-[12px] text-stone">{note}</p> : null}
          </div>
          {action}
        </header>
      ) : null}
      <div className={padded ? "px-4 py-4" : ""}>{children}</div>
    </section>
  );
}
