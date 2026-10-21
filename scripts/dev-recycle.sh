#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
./scripts/generate-go.sh --cached
exec go run ./cmd/dev-init recycle "$@"
