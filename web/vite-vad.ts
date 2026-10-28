import { createReadStream, existsSync, readFileSync, realpathSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import type { Plugin } from "vite";

// The files the in-browser voice detector loads at run time (its audio
// worklet, the Silero model, and onnxruntime's WebAssembly), served from our
// own origin under /vad/: in dev straight out of node_modules, and copied into
// the build. Nothing is fetched from a CDN.
const require = createRequire(import.meta.url);

// Where the files are; null when the packages are missing, which only turns
// voice detection off (a call says so) and never stops the dev server.
function locate(): Record<string, string> | null {
  try {
    const vadDir = dirname(realpathSync(require.resolve("@ricky0123/vad-web/package.json")));
    // onnxruntime-web exports no package.json; pnpm keeps it beside vad-web.
    const ortDir = join(realpathSync(join(vadDir, "..", "..", "onnxruntime-web")), "dist");
    const files: Record<string, string> = {
      "vad.worklet.bundle.min.js": join(vadDir, "dist", "vad.worklet.bundle.min.js"),
      "silero_vad_v5.onnx": join(vadDir, "dist", "silero_vad_v5.onnx"),
      "ort-wasm-simd-threaded.wasm": join(ortDir, "ort-wasm-simd-threaded.wasm"),
      "ort-wasm-simd-threaded.mjs": join(ortDir, "ort-wasm-simd-threaded.mjs"),
    };
    for (const path of Object.values(files)) if (!existsSync(path)) throw new Error(`missing ${path}`);
    return files;
  } catch (e) {
    console.warn(`[silo-vad-assets] voice detection files not found: ${e instanceof Error ? e.message : e}`);
    return null;
  }
}

const files = locate() ?? {};

const types: Record<string, string> = { js: "text/javascript", mjs: "text/javascript", wasm: "application/wasm", onnx: "application/octet-stream" };

export function vadAssets(): Plugin {
  return {
    name: "silo-vad-assets",
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        const name = req.url?.startsWith("/vad/") ? req.url.slice(5).split("?")[0] : "";
        const path = files[name];
        if (!path) return next();
        res.setHeader("Content-Type", types[name.split(".").pop() ?? ""] ?? "application/octet-stream");
        res.setHeader("Cache-Control", "public, max-age=86400");
        createReadStream(path).pipe(res);
      });
    },
    generateBundle() {
      for (const [name, path] of Object.entries(files)) {
        this.emitFile({ type: "asset", fileName: `vad/${name}`, source: readFileSync(path) });
      }
    },
  };
}
