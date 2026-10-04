import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  optimizeDeps: { exclude: ["@novnc/novnc"], esbuildOptions: { target: "es2022" } },
  build: {
    target: "es2022",
    rollupOptions: {
      output: {
        // The libraries that change least get their own files, so an app
        // release leaves them in the browser's cache.
        manualChunks(id) {
          if (!id.includes("node_modules")) return;
          if (/[\\/]node_modules[\\/](react|react-dom|scheduler|react-router|react-router-dom)[\\/]/.test(id)) return "vendor-react";
          if (/[\\/]node_modules[\\/](@bufbuild|@connectrpc)[\\/]/.test(id)) return "vendor-rpc";
        },
      },
    },
  },
  esbuild: { target: "es2022" },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/silo.v1.UI": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/silo.v1.BotWorker": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/vnc": { target: "http://127.0.0.1:8080", ws: true },
      "/console": { target: "http://127.0.0.1:8080", ws: true },
      "/healthz": { target: "http://127.0.0.1:8080" },
      "/oauth": { target: "http://127.0.0.1:8080" },
      "/connectors": { target: "http://127.0.0.1:8080" },
      "/artifacts": { target: "http://127.0.0.1:8080" },
    },
  },
});
