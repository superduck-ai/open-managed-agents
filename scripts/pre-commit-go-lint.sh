#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

if ! command -v golangci-lint >/dev/null 2>&1; then
  echo 'golangci-lint is required for staged Go files. Install the version used by .github/workflows/lint.yml.' >&2
  exit 1
fi

./scripts/generate-go.sh

inputs=()
modules=()
for file in "$@"; do
  [[ -f "$file" ]] || continue

  directory="$(dirname "$file")"
  module="$directory"
  while [[ ! -f "$module/go.mod" && "$module" != '.' ]]; do
    module="$(dirname "$module")"
  done

  case "$(basename "$file")" in
    go.mod|go.sum)
      package='./...'
      ;;
    *)
      package="./${directory#"$module/"}"
      [[ "$module" == '.' ]] && package="./${directory}"
      [[ "$directory" == "$module" ]] && package='.'
      ;;
  esac

  [[ "$file" == scripts/generate-go.sh ]] && package='./...'

  input="$module|$package"
  seen=false
  for existing in "${inputs[@]:-}"; do
    if [[ "$existing" == "$input" ]]; then
      seen=true
      break
    fi
  done

  [[ "$seen" == true ]] || inputs+=("$input")

  seen=false
  for existing in "${modules[@]:-}"; do
    [[ "$existing" == "$module" ]] && seen=true
  done
  [[ "$seen" == true ]] || modules+=("$module")
done

if [[ ${#modules[@]} -eq 0 ]]; then
  exit 0
fi

for module in "${modules[@]}"; do
  packages=()
  for input in "${inputs[@]}"; do
    [[ "${input%%|*}" == "$module" ]] && packages+=("${input#*|}")
  done

  config="$repo_root/.golangci.yml"
  if [[ "$module" != '.' && -f "$module/.golangci.yml" ]]; then
    config="$repo_root/$module/.golangci.yml"
  fi

  (
    cd "$module"
    golangci-lint run --config "$config" "${packages[@]}"
    if [[ "$module" == third_party/crush ]]; then
      golangci-lint run --config "$repo_root/.golangci.yml" ./runtime
    fi
  )
done
