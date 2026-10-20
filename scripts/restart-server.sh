#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
CONFIG_PATH="$REPO_ROOT/.run/config.yaml"
ENV_FILE="$REPO_ROOT/.run/env.json"
STATE_FILE="$REPO_ROOT/.run/generated/state.json"

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
      echo "Port :$port is owned by a process outside this worktree (pid $pid); refusing to stop it" >&2
      exit 1
    fi
  done
  echo "Stopping previous backend listener on :$port"
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
  CONFIG_PATH="$REPO_ROOT/.run/generated/config.yaml"
  if [[ ! -f "$CONFIG_PATH" ]]; then
    echo "Bound environment is missing generated config: $CONFIG_PATH" >&2
    echo "Run: just dev-init <init-file>" >&2
    exit 1
  fi
  if ! grep -q '"phase": "complete"' "$STATE_FILE" 2>/dev/null; then
    echo "Environment initialization is not complete; refusing to start the backend" >&2
    echo "Run: just dev-init <init-file>" >&2
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
else
  PORT="${PORT:-38080}"
  BOUND=0
fi

if [[ ! -f "$CONFIG_PATH" ]]; then
  echo "Required config file not found: $CONFIG_PATH" >&2
  echo "Create .run/ and copy config/config.example.yaml to .run/config.yaml" >&2
  exit 1
fi

export CONFIG_FILE="$CONFIG_PATH"

"$REPO_ROOT/scripts/generate-go.sh" --cached

if [[ "$BOUND" -eq 1 ]]; then
  PREVIOUS_PORT="$(previous_runtime_port api_port)"
  stop_previous_listener "$PREVIOUS_PORT"
  PORT="$(probe_free_port 40000)"
  (cd "$REPO_ROOT" && go run ./cmd/dev-init set-runtime --api-port "$PORT")
  portless alias "$API_ROUTE" "$PORT"
  echo "Backend URL: $(portless get "$API_ROUTE" --no-worktree 2>/dev/null || echo "http://${API_ROUTE}.localhost")"
else
  listening_pids() {
    lsof -nP -tiTCP:"$PORT" -sTCP:LISTEN 2>/dev/null | sort -u || true
  }
  pids="$(listening_pids)"
  if [[ -n "$pids" ]]; then
    echo "Stopping process(es) listening on :$PORT:"
    printf '  %s\n' $pids
    kill $pids || true
    for _ in {1..20}; do
      [[ -z "$(listening_pids)" ]] && break
      sleep 0.25
    done
    pids="$(listening_pids)"
    if [[ -n "$pids" ]]; then
      kill -9 $pids || true
    fi
  else
    echo "No process is listening on :$PORT"
  fi
fi

echo "Starting claude-api-server with $CONFIG_FILE in foreground"
echo "Press Ctrl+C to stop"
cd "$REPO_ROOT"
exec go run .
