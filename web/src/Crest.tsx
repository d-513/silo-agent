import { ChevronDown } from "lucide-react";
import { useEffect, useId, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { BEARD, BOOTS, COAT, FELT, GRADIENTS, HATS, MITTENS, NOSE, PAINT, STOPS, type Part } from "./crestArt.ts";
import { msg, t } from "./i18n.ts";
import { useDismiss } from "./useMenu";

// The order is packed into `crest` as color * 8 + shape. Never reorder, only
// restyle. `detail` follows DESIGN.md: ink on the two light fills, canvas on
// the rest; mist also takes a line-strong outline. A crest is the Bot's own
// skrzat, so its colors are the same in both themes (the light palette's ink
// and canvas); the ink crest gains an edge in the dark, where it would
// otherwise sink into the ground the way mist does on paper. Its shapes and
// shades are in crestArt.ts, generated from brand/.
export const CREST_FILLS = [
  { name: msg("mist"), fill: "#F3F2EF", detail: "ink", outline: true },
  { name: msg("brown"), fill: "#8B5A3C", detail: "canvas" },
  { name: msg("wine"), fill: "#9F1239", detail: "canvas" },
  { name: msg("orange"), fill: "#EA580C", detail: "canvas" },
  { name: msg("yellow"), fill: "#E0AE1C", detail: "ink" },
  { name: msg("green"), fill: "#16A34A", detail: "canvas" },
  { name: msg("emerald"), fill: "#0F7A55", detail: "canvas" },
  { name: msg("cobalt"), fill: "#2B4FC7", detail: "canvas" },
  { name: msg("purple"), fill: "#6D28D9", detail: "canvas" },
  { name: msg("pink"), fill: "#DB2777", detail: "canvas" },
  { name: msg("graphite"), fill: "#55534E", detail: "canvas" },
  { name: msg("ink"), fill: "#1A1917", detail: "canvas", outline: "dark" },
] as const satisfies readonly { name: string; fill: string; detail: "ink" | "canvas"; outline?: true | "dark" }[];

const DETAIL = { ink: "#1A1917", canvas: "#FAF9F7" } as const;

export const CREST_COLORS = CREST_FILLS.map((c) => c.fill);

export const SHAPE_COUNT = 8;
export const COLOR_COUNT = CREST_COLORS.length;

const NAMES = [msg("bobble hat"), msg("bent tip"), msg("wide brim"), msg("paper boat"), msg("cone"), msg("striped cone"), msg("baker's toque"), msg("stocking cap")] as const;

export function packCrest(shape: number, color: number) {
  const s = ((shape % SHAPE_COUNT) + SHAPE_COUNT) % SHAPE_COUNT;
  const c = ((color % COLOR_COUNT) + COLOR_COUNT) % COLOR_COUNT;
  return c * SHAPE_COUNT + s;
}

export function unpackCrest(index: number) {
  const n = SHAPE_COUNT * COLOR_COUNT;
  const i = ((index % n) + n) % n;
  return { shape: i % SHAPE_COUNT, color: Math.floor(i / SHAPE_COUNT) };
}

function Shape({ part, detail }: { part: Part; detail: string }) {
  if (part.t === "circle") return <circle cx={part.cx} cy={part.cy} r={part.r} />;
  if (part.t === "ellipse") return <ellipse cx={part.cx} cy={part.cy} rx={part.rx} ry={part.ry} />;
  if (part.detail) return <path d={part.d} fill="none" stroke={detail} strokeWidth={part.detail} strokeLinecap="round" />;
  return <path d={part.d} />;
}

function Gradient({ id, at, stops }: { id: string; at: { cx: string; cy: string; r: string }; stops: readonly string[] }) {
  return (
    <radialGradient id={id} cx={at.cx} cy={at.cy} r={at.r}>
      <stop offset="0" stopColor={stops[0]} />
      <stop offset=".5" stopColor={stops[1]} />
      <stop offset="1" stopColor={stops[2]} />
    </radialGradient>
  );
}

export function Crest({ index, size }: { index: number; size: number }) {
  const { shape, color } = unpackCrest(index);
  const c: { detail: "ink" | "canvas"; outline?: true | "dark" } = CREST_FILLS[color];
  // A page holds many crests, each with its own colors, so the ids are its own.
  const id = useId().replace(/[^a-zA-Z0-9]/g, "");
  const url = (name: string) => `url(#${id}${name})`;
  const stroke = c.outline === "dark" ? "var(--crest-edge)" : c.outline ? "var(--color-line-strong)" : undefined;
  const edge = stroke ? { style: { stroke }, strokeWidth: 0.8 } : {};
  const detail = DETAIL[c.detail];
  return (
    <svg width={size} height={size} viewBox="0 0 48 48" className="shrink-0 overflow-visible" aria-hidden>
      <defs>
        <Gradient id={`${id}hat`} at={GRADIENTS["hat-felt"]} stops={PAINT[color].hat} />
        <Gradient id={`${id}coat`} at={GRADIENTS["coat-felt"]} stops={PAINT[color].coat} />
        <Gradient id={`${id}beard`} at={GRADIENTS["beard-wool"]} stops={STOPS["beard-wool"]} />
        <Gradient id={`${id}nose`} at={GRADIENTS["nose-felt"]} stops={STOPS["nose-felt"]} />
        <Gradient id={`${id}boot`} at={GRADIENTS["boot-felt"]} stops={STOPS["boot-felt"]} />
        <filter id={`${id}felt`} x="-8%" y="-8%" width="116%" height="116%" colorInterpolationFilters="sRGB">
          <feTurbulence type="fractalNoise" baseFrequency={FELT.frequency} numOctaves={2} seed={2} />
          <feDisplacementMap in="SourceGraphic" scale={FELT.scale} xChannelSelector="R" yChannelSelector="G" />
        </filter>
      </defs>
      <g fill={url("boot")}>
        {BOOTS.map((p, i) => (
          <Shape key={i} part={p} detail={detail} />
        ))}
      </g>
      <g filter={url("felt")}>
        <path d={COAT} fill={url("coat")} {...edge} />
        <g fill={url("hat")} {...edge}>
          {MITTENS.map((p, i) => (
            <Shape key={i} part={p} detail={detail} />
          ))}
        </g>
        <path d={BEARD} fill={url("beard")} />
        <g className="crest-hat" fill={url("hat")} {...edge}>
          {HATS[shape].map((p, i) => (
            <Shape key={i} part={p} detail={detail} />
          ))}
        </g>
      </g>
      <circle cx={NOSE.cx} cy={NOSE.cy} r={NOSE.r} fill={url("nose")} />
    </svg>
  );
}

export function CrestPicker({ value, onChange, bare = false }: { value: number; onChange: (n: number) => void; bare?: boolean }) {
  const { shape, color } = unpackCrest(value);
  return (
    <div className={bare ? "p-3" : "rounded-card bg-surface p-4 shadow-card"}>
      <div className="mb-2 text-label-caps text-ink-3 uppercase">{t("Hat")}</div>
      <div className={`grid grid-cols-4 gap-1.5 ${bare ? "mb-3" : "mb-4"}`}>
        {NAMES.map((name, i) => {
          const on = i === shape;
          return (
            <button
              key={name}
              type="button"
              title={t(name)}
              aria-label={t(name)}
              aria-pressed={on}
              onClick={() => onChange(packCrest(i, color))}
              className={`blink flex items-center justify-center rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${bare ? "h-12" : "h-14"} ${
                on ? "bg-cobalt-pale shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "hover:bg-well"
              }`}
            >
              <Crest index={packCrest(i, color)} size={bare ? 36 : 40} />
            </button>
          );
        })}
      </div>
      <div className="mb-2 text-label-caps text-ink-3 uppercase">{t("Color")}</div>
      <div className={bare ? "grid grid-cols-6 justify-items-center gap-y-2.5 px-0.5" : "flex flex-wrap gap-2 px-0.5"}>
        {CREST_FILLS.map(({ name, fill: hex }, i) => {
          const on = i === color;
          return (
            <button
              key={name}
              type="button"
              title={t(name)}
              aria-label={t(name)}
              aria-pressed={on}
              onClick={() => onChange(packCrest(shape, i))}
              className={`rounded-full transition-[box-shadow,transform] duration-[200ms] ease-quiet active:scale-[.94] active:duration-[70ms] ${bare ? "h-6 w-6" : "h-7 w-7"} ${
                on
                  ? "shadow-[0_0_0_2px_var(--color-surface),0_0_0_4px_var(--color-cobalt)]"
                  : "shadow-[inset_0_0_0_1px_var(--color-line-strong)]"
              }`}
              style={{ background: hex }}
            />
          );
        })}
      </div>
    </div>
  );
}

// A select whose value is the Bot's crest: it sits beside a field the height of
// an input and opens the picker in a slip.
export function CrestSelect({ value, onChange }: { value: number; onChange: (n: number) => void }) {
  const [open, setOpen] = useState(false);
  const trigger = useRef<HTMLButtonElement>(null);
  const menu = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState<CSSProperties>({});
  useDismiss(open, trigger, menu, () => setOpen(false));
  useEffect(() => {
    if (!open) return;
    const r = trigger.current?.getBoundingClientRect();
    if (r) setBox({ left: Math.min(Math.max(8, r.left), window.innerWidth - 300), top: Math.min(r.bottom + 6, window.innerHeight - 290) });
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== "Escape") return;
      setOpen(false);
      trigger.current?.focus();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open]);
  return (
    <>
      <button
        ref={trigger}
        type="button"
        title={t("Change crest")}
        aria-label={t("Change crest")}
        aria-haspopup="dialog"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="blink flex h-9 shrink-0 items-center gap-1 rounded-control bg-surface pr-1.5 pl-1 text-ink-3 shadow-[inset_0_0_0_1px_var(--color-line-strong)] transition-[background-color,box-shadow] duration-[160ms] ease-quiet hover:bg-well aria-expanded:bg-well"
      >
        <Crest index={value} size={30} />
        <ChevronDown size={13} />
      </button>
      {open
        ? createPortal(
            <div ref={menu} role="dialog" aria-label={t("Change crest")} className="fixed z-50 w-[292px] rounded-control bg-surface shadow-slip" style={box}>
              <div className="rise">
                <CrestPicker value={value} onChange={onChange} bare />
              </div>
            </div>,
            document.body,
          )
        : null}
    </>
  );
}
