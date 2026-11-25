---
name: update-deps
description: Update every dependency of the Silo repo, all of them, in one pass - Go modules and toolchain, the web's pnpm packages, every container base image and pinned download (bot, cp, drive, egress, mcp-stdio, caddy and its DNS modules, SiloOS), the dev compose images, GitHub Actions, buf plugins, the iOS Swift packages, and the versions baked into Go code (Chrome fingerprint, YouTube client, vendor API versions, default models). Use when asked to update, bump, upgrade or refresh dependencies, check for outdated packages, move pins, or do a dependency sweep, even if the request names only one ecosystem.
---

# Updating dependencies

Silo pins almost everything (AGENTS.md: "every base is pinned by digest and
every download by version or checksum: move a pin on purpose"). Dependencies
live in many places that no single tool sees. **Check every entry in the
inventory below, every time.** An update request that names one ecosystem
("bump Go deps") still gets the whole sweep reported, even where the answer
is "already newest". Do not stop after `go.mod` and `package.json`.

## How to work

1. Make a checklist (TodoWrite or a scratch file) with one line per section
   below (§1 to §12), and tick each one only after you have looked up its
   newest version. The report at the end covers every line.
2. For each pin: find the newest **stable** release (no betas, RCs or
   nightlies unless the pin already is one), read its changelog for breaking
   changes, then move it. Where a pin is a digest, resolve the digest of the
   new tag yourself (§ Lookup commands), never copy one from memory.
3. Keep the **coupled pins** (§ Must move together) consistent.
4. Run the verification for what you touched (§ Verify), builds one at a
   time with their output visible.
5. Commit per ecosystem or as one `chore(deps): …` commit with a body that
   lists every pin moved (old → new) and every pin deliberately held back
   and why. Conventional Commits; end with an `Assisted-by: <model name>`
   trailer and **no `Co-Authored-By`** (AGENTS.md § Commits).

Never start the control plane or the frontend (`make dev`, `run-*`) to check
an update; ask the user when a check needs the running stack (`make e2e`,
`make test-integration`).

## Inventory: every place a dependency is declared

### 1. Go modules and toolchain - `go.mod`, `go.sum`
- `go 1.26.0` and `toolchain go1.26.10`: move `toolchain` to the newest
  patch (and the `go` line only when a new minor is wanted). CI reads it
  (`go-version-file: go.mod`).
- Every `require` (direct and `// indirect`): `go list -m -u all`, then
  `go get -u ./cmd/... ./internal/...` (never `./...`: it walks `data/`), or
  per module `go get mod@vX`. Then `make tidy`.
- Majors (`/v2`, `/v3`) are not found by `-u`: check each direct module's
  repository for a newer major. `github.com/docker/docker …+incompatible`
  follows the Docker engine API: check `github.com/moby/moby` releases.
- Modules with behaviour pinned by tests or docs, re-check after moving:
  - `github.com/refraction-networking/utls`, `github.com/imroc/req`:
    the search fingerprint (§10).
  - `github.com/modelcontextprotocol/go-sdk`: `internal/mcpx` (OAuth,
    transports).
  - `go.mau.fi/whatsmeow`: WhatsApp breaks when it is old; update it often.
  - `github.com/gotd/td`, `github.com/bwmarrin/discordgo`,
    `github.com/slack-go/slack`: channel adapters.
  - `codeberg.org/readeck/go-readability/v2`,
    `github.com/JohannesKaufmann/html-to-markdown/v2`: run the reader's live
    test by hand (`SILO_READER_LIVE=1 go test ./internal/reader -run 'TestLive$' -v`).
  - `github.com/anthropics/anthropic-sdk-go`, `github.com/openai/openai-go`:
    the providers' tests; live tests only by hand, never looped. A new
    major of the Anthropic SDK can move the Messages API version and the
    auth header (v2: API 2026-10-21, `Authorization: Bearer` instead of
    `x-api-key`, `MessageParam` a union): that is a wire change, so it is
    the user's call and needs `TestLive*`, not a sweep item.
  - `github.com/quic-go/quic-go` is only indirect, through `imroc/req`:
    take the newest version `req` still compiles with (`go build
    ./internal/search`), not the newest.
  - `connectrpc.com/connect`: keep the buf Go plugin in step (§8).
  - `github.com/pion/*`: voice calls (`feature_voice_call_test.go`).
  - `github.com/emersion/go-imap/v2`, `go-smtp`, `go-webdav`: Email,
    Calendar, Mail.

### 2. Web - `web/package.json`, `web/pnpm-lock.yaml`, `web/pnpm-workspace.yaml`
- `cd web && pnpm outdated`, then `pnpm update --latest` (or per package),
  which rewrites the lockfile. Majors (React, Vite, TanStack, Tailwind,
  TypeScript, i18next, `@bufbuild/*`, `@connectrpc/*`) need their migration
  notes read.
- `@ricky0123/vad-web` and the `onnxruntime-web` it pulls in:
  `web/vite-vad.ts` copies their assets by path; check the paths still exist
  after the update.
- `typescript`: `web/src/i18n_test.ts` uses the compiler's JS API
  (`ts.createSourceFile`), which TypeScript 7 (the native compiler) does not
  expose. Moving to 7 means porting that test first. TS 7's `tsc` is still
  worth a one-off run: it reports case-only filename clashes that 6 misses.
- `@playwright/test`: the e2e runner (the user's stack runs it). Never run
  `playwright install`.
- `@bufbuild/protobuf` / `@connectrpc/connect*` must match the code the
  `buf.build/bufbuild/es` and `connectrpc/es` plugins generate (§8): after
  moving them, `make proto` and check `web/src/gen` still typechecks.
- `pnpm-workspace.yaml` `allowBuilds`: a new package with an install script
  must be added there deliberately (allow or deny), never blanket-allowed.
- The pnpm version itself: see §5 (CI and the cp image).

### 3. Container base images - `images/*/Containerfile`
Every `FROM` is `tag@sha256:digest`; move tag and digest together.

| File | Pins |
| --- | --- |
| `images/bot/Containerfile` | `node:<lts>-trixie-slim@…`, `lightpanda/browser:<release>@…` (releases since 1.0: `gh release list -R lightpanda-io/browser`), `ghcr.io/astral-sh/uv:<ver>@…`, `debian:trixie-slim@…` |
| `images/cp/Containerfile` | `node:<lts>-trixie-slim@…` (web build), `golang:<ver>-trixie@…`, `debian:trixie-slim@…` |
| `images/drive/Containerfile` | `rclone/rclone:<ver>@…`, `debian:trixie-slim@…` |
| `images/egress/Containerfile` | `debian:trixie-slim@…` |
| `images/mcp-stdio/Containerfile` | `node:<lts>-trixie-slim@…` (and its `base.name` label), `ghcr.io/astral-sh/uv:<v>@…` (the same pin as the bot image's) |
| `images/caddy/Containerfile` | `caddy:<ver>-builder@…`, `caddy:<ver>@…` |
| `images/os/Containerfile` | `quay.io/fedora/fedora-bootc:<N>`: **tag only on purpose** (quay.io deletes old digests). Check whether a newer stable Fedora is out (`curl -fsS https://fedoraproject.org/releases.json`; the quay tags run ahead to rawhide); moving it is a deliberate change, say so, do not do it silently |

Also re-check whether Node's Active LTS line moved (an even major turns LTS
each October; all three `node:` pins, the mcp-stdio `base.name` label,
CI's `node-version` and AGENTS.md's mention move together) and whether Debian's stable moved past trixie (all
`*-trixie*` pins move together, and the apt package names in the bot image
may change).

### 4. Pinned downloads inside the images
- `images/bot/Containerfile`:
  - pip: `playwright==`, `python-pptx==`, `pdfplumber==`, `duckdb==`,
    `"yt-dlp[default]=="` (`pip index versions <pkg>`). yt-dlp breaks
    against YouTube quickly: always move it.
  - `npm install -g pnpm@<ver>` (the Bot's own pnpm; independent of §5).
  - uBlock Origin Lite: `ADD --checksum=sha256:… https://github.com/uBlockOrigin/uBOL-home/releases/download/<ver>/uBOLite_<ver>.chromium.zip`.
    Move the URL and recompute the checksum (`curl -L … | shasum -a 256`).
  - apt packages are unpinned (they follow the Debian digest); after moving
    `debian:trixie-slim` the image's last `RUN` check proves every tool is
    still there.
  - `images/bot/chromium/stealth/` is ours (no upstream).
- `images/caddy/Containerfile`: `xcaddy build v<caddy ver>` and every
  `--with github.com/caddy-dns/<provider>@<ver>` (pseudo-versions included:
  check the repository for a tag since). See § Must move together.
- `images/cp/Containerfile`: `npm install -g pnpm@<ver>` (must equal CI, §5).
- `images/os/Containerfile`: `dnf install` packages follow the Fedora base.
- `images/os/rootfs/etc/containers/systemd/users/1000/silodb.container`:
  `Image=docker.io/pgvector/pgvector:pg17@sha256:…` - move the digest to the
  newest `pg17` (newest pgvector on Postgres 17). A Postgres major change is
  a data migration: do not do it in a sweep.
- `Makefile`: `SEED_IMAGE ?= docker.io/library/alpine:<ver>@sha256:…` (move
  both); `BIB_IMAGE ?= quay.io/centos-bootc/bootc-image-builder:latest`
  (tag only on purpose, nothing to move).

### 5. CI - `.github/workflows/checks.yml`, `images.yml`
- Every `uses: owner/action@<commit sha> # vN`: find the newest release
  (`gh release list -R owner/action`), resolve its tag to a commit
  (`gh api repos/owner/action/commits/<tag> --jq .sha`), replace the SHA
  **and** the `# vN` comment. Actions in use: `actions/checkout`,
  `actions/setup-go`, `actions/setup-node`, `actions/upload-artifact`,
  `actions/download-artifact`, `pnpm/action-setup`,
  `docker/setup-buildx-action`, `docker/login-action`,
  `docker/build-push-action` (grep `uses:` again in case one was added).
- `pnpm/action-setup` `version:` and `setup-node` `node-version:`.

### 6. Dev dependencies - `dev/compose.yml`
- `pgvector/pgvector:pg17` (follow the tag; keep the major equal to
  SiloOS's `silodb.container`), `rnwood/smtp4dev:latest`,
  `tomsquest/docker-radicale:latest`, `ghcr.io/dexidp/dex:latest`, the
  commented `lightpanda/browser:nightly`. These are floating tags: nothing
  to edit, but tell the user `make db-down && podman pull … && make db-up`
  refreshes them (you may run `make db-up`; never `make db-reset`).
- `dev/dex/config.yaml`, `dev/radicale/`: check a dex or radicale major did
  not rename a key.

### 7. Claude Code binary - `Makefile` `claude-bin`
- `npm pack @anthropic-ai/claude-agent-sdk-$(CLAUDE_PLATFORM)` is always the
  newest. After a new Claude Code release, the AGENTS.md § Claude Agent rule
  applies: re-run `TestLiveClaude` (tag `live`) by hand, and
  `internal/llm/claude*.go` launch flags must still be accepted.

### 8. Protobuf toolchain - `proto/buf.gen.yaml`, `proto/buf.gen.swift.yaml`
- `buf` itself (run as `go run github.com/bufbuild/buf/cmd/buf@<ver>` here):
  newest release.
- Remote plugins: `buf.build/connectrpc/go:v<ver>` is pinned; the others
  (`protocolbuffers/go`, `bufbuild/es`, `connectrpc/es`, `apple/swift`,
  `connectrpc/swift`) float. Pin-check `connectrpc/go` against
  `connectrpc.com/connect` in `go.mod`, the es plugins against the web's
  `@bufbuild/protobuf`/`@connectrpc/*` majors, the Swift ones against
  `swift-protobuf`/`connect-swift` (§9).
- After any of these: `make proto`, the Swift stubs
  (`buf generate proto --template proto/buf.gen.swift.yaml`), rebuild CP,
  worker, drive, egress; commit the regenerated `gen/`, `web/src/gen`,
  `ios/SiloClient/Sources/SiloClient/Generated`.

### 9. iOS - `ios/SiloClient/Package.swift`, `Package.resolved`, the pbxproj
- `connect-swift` (`from:`), `swift-protobuf` (`from:`), `stasel/WebRTC`
  (`exact:`), `onnxruntime-swift-package-manager` (`exact:`): check each
  repository's newest tag; move `exact:` pins deliberately (WebRTC and
  onnxruntime are binaries; `SiloCall` must still build and the Silero model
  still load).
- **`microsoft/onnxruntime-swift-package-manager` disappeared from GitHub on
  2026-11-18**, and nothing resolves while Package.swift names it. Check
  whether it is back or the user has chosen a replacement source; never
  switch to a third-party mirror on your own.
- `ios/silo-agent.xcodeproj/project.xcworkspace/xcshareddata/swiftpm/Package.resolved`:
  resolve again (`xcodebuild -resolvePackageDependencies` or in
  `ios/SiloClient`: `swift package update`), so the transitive
  `swift-nio*`, `swift-atomics`, `swift-collections`, `swift-system` move.
- Deployment targets (`IPHONEOS_/WATCHOS_/MACOSX_DEPLOYMENT_TARGET` in the
  pbxproj, `platforms:` in Package.swift) and `swift-tools-version`: report,
  do not raise without the user.
- Verify with `swift test` in `ios/SiloClient` and an `xcodebuild` build
  (memory `ios-build-and-test`).

### 10. Versions baked into Go code (no package manager sees these)
- `internal/search/http.go`: `chromeUA` (Chrome/145), `chromeBrands`,
  `utls.HelloChrome_133`, the HTTP/2 SETTINGS and header order. When stable
  Chrome is several versions ahead, move all of it together from a real
  Chrome's values (never guessed), check utls for a newer `HelloChrome_*`,
  and run `TestFingerprint` (live) by hand. `internal/reader` uses the same
  client (`search.Chrome()`).
- `internal/reader/youtube.go`: `androidVersion` (the YouTube Android client
  the player API is asked as). Move it when YouTube refuses the old one.
- `internal/llm/cartesia.go`: `cartesiaVersion` (the `Cartesia-Version`
  header). Grep `internal/llm` for other dated API versions
  (`grep -rnE '"20[0-9]{2}-[0-9]{2}-[0-9]{2}"' internal/llm`) and check each
  vendor's changelog.
- `internal/config/config.go`: `DefaultModel`, `DefaultEmbeddingModel`,
  `DefaultTranscribeModel`, `DefaultSpeechModel`. Report newer models a
  provider offers; changing a default is the user's call (it changes cost
  and behaviour), and `docs/CONFIGURATION.md` and AGENTS.md name them.
- `internal/config/egress.go`: the built-in threat feed URLs: fetch each
  (`curl -s -o /dev/null -w '%{http_code}' -r 0-100 <url>`) and check that it
  is maintained. A feed that is gone (abuse.ch's URLhaus and ThreatFox host files
  answered 410 Gone in 2026-11 and were removed) comes out of the list and
  out of every sentence that counts the feeds (CONFIGURATION.md, AGENTS.md,
  the Network settings help in `web/src/admin/fields.ts` and both locales);
  adding a new feed is a security-policy choice: report it.
- `internal/catalog/connectors.json`: STDIO presets run `npx -y <package>`
  unversioned (`server-perplexity-ask`, `mongodb-mcp-server`,
  `@bytebase/dbhub`, `@azure/mcp`, `@heroku/mcp-server`,
  `obsidian-mcp-server`; grep `stdio_args` for new ones): check each package
  still exists under that name and is not deprecated or renamed. HTTP
  presets' URLs: check none moved. `npm view <pkg> version deprecated`;
  a deprecated package moves to its vendor's official one. The catalog
  seeds insert-if-absent, so an existing library row keeps the old
  arguments until Re-add defaults: say so.
- `internal/drives/catalog/*/drive.yaml` with
  `internal/drives/testdata/rclone_options.json`: tied to rclone (§ Must
  move together).

### 11. Docs that state versions
- `docs/DEVELOPMENT.md` § Prerequisites (Go, Node, pnpm, Postgres),
  `AGENTS.md` (Node 24 image, Chrome 145 / utls 133, rclone notes, pgvector,
  "checked 2026-xx" dates on vendor protocols), `images/os/README.md`
  (Fedora), `docs/CONFIGURATION.md` (default models, images). Update the
  sentence in the same commit as the pin it describes.

### 12. Anything new
Before reporting, sweep for pins this list does not know yet:

```sh
git ls-files | grep -Ei 'go\.mod|package\.json|Containerfile|compose|Package\.swift|requirements|pyproject|\.ya?ml$|\.container$' 
git grep -nE '@sha256:|==[0-9]|@v[0-9]+\.[0-9]|exact: "|from: "|--checksum' -- ':!**/go.sum' ':!web/pnpm-lock.yaml' ':!gen' ':!web/src/gen'
```

Add whatever turns up to this skill (`.claude/skills/update-deps/SKILL.md`)
in the same commit.

## Must move together

- **Debian `trixie-slim` digest**: the same digest in bot, cp, drive,
  egress. One value everywhere.
- **`node:<lts>-trixie-slim` digest**: bot, cp, mcp-stdio. One value.
- **`ghcr.io/astral-sh/uv`**: bot and mcp-stdio. One value.
- **pnpm**: `images/cp/Containerfile` and `checks.yml` `version:` are the
  same version, and that version must read `web/pnpm-lock.yaml`
  (lockfileVersion). The bot image's pnpm is the Bot's own tool and may be
  newer.
- **Go**: `go.mod` `toolchain go1.X.Y` = the `golang:1.X.Y-trixie` base in
  `images/cp/Containerfile`.
- **Caddy**: `caddy:<v>-builder`, `xcaddy build v<v>`, `caddy:<v>` all the
  same `<v>`. Each `caddy-dns` module moved means checking its provider's
  field names in `internal/caddy/dns.go` against the libdns provider's
  source; `TestDNSProvidersAreBuilt` reads the Containerfile list.
- **rclone**: moving `rclone/rclone` means regenerating
  `internal/drives/testdata/rclone_options.json` from the new image
  (`rclone config providers`, read by `internal/drives/drives_test.go`), re-running the
  drive template tests, `TestJournalReadsRcloneLines`, and
  `go test -tags engine ./internal/dockerx -run TestDriveEngine` (the
  journal reads rclone's DEBUG log lines, which a release may reword).
- **Lightpanda**: digest moved → the reader's render checks
  (`feature_webread_test.go`, `cmd/silo-worker/render_test.go`) and the
  flags in `cmd/silo-worker/render.go` (`fetch --json --dump html`,
  `--ca-cert`, `--block-private-networks`) against `lightpanda --help`.
- **Connect / protobuf**: `connectrpc.com/connect` ↔ buf `connectrpc/go`
  plugin; web `@connectrpc/*` / `@bufbuild/protobuf` ↔ es plugins; iOS
  `connect-swift` / `swift-protobuf` ↔ Swift plugins. Regenerate after.
- **Chrome fingerprint**: UA, brands, utls hello, SETTINGS, header order as
  one set (§10).
- **Postgres major**: dev compose `pgvector:pgNN` and SiloOS `silodb` the
  same major; never changed in a sweep.

## Lookup commands

```sh
# Go
go list -m -u all | grep '\['             # modules with an update
go list -m -versions <module>              # every version of one
# Web
cd web && pnpm outdated
# Container tags and digests (podman is the engine here)
skopeo list-tags docker://docker.io/library/caddy | tail
skopeo inspect --format '{{.Digest}}' docker://docker.io/library/debian:trixie-slim
#   no skopeo (the user's Mac has none): ask the registry for the index
#   digest (multi-arch), never one platform's. Docker Hub:
tok=$(curl -fsS "https://auth.docker.io/token?service=registry.docker.io&scope=repository:library/debian:pull" | jq -r .token)
curl -fsSI -H "Authorization: Bearer $tok" \
  -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json' \
  https://registry-1.docker.io/v2/library/debian/manifests/trixie-slim | grep -i docker-content-digest
curl -fsS -H "Authorization: Bearer $tok" "https://registry-1.docker.io/v2/library/debian/tags/list?n=10000" | jq -r '.tags[]'
#   ghcr.io: token from https://ghcr.io/token?scope=repository:<repo>:pull, host ghcr.io
#   (its tag list pages: prefer `gh release list` for the version).
#   quay.io: curl -fsS 'https://quay.io/api/v1/repository/<org>/<repo>/tag/?onlyActiveTags=true&limit=100'
# A same-tag rebuild moves the digest too (caddy's -builder): compare digests, not only tags.
# GitHub releases and action SHAs (pin to the release tag's commit, comment `# vX.Y.Z`)
gh release list -R <owner>/<repo> --limit 5
gh api repos/<owner>/<repo>/commits/<tag> --jq .sha
# pip: the local pip index can be stale, ask PyPI
curl -fsS https://pypi.org/pypi/yt-dlp/json | jq -r .info.version
# npm
npm view <pkg> version deprecated
# Go: is there a new major of a direct module?
curl -s https://proxy.golang.org/<module>/v<N+1>/@latest   # 404 = none; rc/beta = not yet
# Checksums of a pinned download
curl -fsSL <url> | shasum -a 256
# Swift
cd ios/SiloClient && swift package update && swift package show-dependencies
```

## Verify

Run what the touched sections need, one step at a time:

| Touched | Run |
| --- | --- |
| §1 Go | `make tidy`, `make vet`, `make build-cp`, `make build-worker build-bridge build-drive build-egress`, `make test` (needs `make db-up`; container tier needs `make images`) |
| §2 web | `cd web && npx tsc --noEmit`, `make test-web`, `make web-build`; ask the user to run `make e2e` |
| §3/§4 images | `make bot-image`, `make stdio-image`, `make drive-image`, `make egress-image`, `make caddy-image`, `make cp-image` (each alone), then `make test-containers`; `make os-image` when §3's Fedora or silodb moved |
| §5 CI | push nothing; check workflow syntax by reading the diff, the run happens on the user's push |
| §8 proto | `make proto`, Swift stubs, rebuild CP + worker + drive + egress, `make test-fast` |
| §9 iOS | `swift test` in `ios/SiloClient`, `xcodebuild` build of scheme `MyApp` |
| §10 baked | the named tests; live tests (`TestFingerprint`, reader `TestLive`, `TestLiveClaude`) by hand, once, never looped |

A running Bot keeps its old image until stopped and started (or Reset
container): tell the user after an image update.

## Report

End with a table of **every** inventory line: what it was, what it is now,
or "newest already", or "held back: <reason>" (a breaking major, a
deliberate tag-only pin, a default model that is the user's call, a
Postgres major), and which checks ran and passed or failed (with output for
failures).
