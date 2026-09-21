# Toolchain / environment. Everything can be overridden on the command line,
# e.g. `make DOCKER_HOST=unix:///run/podman/podman.sock run-control`.
export CGO_ENABLED ?= 0
export DOCKER_HOST ?= unix:///run/user/1000/podman/podman.sock

GO          ?= go
PNPM        ?= pnpm
PODMAN      ?= podman
CONTAINER_ARCH ?= $(shell $(PODMAN) info --format '{{.Host.Arch}}' 2>/dev/null || $(GO) env GOARCH)

BOT_IMAGE   ?= localhost/silo-bot:v1
STDIO_IMAGE ?= localhost/silo-mcp-stdio:v1

# tmux session name used by `make dev`.
SESSION ?= silo

# --- watchexec -------------------------------------------------------------
# A sane watch: only sources, only relevant extensions, a real debounce so a
# flurry of editor writes coalesces into one restart, and SIGTERM + a grace
# period so the CP can release :8080 before the next build starts.
WATCH_PATHS := cmd internal go.mod go.sum silo.yaml
WATCH_EXTS  := go,mod,sum,md,yaml,yml,json
WATCH_OPTS  := \
	--shell=sh \
	$(foreach p,$(WATCH_PATHS),--watch $(p)) \
	--exts $(WATCH_EXTS) \
	--ignore '**/node_modules/**' \
	--ignore '**/*~' --ignore '**/*.tmp' --ignore '**/*.swp' --ignore '**/.*.swp' \
	--ignore data --ignore bin --ignore web --ignore tmp --ignore .git \
	--no-vcs-ignore \
	--debounce 750ms \
	--on-busy-update restart \
	--stop-signal SIGTERM \
	--stop-timeout 5s

.DEFAULT_GOAL := help

.PHONY: help install deps web-install build build-cp build-worker \
	build-bridge build-all images bot-image stdio-image run run-control \
	run-frontend watch-control dev attach test fmt vet tidy proto rebuild \
	clean clean-images cleanup reset-data

## ---------------------------------------------------------------------------

help: ## Show this help
	@printf "Silo Agent — common tasks\n\n"
	@grep -hE '^[a-zA-Z0-9_-]+:.*?## ' $(MAKEFILE_LIST) \
		| sort \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

## --- setup -----------------------------------------------------------------

install: silo.yaml deps web-install ## Install Go modules + web deps, seed silo.yaml

silo.yaml: ## Create silo.yaml from the example if it is missing
	@test -f silo.yaml || { cp silo.yaml.example silo.yaml; echo "created silo.yaml — add providers.openrouter.api_key"; }

deps: ## Download Go module dependencies
	$(GO) mod download

web-install: ## Install frontend dependencies
	$(PNPM) --dir web install

## --- build -----------------------------------------------------------------

build: build-cp ## Build the control plane (alias for build-cp)

build-cp: ## Build the control plane to bin/silo
	$(GO) build -o bin/silo ./cmd/silo

build-worker: ## Cross-build the in-container worker to bin/silo-worker
	GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GO) build -o bin/silo-worker ./cmd/silo-worker

build-bridge: ## Cross-build the STDIO MCP bridge to bin/silo-mcp-bridge
	GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GO) build -o bin/silo-mcp-bridge ./cmd/silo-mcp-bridge

build-all: build-cp build-worker build-bridge ## Build control plane, worker, and bridge

images: bot-image stdio-image ## Build both container images

bot-image: build-worker ## Build the local bot image
	$(PODMAN) build -t $(BOT_IMAGE) -f botimage/Containerfile .

stdio-image: build-bridge ## Build the local STDIO MCP sidecar image
	$(PODMAN) build -t $(STDIO_IMAGE) -f mcpimage/Containerfile .

rebuild: ## Full image rebuild via rebuild.sh (MODE=cp|bot|stdio|all)
	./rebuild.sh $(or $(MODE),all)

## --- run -------------------------------------------------------------------

run: run-control ## Alias for run-control

run-control: build-cp ## Build and run the control plane on :8080
	./bin/silo

run-frontend: web-install ## Run the Vite dev server on :5173
	$(PNPM) --dir web dev

watch-control: ## Run the control plane under watchexec (auto-rebuild on change)
	watchexec $(WATCH_OPTS) -- '$(GO) build -o bin/silo ./cmd/silo && exec ./bin/silo'

dev: ## Start the watchexec CP + Vite in a tmux session
	@command -v tmux >/dev/null || { echo "tmux not found" >&2; exit 1; }
	@tmux kill-session -t $(SESSION) 2>/dev/null || true
	@tmux new-session -d -s $(SESSION) -n dev -c "$(CURDIR)" '$(MAKE) watch-control'
	@tmux split-window -h -t $(SESSION):dev -c "$(CURDIR)" '$(MAKE) run-frontend'
	@tmux select-layout -t $(SESSION):dev even-horizontal >/dev/null
	@tmux select-pane -t $(SESSION):dev.0
	@tmux attach -t $(SESSION) || true
	@echo "silo dev session: tmux attach -t $(SESSION)"

attach: ## Attach to the running dev tmux session
	tmux attach -t $(SESSION)

## --- quality ---------------------------------------------------------------

test: ## Run the Go test suite
	$(GO) test ./cmd/... ./internal/...

fmt: ## Format Go sources
	$(GO) fmt ./cmd/... ./internal/...

vet: ## Run go vet
	$(GO) vet ./cmd/... ./internal/...

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

proto: ## Regenerate protobuf stubs (Go + TS)
	buf generate

## --- cleanup ---------------------------------------------------------------

clean: ## Remove build artifacts (bin/, web/dist)
	rm -rf bin web/dist

clean-images: ## Remove the local bot and STDIO images
	-$(PODMAN) rmi -f $(BOT_IMAGE) $(STDIO_IMAGE)

cleanup: ## Force-remove every silo-* container
	@ids=$$($(PODMAN) ps -aq --filter name='^silo-' || true); \
	if [ -n "$$ids" ]; then \
		$(PODMAN) rm -f $$ids; \
	else \
		echo "no silo containers"; \
	fi

reset-data: ## Delete ./data (SQLite, chats, workspaces). Asks first.
	@printf "Delete ./data (SQLite + bot workspaces)? [y/N] "; read -r ans; \
	[ "$$ans" = y ] || { echo "aborted"; exit 1; }; \
	rm -rf data && echo "data/ removed"
