import { type ButtonHTMLAttributes, type ReactNode } from "react";

export type BtnKind = "primary" | "secondary" | "deny" | "ghost";
export type BtnSize = "md" | "sm";

const kindClass: Record<BtnKind, string> = {
  primary: "bg-ink text-white hover:bg-black",
  secondary: "bg-surface text-ink shadow-[inset_0_0_0_1px_var(--color-line-strong)] hover:bg-well",
  deny: "text-vermilion hover:bg-vermilion-pale",
  ghost: "text-ink-2 hover:bg-well hover:text-ink",
};

const glyphWell: Record<BtnKind, string> = {
  primary: "bg-white/20 text-white",
  secondary: "bg-well text-ink-2",
  deny: "bg-vermilion-pale text-vermilion",
  ghost: "bg-well text-ink-2",
};

const sizeClass: Record<BtnSize, { box: string; icon: string }> = {
  md: { box: "h-9 gap-2 px-3 text-[14px]", icon: "w-9 px-0" },
  sm: { box: "h-8 gap-1.5 px-2.5 text-[12.5px]", icon: "w-8 px-0" },
};

// Buttons press down in 70ms and settle back over the 160ms hover curve.
export const pressClass =
  "transition-[transform,background-color,color,box-shadow] duration-[160ms] ease-quiet active:scale-[.97] active:duration-[70ms] motion-reduce:active:scale-100";

export function btnClass(kind: BtnKind = "secondary", extra = "", size: BtnSize = "md", iconOnly = false) {
  const s = sizeClass[size];
  return [
    "relative inline-flex shrink-0 items-center whitespace-nowrap rounded-control font-medium outline-none select-none",
    s.box,
    iconOnly ? `justify-center ${s.icon}` : "",
    pressClass,
    "disabled:cursor-not-allowed disabled:opacity-50 disabled:active:scale-100",
    kindClass[kind],
    extra,
  ].join(" ");
}

export function Btn({
  kind = "secondary",
  size = "md",
  iconOnly = false,
  icon,
  className = "",
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { kind?: BtnKind; size?: BtnSize; iconOnly?: boolean; icon?: ReactNode }) {
  if (iconOnly) {
    return (
      <button className={btnClass(kind, className, size, true)} {...rest}>
        {icon ?? children}
      </button>
    );
  }
  return (
    <button className={btnClass(kind, `${icon ? "pr-[7px]" : ""} ${className}`, size)} {...rest}>
      {children}
      {icon ? (
        <span className={`flex h-[22px] w-[22px] shrink-0 items-center justify-center rounded-xs ${glyphWell[kind]}`}>
          {icon}
        </span>
      ) : null}
    </button>
  );
}
