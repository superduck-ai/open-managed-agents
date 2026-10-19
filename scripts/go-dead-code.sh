#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

./scripts/generate-go.sh

exec ./scripts/golangci-lint.sh run --config .golangci-dead-code.yml ./...
