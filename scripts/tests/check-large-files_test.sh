#!/usr/bin/env bash

set -euo pipefail

readonly default_max_bytes=1048576
readonly directory_servers_max_bytes=1310720

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
checker="$repo_root/scripts/check-large-files.sh"
temp_root="$(mktemp -d)"
trap 'rm -rf "$temp_root"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

new_repo() {
  local name="$1"
  local path="$temp_root/$name"

  mkdir -p "$path"
  git -C "$path" init -q
  printf '%s\n' "$path"
}

make_file() {
  local path="$1"
  local size="$2"

  mkdir -p "$(dirname "$path")"
  dd if=/dev/zero of="$path" bs=1 count=0 seek="$size" 2>/dev/null
}

assert_rejected() {
  local repo="$1"
  local expected_path="$2"
  local output

  if output="$(cd "$repo" && "$checker" 2>&1)"; then
    fail "expected $expected_path to exceed its size budget"
  fi
  [[ "$output" == *"$expected_path"* ]] || fail "failure output did not name $expected_path"
  [[ "$output" == *"Git LFS"* ]] || fail "failure output did not explain how to handle intentional large files"
}

# Failure cases come first: ordinary files and the bounded legacy snapshot must
# both be rejected as soon as they exceed their respective budgets.
repo="$(new_repo default-over-budget)"
make_file "$repo/oversized file.bin" "$((default_max_bytes + 1))"
git -C "$repo" add -- "oversized file.bin"
assert_rejected "$repo" "oversized file.bin"

repo="$(new_repo snapshot-over-budget)"
make_file "$repo/internal/platformapi/directory_servers.json" "$((directory_servers_max_bytes + 1))"
git -C "$repo" add -- internal/platformapi/directory_servers.json
assert_rejected "$repo" "internal/platformapi/directory_servers.json"

# Exact boundaries pass, including a filename with spaces.
repo="$(new_repo exact-default-boundary)"
make_file "$repo/exact boundary.bin" "$default_max_bytes"
git -C "$repo" add -- "exact boundary.bin"
(cd "$repo" && "$checker") >/dev/null || fail "a file at the default boundary was rejected"

repo="$(new_repo exact-snapshot-boundary)"
make_file "$repo/internal/platformapi/directory_servers.json" "$directory_servers_max_bytes"
git -C "$repo" add -- internal/platformapi/directory_servers.json
(cd "$repo" && "$checker") >/dev/null || fail "the snapshot at its boundary was rejected"

# The scanner intentionally evaluates the Git index, so an untracked working
# tree artifact cannot make an otherwise valid commit fail.
repo="$(new_repo untracked-file)"
make_file "$repo/tracked.txt" 32
make_file "$repo/untracked.bin" "$((default_max_bytes + 1))"
git -C "$repo" add -- tracked.txt
(cd "$repo" && "$checker") >/dev/null || fail "an untracked file affected the result"

repo="$(new_repo unstaged-growth)"
make_file "$repo/tracked.bin" 32
git -C "$repo" add -- tracked.bin
make_file "$repo/tracked.bin" "$((default_max_bytes + 1))"
(cd "$repo" && "$checker") >/dev/null || fail "unstaged growth affected the indexed result"

printf 'PASS: large file guardrail scenarios\n'
