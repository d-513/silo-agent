import { Check, Copy } from "lucide-react";
import { useCallback, useEffect, useRef, useState, type ButtonHTMLAttributes, type MouseEvent, type ReactNode } from "react";
import { btnClass, type BtnKind, type BtnSize } from "./Btn";

/**
 * Destructive confirm: the first call arms for `ms`, the second fires.
 * Returns `armed` so the button can show its vermilion state and drain bar.
 */
export function useArmed(ms = 3000) {
  const [armed, setArmed] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const disarm = useCallback(() => {
    clearTimeout(timer.current);
    setArmed(false);
  }, []);
  const fire = useCallback(
    (action: () => void) => {
      if (!armed) {
        setArmed(true);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => setArmed(false), ms);
        return;
      }
      disarm();
      action();
    },
    [armed, ms, disarm],
  );
  return { armed, fire, disarm, ms };
}

/** A button that arms on first click (vermilion + draining bar) and acts on the second. */
export function ArmedButton({
  onConfirm,
  armedLabel = "Click again to delete",
  kind = "secondary",
  size = "md",
  iconOnly = false,
  icon,
  className = "",
  children,
  disabled,
  title,
  ms = 3000,
}: {
  onConfirm: () => void;
  armedLabel?: string;
  kind?: BtnKind;
  size?: BtnSize;
  iconOnly?: boolean;
  icon?: ReactNode;
  className?: string;
  children?: ReactNode;
  disabled?: boolean;
  title?: string;
  ms?: number;
}) {
  const { armed, fire } = useArmed(ms);
  const armedCls = "!bg-vermilion !text-white hover:!bg-vermilion-deep !shadow-none overflow-hidden";
  return (
    <button
      type="button"
      disabled={disabled}
      title={armed ? armedLabel : title}
      aria-label={armed ? armedLabel : title}
      onClick={(e) => {
        e.preventDefault();
        e.stopPropagation();
        fire(onConfirm);
      }}
      className={btnClass(kind, `${armed ? armedCls : ""} ${className}`, size, iconOnly && !armed)}
    >
      {iconOnly && !armed ? icon : null}
      {!iconOnly || armed ? <span>{armed ? armedLabel : children}</span> : null}
      {icon && !iconOnly && !armed ? <span className="flex shrink-0 items-center">{icon}</span> : null}
      {armed ? (
        <span aria-hidden className="drain absolute inset-x-0 bottom-0 h-[3px] bg-white" style={{ animationDuration: `${ms}ms` }} />
      ) : null}
    </button>
  );
}

/** Copy icon that morphs into an emerald check for 1.5s. */
export function CopyButton({
  text,
  label,
  title = "Copy",
  className = "",
  size = 14,
}: {
  text: string | (() => string);
  label?: boolean;
  title?: string;
  className?: string;
  size?: number;
}) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const copy = async (e: MouseEvent) => {
    e.preventDefault();
    e.stopPropagation();
    try {
      await navigator.clipboard.writeText(typeof text === "function" ? text() : text);
      setCopied(true);
      clearTimeout(timer.current);
      timer.current = setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard unavailable */
    }
  };
  const morph = "col-start-1 row-start-1 transition-[opacity,transform] duration-[240ms] ease-settle motion-reduce:transition-opacity";
  return (
    <button
      type="button"
      onClick={copy}
      title={copied ? "Copied" : title}
      aria-label={copied ? "Copied" : title}
      className={`inline-flex items-center gap-1.5 rounded-sm text-ink-2 transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] ${
        label ? "h-7 px-2 text-[12px] font-medium" : "h-7 w-7 justify-center"
      } ${className}`}
    >
      <span className="grid place-items-center">
        <Copy
          size={size}
          className={`${morph} ${copied ? "scale-50 rotate-[30deg] opacity-0" : "opacity-100"}`}
        />
        <Check
          size={size}
          className={`${morph} text-emerald ${copied ? "opacity-100" : "scale-50 -rotate-[30deg] opacity-0"}`}
        />
      </span>
      {label ? <span>{copied ? "Copied" : "Copy"}</span> : null}
    </button>
  );
}

export type SaveState = "idle" | "saving" | "saved";

/** Runs a save and walks the button through Saving… → Saved → Save. Errors rethrow. */
export function useSave(savedMs = 1700) {
  const [state, setState] = useState<SaveState>("idle");
  const timer = useRef<ReturnType<typeof setTimeout> | undefined>(undefined);
  useEffect(() => () => clearTimeout(timer.current), []);
  const run = useCallback(
    async <T,>(fn: () => Promise<T>): Promise<T> => {
      clearTimeout(timer.current);
      setState("saving");
      try {
        const out = await fn();
        setState("saved");
        timer.current = setTimeout(() => setState("idle"), savedMs);
        return out;
      } catch (e) {
        setState("idle");
        throw e;
      }
    },
    [savedMs],
  );
  return { state, run };
}

export function Spinner({ size = 14, className = "", tone = "ink" }: { size?: number; className?: string; tone?: "ink" | "white" }) {
  return (
    <span
      aria-hidden
      className={`spin inline-block shrink-0 rounded-full ${className}`}
      style={{
        width: size,
        height: size,
        border: `1.5px solid ${tone === "white" ? "rgb(255 255 255 / 0.3)" : "var(--color-pressed)"}`,
        borderTopColor: tone === "white" ? "#fff" : "var(--color-ink)",
      }}
    />
  );
}

/** Save → Saving… → Saved ✓ → Save. The button itself is the feedback. */
export function SaveButton({
  state,
  kind = "primary",
  size = "md",
  children = "Save",
  savingLabel = "Saving…",
  savedLabel = "Saved",
  className = "",
  disabled,
  ...rest
}: Omit<ButtonHTMLAttributes<HTMLButtonElement>, "children"> & {
  state: SaveState;
  kind?: BtnKind;
  size?: BtnSize;
  children?: ReactNode;
  savingLabel?: string;
  savedLabel?: string;
}) {
  return (
    <button
      {...rest}
      disabled={disabled || state === "saving"}
      aria-live="polite"
      className={btnClass(kind, `${state === "saving" ? "disabled:!opacity-100" : ""} ${className}`, size)}
    >
      {state === "saving" ? <Spinner size={13} tone={kind === "primary" ? "white" : "ink"} /> : null}
      {state === "saved" ? <Check key="saved" size={14} className="pop" /> : null}
      <span>{state === "saving" ? savingLabel : state === "saved" ? savedLabel : children}</span>
    </button>
  );
}
