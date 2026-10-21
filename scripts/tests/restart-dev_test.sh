#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture_dir="$(mktemp -d)"
fixture_dir="$(cd "$fixture_dir" && pwd -P)"
trap 'rm -rf "$fixture_dir"' EXIT
mkdir -p "$fixture_dir/repo/scripts" "$fixture_dir/bin"
git init -q "$fixture_dir/repo"
mkdir -p "$fixture_dir/repo/.worktrees/other"
git init -q "$fixture_dir/repo/.worktrees/other"
cp "$repo_root/scripts/restart-dev.sh" "$fixture_dir/repo/scripts/restart-dev.sh"
cat > "$fixture_dir/bin/herdr" <<'MOCK'
#!/usr/bin/env bun
const args = process.argv.slice(2);
const dir = process.env.MOCK_STATE;
const mode = process.env.MOCK_MODE;
const root = process.env.MOCK_ROOT;
const log = `${dir}/calls`;
const previous = await Bun.file(log).text().catch(() => "");
await Bun.write(log, previous + JSON.stringify(args) + "\n");
const reply = result => console.log(JSON.stringify({ result }));
const old = ["oma-server", "oma-brain", "web"].map((label, i) => ({ pane_id: `old-${i}`, tab_id: "old-tab", label, cwd: root }));
if (args[0] === "pane" && args[1] === "list") {
  reply({ panes: [...(["replace", "busy", "self"].includes(mode) ? old : []), { pane_id: "foreign", label: "web", cwd: root + "/.worktrees/other" }] });
} else if (args[0] === "tab" && args[1] === "create") {
  reply({ tab: { tab_id: "new-tab" }, root_pane: { pane_id: "new-server" } });
} else if (args[1] === "split") {
  reply({ pane: { pane_id: args[2] === "new-server" ? "new-brain" : "new-web" } });
} else if (args[1] === "process-info") {
  const id = args[args.indexOf("--pane") + 1];
  if (mode === "process-error" && id === "new-server") {
    console.error("mock process lookup failed"); process.exit(1);
  }
  const file = `${dir}/${id}`;
  const count = Number(await Bun.file(file).text().catch(() => "0")) + 1;
  await Bun.write(file, String(count));
  const shell = { pid: 100, name: "zsh", cwd: root, cmdline: "zsh" };
  let processes = [shell];
  if (id.startsWith("new-") && count === 1) processes = [];
  if ((id.startsWith("new-") && count === 2) || mode === "busy") processes = [{ pid: 101, name: "git", cwd: root, cmdline: "git status" }];
  if (id.startsWith("old-") && mode === "replace" && count === 1) {
    const recipe = ["restart-server", "restart-brain", "restart-web"][Number(id.slice(-1))];
    processes = [{ pid: 101, name: "just", cwd: root, cmdline: `just ${recipe}` }];
  }
  reply({ process_info: { shell_pid: 100, foreground_processes: processes } });
} else if (args[1] === "run") {
  const id = args[2];
  if (Number(await Bun.file(`${dir}/${id}`).text()) < 3) throw new Error("command sent before shell readiness");
  if (mode === "run-error") { console.error("mock command submission failed"); process.exit(1); }
  reply({});
} else if (args[1] === "wait-output" && mode === "wait-error") {
  console.error("mock readiness timed out"); process.exit(1);
} else if (args[1] === "read") {
  console.log("fixture diagnostic: service startup failed");
} else {
  reply({});
}
MOCK
chmod +x "$fixture_dir/bin/herdr"
for scenario in busy self process-error run-error wait-error fresh replace; do
  mkdir -p "$fixture_dir/$scenario"
  caller=control
  [[ "$scenario" != self ]] || caller=old-1
  status=0
  PATH="$fixture_dir/bin:$PATH" HERDR_ENV=1 HERDR_WORKSPACE_ID=mock HERDR_PANE_ID="$caller" \
    MOCK_MODE="$scenario" MOCK_STATE="$fixture_dir/$scenario" MOCK_ROOT="$fixture_dir/repo" \
    bash "$fixture_dir/repo/scripts/restart-dev.sh" > "$fixture_dir/$scenario/output" 2>&1 || status=$?
  bun -e '
    const [dir, mode, status] = process.argv.slice(1);
    const calls = (await Bun.file(dir + "/calls").text()).trim().split("\n").map(JSON.parse);
    const output = await Bun.file(dir + "/output").text();
    const check = (ok, message) => { if (!ok) throw new Error(mode + ": " + message + "\n" + output); };
    const matching = command => calls.filter(a => a[0] + " " + a[1] === command);
    const success = ["fresh", "replace"].includes(mode);
    check((status === "0") === success, "unexpected exit status");
    check(!calls.some(a => a.includes("foreign")), "touched another worktree");
    if (!success) check(matching("tab focus").length === 0, "hid error in another tab");
    if (["busy", "self"].includes(mode)) {
      check(matching("tab create").length === 0, "created new panels before rejecting unsafe old panels");
      check(matching("pane run").length === 0 && matching("pane close").length === 0 && matching("pane send-keys").length === 0, "interrupted an unsafe pane");
      check(output.includes(mode === "busy" ? "pid=101 name=git" : "control pane"), "missing refusal reason");
    } else if (!success) {
      check(matching("pane close").length === 0, "removed failed startup panes");
      check(matching("pane run").length <= 1, "started subsequent services after failure");
      check(output.includes("fixture diagnostic") && output.includes(mode === "process-error" ? "shell initialization failed" : mode === "run-error" ? "could not send" : "startup failed or timed out"), "missing startup diagnostics");
    } else {
      check(matching("pane run").map(a => a[2]).join(",") === "new-server,new-brain,new-web", "incorrect start order or reused pane");
      check(matching("tab create")[0].includes("--no-focus"), "focused before startup succeeded");
      check(calls.at(-1).join(" ") === "tab focus new-tab", "did not focus after readiness");
      if (mode === "fresh") check(matching("pane send-keys").length === 0, "interrupted a new shell");
      if (mode === "replace") {
        check(matching("pane send-keys").length === 3 && matching("pane close").length === 3, "old services were not stopped and closed");
        check(calls.findLastIndex(a => a[1] === "close") < calls.findIndex(a => a[0] === "tab" && a[1] === "create"), "created layout before retiring old panels");
      }
    }
    console.log(mode + ": passed");
  ' "$fixture_dir/$scenario" "$scenario" "$status"
done
