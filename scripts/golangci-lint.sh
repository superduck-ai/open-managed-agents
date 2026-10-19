#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"
args=()
for argument in "$@"; do
  if [[ "$argument" == './...' ]]; then
    packages="$(./scripts/go-packages.sh)"
    while IFS= read -r package; do
      args+=("$package")
    done <<< "$packages"
  else
    args+=("$argument")
  fi
done
exec go run -mod=readonly -modfile="$repo_root/tools/golangci-lint/go.mod" github.com/golangci/golangci-lint/v2/cmd/golangci-lint "${args[@]}"
