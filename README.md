# Silo Agent

A token-conserving, self-hosted, multi-user agent. Each **Bot** is a durable container with a desktop, driven by a Go control plane.

| Where | What |
| --- | --- |
| `cmd/`, `internal/` | The Go control plane and the binaries that run in containers |
| `proto/` (+ `gen/`) | The ConnectRPC API and its buf config; `make proto` regenerates `gen/` and `web/src/gen/` |
| `web/` | The console (Vite + React) |
| `ios/` | The SwiftUI client |
| `site/` | The website: landing and docs (Astro), with the console demo built from `web/` |
| `images/<name>/` | One folder per container image, built as `silo-<name>` (`bot`, `mcp-stdio`, `drive`, `egress`, `caddy`, `cp` = the control plane, `os` = SiloOS) |
| `dev/` | The dev dependencies `make db-up` starts (`compose.yml`, plus Dex, Postgres and Radicale config) |
| `docs/` | Design, development, testing, configuration, architecture and research notes |
| `brand/` | The logo and the Bots' crests |
| `test/integration/` | Tests against a running stack |

Start with [AGENTS.md](AGENTS.md) (how everything works and what must not break) and [docs/DEVELOPMENT.md](docs/DEVELOPMENT.md) (setup, build, run).
