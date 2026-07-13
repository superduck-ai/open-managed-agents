#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo 'golangci-lint is required for Go dead-code checks. Install the version used by .github/workflows/dead-code.yml.' >&2
  exit 1
fi

packages=()
while IFS= read -r directory; do
  [[ "$directory" == "$repo_root/web/node_modules/"* ]] && continue

  if [[ "$directory" == "$repo_root" ]]; then
    packages+=(.)
  else
    packages+=("./${directory#"$repo_root/"}")
  fi
done < <(go list -f '{{.Dir}}' ./...)

exec golangci-lint run --config .golangci-dead-code.yml "${packages[@]}"
