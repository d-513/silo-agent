import { HUES, MONO, SHAPES } from "./crestArt.ts";

// The Stack: a Bot's face, a small head on a pile of rounded layers, drawn on
// a 100 x 100 grid. A Bot is one shape plus one hue; its state moves the
// pieces. This file is the geometry and nothing else: Crest.tsx draws it,
// brand/render.ts turns it into the icons, and iOS ports it
// (ios/SiloClient/Sources/SiloClient/Logic/Stack.swift: a change here is a
// change there). brand/reference holds the designer's drawings, and
// stack_test.ts holds renderStack to them byte for byte.

export const STATES = ["idle", "listening", "working", "needs_you", "done", "asleep"] as const;
export type State = (typeof STATES)[number];

export type Box = { x: number; y: number; w: number; h: number };
export type Eyes = "dot" | "bright" | "happy" | "sleepy" | "dash" | "wink" | "wide";
export type Shape = {
  name: string;
  head: Box;
  /** Top to bottom, under the head. */
  layers: readonly Box[];
  /** "pill" is half the height; a number is the largest radius. */
  corner: "pill" | number;
  /** The eyes at rest. */
  eyes: Eyes;
  /** Half the distance between the eyes. */
  eyeDx: number;
  smile: boolean;
  /** The head's rotation at rest, in degrees. */
  tilt?: number;
};
export type Hue = { name: string; ground: string; head: string; a: string; b: string; c: string };

/** How much is drawn at a size: under 76 px the smile goes, under 20 the state. */
export type Detail = "full" | "medium" | "tiny";
export const detailFor = (size: number): Detail => (size >= 76 ? "full" : size >= 20 ? "medium" : "tiny");
/** Under 48 px the mark fills the tile and the app draws the badge itself. */
export const tightAt = (size: number) => size < 48;

// The eyes each state puts on; at rest a shape wears its own.
const STATE_EYES: Partial<Record<State, Eyes>> = { listening: "bright", working: "dash", needs_you: "wide", done: "happy", asleep: "sleepy" };

const WHITE = "#fff";

/** The layers' colors by how many there are. They never change with the state. */
export function layerColors(hue: Hue, count: number): string[] {
  if (count === 1) return [hue.b];
  if (count === 2) return [hue.b, hue.c];
  return [hue.a, hue.b, hue.c];
}

export type Layout = {
  head: Box & { rot: number };
  layers: Box[];
  /** The dashed layer sliding in while it works. */
  incoming: Box | null;
  /** The two marks beside the gap under a raised head. */
  ticks: { y: number; xl: number; xr: number } | null;
  badge: "alert" | "check" | null;
  zz: boolean;
};

