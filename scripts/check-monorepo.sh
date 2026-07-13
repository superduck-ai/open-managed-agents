#!/usr/bin/env bash

set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
moon="$repo_root/scripts/moon.sh"

for project in backend web agent-sdk-test; do
  "$moon" project "$project" --json >/dev/null
done

for target in \
  backend:build \
  backend:test \
  backend:lint \
  web:build \
  web:test \
  web:lint \
  web:lint-naming \
  web:lint-complexity \
  web:format-check \
  agent-sdk-test:typecheck; do
  "$moon" task "$target" --json >/dev/null
done

echo "moon project graph and task boundaries are valid"
