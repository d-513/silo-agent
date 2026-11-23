# Toolchain / environment. Everything can be overridden on the command line,
# e.g. `make DOCKER_HOST=unix:///run/podman/podman.sock run-control`.
# cgo is on for what runs on this host (the control plane, the tests). What
# runs inside a container is built static with CGO_ENABLED=0 below: a cgo
# binary built here links this host's glibc loader, which no image has.
export CGO_ENABLED ?= 1
export DOCKER_HOST ?= unix:///run/user/1000/podman/podman.sock

GO          ?= go
# The npm package that carries Claude Code for this machine (claude-bin).
CLAUDE_PLATFORM ?= $(shell uname -s | tr A-Z a-z)-$(shell uname -m | sed 's/x86_64/x64/;s/aarch64/arm64/')
PNPM        ?= pnpm
PODMAN      ?= podman
COMPOSE     ?= $(PODMAN) compose
DEV_COMPOSE := $(COMPOSE) -f dev/compose.yml
PG_CONTAINER ?= silodb-postgres
CONTAINER_ARCH ?= $(shell $(PODMAN) info --format '{{.Host.Arch}}' 2>/dev/null || $(GO) env GOARCH)

# The release a build is (internal/version). Every target builds dev: local
# images (localhost/silo-*:dev, made by `make images`) that are never pulled.
# Only `make release` and `make nightly` set VERSION and REGISTRY, which bakes
# the registry and tag into the binaries, so a prod control plane always runs
# the images of the commit it was built from. Set them on the command line
# only (the environment does not reach these two).
VERSION       := dev
REGISTRY      :=
PROD_REGISTRY := ghcr.io/d-513
IMAGE_REPO     = $(or $(REGISTRY),localhost)
BOT_IMAGE    ?= $(IMAGE_REPO)/silo-bot:$(VERSION)
STDIO_IMAGE  ?= $(IMAGE_REPO)/silo-mcp-stdio:$(VERSION)
DRIVE_IMAGE  ?= $(IMAGE_REPO)/silo-drive:$(VERSION)
EGRESS_IMAGE ?= $(IMAGE_REPO)/silo-egress:$(VERSION)
# The control plane's own image (images/cp/), which runs the four above.
CP_IMAGE     ?= $(IMAGE_REPO)/silo:$(VERSION)
# Caddy with the DNS providers Silo configures (images/caddy/): SiloOS runs it.
CADDY_IMAGE  ?= $(IMAGE_REPO)/silo-caddy:$(VERSION)
# SiloOS (images/os/): a bootable image of the control plane and Caddy of the same
# VERSION (a dev build runs the nightly's). images/os/README.md § Channels.
OS_IMAGE      ?= $(IMAGE_REPO)/silo-os:$(VERSION)
OS_REPOSITORY := $(PROD_REGISTRY)/silo-os
# What a disk is made from, and so what the machine follows: local (the image
# `make os-image` built, which follows no channel) or a channel of the
# published repository: latest, master, or a version tag (v0.4.2).
OS_CHANNEL    ?= local
OS_SOURCE      = $(if $(filter local,$(OS_CHANNEL)),$(OS_IMAGE),$(OS_REPOSITORY):$(OS_CHANNEL))
# os-disk's format: qcow2 (libvirt, Proxmox, QEMU, Incus, OpenStack), raw (dd
# onto a drive), vmdk, vhd (Azure), ami (AWS), gce (Google Cloud).
OS_DISK_TYPE  ?= qcow2
OS_OUT        ?= build/os
# What os-incus calls the image it imports.
OS_INCUS_ALIAS ?= silo-os
# bootc-image-builder publishes only :latest, and quay.io drops the digests
# it replaces, so it is followed by tag (--pull=newer).
BIB_IMAGE     ?= quay.io/centos-bootc/bootc-image-builder:latest
SEED_IMAGE    ?= docker.io/library/alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
# Packages the control plane image compiles at once (go build -p); empty is
# one per CPU. Its builder needs about 4 GB of memory whatever this says.
CP_BUILD_JOBS ?=
# The commit an image is built from, for its org.opencontainers.image.revision label.
REVISION    ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)$(shell git diff --quiet HEAD 2>/dev/null || echo -dirty)
LDFLAGS      = -X silo.agent/internal/version.Version=$(VERSION) \
               -X silo.agent/internal/version.Commit=$(REVISION) \
               -X silo.agent/internal/version.Registry=$(REGISTRY)
