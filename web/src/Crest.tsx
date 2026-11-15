import { ChevronDown } from "lucide-react";
import { useEffect, useRef, useState, type CSSProperties } from "react";
import { createPortal } from "react-dom";
import { HUES } from "./crestArt.ts";
import { msg, t } from "./i18n.ts";
import { BADGE, HUE_COUNT, packCrest, scene, SHAPE_COUNT, TICKS_WIDTH, unpackCrest, type Prim, type State } from "./stack.ts";
import { useDismiss } from "./useMenu";

export { HUE_COUNT, packCrest, SHAPE_COUNT, unpackCrest };

// A crest is a Bot's face: a Stack, one of eight shapes in one of eight hues,
// packed into `crest` as hue * 8 + shape. Its geometry is stack.ts and its
// colors crestArt.ts (generated from brand/tokens.json); this file draws it
// and moves it. Every piece is its own element so a change of state slides
// each one to its new place (the motion is in index.css, under "The Stack").
// A crest carries its own ground, so it is the same in both themes; the gray
// tile takes an edge in the dark, where it would sink into the page.

// The names are in crestArt.ts's order.
const SHAPE_NAMES = [msg("wobble"), msg("duo"), msg("pyramid"), msg("tower"), msg("blocks"), msg("hourglass"), msg("tilt"), msg("coins")] as const;
const HUE_NAMES = [msg("green"), msg("pink"), msg("amber"), msg("blue"), msg("gray"), msg("violet"), msg("sky"), msg("lime")] as const;
const STATE_WORDS: Record<State, string> = {
  idle: msg("idle"),
  listening: msg("listening"),
  working: msg("working"),
  needs_you: msg("needs you"),
  done: msg("done"),
  asleep: msg("asleep"),
};

function Mark({ p }: { p: Prim }) {
  if (p.t === "circle") return <circle cx={p.cx} cy={p.cy} r={p.r} fill={p.fill} />;
  return <path d={p.d} fill="none" stroke={p.stroke} strokeWidth={p.width} strokeLinecap="round" />;
}

/**
 * A Bot's crest. `state` is what the Bot is doing (statusLook's `crestState`);
 * under 20px only the resting face is drawn, and under 48 the badge is the
 * caller's to overlay (a Lamp on the top right corner). With a `name` it is an
 * image called "Name, state"; without one it is decoration beside that name.
 */
