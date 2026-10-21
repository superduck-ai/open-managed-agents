#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture_dir="$(mktemp -d)"
trap 'rm -rf "$fixture_dir"' EXIT
mkdir -p "$fixture_dir/scripts" "$fixture_dir/owned" "$fixture_dir/foreign" "$fixture_dir/owned/testdata" "$fixture_dir/integration"
cp "$repo_root/scripts/go-packages.sh" "$fixture_dir/scripts/go-packages.sh"
git -C "$fixture_dir" init -q
git -C "$fixture_dir/foreign" init -q
printf 'module example.com/fixture\n\ngo 1.27.0\n' > "$fixture_dir/go.mod"
printf 'package fixture\n' > "$fixture_dir/main.go"
printf 'package owned\n' > "$fixture_dir/owned/owned.go"
printf '//go:build integration\n\npackage integration\n' > "$fixture_dir/integration/integration_test.go"
printf 'package foreign\nvar value missingType\n' > "$fixture_dir/foreign/foreign.go"
printf 'package template\nvar value missingType\n' > "$fixture_dir/owned/testdata/template.go"
actual="$($fixture_dir/scripts/go-packages.sh)"
expected=$'.\n./integration\n./owned'
[[ "$actual" == "$expected" ]] || { printf 'unexpected package ownership:\n%s\n' "$actual" >&2; exit 1; }
printf 'package ownership regression passed\n'