# Go build tags. `webui` compiles the web console into the control plane
# (internal/webui): run `make web-build` first, or use `make build-cp-ui`.
TAGS         :=
GOBUILD      = $(GO) build $(if $(TAGS),-tags '$(TAGS)') -ldflags '$(LDFLAGS)'
IMAGE_ARGS   = --build-arg REVISION=$(REVISION) --build-arg VERSION=$(VERSION)

# tmux session name used by `make dev`.
SESSION ?= silo

# --- watchexec -------------------------------------------------------------
# A sane watch: only sources, only relevant extensions, a real debounce so a
# flurry of editor writes coalesces into one restart, and SIGTERM + a grace
# period so the CP can release :8080 before the next build starts.
WATCH_PATHS := cmd internal go.mod go.sum
WATCH_EXTS  := go,mod,sum,md,yaml,yml,json
WATCH_OPTS  := \
	--shell=sh \
	$(foreach p,$(WATCH_PATHS),--watch $(p)) \
	--exts $(WATCH_EXTS) \
	--ignore '**/node_modules/**' \
	--ignore '**/*~' --ignore '**/*.tmp' --ignore '**/*.swp' --ignore '**/.*.swp' \
	--ignore data --ignore build --ignore bin --ignore web --ignore tmp --ignore .git \
	--no-vcs-ignore \
	--debounce 750ms \
	--on-busy-update restart \
	--stop-signal SIGTERM \
	--stop-timeout 5s

.DEFAULT_GOAL := help

.PHONY: help claude-bin install deps web-install brand build build-cp build-worker \
	build-bridge build-drive build-egress build-siloosd build-all images bot-image stdio-image drive-image egress-image run run-control \
	run-frontend watch-control dev attach release nightly publish cp-image caddy-image os-image os-disk os-iso os-incus os-seed web-build build-cp-ui test-unit test-web test test-fast test-containers \
	test-integration e2e fmt vet tidy proto rebuild \
	clean clean-images cleanup reset-data db-up db-down db-psql db-reset \
	db-check

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

brand: ## Export brand/ to the favicons, app icons and crest tokens (needs ImageMagick with librsvg, and Node)
	python3 -I brand/export.py

web-install: ## Install frontend dependencies
	$(PNPM) --dir web install

## --- build -----------------------------------------------------------------

build-cp: ## Build the control plane to build/bin/silo
	$(GOBUILD) -o build/bin/silo ./cmd/silo

web-build: web-install ## Build the web console into internal/webui/dist, where the webui build tag embeds it
	$(PNPM) --dir web exec vite build --outDir ../internal/webui/dist --emptyOutDir

build-cp-ui: web-build ## Build build/bin/silo with the web console inside (what a release runs; dev builds use Vite)
	$(MAKE) build-cp TAGS=webui VERSION=$(VERSION) REGISTRY=$(REGISTRY)

build-worker: ## Cross-build the in-container worker to build/bin/silo-worker
	CGO_ENABLED=0 GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GOBUILD) -o build/bin/silo-worker ./cmd/silo-worker

build-bridge: ## Cross-build the STDIO MCP bridge to build/bin/silo-mcp-bridge
	CGO_ENABLED=0 GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GOBUILD) -o build/bin/silo-mcp-bridge ./cmd/silo-mcp-bridge

