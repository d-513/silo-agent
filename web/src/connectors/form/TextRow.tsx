import type { InputHTMLAttributes } from "react";

const inputBox = "h-9 w-full rounded-sm shadow-[inset_0_0_0_1px_var(--color-line-strong)] bg-surface px-3";

// A label over a one-line input: the shape most of the connector form is.
export function TextRow({ label, mono, ...input }: { label: string; mono?: boolean } & InputHTMLAttributes<HTMLInputElement>) {
  return (
    <>
      <label className="mb-1 block text-[12px] font-medium text-ink-3">{label}</label>
      <input className={`mb-3 ${inputBox}${mono ? " font-mono" : ""}`} {...input} />
    </>
  );
}
