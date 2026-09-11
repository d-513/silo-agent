#!/bin/sh
set -eu
cd "$(dirname "$0")"
export CGO_ENABLED=0

container_arch="$(podman info --format '{{.Host.Arch}}')"
go build -o bin/silo ./cmd/silo
GOOS=linux GOARCH="$container_arch" go build -o bin/silo-worker ./cmd/silo-worker
GOOS=linux GOARCH="$container_arch" go build -o bin/silo-mcp-bridge ./cmd/silo-mcp-bridge
podman build -t localhost/silo-bot:v1 -f botimage/Containerfile .
podman build -t localhost/silo-mcp-stdio:v1 -f mcpimage/Containerfile .

ids=$(podman ps -aq --filter name='^silo-' || true)
if [ -n "$ids" ]; then
	# shellcheck disable=SC2086
	podman rm -f $ids
fi