build-drive: ## Cross-build the drive sidecar to build/bin/silo-drive
	CGO_ENABLED=0 GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GOBUILD) -o build/bin/silo-drive ./cmd/silo-drive

build-egress: ## Cross-build the network filter gateway to build/bin/silo-egress
	CGO_ENABLED=0 GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GOBUILD) -o build/bin/silo-egress ./cmd/silo-egress

build-siloosd: ## Cross-build the SiloOS host daemon to build/bin/siloosd
	CGO_ENABLED=0 GOOS=linux GOARCH=$(CONTAINER_ARCH) $(GOBUILD) -o build/bin/siloosd ./cmd/siloosd

build-all: build-cp build-worker build-bridge bot-image stdio-image drive-image egress-image ## Build control plane, worker, bridge, drive sidecar, egress gateway

images: bot-image stdio-image drive-image egress-image ## Build all container images

## --- release ---------------------------------------------------------------
# Needs `podman login ghcr.io` with a token that may write packages. Builds
# for this machine's architecture (CONTAINER_ARCH) only.

release: ## Publish a release: make release VERSION=v0.4.2 (images on ghcr.io/d-513, :latest too, and a build/bin/silo that runs them)
	@case "$(VERSION)" in v[0-9]*) ;; *) echo "make release needs VERSION=vX.Y.Z"; exit 1 ;; esac
	@git diff --quiet HEAD || { echo "make release needs a clean tree"; exit 1; }
	$(MAKE) publish VERSION=$(VERSION) REGISTRY=$(PROD_REGISTRY) MOVING=latest

nightly: ## Publish a nightly: images tagged master-<sha> (and :master) on ghcr.io/d-513, and a build/bin/silo that runs them
	@git diff --quiet HEAD || { echo "make nightly needs a clean tree"; exit 1; }
	$(MAKE) publish VERSION=master-$(shell git rev-parse --short HEAD) REGISTRY=$(PROD_REGISTRY) MOVING=master

# publish builds every image and the control plane for VERSION/REGISTRY,
# pushes the images under that tag (never moved afterwards), then under
# MOVING, a tag for people to follow. Binaries never name MOVING.
publish: images cp-image caddy-image os-image build-cp-ui
	@test -n "$(REGISTRY)" || { echo "publish needs REGISTRY: use make release or make nightly"; exit 1; }
	@for img in $(BOT_IMAGE) $(STDIO_IMAGE) $(DRIVE_IMAGE) $(EGRESS_IMAGE) $(CADDY_IMAGE) $(CP_IMAGE) $(OS_IMAGE); do \
		$(PODMAN) push $$img || exit 1; \
		if [ -n "$(MOVING)" ]; then \
			moving=$${img%:*}:$(MOVING); \
			$(PODMAN) tag $$img $$moving && $(PODMAN) push $$moving || exit 1; \
		fi; \
	done
	./build/bin/silo version

bot-image: build-worker ## Build the local bot image
	$(PODMAN) build --pull=newer $(IMAGE_ARGS) -t $(BOT_IMAGE) -f images/bot/Containerfile .

stdio-image: build-bridge ## Build the local STDIO MCP sidecar image
	$(PODMAN) build --pull=newer $(IMAGE_ARGS) -t $(STDIO_IMAGE) -f images/mcp-stdio/Containerfile .

drive-image: build-drive ## Build the local drive (rclone) sidecar image
	$(PODMAN) build --pull=newer $(IMAGE_ARGS) -t $(DRIVE_IMAGE) -f images/drive/Containerfile .

# The control plane is built inside the image (cgo against the image's glibc),
# from the Go module only: images/cp/Containerfile.dockerignore stands in for the
# root .containerignore. Docker format, so HEALTHCHECK is kept.
cp-image: ## Build the control plane image (images/cp/; runs the bot, STDIO, drive and egress images of the same VERSION)
	$(PODMAN) build --pull=newer --format docker $(IMAGE_ARGS) --build-arg REGISTRY=$(REGISTRY) --build-arg BUILD_JOBS=$(CP_BUILD_JOBS) \
		--ignorefile images/cp/Containerfile.dockerignore -t $(CP_IMAGE) -f images/cp/Containerfile .

