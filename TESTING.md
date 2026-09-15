# Testing

The default test suite should be deterministic, local, and fast. Runtime state under `data/` is not part of the test input and must not be traversed by the Go package pattern.

## Current Commands

```sh
# Default unit and component suite
CGO_ENABLED=0 go test ./cmd/... ./internal/...

# Concurrency-sensitive pass before merging changes to worker, hub, or runs
CGO_ENABLED=1 go test -race ./cmd/... ./internal/...

# Frontend typecheck and production compilation
cd web && pnpm build

# Opt-in network checks
SILO_LIVE_TESTS=1 CGO_ENABLED=0 go test ./internal/mcpx -run Live
```

Do not use `go test ./...` from the repository root. Go walks ignored directories as well as source directories; a container-created `data/bots/*/chrome-profile` can be inaccessible to the host user.

## Test Tiers

1. **Unit tests**: pure parsing, formatting, authorization decisions, path validation, masking, and generated tool output. These should have no network, Docker, display, or model dependency.
2. **Component tests**: in-process SQLite, `httptest`, fake Docker hosts, fake MCP servers, and Connect RPC handlers. Most current `internal/app` tests belong here and should verify user-visible state transitions rather than private helper details.
3. **Worker tests**: filesystem, command cancellation, PTY, desktop argument validation, and tool/skill synchronization. Keep these runnable on a host without X11 by faking process boundaries; reserve actual X11 checks for a separate smoke job.
4. **System smoke tests**: start the control plane, Vite, a real Bot image, and a minimal fake MCP server. Verify sign-in, Bot startup/heartbeat, one chat run, one approval, file transfer, and console/VNC connection. These are slower and should run on demand or in CI on changes to protocols, images, lifecycle, or frontend routing.
5. **Live integration checks**: external MCP providers and web services. They are diagnostic only, opt-in with `SILO_LIVE_TESTS=1`, and must never gate ordinary unit/component runs.

## Rationalization

The current suite has useful coverage in security, lifecycle recovery, MCP transport, worker filesystem boundaries, and tool generation. The main problems are organization and missing seams, not simply test count:

- Keep the existing focused pure tests.
- Split the large `internal/app` tests by boundary: lifecycle, chat/run streaming, approvals/security, connectors, files/skills, and HTTP/Connect API.
- Prefer public behavior through handlers or service methods over asserting implementation maps and helper output.
- Give every fake host and fake MCP server failure controls for timeout, disconnect, stale token, and malformed response paths.
- Add a small protocol contract suite shared by the CP and worker for heartbeat, command IDs, run IDs, cancellation, and error propagation.
- Add frontend tests only for routing, stream reconnection, approval rendering, and file-browser state; do not snapshot the whole design system.
- Add one end-to-end smoke path before expanding UI coverage.

The first high-value additions should be: a CP/worker heartbeat-and-cancellation test, a persisted `StreamRun` reconnect test, an approval allow/deny/allow-once test through the API boundary, and the minimal Podman smoke test. These cover the seams where unit tests currently provide the least confidence.
