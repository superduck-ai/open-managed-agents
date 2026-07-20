#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONFIG_PATH="$REPO_ROOT/config/config.yaml"

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "Required config file not found: $CONFIG_PATH" >&2
  echo "Create it with: cp $REPO_ROOT/config/config.example.yaml $CONFIG_PATH" >&2
  exit 1
fi

export CONFIG_FILE="$CONFIG_PATH"

PORT="${PORT:-38080}"

listening_pids() {
  lsof -nP -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | sort -u || true
}

stop_listeners() {
  local pids

  pids="$(listening_pids)"
  if [[ -z "$pids" ]]; then
    echo "No process is listening on :$PORT"
    return
  fi

  echo "Stopping process(es) listening on :$PORT:"
  printf '  %s\n' $pids
  kill $pids || true

  for _ in {1..20}; do
    if [[ -z "$(listening_pids)" ]]; then
      return
    fi
    sleep 0.25
  done

  pids="$(listening_pids)"
  if [[ -n "$pids" ]]; then
    echo "Process(es) still listening on :$PORT, force killing: $pids"
    kill -9 $pids || true
  fi

  for _ in {1..20}; do
    if [[ -z "$(listening_pids)" ]]; then
      return
    fi
    sleep 0.25
  done

  pids="$(listening_pids)"
  if [[ -n "$pids" ]]; then
    echo "Failed to free :$PORT; still listening:"
    printf '  %s\n' $pids
    exit 1
  fi
}

stop_listeners

echo "Starting claude-api-server with $CONFIG_FILE in foreground"
echo "Press Ctrl+C to stop"
cd "$REPO_ROOT"
exec go run .