egress-image: build-egress ## Build the local network filter gateway image
	$(PODMAN) build --pull=newer $(IMAGE_ARGS) -t $(EGRESS_IMAGE) -f images/egress/Containerfile .

# Caddy is compiled inside the image (xcaddy, every DNS provider pinned).
caddy-image: ## Build silo-caddy: Caddy with the DNS providers Silo can use (images/caddy/)
	$(PODMAN) build --pull=newer $(IMAGE_ARGS) -t $(CADDY_IMAGE) -f images/caddy/Containerfile .

# SiloOS copies siloosd and images/os/ only (images/os/Containerfile.dockerignore stands in
# for the root .containerignore). REGISTRY and VERSION say which control
# plane and Caddy it runs and which repository its channels are in.
os-image: build-siloosd ## Build SiloOS, the bootc image that runs Silo (images/os/)
	$(PODMAN) build --pull=newer $(IMAGE_ARGS) --build-arg REGISTRY=$(REGISTRY) \
		--ignorefile images/os/Containerfile.dockerignore -t $(OS_IMAGE) -f images/os/Containerfile .

# The rest make installable media with bootc-image-builder, which needs a
# rootful, privileged Podman and the image in root's storage: they run under
# sudo and ask for your password. A machine follows the image name it was
# installed from, so OS_CHANNEL picks both what is installed and what it
# updates from.
.PHONY: os-source
os-source:
	@if [ "$(OS_CHANNEL)" = local ]; then \
		$(PODMAN) image exists $(OS_IMAGE) || { echo "no $(OS_IMAGE): run make os-image first, or pick OS_CHANNEL=latest|master|vX.Y.Z"; exit 1; }; \
		echo "copying $(OS_IMAGE) into root's storage"; \
		$(PODMAN) save $(OS_IMAGE) | sudo $(PODMAN) load; \
	else \
		sudo $(PODMAN) pull $(OS_SOURCE); \
	fi

# bib OUT TYPE: bootc-image-builder makes TYPE from OS_SOURCE into
# OS_OUT/OUT, owned by you. The media are the same for everyone: no user, no
# key, no setting is baked in. A machine gets its login from cloud-init when
# something hands it user-data, and its first admin is shown on its console
# (images/os/README.md § First boot). images/os/bib.toml, when you write one, passes
# bootc-image-builder options of your own (the root's size, the installer's
# disk).
define bib
	mkdir -p $(OS_OUT)/$(1)
	sudo $(PODMAN) run --rm --privileged --pull=newer \
		--security-opt label=type:unconfined_t \
		-v /var/lib/containers/storage:/var/lib/containers/storage \
		-v $(CURDIR)/$(OS_OUT)/$(1):/output \
		$(if $(wildcard images/os/bib.toml),-v $(CURDIR)/images/os/bib.toml:/config.toml:ro) \
		$(BIB_IMAGE) build --type $(2) --rootfs xfs --chown $(shell id -u):$(shell id -g) $(OS_SOURCE)
	@echo "made $(2) of $(OS_SOURCE) in $(OS_OUT)/$(1)"
endef

images/os/cloud-init/user-data.yaml:
	@echo "images/os/cloud-init/user-data.yaml is the VM's user-data: cp images/os/cloud-init/user-data.example.yaml images/os/cloud-init/user-data.yaml and fill it in"; exit 1

os-disk: os-source ## A disk image (OS_DISK_TYPE=qcow2|raw|vmdk|vhd|ami|gce, OS_CHANNEL=local|latest|master|vX.Y.Z; sudo)
	$(call bib,$(OS_DISK_TYPE),$(OS_DISK_TYPE))

