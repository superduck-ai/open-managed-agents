#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

find ./internal/db -type f -name '*.sqlmap.gen.go' -delete

go generate ./internal/db
