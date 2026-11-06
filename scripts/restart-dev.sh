#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
START_TIMEOUT_MS="${OMA_DEV_START_TIMEOUT_MS:-${HERDR_DEV_START_TIMEOUT_MS:-180000}}"

run_service() {
  local recipe="$1" ready="$2" marker="$3" line
  cd "$REPO_ROOT"
  just "$recipe" 2>&1 | while IFS= read -r line || [[ -n "$line" ]]; do
    printf '%s\n' "$line"
    if [[ "$line" == *"$ready"* ]]; then
      printf 'OMA_READY_%s\n' "$marker"
    fi
  done
}

if [[ "${1:-}" == --service ]]; then
  [[ "$#" -eq 4 ]] || exit 2
  case "$2" in
    restart-server|restart-brain|restart-web) run_service "$2" "$3" "$4" ;;
    *) exit 2 ;;
  esac
  exit
fi

if [[ -n "${REX_SESSION:-}" && -n "${REX_BLOCK:-}" ]]; then
  TERMINAL=rex
  CONTROL_PANE="$REX_BLOCK"
elif [[ "${HERDR_ENV:-}" == 1 && -n "${HERDR_WORKSPACE_ID:-}" ]]; then
  TERMINAL=herdr
  CONTROL_PANE="${HERDR_PANE_ID:-}"
else
  echo "Run just restart inside Herdr or Rex, from a control pane in this repository." >&2
  exit 1
fi

for tool in "$TERMINAL" just bun git; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Missing command: $tool" >&2
    exit 1
  fi
done

if ! [[ "$START_TIMEOUT_MS" =~ ^[1-9][0-9]*$ ]]; then
  echo "OMA_DEV_START_TIMEOUT_MS (or HERDR_DEV_START_TIMEOUT_MS) must be a positive integer" >&2
  exit 1
fi

source "$REPO_ROOT/scripts/restart-dev-$TERMINAL.sh"

stop_service() {
  local pane="$1" recipe="$2" state interrupted=0 deadline=$((SECONDS + 30))
  if [[ "$pane" == "$CONTROL_PANE" ]]; then
    echo "Run just restart from a control pane, outside the service panes." >&2
    return 1
  fi
  while ((SECONDS < deadline)); do
    if ! state="$(pane_process_state "$pane" "$recipe")"; then
      echo "Cannot identify the process in $pane; refusing to interrupt it." >&2
      return 1
    fi
    [[ "$state" == shell ]] && return 0
    if [[ "$state" == busy:* ]]; then
      echo "Cannot identify the process in $pane ($state); refusing to interrupt it." >&2
      return 1
    fi
    if [[ "$state" == service && "$interrupted" -eq 0 ]]; then
      interrupt_pane "$pane" || return 1
      interrupted=1
    fi
    sleep 0.2
  done
  echo "Service in $pane did not stop within 30 seconds; restart cancelled." >&2
  return 1
}

wait_new_shell() {
  local pane="$1" recipe="$2" state=starting deadline=$((SECONDS + 30))
  echo "Waiting for shell in new pane $pane..."
  while ((SECONDS < deadline)); do
    if ! state="$(pane_process_state "$pane" "$recipe")"; then
      echo "Cannot read shell state in new pane $pane." >&2
      return 1
    fi
    [[ "$state" == shell ]] && return 0
    sleep 0.2
  done
  echo "New pane $pane did not reach an idle shell within 30 seconds (last state: $state)." >&2
  return 1
}

service_failure() {
  local pane="$1" name="$2" reason="$3"
  echo "$name: $reason (pane $pane). Recent output:" >&2
  read_pane "$pane" >&2 || true
}

start_service() {
  local pane="$1" name="$2" recipe="$3" ready="$4" command marker
  if ! wait_new_shell "$pane" "$recipe"; then
    service_failure "$pane" "$name" "shell initialization failed"
    return 1
  fi
  marker="$(bun -e 'console.log(crypto.randomUUID())')"
  printf -v command 'bash %q --service %q %q %q' "$REPO_ROOT/scripts/restart-dev.sh" "$recipe" "$ready" "$marker"
  echo "Starting $name in $pane..."
  if ! run_in_pane "$pane" "$command" "$recipe"; then
    service_failure "$pane" "$name" "could not send the startup command"
    return 1
  fi
  if ! wait_ready "$pane" "OMA_READY_$marker"; then
    service_failure "$pane" "$name" "startup failed or timed out after ${START_TIMEOUT_MS}ms"
    return 1
  fi
}

print_url() {
  local label="$1" url="$2"
  if [[ -t 1 && "${TERM:-}" != dumb ]]; then
    printf '  %-5s \033]8;;%s\033\\%s\033]8;;\033\\\n' "$label" "$url" "$url"
  else
    printf '  %-5s %s\n' "$label" "$url"
  fi
}

print_access_urls() {
  local binding="$REPO_ROOT/.run/env.json" name
  [[ -f "$binding" ]] || return 0
  name="$(bun -e '
    const binding = await Bun.file(process.argv[1]).json();
    if (typeof binding.name !== "string" || !binding.name) process.exit(1);
    console.log(binding.name);
  ' "$binding")"
  printf '\nOpen Managed Agents\n'
  print_url Web "http://oma-${name}-web.localhost"
  print_url API "http://oma-${name}-api.localhost"
}

OLD_PANES="$(find_service_panes)"
while IFS='|' read -r OLD_PANE OLD_LABEL; do
  [[ -n "$OLD_PANE" ]] || continue
  if [[ "$OLD_PANE" == "$CONTROL_PANE" ]]; then
    echo "Run just restart from a control pane, outside the service panes." >&2
    exit 1
  fi
done <<< "$OLD_PANES"
while IFS='|' read -r OLD_PANE OLD_LABEL; do
  [[ -n "$OLD_PANE" ]] || continue
  case "$OLD_LABEL" in
    oma-server) OLD_RECIPE=restart-server ;;
    oma-brain) OLD_RECIPE=restart-brain ;;
    web) OLD_RECIPE=restart-web ;;
  esac
  if ! stop_service "$OLD_PANE" "$OLD_RECIPE"; then
    service_failure "$OLD_PANE" "$OLD_LABEL" "could not safely stop the previous service"
    exit 1
  fi
done <<< "$OLD_PANES"
while IFS='|' read -r OLD_PANE OLD_LABEL; do
  [[ -n "$OLD_PANE" ]] || continue
  close_pane "$OLD_PANE"
done <<< "$OLD_PANES"

create_layout

start_service "$SERVER_PANE" oma-server restart-server "claude api server listening"
start_service "$BRAIN_PANE" oma-brain restart-brain "oma-brain-dsh ready"
start_service "$WEB_PANE" web restart-web "Local:"

focus_services

echo "OMA services ready: server=$SERVER_PANE brain=$BRAIN_PANE web=$WEB_PANE"
print_access_urls
