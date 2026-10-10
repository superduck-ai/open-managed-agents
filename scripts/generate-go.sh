#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

tool_directory="$repo_root/tmp/go-generation"
mkdir -p "$tool_directory"
tool_path="$(mktemp "$tool_directory/generate-go.XXXXXX")"
trap 'rm -f "$tool_path"' EXIT
GOOS="$(go env GOHOSTOS)" GOARCH="$(go env GOHOSTARCH)" go build -o "$tool_path" ./cmd/generate-go
mv -f "$tool_path" "$tool_directory/generate-go"
exec "$tool_directory/generate-go" "$@"
