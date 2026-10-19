#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$PROJECT_DIR"

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
