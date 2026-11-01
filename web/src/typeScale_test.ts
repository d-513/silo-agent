import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

// A size is a step of the scale in index.css: a component that writes its own
// pixels is how the scale drifted to 22 sizes, most within 2.5px of each other.
const src = fileURLToPath(new URL(".", import.meta.url));
const walk = (dir: string): string[] =>
  readdirSync(dir, { withFileTypes: true }).flatMap((e) => {
    const p = join(dir, e.name);
    if (e.isDirectory()) return e.name === "gen" || e.name === "locales" ? [] : walk(p);
    return /\.tsx?$/.test(e.name) && !e.name.endsWith("_test.ts") ? [p] : [];
  });

const banned = [/\btext-\[\d[\d.]*(px|rem|em)\]/, /\btext-(xs|sm|base|lg|xl|\dxl)\b/, /\bleading-\[(17|19|21)px\]/];
const bad: string[] = [];
for (const f of walk(src)) {
  readFileSync(f, "utf8")
    .split("\n")
    .forEach((line, i) => {
      if (banned.some((b) => b.test(line))) bad.push(`${f.slice(src.length)}:${i + 1}`);
    });
}
if (bad.length) throw new Error(`Use a size from the scale in index.css:\n  ${bad.join("\n  ")}`);
console.log("type scale ok");