/** Where every piece of a shape is in a state. */
export function layout(shape: Shape, state: State = "idle", { centered = false, compact = false } = {}): Layout {
  const head = { ...shape.head, rot: shape.tilt ?? 0 };
  const layers = shape.layers.map((l) => ({ ...l }));
  const out: Layout = { head, layers, incoming: null, ticks: null, badge: null, zz: false };
  const centerAll = () => {
    head.x = 50 - head.w / 2;
    head.rot = 0;
    for (const l of layers) l.x = 50 - l.w / 2;
  };

  if (state === "listening") {
    // The head lifts and tilts and the body settles. Small sizes move less,
    // so the head stays inside the tile.
    head.y -= compact ? 3 : 5;
    head.rot = compact ? -8 : -10;
    for (const l of layers) l.y += 2;
  } else if (state === "working" && compact) {
    // No room for another layer: the bottom one becomes the dashed one.
    head.rot = 0;
    const last = layers.pop()!;
    out.incoming = { x: Math.min(last.x + 8, 86 - last.w), y: last.y, w: last.w, h: last.h };
  } else if (state === "working") {
    // Everything gets 2 shorter and restacks higher, leaving room for a
    // dashed layer that slides in from the right under the stack.
    let y = Math.max(10, head.y - 8);
    head.rot = 0;
    head.h -= 2;
    head.y = y;
    y += head.h + 2;
    for (const l of layers) {
      l.h = Math.max(10, l.h - 2);
      l.y = y;
      y += l.h + 2;
    }
    const last = layers[layers.length - 1];
    out.incoming = { x: Math.min(last.x + 14, 90 - last.w), y: y + 1, w: last.w, h: Math.min(13, last.h - 1) };
  } else if (state === "needs_you") {
    // The head pops up and leaves a gap, which two ticks mark.
    head.y -= compact ? 4 : 8;
    head.rot = 0;
    for (const l of layers) l.y += 2;
    const first = layers[0];
    out.ticks = {
      y: (head.y + head.h + first.y) / 2 + 1,
      xl: Math.min(head.x, first.x) + 2,
      xr: Math.max(head.x + head.w, first.x + first.w) + 2,
    };
    out.badge = "alert";
  } else if (state === "done") {
    centerAll();
    out.badge = "check";
  } else if (state === "asleep") {
    // The layers flatten and spread, and the head sinks onto them.
    let y = 85;
    for (let i = layers.length - 1; i >= 0; i--) {
      const l = layers[i];
      l.h = Math.min(l.h, 12);
      l.w = 68 - 6 * (layers.length - 1 - i);
      l.x = 50 - l.w / 2;
      y -= l.h;
      l.y = y;
      y -= 2;
    }
    head.h = Math.round(head.h * 0.75);
    head.w += 4;
    head.x = 50 - head.w / 2;
    head.y = y - head.h;
    head.rot = 0;
    out.zz = true;
  }
  if (centered) centerAll();
  return out;
}

const n = (v: number) => String(Math.round(v * 100) / 100);

export type Prim =
  | { t: "circle"; cx: number; cy: number; r: number; fill: string }
  | { t: "line"; d: string; stroke: string; width: number };

/** A face around (cx0, cy): the eyes of a style, and the smile under them. */
export function face(style: Eyes, cx0: number, cy: number, dx: number, ink: string, r: number, sw: number, smile: boolean): Prim[] {
  const xl = cx0 - dx;
  const xr = cx0 + dx;
  const line = (d: string, w: number): Prim => ({ t: "line", d, stroke: ink, width: w * sw });
  const dot = (x: number, rad = r, fill = ink): Prim => ({ t: "circle", cx: x, cy, r: rad, fill });
  const arcUp = (x: number) => `M${n(x - 3.5)} ${n(cy + 1.5)}q3.5-4.5 7 0`;
  const arcDown = (x: number) => `M${n(x - 3.5)} ${n(cy)}q3.5 3.5 7 0`;
  let out: Prim[] = [];
  if (style === "dot") out = [dot(xl), dot(xr)];
  else if (style === "bright") {
    out = [
      dot(xl, r + 0.4),
      dot(xr, r + 0.4),
      { t: "circle", cx: xl + 1.3, cy: cy - 1.3, r: 1.2, fill: WHITE },
      { t: "circle", cx: xr + 1.3, cy: cy - 1.3, r: 1.2, fill: WHITE },
    ];
  } else if (style === "happy") out = [line(arcUp(xl) + arcUp(xr), 2.6)];
  else if (style === "sleepy") out = [line(arcDown(xl) + arcDown(xr), 2.6)];
  else if (style === "dash") out = [line(`M${n(xl - 4)} ${n(cy)}h8M${n(xr - 4)} ${n(cy)}h8`, 3)];
  else if (style === "wink") out = [dot(xl), line(arcUp(xr), 2.6)];
  else if (style === "wide") out = [dot(xl, r + 1.8, WHITE), dot(xr, r + 1.8, WHITE), dot(xl, (r + 1.8) / 2), dot(xr, (r + 1.8) / 2)];
  if (smile) out.push(line(`M${n(cx0 - 3)} ${n(cy + 6)}q3 2.5 6 0`, 2));
  return out;
}

export type Piece = Box & { r: number; fill: string; opacity: number };

