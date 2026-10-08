import fs from "node:fs";
import { eq } from "./testing.ts";
import { flipTheme, resolveTheme, storedTheme, themeKey } from "./theme.ts";
import { palette } from "./tokens.ts";

// What the reader picked wins; with no pick the app follows the system.
eq(resolveTheme("dark", false), "dark", "a stored dark beats a light system");
eq(resolveTheme("light", true), "light", "a stored light beats a dark system");
eq(resolveTheme(null, true), "dark", "no pick follows a dark system");
eq(resolveTheme(null, false), "light", "no pick follows a light system");

eq(storedTheme("dark"), "dark", "dark is read back");
eq(storedTheme("light"), "light", "light is read back");
eq(storedTheme(null), null, "nothing stored");
eq(storedTheme("sepia"), null, "an unknown value is no pick");

eq(flipTheme("light"), "dark", "light flips to dark");
eq(flipTheme("dark"), "light", "dark flips to light");

// index.html sets the theme before the first paint with an inline script that
// cannot import this module. It must agree with resolveTheme for every case.
const html = fs.readFileSync(new URL("../index.html", import.meta.url), "utf8");
const boot = html.match(/<script>([\s\S]*?)<\/script>/)?.[1] ?? "";
if (!boot.includes(JSON.stringify(themeKey))) throw new Error(`the boot script must read ${themeKey}`);
for (const stored of [null, "dark", "light", "sepia"]) {
  for (const systemDark of [false, true]) {
    const root = { dataset: {} as Record<string, string> };
    const env = {
      localStorage: { getItem: (k: string) => (k === themeKey ? stored : null) },
      matchMedia: (q: string) => ({ matches: q.includes("dark") && systemDark }),
      document: { documentElement: root },
    };
    new Function("localStorage", "matchMedia", "document", boot)(env.localStorage, env.matchMedia, env.document);
    eq(root.dataset.theme, resolveTheme(storedTheme(stored), systemDark), `boot script, stored=${stored} systemDark=${systemDark}`);
  }
}
// A private window whose storage throws still gets a theme.
{
  const root = { dataset: {} as Record<string, string> };
  const throwing = { getItem: () => { throw new Error("denied"); } };
  new Function("localStorage", "matchMedia", "document", boot)(throwing, () => ({ matches: true }), { documentElement: root });
  eq(root.dataset.theme, "dark", "boot script survives blocked storage");
}

// ---------------------------------------------------------------- palette
// Both palettes live in index.css: light in @theme, dark as an override of the
// same names. Text has to stay readable on every ground it is set on.

const css = fs.readFileSync(new URL("./index.css", import.meta.url), "utf8");

function block(open: string): string {
  const at = css.indexOf(open);
  if (at < 0) throw new Error(`index.css has no ${open} block`);
  const from = css.indexOf("{", at) + 1;
  return css.slice(from, css.indexOf("\n}", from));
}

function colors(body: string): Record<string, string> {
  const out: Record<string, string> = {};
  for (const m of body.matchAll(/--color-([a-z0-9-]+):\s*([^;]+);/g)) out[m[1]] = m[2].trim();
  return out;
}

const light = colors(block("@theme {"));
const dark = { ...light, ...colors(block('[data-theme="dark"] {')) };

for (const name of Object.keys(colors(block('[data-theme="dark"] {')))) {
  if (!(name in light)) throw new Error(`dark overrides --color-${name}, which the light theme does not define`);
}

// tokens.ts repeats a few of these for painters that cannot read CSS.
eq([palette.canvas, palette.canvasDark], [light.canvas, dark.canvas], "tokens.ts canvas matches index.css");
eq([palette.hatch, palette.matte, palette.matteDark, palette.cobalt], [light.hatch, light.matte, dark.matte, light.cobalt], "tokens.ts hatch palette matches index.css");

function luminance(hex: string): number {
  if (!/^#[0-9a-f]{6}$/i.test(hex)) throw new Error(`expected a 6-digit hex, got ${hex}`);
  const [r, g, b] = [1, 3, 5].map((i) => {
    const c = parseInt(hex.slice(i, i + 2), 16) / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}

function contrast(a: string, b: string): number {
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

function atLeast(theme: string, p: Record<string, string>, text: string, ground: string, min: number) {
  const got = contrast(p[text], p[ground]);
  if (got < min) throw new Error(`${theme}: ${text} on ${ground} is ${got.toFixed(2)}:1, want at least ${min}:1`);
}

for (const [theme, p] of [["light", light], ["dark", dark]] as const) {
  // Body text and the three accents that are also set as words.
  for (const ground of ["canvas", "well", "surface"]) {
    atLeast(theme, p, "ink", ground, 12);
    atLeast(theme, p, "ink-2", ground, 6.5);
    for (const text of ["ink-3", "cobalt", "emerald", "vermilion"]) atLeast(theme, p, text, ground, 4.5);
  }
  // Labels on a fill.
  atLeast(theme, p, "on-ink", "ink", 12);
  atLeast(theme, p, "on-ink", "ink-deep", 12);
  for (const fill of ["cobalt", "emerald", "vermilion", "vermilion-deep"]) atLeast(theme, p, "on-accent", fill, 4.5);
  // Tinted wells keep their word and plain text readable.
  atLeast(theme, p, "vermilion", "vermilion-pale", 4.5);
  atLeast(theme, p, "ink", "cobalt-pale", 10);
  // The hatch is a dark window in both themes.
  atLeast(theme, p, "on-hatch", "hatch", 12);
  atLeast(theme, p, "on-hatch", "matte", 12);
}

// The grounds must stay tellable apart: in the dark, raised is lighter.
const tones = ["canvas", "well", "surface", "pressed"].map((g) => luminance(dark[g]));
for (let i = 1; i < tones.length; i++) {
  if (tones[i] <= tones[i - 1]) throw new Error("dark grounds must get lighter from canvas to well to surface to pressed");
}
if (luminance(dark.hatch) >= luminance(dark.canvas)) throw new Error("the hatch must stay darker than the dark canvas");

console.log("theme ok");
