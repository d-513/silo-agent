import { Check, Lightbulb } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";
import { thinkingLabel } from "./thinking";
import { Tip, TipAction, TipTitle } from "./Tip";

// ThinkingPicker is the composer's thinking-level chip: a bulb (with the level
// beside it once one is chosen) that drops a short menu of the model's levels.
// The list is at most eight rows, so it never scrolls.
export function ThinkingPicker({
  value,
  levels,
  onChange,
  disabled,
}: {
  value: string;
  levels: readonly string[];
  onChange: (level: string) => void;
  disabled?: boolean;
}) {
  const options = ["", ...levels];
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<{ left: number; top: number; up: boolean }>({ left: 0, top: 0, up: true });

  useEffect(() => {
    if (!open) return;
    const r = triggerRef.current?.getBoundingClientRect();
    if (r) {
      // The composer sits at the bottom, so the menu opens upward when it fits.
      const height = options.length * 28 + 8;
      const up = r.top > height + 12 || r.top > window.innerHeight - r.bottom;
      setBox({ left: Math.min(Math.max(8, r.left), window.innerWidth - 148), top: up ? r.top - 6 : r.bottom + 6, up });
    }
    setActive(Math.max(0, options.indexOf(value)));
    const close = () => setOpen(false);
    const onDoc = (e: MouseEvent) => {
      const t = e.target;
      if (t instanceof Node && (triggerRef.current?.contains(t) || menuRef.current?.contains(t))) return;
      close();
    };
    document.addEventListener("mousedown", onDoc);
    window.addEventListener("scroll", close, true);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      window.removeEventListener("scroll", close, true);
      window.removeEventListener("resize", close);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  function pick(level: string) {
    onChange(level);
    setOpen(false);
    triggerRef.current?.focus();
  }

  function onKeyDown(e: KeyboardEvent<HTMLButtonElement>) {
    if (disabled) return;
    if (!open) {
      if (e.key === "ArrowDown" || e.key === "ArrowUp" || e.key === "Enter" || e.key === " ") {
        e.preventDefault();
        setOpen(true);
      }
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActive((i) => Math.min(i + 1, options.length - 1));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActive((i) => Math.max(i - 1, 0));
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      pick(options[active]);
    } else if (e.key === "Escape" || e.key === "Tab") {
      setOpen(false);
    }
  }

  const style: CSSProperties = { left: box.left, top: box.top, transform: box.up ? "translateY(-100%)" : undefined };
  const lit = value !== "" && value !== "off";

  return (
    <>
      <Tip
        content={
          <>
            <TipTitle aside={thinkingLabel(value)}>Thinking</TipTitle>
            <p className="mt-1">How hard the model reasons before it replies.</p>
            <TipAction>Click to change</TipAction>
          </>
        }
      >
        <button
          ref={triggerRef}
          type="button"
          aria-haspopup="listbox"
          aria-expanded={open}
          aria-label={`Thinking: ${thinkingLabel(value)}`}
          disabled={disabled}
          onClick={() => setOpen((v) => !v)}
          onKeyDown={onKeyDown}
          className={`flex h-[34px] min-w-[34px] shrink-0 items-center justify-center gap-1 rounded-control px-2 text-[12px] font-medium transition-[background-color,color,transform] duration-[160ms] ease-quiet hover:bg-well hover:text-ink active:scale-[.94] active:duration-[70ms] disabled:cursor-not-allowed disabled:opacity-40 aria-expanded:bg-well ${
            lit ? "text-ink" : "text-ink-3"
          }`}
        >
          <Lightbulb size={16} className="shrink-0" />
          {value ? <span className="whitespace-nowrap">{thinkingLabel(value)}</span> : null}
        </button>
      </Tip>
      {open
        ? createPortal(
            <div ref={menuRef} role="listbox" aria-label="Thinking level" className="fixed z-50 w-[140px] rounded-control bg-surface p-1 shadow-slip" style={style}>
              <div className="rise">
                {options.map((l, i) => {
                  const selected = l === value;
                  return (
                    <button
                      key={l || "default"}
                      type="button"
                      role="option"
                      aria-selected={selected}
                      onMouseEnter={() => setActive(i)}
                      onClick={() => pick(l)}
                      className={`flex h-7 w-full items-center gap-2 rounded-sm px-2 text-left text-[12.5px] transition-colors duration-[160ms] ${
                        selected ? "bg-cobalt-pale text-ink" : i === active ? "bg-well text-ink" : "text-ink hover:bg-well"
                      } ${l === "" ? "text-ink-2" : ""}`}
                    >
                      <span className="flex-1 truncate">{thinkingLabel(l)}</span>
                      {selected ? <Check size={12} className="shrink-0" /> : null}
                    </button>
                  );
                })}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
