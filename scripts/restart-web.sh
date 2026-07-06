#!/usr/bin/env bash
set -euo pipefail

PORT="${PORT:-5173}"
HOST="${HOST:-127.0.0.1}"
API_PORT="${API_PORT:-38080}"
VITE_API_PROXY_TARGET="${VITE_API_PROXY_TARGET:-http://127.0.0.1:${API_PORT}}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
WEB_DIR="$REPO_ROOT/web"

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

if [[ ! -d "$WEB_DIR" ]]; then
  echo "Frontend directory not found: $WEB_DIR"
  exit 1
fi

if ! command -v bun >/dev/null 2>&1; then
  echo "bun is not installed or not on PATH"
  exit 1
fi

stop_listeners

echo "Starting claude-platform-web on ${HOST}:${PORT} in foreground"
echo "Proxying /api and /v1 to $VITE_API_PROXY_TARGET when using Vite directly"
echo "Press Ctrl+C to stop"
cd "$WEB_DIR"
exec env VITE_API_PROXY_TARGET="$VITE_API_PROXY_TARGET" bun run dev -- --host "$HOST" --port "$PORT"
