#!/usr/bin/env bash
set -euo pipefail

PORT="${PORT:-5173}"
HOST="${HOST:-127.0.0.1}"
API_PORT="${API_PORT:-38080}"
VITE_API_PROXY_TARGET="${VITE_API_PROXY_TARGET:-http://127.0.0.1:${API_PORT}}"
PORT_SCAN_LIMIT="${PORT_SCAN_LIMIT:-50}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd -P)"
REPO_ROOT="$(cd "$SCRIPT_DIR/.." && pwd -P)"
WEB_DIR="$REPO_ROOT/web"
REQUESTED_PORT="$PORT"

listening_pids() {
  local port="$1"
  lsof -nP -tiTCP:"$port" -sTCP:LISTEN 2>/dev/null | sort -u || true
}

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

process_comm() {
  local pid="$1"
  ps -p "$pid" -o comm= 2>/dev/null || true
}

path_is_in_current_repo() {
  local path="$1"

  [[ -n "$path" && -d "$path" ]] || return 1
  path="$(cd "$path" && pwd -P)" || return 1

  [[ "$path" == "$REPO_ROOT" || "$path" == "$REPO_ROOT"/* ]]
}

pid_is_from_current_repo() {
  local pid="$1"
  local cwd

  cwd="$(process_cwd "$pid")"
  path_is_in_current_repo "$cwd"
}

describe_listener_pids() {
  local pid cwd comm scope

  for pid in "$@"; do
    cwd="$(process_cwd "$pid")"
    comm="$(process_comm "$pid")"
    scope="outside this checkout"
    if path_is_in_current_repo "$cwd"; then
      scope="current checkout"
    fi
    printf '  %s%s cwd=%s [%s]\n' "$pid" "${comm:+ ($comm)}" "${cwd:-unknown}" "$scope"
  done
}

CURRENT_REPO_PIDS=()
OTHER_PIDS=()

split_listener_pids() {
  local port="$1"
  local pid

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

target_pids_still_listening() {
  local port="$1"
  shift
  local listener target

  while IFS= read -r listener; do
    [[ -n "$listener" ]] || continue
    for target in "$@"; do
      if [[ "$listener" == "$target" ]]; then
        printf '%s\n' "$target"
        break
      fi
    done
  done < <(listening_pids "$port")
}

stop_current_repo_listeners() {
  local port="$1"
  shift
  local remaining

  if [[ "$#" -eq 0 ]]; then
    return
  fi

  echo "Stopping current-checkout process(es) listening on :$port:"
  describe_listener_pids "$@"
  kill "$@" || true

  for _ in {1..20}; do
    remaining="$(target_pids_still_listening "$port" "$@")"
    if [[ -z "$remaining" ]]; then
      return
    fi
    sleep 0.25
  done

  remaining="$(target_pids_still_listening "$port" "$@")"
  if [[ -n "$remaining" ]]; then
    echo "Process(es) still listening on :$port, force killing:"
    printf '  %s\n' $remaining
    # shellcheck disable=SC2086
    kill -9 $remaining || true
  fi

  for _ in {1..20}; do
    remaining="$(target_pids_still_listening "$port" "$@")"
    if [[ -z "$remaining" ]]; then
      return
    fi
    sleep 0.25
  done

  remaining="$(target_pids_still_listening "$port" "$@")"
  if [[ -n "$remaining" ]]; then
    echo "Failed to stop current-checkout listener(s) on :$port:"
    printf '  %s\n' $remaining
    exit 1
  fi
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
    describe_listener_pids "${OTHER_PIDS[@]}"
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

if ! [[ "$PORT_SCAN_LIMIT" =~ ^[0-9]+$ ]]; then
  echo "PORT_SCAN_LIMIT must be a number: $PORT_SCAN_LIMIT"
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

select_port

echo "Starting claude-platform-web on ${HOST}:${PORT} in foreground"
echo "Proxying /api and /v1 to $VITE_API_PROXY_TARGET when using Vite directly"
echo "Press Ctrl+C to stop"
cd "$WEB_DIR"
exec env VITE_API_PROXY_TARGET="$VITE_API_PROXY_TARGET" bun run dev -- --host "$HOST" --port "$PORT" --strictPort