os-iso: os-source ## An installer ISO for bare metal; it erases the disk it installs to (OS_CHANNEL; sudo)
	$(call bib,iso,anaconda-iso)

# The qcow2 os-disk made, as an Incus VM image (metadata + disk).
os-incus: ## Import the qcow2 from os-disk into Incus as OS_INCUS_ALIAS (images/os/README.md § Incus)
	@test -f $(OS_OUT)/qcow2/qcow2/disk.qcow2 || { echo "no $(OS_OUT)/qcow2/qcow2/disk.qcow2: run make os-disk first"; exit 1; }
	printf 'architecture: %s\ncreation_date: %s\nproperties:\n  os: SiloOS\n  release: %s\n  description: SiloOS (%s)\n' \
		"$$(uname -m)" "$$(date +%s)" "$(OS_CHANNEL)" "$(OS_SOURCE)" > $(OS_OUT)/qcow2/metadata.yaml
	tar -C $(OS_OUT)/qcow2 -cJf $(OS_OUT)/qcow2/metadata.tar.xz metadata.yaml
	incus image import $(OS_OUT)/qcow2/metadata.tar.xz $(OS_OUT)/qcow2/qcow2/disk.qcow2 --alias $(OS_INCUS_ALIAS) --reuse

# A NoCloud seed: the user-data and meta-data a VM's cloud-init reads from a
# CD labelled cidata, for trying a cloud image locally (images/os/README.md).
os-seed: images/os/cloud-init/user-data.yaml ## The cloud-init seed ISO for a local VM, from images/os/cloud-init/user-data.yaml
	mkdir -p $(OS_OUT)/seed
	cp images/os/cloud-init/user-data.yaml $(OS_OUT)/seed/user-data
	cp $(if $(wildcard images/os/cloud-init/meta-data.yaml),images/os/cloud-init/meta-data.yaml,images/os/cloud-init/meta-data.example.yaml) $(OS_OUT)/seed/meta-data
	$(PODMAN) run --rm -v $(CURDIR)/$(OS_OUT)/seed:/seed:z $(SEED_IMAGE) sh -c \
		'apk add --no-cache -q xorriso && xorriso -as mkisofs -quiet -output /seed/seed.iso -volid cidata -joliet -rock /seed/user-data /seed/meta-data'
	@echo "made $(OS_OUT)/seed/seed.iso"

## --- run -------------------------------------------------------------------

run-control: build-cp ## Build and run the control plane on :8080
	./build/bin/silo

run-frontend: web-install ## Run the Vite dev server on :5173
	$(PNPM) --dir web dev

watch-control: ## Run the control plane under watchexec (auto-rebuild on change)
	watchexec $(WATCH_OPTS) -- "$(GOBUILD) -o build/bin/silo ./cmd/silo && exec ./build/bin/silo"

dev: ## Start the watchexec CP + Vite in a tmux session
	@command -v tmux >/dev/null || { echo "tmux not found" >&2; exit 1; }
	@$(MAKE) --no-print-directory db-check || true
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

test: db-check ## Run the full Go suite (includes the real-container tier; needs images). SHARDS=n splits internal/app
	GO=$(GO) scripts/gotest.sh

test-fast: db-check ## Run the Go suite without the container tier (still needs make db-up)
	SILO_SKIP_CONTAINERS=1 GO=$(GO) scripts/gotest.sh

test-unit: ## The Go tests that need no database and no containers (what CI's checks run)
	$(GO) test $$($(GO) list -f '{{.ImportPath}} {{join .TestImports " "}} {{join .XTestImports " "}}' ./cmd/... ./internal/... | grep -vE 'dbtest|apptest' | cut -d' ' -f1)

test-web: ## Typecheck the web console and run its node unit tests
	cd web && npx tsc --noEmit
	@for f in $$(find web/src -name '*_test.ts' | sort); do node --experimental-strip-types $$f >/dev/null || { echo "FAIL $$f"; exit 1; }; done; echo "web tests ok"