export type Scene = {
  state: State;
  detail: Detail;
  tight: boolean;
  viewBox: readonly [number, number, number, number];
  /** The ground's color; the tile is the whole viewBox, its corner 24% of it. */
  tile: string;
  ink: string;
  layers: Piece[];
  incoming: (Box & { r: number; stroke: string; width: number }) | null;
  ticks: { d: string; stroke: string } | null;
  head: Piece & { rot: number; cx: number; cy: number };
  eyes: Eyes;
  /** The face around a centre: the head's own for a string, (0, 0) for a head drawn at its centre. */
  face: (cx: number, cy: number) => Prim[];
  zz: { d: string; stroke: string; shifted: boolean } | null;
  /** Drawn in the mark only at 48 px and up. */
  badge: "alert" | "check" | null;
};

export const TICKS_WIDTH = 2.2;
export const ZZ = "M70 14h8l-8 9h8M83 27h4.5l-4.5 5.5h4.5";
export const BADGE = { cx: 87, cy: 15, r: 8, fill: WHITE, alert: "M87 10.5v5.5", dot: { cx: 87, cy: 19.6, r: 1.4 }, check: "M83 15.2l2.8 2.8 5-5.6" } as const;

export type Look = {
  shape: number;
  hue: number;
  state?: State;
  /** The size it is drawn at, in px: it decides how much is drawn. */
  size?: number;
  /** One color for the whole mark, on `monoBg`. */
  mono?: string | null;
  monoBg?: string;
};

/** Everything a drawing of a Bot holds, as numbers and colors. */
export function scene({ shape: s, hue: h, state: asked = "idle", size = 100, mono = null, monoBg = MONO.background }: Look): Scene {
  const shape: Shape = SHAPES[mod(s, SHAPES.length)];
  const pal: Hue = HUES[mod(h, HUES.length)];
  const detail = detailFor(size);
  const tight = tightAt(size);
  // A state cannot be read under 20 px: the UI beside the crest says it.
  const state = detail === "tiny" ? "idle" : asked;
  const g = layout(shape, state, { centered: detail === "tiny", compact: tight });
  const ink = mono ? monoBg : pal.ground;
  const count = shape.layers.length;
  const fills = mono ? g.layers.map(() => mono) : layerColors(pal, count);
  const opacity: readonly number[] = mono ? MONO.layerOpacity[count as 1 | 2 | 3] : g.layers.map(() => 1);
  const radius = (height: number) => (shape.corner === "pill" ? height / 2 : Math.min(shape.corner, height / 2));
  const eyes = detail === "tiny" ? "dot" : (STATE_EYES[state] ?? shape.eyes);
  const smile = detail === "full" && state === "idle" && shape.smile;
  const r = detail === "full" ? 3.2 : detail === "medium" ? 4.2 : 5.6;
  const dx = shape.eyeDx + (detail === "medium" ? 0.5 : detail === "tiny" ? 3 : 0);
  const sw = detail === "full" ? 1 : 1.25;
  const hd = g.head;
  return {
    state,
    detail,
    tight,
    viewBox: tight ? [10, 10, 80, 80] : [0, 0, 100, 100],
    tile: mono ? monoBg : pal.ground,
    ink,
    layers: g.layers.map((l, i) => ({ ...l, r: radius(l.h), fill: fills[i], opacity: opacity[i] })),
    incoming: g.incoming ? { ...g.incoming, r: radius(g.incoming.h), stroke: mono ?? pal.c, width: tight ? 3 : 2.4 } : null,
    ticks: g.ticks ? { d: `M${n(g.ticks.xl)} ${n(g.ticks.y)}l-5-4M${n(g.ticks.xr)} ${n(g.ticks.y)}l5-4`, stroke: mono ?? pal.b } : null,
    head: { ...hd, r: radius(hd.h), fill: mono ?? pal.head, opacity: 1, cx: hd.x + hd.w / 2, cy: hd.y + hd.h / 2 },
    eyes,
    face: (cx, cy) => face(eyes, cx, cy - (smile ? 1 : 0), dx, ink, r, sw, smile),
    zz: g.zz ? { d: ZZ, stroke: mono ?? pal.b, shifted: tight } : null,
    badge: tight ? null : g.badge,
  };
}

