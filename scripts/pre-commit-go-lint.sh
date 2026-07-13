#!/usr/bin/env bash
set -euo pipefail

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo 'golangci-lint is required for staged Go files. Install the version used by .github/workflows/lint.yml.' >&2
  exit 1
fi

packages=()
for file in "$@"; do
  [[ -f "$file" ]] || continue

  directory="$(dirname "$file")"
  package="./${directory}"
  [[ "$directory" == '.' ]] && package='.'

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
