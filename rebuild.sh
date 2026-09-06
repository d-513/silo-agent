#!/bin/sh
set -eu
cd "$(dirname "$0")"
export CGO_ENABLED=0

go build -o bin/silo ./cmd/silo
go build -o bin/silo-worker ./cmd/silo-worker
podman build -t localhost/silo-bot:v1 -f botimage/Containerfile .

ids=$(podman ps -aq --filter name='^silo-' || true)
if [ -n "$ids" ]; then
	# shellcheck disable=SC2086
	podman rm -f $ids
fi
