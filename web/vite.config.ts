import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  optimizeDeps: { exclude: ["@novnc/novnc"], esbuildOptions: { target: "es2022" } },
  build: { target: "es2022" },
  esbuild: { target: "es2022" },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/silo.v1.UI": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/silo.v1.BotWorker": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/vnc": { target: "http://127.0.0.1:8080", ws: true },
      "/healthz": { target: "http://127.0.0.1:8080" },
    },
  },
});