function mod(i: number, len: number) {
  return ((Math.trunc(i) % len) + len) % len;
}

/** The drawing as an SVG document, for files: icons, the sheet, the tests. */
export function renderStack({ ground = true, label, ...look }: Look & { ground?: boolean; label: string }): string {
  const sc = scene(look);
  const size = look.size ?? 100;
  const vb = sc.viewBox;
  let s = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vb.join(" ")}" width="${size}" height="${size}" role="img" aria-label="${label}">`;
  if (ground) s += `<rect x="${vb[0]}" y="${vb[1]}" width="${vb[2]}" height="${vb[3]}" rx="${n(vb[2] * 0.24)}" fill="${sc.tile}"/>`;
  const box = (b: Box & { r: number }) => `x="${n(b.x)}" y="${n(b.y)}" width="${n(b.w)}" height="${n(b.h)}" rx="${n(b.r)}"`;
  if (sc.incoming) s += `<rect ${box(sc.incoming)} fill="none" stroke="${sc.incoming.stroke}" stroke-width="${sc.incoming.width}" stroke-dasharray="5 4"/>`;
  for (const l of sc.layers) s += `<rect ${box(l)} fill="${l.fill}"${l.opacity < 1 ? ` opacity="${l.opacity}"` : ""}/>`;
  if (sc.ticks) s += `<path d="${sc.ticks.d}" fill="none" stroke="${sc.ticks.stroke}" stroke-width="${TICKS_WIDTH}" stroke-linecap="round"/>`;
  const h = sc.head;
  s += `<g${h.rot ? ` transform="rotate(${h.rot} ${n(h.cx)} ${n(h.cy)})"` : ""}>`;
  s += `<rect ${box(h)} fill="${h.fill}"/>`;
  for (const p of sc.face(h.cx, h.cy)) {
    s +=
      p.t === "circle"
        ? `<circle cx="${n(p.cx)}" cy="${n(p.cy)}" r="${n(p.r)}" fill="${p.fill}"/>`
        : `<path d="${p.d}" fill="none" stroke="${p.stroke}" stroke-width="${n(p.width)}" stroke-linecap="round"/>`;
  }
  s += "</g>";
  if (sc.zz) s += `<path${sc.zz.shifted ? ' transform="translate(-7 3)"' : ""} d="${sc.zz.d}" fill="none" stroke="${sc.zz.stroke}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`;
  if (sc.badge) {
    s += `<circle cx="${BADGE.cx}" cy="${BADGE.cy}" r="${BADGE.r}" fill="${BADGE.fill}"/>`;
    s +=
      sc.badge === "alert"
        ? `<path d="${BADGE.alert}" stroke="${sc.ink}" stroke-width="2.4" stroke-linecap="round"/><circle cx="${BADGE.dot.cx}" cy="${BADGE.dot.cy}" r="${BADGE.dot.r}" fill="${sc.ink}"/>`
        : `<path d="${BADGE.check}" fill="none" stroke="${sc.ink}" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/>`;
  }
  return s + "</svg>";
}

// ------------------------------------------------------------- the crest int
// A Bot stores its look as one number, hue * 8 + shape, in the order of
// crestArt.ts (brand/tokens.json). Never reorder those, only restyle.

export const SHAPE_COUNT = SHAPES.length;
export const HUE_COUNT = HUES.length;

export function packCrest(shape: number, hue: number) {
  return mod(hue, HUE_COUNT) * SHAPE_COUNT + mod(shape, SHAPE_COUNT);
}

export function unpackCrest(index: number) {
  const i = mod(index, SHAPE_COUNT * HUE_COUNT);
  return { shape: i % SHAPE_COUNT, hue: Math.floor(i / SHAPE_COUNT) };
}

/** A new Bot's look: the first shape, in a hue no Bot beside it wears yet, else in `spare`. */
export function freshCrest(taken: readonly number[], spare: number) {
  const used = new Set(taken.map((c) => unpackCrest(c).hue));
  const free = HUES.findIndex((_, i) => !used.has(i));
  return packCrest(0, free < 0 ? spare : free);
}
