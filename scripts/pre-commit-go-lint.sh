#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo 'golangci-lint is required for staged Go files. Install the version used by .github/workflows/lint.yml.' >&2
  exit 1
fi

./scripts/generate-go.sh

packages=()
for file in "$@"; do
  [[ -f "$file" ]] || continue

  case "$file" in
    go.mod|go.sum|scripts/generate-go.sh)
      package='./...'
      ;;
    *)
      directory="$(dirname "$file")"
      package="./${directory}"
      [[ "$directory" == '.' ]] && package='.'
      ;;
  esac

  seen=false
  for existing in "${packages[@]:-}"; do
    if [[ "$existing" == "$package" ]]; then
      seen=true
      break
    fi
  done

  [[ "$seen" == true ]] || packages+=("$package")
done

if [[ ${#packages[@]} -eq 0 ]]; then
  exit 0
fi

exec golangci-lint run --config .golangci.yml "${packages[@]}"
