import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import { vadAssets } from "./vite-vad.ts";

export default defineConfig({
  plugins: [react(), tailwindcss(), vadAssets()],
  optimizeDeps: { exclude: ["@novnc/novnc"], rolldownOptions: { transform: { target: "es2022" } } },
  build: {
    target: "es2022",
    rolldownOptions: {
      output: {
        // The libraries that change least get their own files, so an app
        // release leaves them in the browser's cache.
        codeSplitting: {
          groups: [
            { name: "vendor-react", test: /[\\/]node_modules[\\/](react|react-dom|scheduler|@tanstack)[\\/]/ },
            { name: "vendor-rpc", test: /[\\/]node_modules[\\/](@bufbuild|@connectrpc)[\\/]/ },
          ],
        },
      },
    },
  },
  oxc: { target: "es2022" },
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/silo.v1.UI": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/silo.v1.BotWorker": { target: "http://127.0.0.1:8080", changeOrigin: true },
      "/vnc": { target: "http://127.0.0.1:8080", ws: true },
      "/console": { target: "http://127.0.0.1:8080", ws: true },
      // A voice call's control socket, and Read aloud's audio.
      "/voice": { target: "http://127.0.0.1:8080", ws: true },
      "/healthz": { target: "http://127.0.0.1:8080" },
      "/oauth": { target: "http://127.0.0.1:8080" },
      // Signing in with the OIDC provider starts and ends on the control plane.
      "/auth": { target: "http://127.0.0.1:8080" },
      "/connectors": { target: "http://127.0.0.1:8080" },
      "/artifacts": { target: "http://127.0.0.1:8080" },
      // The private-tunnel sign-in handoff starts at public_url (the Vite origin).
      "/tunnels": { target: "http://127.0.0.1:8080" },
      // A received message's original bytes (.eml download).
      "/mail/raw": { target: "http://127.0.0.1:8080" },
      // A channel's webhook (a platform that delivers by calling in).
      "/channels/hook": { target: "http://127.0.0.1:8080" },
    },
  },
});
