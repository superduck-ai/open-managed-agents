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
    const owns = cwd => Bun.spawnSync(["git", "-C", cwd, "rev-parse", "--show-toplevel"]).stdout.toString().trim() === root;
    const names = ["oma-server", "oma-brain", "web"];
    for (const pane of result.panes) {
      const cwd = pane.foreground_cwd ?? pane.cwd;
      if (names.includes(pane.label) && cwd && owns(cwd)) {
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
      if (!p.cwd || Bun.spawnSync(["git", "-C", p.cwd, "rev-parse", "--show-toplevel"]).stdout.toString().trim() !== root) return false;
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

interrupt_pane() {
  herdr pane send-keys "$1" ctrl+c >/dev/null
}

read_pane() {
  herdr pane read "$1" --source recent-unwrapped --lines 30 --raw
}

run_in_pane() {
  herdr pane run "$1" "$2" >/dev/null
}

wait_ready() {
  herdr pane wait-output "$1" --match "$2" --timeout "$START_TIMEOUT_MS" >/dev/null
}

close_pane() {
  herdr pane close "$1" >/dev/null
}

create_layout() {
  local layout
  layout="$(herdr tab create --workspace "$HERDR_WORKSPACE_ID" --cwd "$REPO_ROOT" --label "OMA services" --no-focus | bun -e '
    const { result } = JSON.parse(await Bun.stdin.text());
    if (!result.tab?.tab_id || !result.root_pane?.pane_id) process.exit(1);
    console.log(result.tab.tab_id + "|" + result.root_pane.pane_id);
  ')"
  IFS='|' read -r SERVICE_TAB SERVER_PANE <<< "$layout"
  BRAIN_PANE="$(herdr pane split "$SERVER_PANE" --direction right --cwd "$REPO_ROOT" --no-focus | pane_id)"
  WEB_PANE="$(herdr pane split "$BRAIN_PANE" --direction down --cwd "$REPO_ROOT" --no-focus | pane_id)"
  herdr pane rename "$SERVER_PANE" oma-server >/dev/null
  herdr pane rename "$BRAIN_PANE" oma-brain >/dev/null
  herdr pane rename "$WEB_PANE" web >/dev/null
}

focus_services() {
  herdr tab focus "$SERVICE_TAB" >/dev/null
}
