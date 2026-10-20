#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$PROJECT_DIR"

REPO_ROOT="$(cd "$PROJECT_DIR/.." && pwd -P)"
ENV_FILE="$REPO_ROOT/.run/env.json"
if [[ -f "$ENV_FILE" ]]; then
  ENV_NAME="$(sed -n 's/.*"name": *"\([^"]*\)".*/\1/p' "$ENV_FILE" | head -n 1)"
  if [[ -z "$ENV_NAME" ]]; then
    echo "Invalid environment binding: $ENV_FILE" >&2
    exit 1
  fi
  API_ROUTE="oma-${ENV_NAME}-api"
  case "${OMA_API_URL:-}" in
    ""|"http://$API_ROUTE.localhost"|"http://$API_ROUTE.localhost:"*) ;;
    *)
      echo "OMA_API_URL=$OMA_API_URL conflicts with bound environment (backend route $API_ROUTE)" >&2
      exit 1
      ;;
  esac
  if [[ -n "${OMA_BRAIN_CLAIM_TOKEN:-}" ]]; then
    echo "OMA_BRAIN_CLAIM_TOKEN is managed by the bound environment; unset it before restarting" >&2
    exit 1
  fi
  if ! grep -q '"phase": "complete"' "$REPO_ROOT/.run/generated/state.json" 2>/dev/null; then
    echo "Environment initialization is not complete; refusing to start the Brain" >&2
    exit 1
  fi
  if ! command -v portless >/dev/null 2>&1; then
    echo "Bound environments route through portless; install it with: npm install -g portless" >&2
    exit 1
  fi
  OMA_API_URL="$(portless get "$API_ROUTE" --no-worktree 2>/dev/null || echo "http://${API_ROUTE}.localhost")"
  OMA_BRAIN_CLAIM_TOKEN="$(cat "$REPO_ROOT/.run/generated/secrets/brain-claim-token")"
  if [[ -z "${OMA_BRAIN_CLAIM_TOKEN//[[:space:]]/}" ]]; then
    echo "Bound environment Brain claim token is empty; restore the generated token before restarting" >&2
    exit 1
  fi
  export OMA_API_URL OMA_BRAIN_CLAIM_TOKEN
fi

STATE_DIR="$PROJECT_DIR/.run"
PID_FILE="$STATE_DIR/brain.pid"
LOCK_DIR="$STATE_DIR/restart.lock"

for tool in bun lsof; do
  command -v "$tool" >/dev/null || { echo "Missing command: $tool" >&2; exit 1; }
done
mkdir -p "$STATE_DIR"
mkdir "$LOCK_DIR" 2>/dev/null || { echo "Another restart is running: $LOCK_DIR" >&2; exit 1; }
trap 'rmdir "$LOCK_DIR"' EXIT

# Build before stopping the running service.
bun run build

is_project_process() {
  local pid="$1" command cwd
  command="$(ps -p "$pid" -o command= 2>/dev/null)" || return 1
  case "$command" in
    bun\ *|*/bun\ *) command="bun ${command#* }" ;;
    *) return 1 ;;
  esac
  case "$command" in
    "bun src/main.ts"|"bun run src/main.ts"|"bun dist/main.js"|\
    "bun $PROJECT_DIR/src/main.ts"|"bun $PROJECT_DIR/dist/main.js"|\
    "bun $PROJECT_DIR/bin/oma-brain-dsh"|"bun bin/oma-brain-dsh") ;;
    *) return 1 ;;
  esac
  cwd="$(lsof -a -p "$pid" -d cwd -Fn 2>/dev/null | sed -n 's/^n//p')"
  [[ "$cwd" == "$PROJECT_DIR" ]]
}

# Find direct Bun launches as well as processes started by this script.
PIDS=()
while read -r pid command; do
  case "$command" in bun\ *|*/bun\ *) ;; *) continue ;; esac
  if is_project_process "$pid"; then
    PIDS+=("$pid")
  fi
done < <(ps -axo pid=,command=)

for pid in ${PIDS[@]+"${PIDS[@]}"}; do
  echo "Stopping Brain (PID $pid)..."
  kill -TERM "$pid"
done
for pid in ${PIDS[@]+"${PIDS[@]}"}; do
  for ((attempt = 0; attempt < 30; attempt++)); do
    if ! is_project_process "$pid"; then break; fi
    sleep 1
  done
  if is_project_process "$pid"; then
    echo "Brain (PID $pid) did not stop within 30 seconds; restart cancelled." >&2
    exit 1
  fi
done
rm -f "$PID_FILE"

echo "Starting Brain..."
# Release the restart lock before replacing the shell with the foreground service.
rmdir "$LOCK_DIR"
trap - EXIT
exec bun dist/main.js
