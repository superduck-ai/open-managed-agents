#!/usr/bin/env bash

set -euo pipefail

readonly production_limit=500
readonly test_limit=1000

repo_root="$(git rev-parse --show-toplevel)"
budget_file="$repo_root/scripts/go-file-line-budgets.txt"
cd "$repo_root"

line_count() {
  awk 'END { print NR + 0 }' "$1"
}

is_generated() {
  local header
  header="$(sed -n '1,20p' "$1")"
  grep -Eq '^// Code generated .* DO NOT EDIT\.$' <<<"$header"
}

default_limit_for() {
  case "$1" in
    *_test.go) printf '%s\n' "$test_limit" ;;
    *) printf '%s\n' "$production_limit" ;;
  esac
}

budget_for() {
  awk -v target="$1" '!/^#/ && NF >= 2 && $2 == target { print $1; exit }' "$budget_file"
}

failure_count=0

# Legacy budgets are exact ratchets. Shrinking a file requires lowering its
# recorded budget in the same change; growth is rejected.
while read -r budget path extra; do
  [[ -n "${budget:-}" && "${budget:0:1}" != "#" ]] || continue

  if [[ ! "$budget" =~ ^[0-9]+$ || -z "${path:-}" || -n "${extra:-}" ]]; then
    printf 'Invalid Go file line budget entry: %s %s %s\n' "$budget" "${path:-}" "${extra:-}" >&2
    ((failure_count += 1))
    continue
  fi
  if ! git ls-files --error-unmatch -- "$path" >/dev/null 2>&1; then
    printf 'Go file line budget references an untracked path: %s\n' "$path" >&2
    ((failure_count += 1))
    continue
  fi
  if is_generated "$path"; then
    printf 'Generated Go file has an unnecessary line budget: %s\n' "$path" >&2
    ((failure_count += 1))
    continue
  fi

  default_limit="$(default_limit_for "$path")"
  actual="$(line_count "$path")"
  if ((budget <= default_limit)); then
    printf 'Go file line budget is not above the default for %s: %s <= %s\n' "$path" "$budget" "$default_limit" >&2
    ((failure_count += 1))
  elif ((actual > budget)); then
    printf 'Go file exceeds its line budget: %s has %s lines (limit: %s)\n' "$path" "$actual" "$budget" >&2
    ((failure_count += 1))
  elif ((actual < budget)); then
    printf 'Go file line budget is stale: %s has %s lines; lower its budget from %s\n' "$path" "$actual" "$budget" >&2
    ((failure_count += 1))
  fi
done < "$budget_file"

while IFS= read -r -d '' path; do
  is_generated "$path" && continue
  [[ -z "$(budget_for "$path")" ]] || continue

  actual="$(line_count "$path")"
  limit="$(default_limit_for "$path")"
  if ((actual > limit)); then
    printf 'Go file exceeds the default line limit without a ratchet budget: %s has %s lines (limit: %s)\n' "$path" "$actual" "$limit" >&2
    ((failure_count += 1))
  fi
done < <(git ls-files -z '*.go')

if ((failure_count > 0)); then
  exit 1
fi

printf 'Go file line check passed (production: %s, tests: %s).\n' "$production_limit" "$test_limit"
