#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
moon="$repo_root/web/node_modules/.bin/moon"

if [[ ! -x "$moon" ]]; then
  echo "moon is not installed; run 'cd web && bun install --frozen-lockfile'" >&2
  exit 1
fi

cd "$repo_root"
exec "$moon" "$@"
