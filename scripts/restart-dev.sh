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

find_layout() {
  herdr pane list --workspace "$HERDR_WORKSPACE_ID" | bun -e '
    const { result } = JSON.parse(await Bun.stdin.text());
    const root = process.argv[1];
    const names = ["oma-server", "oma-brain", "web"];
    const tabs = new Map();
    for (const pane of result.panes) {
      const cwd = pane.foreground_cwd ?? pane.cwd;
      if (!names.includes(pane.label) || !cwd || (cwd !== root && !cwd.startsWith(root + "/"))) continue;
      const panes = tabs.get(pane.tab_id) ?? new Map();
      if (panes.has(pane.label)) throw new Error("Duplicate service panes in " + pane.tab_id);
      panes.set(pane.label, pane.pane_id);
      tabs.set(pane.tab_id, panes);
    }
    const selected = [...tabs].sort((a, b) => a[1].size - b[1].size).at(-1);
    console.log(selected ? [selected[0], ...names.map(name => selected[1].get(name) ?? "")].join("|") : "|||");
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
    if (!known) process.exit(1);
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
    if [[ "$state" == service && "$interrupted" -eq 0 ]]; then
      herdr pane send-keys "$pane" ctrl+c >/dev/null
      interrupted=1
    fi
    sleep 0.2
  done
  echo "Service in $pane did not stop within 30 seconds; restart cancelled." >&2
  return 1
}

start_service() {
  local pane="$1" name="$2" recipe="$3" ready="$4" command marker
  stop_service "$pane" "$recipe"
  marker="$(bun -e 'console.log(crypto.randomUUID())')"
  printf -v command 'bash %q --service %q %q %q' "$REPO_ROOT/scripts/restart-dev.sh" "$recipe" "$ready" "$marker"
  herdr pane run "$pane" "$command" >/dev/null
  if ! herdr pane wait-output "$pane" --match "OMA_READY_$marker" --timeout "$START_TIMEOUT_MS" >/dev/null; then
    echo "$name did not become ready. Inspect pane $pane; startup logs are preserved." >&2
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

SERVICE_LAYOUT="$(find_layout)"
IFS='|' read -r SERVICE_TAB SERVER_PANE BRAIN_PANE WEB_PANE <<< "$SERVICE_LAYOUT"
if [[ -z "$SERVICE_TAB" ]]; then
  SERVER_PANE="$(herdr tab create --workspace "$HERDR_WORKSPACE_ID" --cwd "$REPO_ROOT" --label "OMA services" --focus | pane_id)"
else
  herdr tab focus "$SERVICE_TAB" >/dev/null
fi
if [[ -z "$SERVER_PANE" ]]; then
  SERVER_PANE="$(herdr pane split "${BRAIN_PANE:-$WEB_PANE}" --direction right --cwd "$REPO_ROOT" --no-focus | pane_id)"
fi
if [[ -z "$BRAIN_PANE" ]]; then
  BRAIN_PANE="$(herdr pane split "$SERVER_PANE" --direction right --cwd "$REPO_ROOT" --no-focus | pane_id)"
fi
if [[ -z "$WEB_PANE" ]]; then
  WEB_PANE="$(herdr pane split "$BRAIN_PANE" --direction down --cwd "$REPO_ROOT" --no-focus | pane_id)"
fi

herdr pane rename "$SERVER_PANE" oma-server >/dev/null
herdr pane rename "$BRAIN_PANE" oma-brain >/dev/null
herdr pane rename "$WEB_PANE" web >/dev/null

start_service "$SERVER_PANE" oma-server restart-server "claude api server listening"
start_service "$BRAIN_PANE" oma-brain restart-brain "oma-brain-dsh ready"
start_service "$WEB_PANE" web restart-web "Local:"

echo "OMA services ready: server=$SERVER_PANE brain=$BRAIN_PANE web=$WEB_PANE"
print_access_urls
