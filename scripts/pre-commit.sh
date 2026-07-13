#!/usr/bin/env bash
set -euo pipefail

readonly pre_commit_version='4.6.0'

if command -v pre-commit >/dev/null 2>&1; then
  pre_commit_bin="$(command -v pre-commit)"
elif command -v uv >/dev/null 2>&1; then
  pre_commit_bin="$(uv tool dir --bin)/pre-commit"
  if [[ ! -x "$pre_commit_bin" ]]; then
    echo "Installing pre-commit ${pre_commit_version} with uv..." >&2
    uv tool install "pre-commit==${pre_commit_version}"
  fi
else
  echo 'pre-commit is required. Install it directly, or install uv so this script can bootstrap the pinned version.' >&2
  exit 1
fi

exec "$pre_commit_bin" "$@"
