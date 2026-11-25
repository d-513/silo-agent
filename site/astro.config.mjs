import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import tailwindcss from "@tailwindcss/vite";

// Silo's website: the landing (src/pages) and the docs (Starlight, under
// /docs). It builds to static files for Cloudflare Pages. The console demo in
// public/demo is the real console built by `pnpm --dir ../web build:demo`.

const repo = "https://github.com/d-513/silo-agent";

export default defineConfig({
  // The public address, for the sitemap and canonical links.
  site: process.env.SITE_URL || undefined,
  redirects: { "/docs/run/install": "/docs/install/" },
  integrations: [
    starlight({
      title: "Silo",
      description: "A self-hosted agent for several people. Each Bot is a durable machine with a desktop; the control plane holds every key.",
      logo: { src: "./src/assets/mark.svg", alt: "" },
      favicon: "/favicon.svg",
      customCss: ["./src/styles/docs.css"],
      social: [{ icon: "github", label: "GitHub", href: repo }],
      editLink: { baseUrl: `${repo}/edit/master/site/` },
      // The docs follow the console's theme, and the demo in a frame follows both.
      head: [{ tag: "script", content: "try{var t=localStorage.getItem('starlight-theme');if(t==='light'||t==='dark')localStorage.setItem('silo.theme',t);else localStorage.removeItem('silo.theme')}catch(e){}" }],
      sidebar: [
        {
          label: "Start here",
          items: [
            { label: "What Silo is", slug: "docs" },
            { label: "A day in a Bot's life", slug: "docs/a-day" },
            { label: "Try the console", slug: "docs/try" },
          ],
        },
        {
          label: "How it works",
          items: [
            { label: "Bots and their machines", slug: "docs/concepts/bots" },
            { label: "Rules and approvals", slug: "docs/concepts/rules" },
            { label: "What a Bot remembers", slug: "docs/concepts/context" },
          ],
        },
        {
          label: "What happens when…",
          items: [
            { label: "A web page tries to steer the Bot", slug: "docs/attacks/injected-page" },
            { label: "Code in the box goes for a key", slug: "docs/attacks/stolen-key" },
          ],
        },
        {
          label: "Install",
          items: [
            { label: "Ways to install", slug: "docs/install" },
            { label: "SiloOS", slug: "docs/install/siloos" },
            { label: "Podman", slug: "docs/install/podman" },
            { label: "Docker", slug: "docs/install/docker" },
          ],
        },
        {
          label: "Running Silo",
          items: [{ label: "Configuration", slug: "docs/run/configuration" }],
        },
      ],
    }),
  ],
  vite: {
    plugins: [tailwindcss()],
    // The crest and the tokens are the console's own files.
    server: { fs: { allow: [".."] } },
  },
});
