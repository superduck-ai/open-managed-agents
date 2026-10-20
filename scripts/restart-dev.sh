#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
START_TIMEOUT_MS="${HERDR_DEV_START_TIMEOUT_MS:-180000}"

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

if [[ "${HERDR_ENV:-}" != 1 || -z "${HERDR_WORKSPACE_ID:-}" ]]; then
  echo "Run this command inside Herdr: open herdr, then run just restart in this repository." >&2
  exit 1
fi

for tool in herdr just bun; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "Missing command: $tool" >&2
    exit 1
  fi
done

if ! [[ "$START_TIMEOUT_MS" =~ ^[1-9][0-9]*$ ]]; then
  echo "HERDR_DEV_START_TIMEOUT_MS must be a positive integer" >&2
  exit 1
fi

pane_id() {
  bun -e '
    const response = JSON.parse(await Bun.stdin.text());
    const pane = response.result?.root_pane ?? response.result?.pane;
    if (typeof pane?.pane_id !== "string") process.exit(1);
    console.log(pane.pane_id);
  '
}

find_service_panes() {
  herdr pane list --workspace "$HERDR_WORKSPACE_ID" | bun -e '
    const { result } = JSON.parse(await Bun.stdin.text());
    const root = process.argv[1];
    const names = ["oma-server", "oma-brain", "web"];
    for (const pane of result.panes) {
      const cwd = pane.foreground_cwd ?? pane.cwd;
      if (names.includes(pane.label) && cwd && (cwd === root || cwd.startsWith(root + "/"))) {
        console.log(pane.pane_id + "|" + pane.label);
      }
    }
  ' "$REPO_ROOT"
}

pane_process_state() {
  local pane="$1" recipe="$2"
  herdr pane process-info --pane "$pane" | bun -e '
    const { result } = JSON.parse(await Bun.stdin.text());
    const info = result.process_info;
    const processes = info.foreground_processes;
    if (!Array.isArray(processes) || !info.shell_pid || processes.length === 0) {
      console.log("starting");
      process.exit(0);
    }
    if (processes.length > 0 && processes.every(p => p.pid === info.shell_pid)) {
      console.log("shell");
      process.exit(0);
    }
    const [root, recipe] = process.argv.slice(1);
    const known = processes.some(p => {
      if (!p.cwd || (p.cwd !== root && !p.cwd.startsWith(root + "/"))) return false;
      const command = p.cmdline ?? p.argv?.join(" ") ?? "";
      if (command.includes(recipe)) return true;
      if (recipe === "restart-server") return /go run \.|\/exe\/open-managed-agents/.test(command);
      if (recipe === "restart-brain") return /bun (?:dist\/main\.js|src\/main\.ts)/.test(command);
      return /(?:^|\/)vite(?: |$)/.test(command);
    });
    if (!known) {
      console.log("busy: " + processes.map(p => "pid=" + p.pid + " name=" + p.name).join(", "));
      process.exit(0);
    }
    console.log("service");
  ' "$REPO_ROOT" "$recipe"
}

stop_service() {
  local pane="$1" recipe="$2" state interrupted=0 deadline=$((SECONDS + 30))
  if [[ "$pane" == "${HERDR_PANE_ID:-}" ]]; then
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
      herdr pane send-keys "$pane" ctrl+c >/dev/null || return 1
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
  herdr pane read "$pane" --source recent-unwrapped --lines 30 --raw >&2 || true
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
  if ! herdr pane run "$pane" "$command" >/dev/null; then
    service_failure "$pane" "$name" "could not send the startup command"
    return 1
  fi
  if ! herdr pane wait-output "$pane" --match "OMA_READY_$marker" --timeout "$START_TIMEOUT_MS" >/dev/null; then
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
  if [[ "$OLD_PANE" == "${HERDR_PANE_ID:-}" ]]; then
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
  herdr pane close "$OLD_PANE" >/dev/null
done <<< "$OLD_PANES"

SERVICE_LAYOUT="$(herdr tab create --workspace "$HERDR_WORKSPACE_ID" --cwd "$REPO_ROOT" --label "OMA services" --no-focus | bun -e '
  const { result } = JSON.parse(await Bun.stdin.text());
  if (!result.tab?.tab_id || !result.root_pane?.pane_id) process.exit(1);
  console.log(result.tab.tab_id + "|" + result.root_pane.pane_id);
')"
IFS='|' read -r SERVICE_TAB SERVER_PANE <<< "$SERVICE_LAYOUT"
BRAIN_PANE="$(herdr pane split "$SERVER_PANE" --direction right --cwd "$REPO_ROOT" --no-focus | pane_id)"
WEB_PANE="$(herdr pane split "$BRAIN_PANE" --direction down --cwd "$REPO_ROOT" --no-focus | pane_id)"

herdr pane rename "$SERVER_PANE" oma-server >/dev/null
herdr pane rename "$BRAIN_PANE" oma-brain >/dev/null
herdr pane rename "$WEB_PANE" web >/dev/null

start_service "$SERVER_PANE" oma-server restart-server "claude api server listening"
start_service "$BRAIN_PANE" oma-brain restart-brain "oma-brain-dsh ready"
start_service "$WEB_PANE" web restart-web "Local:"

herdr tab focus "$SERVICE_TAB" >/dev/null

echo "OMA services ready: server=$SERVER_PANE brain=$BRAIN_PANE web=$WEB_PANE"
print_access_urls
