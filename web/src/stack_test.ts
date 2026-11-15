import fs from "node:fs";
import { HUES, SHAPES } from "./crestArt.ts";
import { freshCrest, HUE_COUNT, layout, packCrest, renderStack, scene, SHAPE_COUNT, STATES, unpackCrest, type Box } from "./stack.ts";
import { eq } from "./testing.ts";

// The designer's drawings are brand/reference; brand/tokens.json names the
// roster that wears them. The renderer is held to them byte for byte.
const brand = new URL("../../brand/", import.meta.url);
const read = (path: string) => fs.readFileSync(new URL(path, brand), "utf8");
const tokens = JSON.parse(read("tokens.json")) as { bots: Record<string, { name: string; shape: string; hue: string }> };
const shapeOf = (name: string) => SHAPES.findIndex((s) => s.name === name);
const hueOf = (name: string) => HUES.findIndex((h) => h.name === name);
const words = { idle: "idle", listening: "listening", working: "working", needs_you: "needs you", done: "done", asleep: "asleep" };

for (const [key, bot] of Object.entries(tokens.bots)) {
  for (const state of STATES) {
    const got = renderStack({ shape: shapeOf(bot.shape), hue: hueOf(bot.hue), state, size: 100, label: `${bot.name}, ${words[state]}` });
    eq(got, read(`reference/bots/${key}-${state}.svg`), `${key} ${state} at 100px`);
  }
}
const silo = { shape: shapeOf("wobble"), hue: hueOf("green") };
eq(packCrest(silo.shape, silo.hue), 0, "the brand mark is crest 0");
for (const size of [40, 24, 16]) {
  eq(renderStack({ ...silo, size, label: "Silo, idle" }), read(`reference/small/silo-idle-${size}.svg`), `the mark at ${size}px`);
}
eq(renderStack({ ...silo, label: "Silo" }), read("reference/mark/stack-mark.svg"), "the mark");
eq(renderStack({ ...silo, ground: false, label: "Silo" }), read("reference/mark/stack-glyph.svg"), "the mark with no ground");
eq(renderStack({ ...silo, mono: "#F4F5F7", label: "Silo" }), read("reference/mark/stack-mark-mono.svg"), "the mark in one color");

// At sidebar sizes nothing leaves the tile, in any shape or state: a rotated
// head is measured at its corners.
function corners(b: Box, rot = 0): [number, number][] {
  const cx = b.x + b.w / 2;
  const cy = b.y + b.h / 2;
  const a = (rot * Math.PI) / 180;
  return [[-1, -1], [1, -1], [1, 1], [-1, 1]].map(([sx, sy]) => {
    const dx = (sx * b.w) / 2;
    const dy = (sy * b.h) / 2;
    return [cx + dx * Math.cos(a) - dy * Math.sin(a), cy + dx * Math.sin(a) + dy * Math.cos(a)];
  });
}
for (let shape = 0; shape < SHAPE_COUNT; shape++) {
  for (const state of STATES) {
    for (const size of [47, 40, 20]) {
      const sc = scene({ shape, hue: 0, state, size });
      const [x, y, w, h] = sc.viewBox;
      const pts = [...corners(sc.head, sc.head.rot), ...sc.layers.flatMap((l) => corners(l)), ...(sc.incoming ? corners(sc.incoming) : [])];
      for (const [px, py] of pts) {
        if (px < x || px > x + w || py < y || py > y + h) throw new Error(`${SHAPES[shape].name} ${state} at ${size}px leaves the tile at ${px.toFixed(1)}, ${py.toFixed(1)}`);
      }
      eq(sc.badge, null, "under 48px the badge is the app's to draw");
    }
  }
}

// At 16px every shape still has two eyes apart, and only the resting face.
for (let shape = 0; shape < SHAPE_COUNT; shape++) {
  const sc = scene({ shape, hue: 0, state: "working", size: 16 });
  eq(sc.state, "idle", "a state is not drawn under 20px");
  const eyes = sc.face(sc.head.cx, sc.head.cy);
  eq(eyes.map((p) => p.t), ["circle", "circle"], `${SHAPES[shape].name} has two dot eyes at 16px`);
  const [l, r] = eyes as { cx: number; r: number }[];
  if (r.cx - l.cx <= l.r + r.r) throw new Error(`${SHAPES[shape].name}: the eyes touch at 16px`);
  if (l.cx - l.r < sc.head.x || r.cx + r.r > sc.head.x + sc.head.w) throw new Error(`${SHAPES[shape].name}: the eyes leave the head at 16px`);
}

// Any shape takes any hue, and the layers' colors follow the layer count.
for (let hue = 0; hue < HUE_COUNT; hue++) {
  for (let shape = 0; shape < SHAPE_COUNT; shape++) {
    const sc = scene({ shape, hue });
    const pal = HUES[hue];
    eq(sc.tile, pal.ground, "the ground");
    eq(sc.head.fill, pal.head, "the head");
    eq(sc.layers.map((l) => l.fill), [[pal.b], [pal.b, pal.c], [pal.a, pal.b, pal.c]][sc.layers.length - 1], `${SHAPES[shape].name} in ${pal.name}`);
    eq(unpackCrest(packCrest(shape, hue)), { shape, hue }, "a crest packs and unpacks");
  }
}
eq(unpackCrest(-1), { shape: 7, hue: 7 }, "any number is some crest");
eq(layout(SHAPES[shapeOf("tilt")], "done").head.rot, 0, "done straightens a tilted head");

// A new Bot is the first shape in the first hue nobody wears.
eq(freshCrest([], 5), packCrest(0, 0), "the first Bot is green");
eq(freshCrest([packCrest(3, 0), packCrest(1, 1)], 5), packCrest(0, 2), "the next free hue");
eq(freshCrest(HUES.map((_, i) => packCrest(0, i)), 5), packCrest(0, 5), "with every hue worn, the spare one");

console.log("stack: ok");
