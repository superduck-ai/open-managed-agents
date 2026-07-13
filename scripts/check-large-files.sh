#!/usr/bin/env bash

set -euo pipefail

readonly default_max_bytes=1048576
readonly directory_servers_max_bytes=1310720

size_limit_for() {
  case "$1" in
    internal/platformapi/directory_servers.json)
      # This embedded upstream directory snapshot already exceeds the default.
      # Keep a narrow ratchet so future growth is still detected.
      printf '%s\n' "$directory_servers_max_bytes"
      ;;
    *)
      printf '%s\n' "$default_max_bytes"
      ;;
  esac
}

repo_root="$(git rev-parse --show-toplevel)"
cd "$repo_root"

checked_count=0
failure_count=0

# Inspect blobs from the Git index rather than the working tree. This makes the
# hook evaluate exactly what will be committed and makes CI scan every tracked
# file without being affected by unstaged changes.
while IFS= read -r -d '' entry; do
  metadata="${entry%%$'\t'*}"
  path="${entry#*$'\t'}"
  read -r mode object stage <<<"$metadata"

  [[ "$stage" == "0" ]] || continue
  case "$mode" in
    100644 | 100755) ;;
    *) continue ;;
  esac

  ((checked_count += 1))
  size="$(git cat-file -s "$object")"
  limit="$(size_limit_for "$path")"

  if ((size > limit)); then
    if ((failure_count == 0)); then
      printf 'Tracked files exceed their size budget:\n' >&2
    fi
    printf '  - %s: %s bytes (limit: %s bytes)\n' "$path" "$size" "$limit" >&2
    ((failure_count += 1))
  fi
done < <(git ls-files --stage -z)

if ((failure_count > 0)); then
  printf '\nShrink or remove these files before committing. If a large binary is intentional, propose a reviewed Git LFS policy.\n' >&2
  exit 1
fi

printf 'Large file check passed: %s tracked files are within their size budgets.\n' "$checked_count"
