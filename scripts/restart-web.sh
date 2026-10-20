#!/usr/bin/env bash
set -euo pipefail

USER_PORT="${PORT:-}"
USER_API_PORT="${API_PORT:-}"
USER_PROXY_TARGET="${VITE_API_PROXY_TARGET:-}"

PORT="${PORT:-5173}"
HOST="${HOST:-127.0.0.1}"
API_PORT="${API_PORT:-38080}"
VITE_API_PROXY_TARGET="${VITE_API_PROXY_TARGET:-http://127.0.0.1:${API_PORT}}"
PORT_SCAN_LIMIT="${PORT_SCAN_LIMIT:-50}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
WEB_DIR="$REPO_ROOT/web"
ENV_FILE="$REPO_ROOT/.run/env.json"
STATE_FILE="$REPO_ROOT/.run/generated/state.json"
BOUND=0

process_cwd() {
  local pid="$1"
  local output line
  output="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null || true)"
  while IFS= read -r line; do
    if [[ "$line" == n* ]]; then
      printf '%s\n' "${line#n}"
      return
    fi
  done <<< "$output"
}

pid_is_from_current_repo() {
  local pid="$1" cwd
  cwd="$(process_cwd "$pid")"
  [[ -n "$cwd" && ( "$cwd" == "$REPO_ROOT" || "$cwd" == "$REPO_ROOT"/* ) ]]
}

json_field() {
  sed -n "s/.*\"$2\": *\"\{0,1\}\([^\",]*\).*/\1/p" "$1" | head -n 1
}

previous_runtime_port() {
  local field="$1"
  [[ -f "$STATE_FILE" ]] || return 0
  sed -n "s/.*\"$field\": *\([0-9][0-9]*\).*/\1/p" "$STATE_FILE" | head -n 1
}

probe_free_port() {
  local base="$1" offset port
  for ((offset = 0; offset <= 200; offset++)); do
    port=$((base + offset))
    if [[ -z "$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null)" ]]; then
      printf '%s\n' "$port"
      return
    fi
  done
  echo "No free loopback port from :$base" >&2
  exit 1
}

stop_previous_listener() {
  local port="$1" pid pids
  [[ -n "$port" ]] || return 0
  pids="$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true)"
  [[ -n "$pids" ]] || return 0
  for pid in $pids; do
    if ! pid_is_from_current_repo "$pid"; then
      echo "Port :$port is owned by a process outside this worktree; refusing to stop it" >&2
      exit 1
    fi
  done
  echo "Stopping previous frontend listener on :$port"
  kill $pids || true
  for _ in {1..20}; do
    [[ -z "$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null)" ]] && return
    sleep 0.25
  done
  kill -9 $pids 2>/dev/null || true
}

if [[ -f "$ENV_FILE" ]]; then
  ENV_NAME="$(json_field "$ENV_FILE" name)"
  if [[ -z "$ENV_NAME" ]]; then
    echo "Invalid environment binding: $ENV_FILE" >&2
    exit 1
  fi
  API_ROUTE="oma-${ENV_NAME}-api"
  WEB_ROUTE="oma-${ENV_NAME}-web"
  if ! grep -q '"phase": "complete"' "$STATE_FILE" 2>/dev/null; then
    echo "Environment initialization is not complete; refusing to start the frontend" >&2
    echo "Run: just dev-init <init-file>" >&2
    exit 1
  fi
  if [[ -n "$USER_PORT" || -n "$USER_API_PORT" ]]; then
    echo "PORT/API_PORT are managed by the bound environment; unset them before restarting" >&2
    exit 1
  fi
  if [[ -n "$USER_PROXY_TARGET" && "$USER_PROXY_TARGET" != "http://$API_ROUTE.localhost" && "$USER_PROXY_TARGET" != "http://$API_ROUTE.localhost:"* ]]; then
    echo "VITE_API_PROXY_TARGET=$USER_PROXY_TARGET conflicts with bound environment (backend route $API_ROUTE)" >&2
    exit 1
  fi
  if ! command -v portless >/dev/null 2>&1; then
    echo "Bound environments route through portless; install it with: npm install -g portless" >&2
    exit 1
  fi
  portless proxy start --no-tls || {
    echo "Failed to start the portless proxy (sudo may be required once: sudo portless proxy start --no-tls)" >&2
    exit 1
  }
  BOUND=1
fi

REQUESTED_PORT="$PORT"

listening_pids() {
  local port="$1"
  lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true
}

path_is_in_current_repo() {
  local path="$1"
  [[ -n "$path" && -d "$path" ]] || return 1
  path="$(cd "$path" && pwd -P)" || return 1
  [[ "$path" == "$REPO_ROOT" || "$path" == "$REPO_ROOT"/* ]]
}

split_listener_pids() {
  local port="$1" pid
  CURRENT_REPO_PIDS=()
  OTHER_PIDS=()
  while IFS= read -r pid; do
    [[ -n "$pid" ]] || continue
    if pid_is_from_current_repo "$pid"; then
      CURRENT_REPO_PIDS+=("$pid")
    else
      OTHER_PIDS+=("$pid")
    fi
  done < <(listening_pids "$port")
}

stop_current_repo_listeners() {
  local port="$1"
  shift
  local remaining
  [[ "$#" -eq 0 ]] && return
  echo "Stopping current-checkout process(es) listening on :$port:"
  printf '  %s\n' "$@"
  kill "$@" || true
  for _ in {1..20}; do
    remaining="$(lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null || true)"
    [[ -z "$remaining" ]] && return
    sleep 0.25
  done
  kill -9 "$@" 2>/dev/null || true
}

select_port() {
  local offset port
  for ((offset = 0; offset <= PORT_SCAN_LIMIT; offset++)); do
    port=$((REQUESTED_PORT + offset))
    if ((port > 65535)); then
      break
    fi
    while true; do
      split_listener_pids "$port"
      if [[ "${#CURRENT_REPO_PIDS[@]}" -eq 0 ]]; then
        break
      fi
      stop_current_repo_listeners "$port" "${CURRENT_REPO_PIDS[@]}"
    done
    split_listener_pids "$port"
    if [[ "${#OTHER_PIDS[@]}" -eq 0 ]]; then
      PORT="$port"
      if [[ "$PORT" != "$REQUESTED_PORT" ]]; then
        echo "Selected frontend port :$PORT because :$REQUESTED_PORT is used outside this checkout"
      else
        echo "Using frontend port :$PORT"
      fi
      return
    fi
    echo "Port :$port is used outside this checkout; leaving it running and trying the next port:"
    for pid in "${OTHER_PIDS[@]}"; do
      echo "  $pid cwd=$(process_cwd "$pid")"
    done
  done
  echo "No free frontend port found from :$REQUESTED_PORT through :$((REQUESTED_PORT + PORT_SCAN_LIMIT))"
  exit 1
}

if [[ ! -d "$WEB_DIR" ]]; then
  echo "Frontend directory not found: $WEB_DIR"
  exit 1
fi
WEB_DIR="$(cd "$WEB_DIR" && pwd -P)"

if ! [[ "$REQUESTED_PORT" =~ ^[0-9]+$ ]]; then
  echo "PORT must be a number: $REQUESTED_PORT"
  exit 1
fi

if ((REQUESTED_PORT < 1 || REQUESTED_PORT > 65535)); then
  echo "PORT must be between 1 and 65535: $REQUESTED_PORT"
  exit 1
fi

if ! command -v lsof >/dev/null 2>&1; then
  echo "lsof is required to inspect existing frontend listeners"
  exit 1
fi

if ! command -v bun >/dev/null 2>&1; then
  echo "bun is not installed or not on PATH"
  exit 1
fi

if [[ "$BOUND" -eq 1 ]]; then
  stop_previous_listener "$(previous_runtime_port web_port)"
  PORT="$(probe_free_port 61000)"
  (cd "$REPO_ROOT" && go run ./cmd/dev-init set-runtime --web-port "$PORT")
  # Proxy straight to the backend's loopback port: routing through the
  # portless name would pass through the proxy twice and trip its loop guard.
  API_DIRECT_PORT="$(previous_runtime_port api_port)"
  if [[ -n "$API_DIRECT_PORT" ]]; then
    VITE_API_PROXY_TARGET="http://127.0.0.1:$API_DIRECT_PORT"
  else
    VITE_API_PROXY_TARGET="$(portless get "$API_ROUTE" --no-worktree 2>/dev/null || echo "http://${API_ROUTE}.localhost")"
  fi
  portless alias "$WEB_ROUTE" "$PORT"
  echo "Using frontend port :$PORT (bound environment)"
  echo "Web URL: $(portless get "$WEB_ROUTE" --no-worktree 2>/dev/null || echo "http://${WEB_ROUTE}.localhost")"
else
  select_port
fi

echo "Starting claude-platform-web on ${HOST}:${PORT} in foreground"
echo "Proxying /api and /v1 to $VITE_API_PROXY_TARGET"
echo "Press Ctrl+C to stop"
cd "$WEB_DIR"
exec env VITE_API_PROXY_TARGET="$VITE_API_PROXY_TARGET" bun run dev -- --host "$HOST" --port "$PORT" --strictPort
