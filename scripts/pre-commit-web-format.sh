#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
prettier_bin="$repo_root/web/node_modules/.bin/prettier"

if [[ ! -x "$prettier_bin" ]]; then
  echo 'Prettier dependencies are missing. Run `cd web && bun install --frozen-lockfile`.' >&2
  exit 1
fi

files=()
for file in "$@"; do
  case "$file" in
    web/*)
      files+=("${file#web/}")
      ;;
  esac
done

if [[ ${#files[@]} -eq 0 ]]; then
  exit 0
fi

cd "$repo_root/web"
exec "$prettier_bin" --write --ignore-unknown --ignore-path .prettierignore "${files[@]}"
