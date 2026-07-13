#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
jscpd="$repo_root/web/node_modules/.bin/jscpd"

if [[ ! -x "$jscpd" ]]; then
  echo "jscpd is not installed; run 'cd web && bun install --frozen-lockfile'" >&2
  exit 1
fi

check_duplicates() {
  local label="$1"
  local config="$2"
  shift 2

  if "$jscpd" --config "$config" --no-tips "$@"; then
    echo "$label duplicate-code budget passed"
    return
  fi

  echo "$label duplicate-code budget exceeded; reporting detected clones:" >&2
  "$jscpd" --config "$config" --reporters ai --no-tips "$@" || true
  return 1
}

check_duplicates \
  "Go production code" \
  "$repo_root/.jscpd.json" \
  "$repo_root/main.go" \
  "$repo_root/cmd" \
  "$repo_root/internal"

check_duplicates \
  "TypeScript production code" \
  "$repo_root/web/.jscpd.json" \
  "$repo_root/web/src"
