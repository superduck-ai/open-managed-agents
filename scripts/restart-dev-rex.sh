REX_SERVICE_KEY="$(bun -e 'console.log(new Bun.CryptoHasher("sha256").update(process.argv[1]).digest("hex"))' "$REPO_ROOT")"

rex_process() {
  rex block call -s "$REX_SESSION" -b "$1" process
}

find_service_panes() {
  local panes pane label
  panes="$(rex block ls -s "$REX_SESSION" --json | bun -e '
    const { blocks } = JSON.parse(await Bun.stdin.text());
    for (const block of blocks) {
      if (block.creator_name === "com.superlogical.terminal" && ["oma-server", "oma-brain", "web"].includes(block.label)) {
        console.log(block.block_id + "|" + block.label);
      }
    }
  ')" || return 1
  while IFS='|' read -r pane label; do
    [[ -n "$pane" ]] || continue
    rex_process "$pane" | bun -e '
      const info = JSON.parse(await Bun.stdin.text());
      const [root, pane, label] = process.argv.slice(1);
      const cwd = info.foreground?.cwd ?? info.child?.cwd;
      if (cwd && Bun.spawnSync(["git", "-C", cwd, "rev-parse", "--show-toplevel"]).stdout.toString().trim() === root) {
        console.log(pane + "|" + label);
      }
    ' "$REPO_ROOT" "$pane" "$label" || return 1
  done <<< "$panes"
}

pane_process_state() {
  rex_process "$1" | bun -e '
    const info = JSON.parse(await Bun.stdin.text());
    const [root, recipe, key] = process.argv.slice(1);
    const foreground = info.foreground;
    if (!foreground || !info.child) {
      console.log("starting");
      process.exit(0);
    }
    const ancestors = info.ancestors ?? [];
    const shells = ["sh", "bash", "zsh", "fish", "dash", "ksh", "nu"];
    if (shells.includes(foreground.name) && !foreground.script &&
        (foreground.pid === info.child.pid ||
         (info.child.name === "login" && ancestors.length === 1 && ancestors[0].pid === info.child.pid))) {
      console.log("shell");
      process.exit(0);
    }
    const scripts = [root + "/scripts/restart-dev.sh", recipe === "restart-brain"
      ? root + "/oma-brain-dsh/scripts/restart.sh" : root + "/scripts/" + recipe + ".sh"];
    const known = [foreground, ...ancestors].some(p => p.cwd &&
      (scripts.includes(p.script) || p.argv0 === "oma-" + recipe + ":" + key) &&
      Bun.spawnSync(["git", "-C", p.cwd, "rev-parse", "--show-toplevel"]).stdout.toString().trim() === root);
    console.log(known ? "service" : "busy: pid=" + foreground.pid + " name=" + foreground.name);
  ' "$REPO_ROOT" "$2" "$REX_SERVICE_KEY"
}

interrupt_pane() {
  rex send-key -s "$REX_SESSION" -b "$1" ctrl+c >/dev/null
}

read_pane() {
  rex capture -s "$REX_SESSION" -b "$1" --trim --unwrap | tail -n 30
}

run_in_pane() {
  local launch command
  printf -v launch 'exec -a %q %s' "oma-$3:$REX_SERVICE_KEY" "$2"
  printf -v command 'bash -c %q' "$launch"
  rex send -s "$REX_SESSION" -b "$1" "$command" >/dev/null &&
    rex send-key -s "$REX_SESSION" -b "$1" enter >/dev/null
}

wait_ready() {
  local output deadline
  deadline="$(bun -e 'console.log(Date.now() + Number(process.argv[1]))' "$START_TIMEOUT_MS")"
  while bun -e 'process.exit(Date.now() < Number(process.argv[1]) ? 0 : 1)' "$deadline"; do
    output="$(rex capture -s "$REX_SESSION" -b "$1" --trim --unwrap)" || return 1
    if [[ "$output" == *"$2"* ]]; then
      return 0
    fi
    sleep 0.2
  done
  return 1
}

close_pane() {
  rex block close -s "$REX_SESSION" "$1" >/dev/null
}

pane_id() {
  bun -e '
    const result = JSON.parse(await Bun.stdin.text());
    if (typeof result.block_ids?.[0] !== "string") process.exit(1);
    console.log(result.block_ids[0]);
  '
}

create_layout() {
  local layout
  layout="$(rex window new -s "$REX_SESSION" "OMA services" --cwd "$REPO_ROOT" --focus=false --json | bun -e '
    const result = JSON.parse(await Bun.stdin.text());
    if (!result.window_id || !result.block_ids?.[0]) process.exit(1);
    console.log(result.window_id + "|" + result.block_ids[0]);
  ')"
  IFS='|' read -r SERVICE_TAB SERVER_PANE <<< "$layout"
  BRAIN_PANE="$(rex split -s "$REX_SESSION" -b "$SERVER_PANE" --split=right --cwd "$REPO_ROOT" --focus=false --json | pane_id)"
  WEB_PANE="$(rex split -s "$REX_SESSION" -b "$BRAIN_PANE" --split=below --cwd "$REPO_ROOT" --focus=false --json | pane_id)"
  rex block rename -s "$REX_SESSION" "$SERVER_PANE" oma-server >/dev/null
  rex block rename -s "$REX_SESSION" "$BRAIN_PANE" oma-brain >/dev/null
  rex block rename -s "$REX_SESSION" "$WEB_PANE" web >/dev/null
}

focus_services() {
  rex focus -s "$REX_SESSION" "$SERVICE_TAB" >/dev/null
}
