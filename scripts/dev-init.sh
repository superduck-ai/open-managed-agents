#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"

for tool in go bun git; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Missing command: $tool" >&2
    exit 1
  fi
done

if [[ ! -x "$(git rev-parse --git-path hooks/pre-commit)" ]]; then
  ./scripts/pre-commit.sh install --install-hooks
fi

go mod download
(cd web && bun install --frozen-lockfile)
(cd oma-brain-dsh && bun install --frozen-lockfile)
./scripts/generate-go.sh
exec go run ./cmd/dev-init "$@"
