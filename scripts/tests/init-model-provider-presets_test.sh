#!/usr/bin/env bash
set -euo pipefail

script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo_root="$(cd "$script_directory/../.." && pwd)"
initializer="$repo_root/scripts/init-model-provider-presets.sh"
test_directory="$(mktemp -d)"
trap 'rm -rf -- "$test_directory"' EXIT

if "$initializer" --unknown > /dev/null 2>&1; then
  echo "unknown argument was accepted" >&2
  exit 1
fi
if "$initializer" one.json two.json > /dev/null 2>&1; then
  echo "multiple output paths were accepted" >&2
  exit 1
fi

preview_path="$test_directory/not-created/providers.json"
"$initializer" --dry-run "$preview_path" > "$test_directory/preview.json"
[[ ! -e "$test_directory/not-created" ]]
cmp "$test_directory/preview.json" "$repo_root/internal/config/model-provider-presets.json"

existing_path="$test_directory/existing.json"
printf '%s\n' '{"version":1,"providers":[]}' > "$existing_path"
cp "$existing_path" "$test_directory/before.json"
"$initializer" "$existing_path" > /dev/null
cmp "$existing_path" "$test_directory/before.json"

ln -s "$test_directory/missing.json" "$test_directory/link.json"
"$initializer" "$test_directory/link.json" > /dev/null
[[ -L "$test_directory/link.json" && ! -e "$test_directory/missing.json" ]]

output_path="$test_directory/nested path/providers.json"
"$initializer" "$output_path" > /dev/null
cmp "$output_path" "$test_directory/preview.json"
"$initializer" "$output_path" > /dev/null
cmp "$output_path" "$test_directory/preview.json"

concurrent_path="$test_directory/concurrent.json"
"$initializer" "$concurrent_path" > /dev/null &
first_pid=$!
"$initializer" "$concurrent_path" > /dev/null &
second_pid=$!
wait "$first_pid"
wait "$second_pid"
cmp "$concurrent_path" "$test_directory/preview.json"

if compgen -G "$test_directory/.model-provider-presets.*" > /dev/null; then
  echo "temporary files were left behind" >&2
  exit 1
fi
printf '%s\n' 'Supplier preset initializer tests passed'