export function Crest({ index, size, state = "idle", name }: { index: number; size: number; state?: State; name?: string }) {
  const { shape, hue } = unpackCrest(index);
  const sc = scene({ shape, hue, state, size });
  // Each crest blinks to its own clock, every 4 to 7 seconds.
  const [clock] = useState(() => ({ every: 4 + Math.random() * 3, at: Math.random() * 7 }));
  // With reduced motion a change of state is a short fade, restarted by flipping its name.
  const [seen, setSeen] = useState({ state: sc.state, n: 0 });
  if (seen.state !== sc.state) setSeen({ state: sc.state, n: seen.n + 1 });
  const [x, y, w, h] = sc.viewBox;
  const hd = sc.head;
  const vars = { "--crest-blink": `${clock.every.toFixed(2)}s`, "--crest-blink-at": `${clock.at.toFixed(2)}s`, "--crest-slide": sc.tight ? "12px" : "30px" } as CSSProperties;
  const label = name ? { role: "img", "aria-label": `${name}, ${t(STATE_WORDS[sc.state])}` } : { "aria-hidden": true };
  return (
    <svg
      width={size}
      height={size}
      viewBox={sc.viewBox.join(" ")}
      className="crest shrink-0"
      data-state={sc.state}
      data-quiet={sc.detail === "tiny" ? "" : undefined}
      data-fade={seen.n ? seen.n % 2 : undefined}
      style={vars}
      {...label}
    >
      <rect
        x={x}
        y={y}
        width={w}
        height={h}
        rx={w * 0.24}
        fill={sc.tile}
        {...(HUES[hue].name === "gray" ? { style: { stroke: "var(--crest-edge)" }, strokeWidth: 2, vectorEffect: "non-scaling-stroke" } : {})}
      />
      {sc.incoming ? (
        <rect
          className="crest-incoming"
          x={sc.incoming.x}
          y={sc.incoming.y}
          width={sc.incoming.w}
          height={sc.incoming.h}
          rx={sc.incoming.r}
          fill="none"
          stroke={sc.incoming.stroke}
          strokeWidth={sc.incoming.width}
          strokeDasharray="5 4"
        />
      ) : null}
      {sc.layers.map((l, i) => (
        <rect key={i} className="crest-piece crest-sway" style={{ "--i": i + 1 } as CSSProperties} x={l.x} y={l.y} width={l.w} height={l.h} rx={l.r} fill={l.fill} opacity={l.opacity} />
      ))}
      {sc.ticks ? <path className="crest-ticks" d={sc.ticks.d} fill="none" stroke={sc.ticks.stroke} strokeWidth={TICKS_WIDTH} strokeLinecap="round" /> : null}
      {/* The head is drawn around its own centre, so one transform both places and tilts it. */}
      <g className="crest-head" style={{ transform: `translate(${hd.cx}px, ${hd.cy}px) rotate(${hd.rot}deg)` }}>
        <g className="crest-bob crest-sway" style={{ "--i": 0 } as CSSProperties}>
          <rect className="crest-piece" x={-hd.w / 2} y={-hd.h / 2} width={hd.w} height={hd.h} rx={hd.r} fill={hd.fill} />
          <g key={sc.eyes} className="crest-eyes">
            {sc.face(0, 0).map((p, i) => (
              <Mark key={i} p={p} />
            ))}
          </g>
        </g>
      </g>
      {sc.zz ? (
        <g transform={sc.zz.shifted ? "translate(-7 3)" : undefined}>
          <path className="crest-zz" d={sc.zz.d} fill="none" stroke={sc.zz.stroke} strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
        </g>
      ) : null}
      {sc.badge ? (
        <g className="crest-badge">
          <circle cx={BADGE.cx} cy={BADGE.cy} r={BADGE.r} fill={BADGE.fill} />
          {sc.badge === "alert" ? (
            <>
              <path d={BADGE.alert} stroke={sc.ink} strokeWidth={2.4} strokeLinecap="round" />
              <circle cx={BADGE.dot.cx} cy={BADGE.dot.cy} r={BADGE.dot.r} fill={sc.ink} />
            </>
          ) : (
            <path className="crest-check" d={BADGE.check} fill="none" stroke={sc.ink} strokeWidth={2.2} strokeLinecap="round" strokeLinejoin="round" />
          )}
        </g>
      ) : null}
    </svg>
  );
}

export function CrestPicker({ value, onChange, bare = false }: { value: number; onChange: (n: number) => void; bare?: boolean }) {
  const { shape, hue } = unpackCrest(value);
  return (
    <div className={bare ? "p-3" : "rounded-card bg-surface p-4 shadow-card"}>
      <div className="mb-2 text-section text-ink-3">{t("Shape")}</div>
      <div className={`grid grid-cols-4 gap-1.5 ${bare ? "mb-3" : "mb-4"}`}>
        {SHAPE_NAMES.map((name, i) => {
          const on = i === shape;
          return (
            <button
              key={name}
              type="button"
              title={t(name)}
              aria-label={t(name)}
              aria-pressed={on}
              onClick={() => onChange(packCrest(i, hue))}
              className={`blink flex items-center justify-center rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${bare ? "h-12" : "h-14"} ${
                on ? "bg-cobalt-pale shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "hover:bg-well"
              }`}
            >
              <Crest index={packCrest(i, hue)} size={bare ? 36 : 40} />
            </button>
          );
        })}
      </div>
      <div className="mb-2 text-section text-ink-3">{t("Color")}</div>
      <div className={bare ? "grid grid-cols-8 justify-items-center px-0.5" : "flex flex-wrap gap-2 px-0.5"}>
        {HUE_NAMES.map((name, i) => {
          const on = i === hue;
          return (
            <button
              key={name}
              type="button"
              title={t(name)}
              aria-label={t(name)}
              aria-pressed={on}
              onClick={() => onChange(packCrest(shape, i))}
              className={`flex items-center justify-center rounded-full transition-[box-shadow,transform] duration-[200ms] ease-quiet active:scale-[.94] active:duration-[70ms] ${bare ? "h-6 w-6" : "h-7 w-7"} ${
                on
                  ? "shadow-[0_0_0_2px_var(--color-surface),0_0_0_4px_var(--color-cobalt)]"
                  : "shadow-[inset_0_0_0_1px_var(--crest-edge)]"
              }`}
              style={{ background: HUES[i].ground }}
            >
              {/* The ground is the swatch; the dot says which hue the dark ground belongs to. */}
              <span aria-hidden className="h-2.5 w-2.5 rounded-full" style={{ background: HUES[i].b }} />
            </button>
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
