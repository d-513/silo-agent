export const CREST_COLORS = [
  "#F3F0E8", // plaster
  "#7A4E2A", // brown
  "#A33B4A", // carmine
  "#E07A2F", // orange
  "#E4C04A", // yellow
  "#4A8F4A", // green
  "#3D6F6A", // pine
  "#2A3F5F", // bindery
  "#6B4C8A", // purple
  "#D47A8C", // pink
  "#5F5E58", // stone
  "#1E2126", // iron
] as const;

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

function luminance(hex: string) {
  const n = parseInt(hex.slice(1), 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return (r * 299 + g * 587 + b * 114) / 1000;
}

function Eyes({ cx, cy, fill, spread = 4.6 }: { cx: number; cy: number; fill: string; spread?: number }) {
  return (
    <g fill={fill}>
      <circle cx={cx - spread} cy={cy} r="1.65" />
      <circle cx={cx + spread} cy={cy} r="1.65" />
    </g>
  );
}

function body(fill: string, stroke?: string) {
  return { fill, stroke: stroke ?? "none", strokeWidth: stroke ? 1.4 : 0 };
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
  const fill = CREST_COLORS[color];
  const light = luminance(fill) > 140;
  const eyes = light ? "#1E2126" : "#F3F0E8";
  return (
    <svg width={size} height={size} viewBox="0 0 48 48" className="shrink-0" aria-hidden>
      <ShapeMark shape={shape} fill={fill} eyes={eyes} stroke={light ? "#C9C3B6" : undefined} />
    </svg>
  );
}

export function CrestPicker({ value, onChange }: { value: number; onChange: (n: number) => void }) {
  const { shape, color } = unpackCrest(value);
  return (
    <div className="rounded-[10px] border border-thread bg-folio p-3">
      <div className="mb-2 text-[11px] font-medium tracking-wide text-stone">Shape</div>
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
              className={`flex h-14 items-center justify-center rounded-lg outline-none transition-[background-color,box-shadow] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] ${
                on ? "bg-bindery-pale shadow-[inset_0_0_0_1px_#2A3F5F]" : "hover:bg-cloth"
              }`}
            >
              <Crest index={packCrest(i, color)} size={40} />
            </button>
          );
        })}
      </div>
      <div className="mb-2 text-[11px] font-medium tracking-wide text-stone">Color</div>
      <div className="flex flex-wrap gap-2 px-0.5">
        {CREST_COLORS.map((hex, i) => {
          const on = i === color;
          return (
            <button
              key={hex}
              type="button"
              title={hex}
              aria-label={`color ${i + 1}`}
              aria-pressed={on}
              onClick={() => onChange(packCrest(shape, i))}
              className={`h-7 w-7 rounded-full outline-none transition-[box-shadow,transform] duration-200 ease-[cubic-bezier(0.32,0.72,0,1)] active:scale-[0.96] ${
                on ? "shadow-[0_0_0_2px_#F3F0E8,0_0_0_4px_#2A3F5F]" : "shadow-[inset_0_0_0_1px_#C9C3B6]"
              }`}
              style={{ background: hex }}
            />
          );
        })}
      </div>
    </div>
  );
}
