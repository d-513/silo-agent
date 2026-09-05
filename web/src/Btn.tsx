import { type ButtonHTMLAttributes, type ReactNode } from "react";

export type BtnKind = "primary" | "secondary" | "deny" | "ghost";

const kindClass: Record<BtnKind, string> = {
  primary: "border border-transparent bg-bindery text-plaster hover:bg-bindery-deep",
  secondary: "border border-thread bg-folio text-iron hover:border-[#B9B3A6] hover:bg-linen",
  deny: "border border-transparent bg-carmine text-plaster hover:bg-[#6E2230]",
  ghost: "border border-transparent text-stone hover:text-iron",
};

const glyphWell: Record<BtnKind, string> = {
  primary: "bg-bindery-deep/45",
  secondary: "bg-linen",
  deny: "bg-plaster/20",
  ghost: "bg-linen",
};

export function btnClass(kind: BtnKind = "secondary", extra = "") {
  return [
    "inline-flex h-9 items-center gap-2 rounded px-3 text-[14px] font-medium outline-none",
    "transition-[transform,background-color,border-color] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)]",
    "active:scale-[0.98] focus-visible:border-bindery disabled:opacity-50",
    kindClass[kind],
    extra,
  ].join(" ");
}

export function Btn({
  kind = "secondary",
  icon,
  className = "",
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { kind?: BtnKind; icon?: ReactNode }) {
  return (
    <button className={btnClass(kind, `${icon ? "pr-1.5" : ""} ${className}`)} {...rest}>
      {children}
      {icon ? (
        <span className={`flex h-5 w-5 shrink-0 items-center justify-center rounded-[4px] ${glyphWell[kind]}`}>
          {icon}
        </span>
      ) : null}
    </button>
  );
}
