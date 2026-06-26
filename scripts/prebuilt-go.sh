#!/usr/bin/env bash
# GoReleaser gobinary shim: instead of compiling, copy the matching binary that
# CI already built natively (artifacts/binary-<goos>-<goarch>/duckmigrate). The
# OSS edition has no prebuilt builder, and DuckDB's Windows libs can't be
# cross-compiled, so binaries are produced per-runner and assembled here.
# Any non-`build` invocation (go env, go list, go version -m, ...) passes through
# to the real go.
set -euo pipefail

if [ "${1:-}" != "build" ]; then
  exec go "$@"
fi

out=""
args=("$@")
for ((i = 0; i < ${#args[@]}; i++)); do
  if [ "${args[$i]}" = "-o" ]; then
    out="${args[$((i + 1))]}"
    break
  fi
done
if [ -z "$out" ]; then
  echo "prebuilt-go: 'go build' without -o: $*" >&2
  exit 1
fi

ext=""
[ "${GOOS:-}" = "windows" ] && ext=".exe"
src="artifacts/binary-${GOOS:-}-${GOARCH:-}/duckmigrate${ext}"
if [ ! -f "$src" ]; then
  echo "prebuilt-go: missing prebuilt binary: $src" >&2
  exit 1
fi

mkdir -p "$(dirname "$out")"
cp "$src" "$out"
chmod +x "$out"
