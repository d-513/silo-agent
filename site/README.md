# Silo's website

The landing page and the docs, built with [Astro](https://astro.build) and Starlight into static files for Cloudflare Pages.

| Where | What |
| --- | --- |
| `src/pages/` | The landing page |
| `src/content/docs/docs/` | The docs, served under `/docs` (Markdown and MDX) |
| `src/components/Demo.astro` | The console demo in a frame, for the landing and any docs page |
| `src/components/Crest.astro` | A Bot's crest, drawn at build time by the console's `web/src/stack.ts` |
| `src/styles/` | `site.css` (Tailwind for the landing) and `docs.css` (Starlight's variables), both over `web/src/tokens.css` |
| `public/demo/` | The console demo, built from `web/` (not in git) |

## The console demo

The frame on the landing page is the real console, not a copy. `web/vite.demo.config.ts` builds it with three modules swapped for ones in `web/src/demo/`: the API client (answered by a backend that runs in the page), the router's history (kept in memory) and the Desktop and Console pane (which need a live machine). What the demo's Bots say is written in `web/src/demo/scenes.ts` and what they have in `world.ts`.

A docs page opens the demo on any console page with `<Demo at="/bots/ledger" />`.

## Commands

```sh
make site-build                 # the demo, then the site, into site/dist
pnpm --dir web dev:demo         # the demo alone on :5174, with hot reload
pnpm --dir site dev             # the site on :4321 (build the demo first)
```

## Cloudflare Pages

- Build command: `pnpm --dir web install --frozen-lockfile && pnpm --dir web build:demo && pnpm --dir site install --frozen-lockfile && pnpm --dir site build`
- Output directory: `site/dist`
- Root directory: the repository root (the site reads `web/`, `brand/` and `internal/catalog/`)
- `SITE_URL`: the public address, for the sitemap
- Watch paths: `site/**`, `web/src/**`, `brand/**`, `internal/catalog/**`, `internal/drives/catalog/**`

Nothing needs a Worker yet. `public/_headers` sets the caching.
