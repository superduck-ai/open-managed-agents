#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$repo_root"
directories="$(go list -f '{{.Dir}}' ./...)"
while IFS= read -r directory; do
  owner="$(git -C "$directory" rev-parse --show-toplevel)"
  [[ "$owner" == "$repo_root" ]] || continue
  if [[ "$directory" == "$repo_root" ]]; then
    printf '.\n'
  else
    printf './%s\n' "${directory#"$repo_root/"}"
  fi
done <<< "$directories"
