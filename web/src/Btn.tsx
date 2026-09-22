import { type ButtonHTMLAttributes, type ReactNode } from "react";

export type BtnKind = "primary" | "secondary" | "deny" | "ghost";

const kindClass: Record<BtnKind, string> = {
  primary: "border border-transparent bg-bindery text-white hover:bg-bindery-deep shadow-xs",
  secondary: "border border-thread bg-folio text-iron hover:border-hover hover:bg-cloth shadow-xs",
  deny: "border border-transparent bg-carmine text-white hover:bg-[#b91c1c] shadow-xs",
  ghost: "border border-transparent text-stone hover:bg-cloth hover:text-iron",
};

const glyphWell: Record<BtnKind, string> = {
  primary: "bg-white/20 text-white",
  secondary: "bg-cloth text-stone",
  deny: "bg-white/20 text-white",
  ghost: "bg-cloth text-stone",
};

export function btnClass(kind: BtnKind = "secondary", extra = "") {
  return [
    "inline-flex h-9 shrink-0 items-center gap-2 whitespace-nowrap rounded-[6px] px-3 text-[14px] font-medium outline-none",
    "transition-[transform,background-color,border-color] duration-200 ease-quiet",
    "active:scale-[0.98] focus-visible:border-bindery disabled:cursor-not-allowed disabled:opacity-50 disabled:active:scale-100",
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
