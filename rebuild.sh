#!/bin/sh
set -eu
cd "$(dirname "$0")"
export CGO_ENABLED=0

mode="${1:-all}"

container_arch="$(podman info --format '{{.Host.Arch}}')"

build_cp() {
	go build -o bin/silo ./cmd/silo
}

build_bot() {
	GOOS=linux GOARCH="$container_arch" go build -o bin/silo-worker ./cmd/silo-worker
	podman build -t localhost/silo-bot:v1 -f botimage/Containerfile .
}

build_stdio() {
	GOOS=linux GOARCH="$container_arch" go build -o bin/silo-mcp-bridge ./cmd/silo-mcp-bridge
	podman build -t localhost/silo-mcp-stdio:v1 -f mcpimage/Containerfile .
}

case "$mode" in
	cp)
		build_cp
		exit 0
		;;
	bot)
		build_bot
		exit 0
		;;
	stdio)
		build_stdio
		exit 0
		;;
	all)
		build_cp
		build_bot
		build_stdio
		;;
	*)
		echo "usage: $0 [cp|bot|stdio|all]" >&2
		exit 2
		;;
esac

ids=$(podman ps -aq --filter name='^silo-' || true)
if [ -n "$ids" ]; then
	# shellcheck disable=SC2086
	podman rm -f $ids
fi
