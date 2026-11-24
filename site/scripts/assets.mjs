// What the site takes from the rest of the repository, copied in before a
// build so there is one source for each: the favicons the console serves
// (web/public) and the mark (brand/logo, made by `make brand`). The console
// demo is built into public/demo by `pnpm --dir ../web build:demo`.
import { cpSync, existsSync, mkdirSync } from "node:fs";

const at = (p) => new URL(p, import.meta.url);

mkdirSync(at("../public"), { recursive: true });
mkdirSync(at("../src/assets"), { recursive: true });
for (const f of ["favicon.ico", "favicon.svg", "favicon-32.png", "apple-touch-icon.png", "icon-512.png"]) cpSync(at(`../../web/public/${f}`), at(`../public/${f}`));
cpSync(at("../../brand/logo/mark.svg"), at("../src/assets/mark.svg"));

if (!existsSync(at("../public/demo/index.html"))) {
  console.warn("\n  The console demo is not built: the landing's frame will be empty.\n  Run `make site-demo` (or `pnpm --dir ../web build:demo`) first.\n");
}
