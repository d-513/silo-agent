#!/usr/bin/env bash
# gotest.sh runs the Go suite with internal/app split across SHARDS processes.
#
# internal/app is one package of ~400 tests, each starting a whole control
# plane on its own Postgres schema; they share package globals (the dummy
# provider's scripts, search budgets), so they cannot use t.Parallel. Separate
# processes have separate globals, so the package runs as SHARDS `go test`
# processes at once, each taking every SHARDS-th test, beside the other
# packages. Extra arguments go to every `go test` (e.g. -count=1).
set -uo pipefail

GO=${GO:-go}
SHARDS=${SHARDS:-8}
APP=./internal/app
logs=$(mktemp -d)
trap 'rm -rf "$logs"' EXIT

# Build once and list the package's tests; the shards reuse the build cache.
mapfile -t tests < <($GO test -list '.*' "$APP" | grep -E '^(Test|Example|Fuzz)' )
if [ "${#tests[@]}" -eq 0 ]; then
	echo "gotest: no tests listed in $APP" >&2
	exit 1
fi

# One container sweep for the whole run: a shard's own sweep would remove the
# containers another shard's tests are using.
$GO test -count=1 -run '^$' "$APP" >/dev/null

pids=()
$GO test "$@" $($GO list ./cmd/... ./internal/... | grep -vx 'silo.agent/internal/app') >"$logs/rest" 2>&1 &
pids+=($!)
for ((s = 0; s < SHARDS; s++)); do
	names=()
	for ((i = s; i < ${#tests[@]}; i += SHARDS)); do
		names+=("${tests[i]}")
	done
	[ "${#names[@]}" -eq 0 ] && continue
	re="^($(IFS='|'; echo "${names[*]}"))\$"
	SILO_TEST_SHARD=$s $GO test "$@" -run "$re" "$APP" >"$logs/app$s" 2>&1 &
	pids+=($!)
done

fail=0
for p in "${pids[@]}"; do
	wait "$p" || fail=1
done
$GO test -count=1 -run '^$' "$APP" >/dev/null

# Package results first; a failing log in full, a passing one by its summary.
for f in "$logs"/rest "$logs"/app*; do
	if grep -qE '^(FAIL|--- FAIL|panic:)' "$f"; then
		cat "$f"
	else
		grep -E '^(ok|\?|FAIL)\s' "$f"
	fi
done
exit $fail
