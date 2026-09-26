// The order is packed into `crest` as color * 8 + shape. Never reorder, only
// restyle. `eyes` follows DESIGN.md: ink on the two light fills, canvas on
// the rest; mist also takes a line-strong outline.
export const CREST_FILLS = [
  { name: "mist", fill: "#F3F2EF", eyes: "ink", outline: true },
  { name: "brown", fill: "#8B5A3C", eyes: "canvas" },
  { name: "wine", fill: "#9F1239", eyes: "canvas" },
  { name: "orange", fill: "#EA580C", eyes: "canvas" },
  { name: "yellow", fill: "#E0AE1C", eyes: "ink" },
  { name: "green", fill: "#16A34A", eyes: "canvas" },
  { name: "emerald", fill: "#0F7A55", eyes: "canvas" },
  { name: "cobalt", fill: "#2B4FC7", eyes: "canvas" },
  { name: "purple", fill: "#6D28D9", eyes: "canvas" },
  { name: "pink", fill: "#DB2777", eyes: "canvas" },
  { name: "graphite", fill: "#55534E", eyes: "canvas" },
  { name: "ink", fill: "#1A1917", eyes: "canvas" },
] as const satisfies readonly { name: string; fill: string; eyes: "ink" | "canvas"; outline?: boolean }[];

export const CREST_COLORS = CREST_FILLS.map((c) => c.fill);

export const SHAPE_COUNT = 8;
export const COLOR_COUNT = CREST_COLORS.length;

const NAMES = ["circle", "blob", "squircle", "pill", "triangle", "hex", "cloud", "drop"] as const;

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

function Eyes({ cx, cy, fill, spread = 4.6 }: { cx: number; cy: number; fill: string; spread?: number }) {
  return (
    <g className="crest-eyes" style={{ fill }}>
      <circle cx={cx - spread} cy={cy} r="1.65" />
      <circle cx={cx + spread} cy={cy} r="1.65" />
    </g>
  );
}

function body(fill: string, stroke?: string) {
  return { fill, style: stroke ? { stroke } : undefined, strokeWidth: stroke ? 1.4 : 0 };
}

function ShapeMark({ shape, fill, eyes, stroke }: { shape: number; fill: string; eyes: string; stroke?: string }) {
  const b = body(fill, stroke);
  const E = (p: { cx: number; cy: number; spread?: number }) => <Eyes {...p} fill={eyes} />;
  switch (shape) {
    case 1:
      return (
        <g>
          <path
            {...b}
            d="M24.2 6.4c6.8.2 13.6 5.2 14.6 12.2 1 6.6-2.4 10.8-4.2 14.4-2.4 4.8-7.2 8.2-12.8 8.4-6.4.2-11.8-3.2-14.2-8.8C5.2 27.2 6.6 21.4 8.4 16.6 10.6 10.6 17.2 6.2 24.2 6.4Z"
          />
          <E cx={23.2} cy={20} />
        </g>
      );
    case 2:
      return (
        <g>
          <rect x="8" y="8" width="32" height="32" rx="11" {...b} />
          <E cx={24} cy={21} />
        </g>
      );
    case 3:
      return (
        <g>
          <rect x="5" y="15" width="38" height="18" rx="9" {...b} />
          <E cx={24} cy={24} spread={6.2} />
        </g>
      );
    case 4:
      return (
        <g>
          <path {...b} d="M24 6.5c1.4 0 15.8 26.2 15.8 29.2 0 5.2-6.8 7.8-15.8 7.8S8.2 40.9 8.2 35.7C8.2 32.7 22.6 6.5 24 6.5Z" />
          <E cx={24} cy={26.5} />
        </g>
      );
    case 5:
      return (
        <g>
          <path {...b} d="M24 6.5 39.2 15.4v17.2L24 41.5 8.8 32.6V15.4Z" />
          <E cx={24} cy={22} />
        </g>
      );
    case 6:
      return (
        <g>
          <path
            {...b}
            d="M16.2 20.2a8.2 8.2 0 0 1 7.6-6.6 9 9 0 0 1 8.6 6.2 8 8 0 0 1 8.4 7.8c0 5.4-4.6 9.4-10.6 9.4H16.8c-5.6 0-9.8-3.8-9.8-8.8 0-4.2 2.8-7.6 9.2-8Z"
          />
          <E cx={24} cy={24.5} />
        </g>
      );
    case 7:
      return (
        <g>
          <path {...b} d="M24 6.8c7.6 8.2 15.4 16.2 15.4 24.2 0 7.2-6.4 11.6-15.4 11.6S8.6 38.2 8.6 31c0-8 7.8-16 15.4-24.2Z" />
          <E cx={24} cy={27} />
        </g>
      );
    default:
      return (
        <g>
          <circle cx="24" cy="24" r="16.5" {...b} />
          <E cx={24} cy={21} />
        </g>
      );
  }
}

export function Crest({ index, size }: { index: number; size: number }) {
  const { shape, color } = unpackCrest(index);
  const c: { fill: string; eyes: "ink" | "canvas"; outline?: boolean } = CREST_FILLS[color];
  return (
    <svg width={size} height={size} viewBox="0 0 48 48" className="shrink-0 overflow-visible" aria-hidden>
      <ShapeMark
        shape={shape}
        fill={c.fill}
        eyes={`var(--color-${c.eyes})`}
        stroke={c.outline ? "var(--color-line-strong)" : undefined}
      />
    </svg>
  );
}

export function CrestPicker({ value, onChange }: { value: number; onChange: (n: number) => void }) {
  const { shape, color } = unpackCrest(value);
  return (
    <div className="rounded-card bg-surface p-4 shadow-card">
      <div className="mb-2 text-[11px] font-medium tracking-[0.08em] text-ink-3 uppercase">Shape</div>
      <div className="mb-4 grid grid-cols-4 gap-1.5">
        {NAMES.map((name, i) => {
          const on = i === shape;
          return (
            <button
              key={name}
              type="button"
              title={name}
              aria-label={name}
              aria-pressed={on}
              onClick={() => onChange(packCrest(i, color))}
              className={`blink flex h-14 items-center justify-center rounded-control transition-[background-color,box-shadow] duration-[160ms] ease-quiet ${
                on ? "bg-cobalt-pale shadow-[inset_0_0_0_1px_var(--color-cobalt)]" : "hover:bg-well"
              }`}
            >
              <Crest index={packCrest(i, color)} size={40} />
            </button>
          );
        })}
      </div>
      <div className="mb-2 text-[11px] font-medium tracking-[0.08em] text-ink-3 uppercase">Color</div>
      <div className="flex flex-wrap gap-2 px-0.5">
        {CREST_FILLS.map(({ name, fill: hex }, i) => {
          const on = i === color;
          return (
            <button
              key={name}
              type="button"
              title={name}
              aria-label={`color ${i + 1}`}
              aria-pressed={on}
              onClick={() => onChange(packCrest(shape, i))}
              className={`h-7 w-7 rounded-full transition-[box-shadow,transform] duration-[200ms] ease-quiet active:scale-[.94] active:duration-[70ms] ${
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