test-containers: ## Run only the real-container tests and fail if Podman is missing
	SILO_REQUIRE_CONTAINERS=1 $(GO) test ./internal/app -run 'TestContainer' -count=1 -v

test-integration: ## Run the Go client integration tests against a running CP
	SILO_INTEGRATION=1 $(GO) test -tags integration ./test/integration/... -count=1 -v

e2e: ## Run Playwright against the already-running dev stack (make dev)
	$(PNPM) --dir web exec playwright test

fmt: ## Format Go sources
	$(GO) fmt ./cmd/... ./internal/...

vet: ## Run go vet
	$(GO) vet ./cmd/... ./internal/...

tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

proto: ## Regenerate protobuf stubs (Go + TS)
	buf generate proto --template proto/buf.gen.yaml

claude-bin: ## Download the official Claude Code binary to build/claude (the Claude Agent provider, for development)
	rm -rf build/claude
	mkdir -p build/claude
	cd build/claude && npm pack --silent @anthropic-ai/claude-agent-sdk-$(CLAUDE_PLATFORM) && tar xzf *.tgz && rm -f *.tgz
	build/claude/package/claude --version
	@echo "Set providers.claude.binary: build/claude/package/claude in silo.yaml"

## --- database ------------------------------------------------------------

db-up: ## Start dev Postgres (pgvector) :5433 + Lightpanda MCP :9223 + smtp4dev :1025/:1143/:8025 + Radicale :5232 + Dex (OIDC) :5556
	$(DEV_COMPOSE) up -d
	@for i in $$(seq 1 60); do \
		$(PODMAN) exec $(PG_CONTAINER) pg_isready -q -U silo -d silo && { echo "postgres ready on localhost:5433"; exit 0; }; \
		sleep 1; \
	done; echo "postgres did not become ready"; exit 1

db-down: ## Stop dev Postgres + Lightpanda + smtp4dev + Radicale + Dex (keeps the volumes)
	$(DEV_COMPOSE) down

db-psql: ## Open psql on the dev database
	$(PODMAN) exec -it $(PG_CONTAINER) psql -U silo -d silo

db-reset: ## Drop dev Postgres and its volume (all data). Asks first.
	@printf "Drop the dev Postgres volume (users, bots, chats, memories)? [y/N] "; read -r ans; \
	[ "$$ans" = y ] || { echo "aborted"; exit 1; }; \
	$(DEV_COMPOSE) down -v && echo "postgres volume removed"

db-check:
	@$(PODMAN) exec $(PG_CONTAINER) pg_isready -q -U silo -d silo_test 2>/dev/null || \
		{ echo "Postgres is not running: make db-up"; exit 1; }

## --- cleanup ---------------------------------------------------------------

clean: ## Remove build artifacts (build/: binaries, SiloOS media, the Claude download; web/dist)
	rm -rf build web/dist

clean-images: ## Remove the local bot, STDIO, drive and egress images
	-$(PODMAN) rmi -f $(BOT_IMAGE) $(STDIO_IMAGE) $(DRIVE_IMAGE) $(EGRESS_IMAGE) $(CP_IMAGE) $(CADDY_IMAGE) $(OS_IMAGE)

cleanup: ## Force-remove every container Silo made (named silo-* or labelled silo.managed)
	@ids=$$({ $(PODMAN) ps -aq --filter name='^silo-'; $(PODMAN) ps -aq --filter label=silo.managed; } 2>/dev/null | sort -u); \
	if [ -n "$$ids" ]; then \
		$(PODMAN) rm -f $$ids; \
	else \
		echo "no silo containers"; \
	fi

reset-data: ## Delete ./data (workspaces, skills). The DB is make db-reset. Asks first.
	@printf "Delete ./data (bot workspaces + skills)? [y/N] "; read -r ans; \
	[ "$$ans" = y ] || { echo "aborted"; exit 1; }; \
	rm -rf data && echo "data/ removed"
