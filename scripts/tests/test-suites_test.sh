#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$repo_root"

unit_packages="$(go list ./...)"
if grep -Eq '/tests($|/)' <<< "$unit_packages"; then
  echo 'integration test packages entered the default build' >&2
  exit 1
fi

unit_files="$(go list -f '{{range .TestGoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}' ./...)"
if grep -Eq '(_postgres_test|_integration_test)\.go$' <<< "$unit_files"; then
  echo 'external integration tests entered the default build' >&2
  exit 1
fi

integration_files="$(go list -tags=integration -f '{{range .TestGoFiles}}{{$.Dir}}/{{.}}{{"\n"}}{{end}}' ./...)"
for file in tests/files_api_test.go internal/db/admin_api_keys_mapper_postgres_test.go internal/tunnels/request_bindings_integration_test.go; do
  if ! grep -Fqx "$repo_root/$file" <<< "$integration_files"; then
    echo "integration test missing from explicit build: $file" >&2
    exit 1
  fi
done

for tag in e2e e2b_integration; do
  files="$(go list -tags="$tag" -f '{{join .TestGoFiles "\n"}}' ./tests)"
  if ! grep -Fxq files_api_test.go <<< "$files"; then
    echo "$tag build lost its shared fixtures" >&2
    exit 1
  fi
done

echo 'test suite boundaries passed'
