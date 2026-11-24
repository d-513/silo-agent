import { fileURLToPath } from "node:url";
import { defineConfig, type Plugin } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

// The demo build: the console as it ships, with the modules that reach a
// server swapped for ones that answer in the page (src/demo/). The website
// (site/) shows it in a frame. `pnpm build:demo` writes it to site/public/demo;
// `pnpm dev:demo` serves it on :5174.

const at = (p: string) => fileURLToPath(new URL(p, import.meta.url));

const swaps = new Map([
  [at("./src/api.ts"), at("./src/demo/api.ts")],
  [at("./src/history.ts"), at("./src/demo/history.ts")],
  [at("./src/bot/MachinePane.tsx"), at("./src/demo/MachinePane.tsx")],
]);

function swapModules(): Plugin {
  return {
    name: "silo-demo-swap",
    enforce: "pre",
    async resolveId(source, importer, options) {
      const found = await this.resolve(source, importer, { ...options, skipSelf: true });
      const to = found && swaps.get(found.id.split("?")[0]);
      return to ? { id: to } : null;
    },
  };
}

export default defineConfig({
  // Relative, so the bundle works under whatever path the site serves it at.
  base: "./",
  plugins: [swapModules(), react(), tailwindcss()],
  optimizeDeps: { exclude: ["@novnc/novnc"], rolldownOptions: { transform: { target: "es2022" } } },
  build: { target: "es2022", outDir: "../site/public/demo", emptyOutDir: true },
  oxc: { target: "es2022" },
  server: { port: 5174, strictPort: true },
});
