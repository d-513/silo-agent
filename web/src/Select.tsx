import { CaretDown, Check } from "@phosphor-icons/react";
import { useEffect, useRef, useState, type CSSProperties, type KeyboardEvent } from "react";
import { createPortal } from "react-dom";

export interface SelectOption {
  value: string;
  label: string;
  hint?: string;
  disabled?: boolean;
}

type Variant = "field" | "ghost";

const fieldTrigger =
  "flex h-9 w-full items-center justify-between gap-2 rounded-[6px] border border-thread bg-folio px-3 text-left text-[14px] text-iron outline-none transition-colors hover:border-hover focus-visible:border-bindery disabled:cursor-not-allowed disabled:bg-cloth disabled:text-stone";

const ghostTrigger =
  "inline-flex max-w-full items-center gap-1.5 rounded-[6px] bg-transparent py-1 pl-2.5 pr-2 text-[12px] font-medium text-stone outline-none transition-colors duration-150 hover:bg-cloth hover:text-iron focus-visible:bg-cloth focus-visible:text-iron disabled:cursor-not-allowed disabled:opacity-40";

export function Select({
  value,
  onChange,
  options,
  placeholder = "Select…",
  emptyLabel = "No options",
  disabled = false,
  variant = "field",
  className = "",
  menuClassName = "",
  title,
  ariaLabel,
}: {
  value: string;
  onChange: (v: string) => void;
  options: SelectOption[];
  placeholder?: string;
  emptyLabel?: string;
  disabled?: boolean;
  variant?: Variant;
  className?: string;
  menuClassName?: string;
  title?: string;
  ariaLabel?: string;
}) {
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);
  const triggerRef = useRef<HTMLButtonElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<{ top: number; left: number; width: number; up: boolean }>({
    top: 0,
    left: 0,
    width: 0,
    up: false,
  });

  const current = options.find((o) => o.value === value);
  const label = current?.label ?? (value || placeholder);

  function place() {
    const el = triggerRef.current;
    if (!el) return;
    const r = el.getBoundingClientRect();
    const gap = 6;
    const below = window.innerHeight - r.bottom;
    const above = r.top;
    const up = below < Math.min(options.length * 36 + 16, 220) && above > below;
    const width = Math.max(r.width, variant === "field" ? r.width : 280);
    const left = Math.min(Math.max(8, r.left), Math.max(8, window.innerWidth - width - 8));
    setBox({ top: up ? r.top - gap : r.bottom + gap, left, width, up });
  }

  useEffect(() => {
    if (!open) return;
    place();
    const i = options.findIndex((o) => o.value === value);
    setActive(i >= 0 ? i : 0);
    const close = () => setOpen(false);
    const onDoc = (e: MouseEvent) => {
      const t = e.target;
      if (!(t instanceof Node)) return;
      if (triggerRef.current?.contains(t) || menuRef.current?.contains(t)) return;
      close();
    };
    const onScroll = (e: Event) => {
      const t = e.target;
      if (menuRef.current && t instanceof Node && menuRef.current.contains(t)) return;
      close();
    };
    document.addEventListener("mousedown", onDoc);
    window.addEventListener("scroll", onScroll, true);
    window.addEventListener("resize", close);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      window.removeEventListener("scroll", onScroll, true);
      window.removeEventListener("resize", close);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const items = menuRef.current?.querySelectorAll<HTMLElement>("[data-option]");
    items?.[active]?.scrollIntoView({ block: "nearest" });
  }, [active, open]);

  function pick(o: SelectOption) {
    if (o.disabled) return;
    onChange(o.value);
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
    } else if (e.key === "Home") {
      e.preventDefault();
      setActive(0);
    } else if (e.key === "End") {
      e.preventDefault();
      setActive(options.length - 1);
    } else if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      const o = options[active];
      if (o) pick(o);
    } else if (e.key === "Escape" || e.key === "Tab") {
      setOpen(false);
    }
  }

  const style: CSSProperties = {
    top: box.top,
    left: box.left,
    width: box.width,
    transform: box.up ? "translateY(-100%)" : undefined,
  };

  return (
    <>
      <button
        ref={triggerRef}
        type="button"
        role="combobox"
        aria-haspopup="listbox"
        aria-expanded={open}
        aria-label={ariaLabel}
        title={title}
        disabled={disabled}
        className={`${variant === "field" ? fieldTrigger : ghostTrigger} ${className}`}
        onClick={() => setOpen((v) => !v)}
        onKeyDown={onKeyDown}
      >
        <span className={`min-w-0 flex-1 truncate ${!current && !value ? "text-stone/70" : ""}`}>{label}</span>
        <CaretDown size={variant === "ghost" ? 11 : 14} weight="bold" className="shrink-0 text-stone" />
      </button>
      {open
        ? createPortal(
            <div
              ref={menuRef}
              role="listbox"
              className={`fixed z-50 rounded-[10px] border border-thread bg-folio p-1 shadow-[0_12px_34px_-10px_rgba(30,33,38,0.28),0_2px_6px_rgba(30,33,38,0.06)] ${menuClassName}`}
              style={style}
            >
              {/* The entrance animation lives on the inner box: an animated
                  transform on the positioned element would override the
                  translateY(-100%) that flips an upward menu into place. */}
              <div className={`silo-enter max-h-[280px] overflow-y-auto overscroll-contain ${variant === "ghost" ? "text-[12px]" : ""}`}>
                {options.length === 0 ? (
                  <div className="px-2.5 py-2 text-[13px] text-stone">{emptyLabel}</div>
                ) : (
                  options.map((o, i) => {
                    const selected = o.value === value;
                    const compact = variant === "ghost";
                    return (
                      <button
                        key={o.value}
                        data-option
                        type="button"
                        role="option"
                        aria-selected={selected}
                        disabled={o.disabled}
                        onMouseEnter={() => !o.disabled && setActive(i)}
                        onClick={() => pick(o)}
                        className={`flex w-full items-center gap-2 rounded-md text-left transition-colors ${
                          compact ? "px-2 py-1" : "px-2.5 py-1.5"
                        } ${
                          o.disabled
                            ? "cursor-not-allowed text-stone/50"
                            : selected
                              ? "bg-bindery-pale text-iron"
                              : i === active
                                ? "bg-cloth text-iron"
                                : "text-iron hover:bg-cloth"
                        }`}
                      >
                        <span className="min-w-0 flex-1">
                          <span className={`block truncate ${compact ? "text-[12px]" : "text-[13px]"}`}>{o.label}</span>
                          {o.hint ? <span className={`block truncate text-stone ${compact ? "text-[10px]" : "text-[11px]"}`}>{o.hint}</span> : null}
                        </span>
                        {selected ? <Check size={compact ? 12 : 14} weight="bold" className="shrink-0 text-bindery" /> : null}
                      </button>
                    );
                  })
                )}
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
