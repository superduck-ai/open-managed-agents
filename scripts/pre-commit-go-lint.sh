#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

./scripts/generate-go.sh

allowed_packages="$(./scripts/go-packages.sh)"
packages=()
for file in "$@"; do
  [[ -f "$file" ]] || continue

  case "$file" in
    go.mod|go.sum|tools/golangci-lint/go.mod|tools/golangci-lint/go.sum|scripts/generate-go.sh|scripts/golangci-lint.sh|scripts/go-packages.sh)
      package='./...'
      ;;
    *)
      directory="$(dirname "$file")"
      package="./${directory}"
      [[ "$directory" == '.' ]] && package='.'
      ;;
  esac

  if [[ "$package" != './...' ]]; then
    case $'\n'"$allowed_packages"$'\n' in
      *$'\n'"$package"$'\n'*) ;;
      *) continue ;;
    esac
  fi

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

exec ./scripts/golangci-lint.sh run --config .golangci.yml "${packages[@]}"
