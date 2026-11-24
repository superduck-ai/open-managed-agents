#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture_dir="$(mktemp -d)"
fixture_dir="$(cd "$fixture_dir" && pwd -P)"
launcher_pid=
cleanup() {
  if [[ -n "$launcher_pid" ]]; then
    kill -TERM "$launcher_pid" 2>/dev/null || true
    wait "$launcher_pid" 2>/dev/null || true
  fi
  rm -rf "$fixture_dir"
}
trap cleanup EXIT
mkdir -p "$fixture_dir/repo/scripts" "$fixture_dir/repo/.run/generated" "$fixture_dir/bin"
git init -q "$fixture_dir/repo"
mkdir -p "$fixture_dir/repo/.worktrees/other"
git init -q "$fixture_dir/repo/.worktrees/other"
cp "$repo_root/scripts/restart-dev.sh" "$repo_root/scripts/restart-harness.sh" "$repo_root/scripts/harness-status.ts" "$fixture_dir/repo/scripts/"
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
const oldLabels = ["oma-server", mode === "replace-harness" ? "oma-harness" : "oma-brain", "web"];
if (["replace-harness", "busy-status"].includes(mode)) oldLabels.push("harness-status");
const old = oldLabels.map((label, i) => ({ pane_id: `old-${i}`, tab_id: "old-tab", label, cwd: root }));
if (args[0] === "pane" && args[1] === "list") {
  const local = mode === "busy-status" ? old.slice(-1) : (["replace", "replace-harness", "busy", "self"].includes(mode) ? old : []);
  reply({ panes: [...local, { pane_id: "foreign", label: "oma-harness", cwd: root + "/.worktrees/other" },
    { pane_id: "foreign-status", label: "harness-status", cwd: root + "/.worktrees/other" }] });
} else if (args[0] === "tab" && args[1] === "create") {
  reply({ tab: { tab_id: "new-tab" }, root_pane: { pane_id: "new-server" } });
} else if (args[1] === "split") {
  const pane = args.includes("0.72") ? "new-status" : args[2] === "new-server" ? "new-brain" : "new-web";
  reply({ pane: { pane_id: pane } });
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
  if (mode === "monitor") {
    const workerPath = root + "/tmp/dev-harness/exe/oma-harness-worker";
    processes = [{ pid: 999, cwd: root + "/.worktrees/other", argv: [workerPath] }];
    const worker = [301, 301, 301, 304][count - 1];
    if (count <= 5) processes.push({ pid: 200, cwd: root, argv: ["bash", "./scripts/restart-harness.sh"] });
    if (worker) processes.push({ pid: worker, cwd: root, argv: [workerPath] });
  }
  if (id.startsWith("new-") && count === 1) processes = [];
  if ((id.startsWith("new-") && count === 2) || mode === "busy") processes = [{ pid: 101, name: "git", cwd: root, cmdline: "git status" }];
  if (mode === "busy-status") processes = [{ pid: 101, name: "vim", cwd: root, cmdline: "vim " + root + "/scripts/harness-status.ts" }];
  if (id.startsWith("old-") && mode.startsWith("replace") && count === 1) {
    const index = Number(id.slice(-1));
    const command = ["/tmp/go-build123/b001/exe/oma-server", "just restart-brain", "just restart-web",
      "bun " + root + "/scripts/harness-status.ts old-1 " + root];
    if (mode === "replace-harness") command[1] = root + "/tmp/dev-harness/exe/oma-harness-worker -api-url http://oma-fixture-api.localhost";
    const cmdline = command[index];
    processes = [{ pid: 101, cwd: root, cmdline }];
  }
  reply({ process_info: { shell_pid: 100, foreground_processes: processes } });
} else if (args[1] === "run") {
  const id = args[2];
  if (Number(await Bun.file(`${dir}/${id}`).text()) < 3) throw new Error("command sent before shell readiness");
  if (mode === "run-error") { console.error("mock command submission failed"); process.exit(1); }
  reply({});
} else if (args[1] === "wait-output" && mode === "wait-error") {
  console.error("mock readiness timed out"); process.exit(1);
} else if (args[1] === "wait-output" && mode === "harness" && args[2] === "new-server") {
  await Bun.write(root + "/.run/generated/config.yaml", "environment_runner: { cloud_harness: true }\n");
  reply({});
} else if (args[1] === "read" && mode === "monitor") {
  const count = Number(await Bun.file(`${dir}/monitor-worker`).text());
  const logs = [
    ["11:00:00.100 INFO harness worker ready"],
    ["11:00:01.100 INFO [harness_worker] harness turn claimed task_id=test task_kind=model"],
    ["11:00:03.100 INFO [harness_worker] harness turn claimed task_id=tool task_kind=tool"],
    ["11:00:04.100 INFO [harness_worker] harness turn acknowledged state=waiting_tool",
      "harness worker process exited pid=301 status=0 elapsed=4s",
      "11:00:05.100 INFO harness worker ready"],
    ["11:00:06.100 INFO [harness_worker] harness turn claimed task_id=resume task_kind=model",
      "11:00:07.100 INFO [harness_worker] harness turn acknowledged state=completed",
      "harness worker process exited pid=304 status=0 elapsed=2s",
      "11:00:07.200 INFO harness worker ready"],
    ["11:00:37.200 INFO [harness_worker] harness turn claimed task_id=retry task_kind=tool",
      "11:00:38.100 ERROR harness worker stopped error=\"harness turn needs redelivery\\nprivate-sandbox-detail\"",
      "harness worker process exited pid=305 status=1 elapsed=1s"],
  ];
  console.log(logs.slice(0, count).flat().join("\n"));
} else if (args[1] === "read") {
  console.log("fixture diagnostic: service startup failed");
} else {
  reply({});
}
MOCK
chmod +x "$fixture_dir/bin/herdr"
for scenario in busy busy-status self process-error run-error wait-error fresh replace harness replace-harness; do
  mkdir -p "$fixture_dir/$scenario"
  printf '%s\n' 'environment_runner: { cloud_harness: false }' > "$fixture_dir/repo/.run/config.yaml"
  if [[ "$scenario" == harness ]]; then
    printf '%s\n' '{"name": "fixture"}' > "$fixture_dir/repo/.run/env.json"
    printf '%s\n' 'environment_runner: { cloud_harness: false }' > "$fixture_dir/repo/.run/generated/config.yaml"
  else
    rm -f "$fixture_dir/repo/.run/env.json"
  fi
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
    const success = ["fresh", "replace", "harness", "replace-harness"].includes(mode);
    check((status === "0") === success, "unexpected exit status");
    check(!calls.some(a => a.includes("foreign") || a.includes("foreign-status")), "touched another worktree");
    if (!success) check(matching("tab focus").length === 0, "hid error in another tab");
    if (["busy", "busy-status", "self"].includes(mode)) {
      check(matching("tab create").length === 0, "created new panels before rejecting unsafe old panels");
      check(matching("pane run").length === 0 && matching("pane close").length === 0 && matching("pane send-keys").length === 0, "interrupted an unsafe pane");
      check(output.includes(mode === "self" ? "control pane" : mode === "busy" ? "pid=101 name=git" : "pid=101 name=vim"), "missing refusal reason");
    } else if (!success) {
      check(matching("pane close").length === 0, "removed failed startup panes");
      check(matching("pane run").length <= 1, "started subsequent services after failure");
      check(output.includes("fixture diagnostic") && output.includes(mode === "process-error" ? "shell initialization failed" : mode === "run-error" ? "could not send" : "startup failed or timed out"), "missing startup diagnostics");
    } else {
      const startOrder = mode === "harness" ? "new-server,new-brain,new-web,new-status" : "new-server,new-brain,new-web";
      check(matching("pane run").map(a => a[2]).join(",") === startOrder, "incorrect start order or reused pane");
      const worker = mode === "harness" ? "oma-harness" : "oma-brain";
      const recipe = mode === "harness" ? "restart-harness" : "restart-brain";
      check(matching("pane rename").some(a => a[2] === "new-brain" && a[3] === worker), "incorrect worker pane label");
      check(matching("pane run")[1][3].includes("--service " + recipe + " "), "incorrect worker recipe");
      if (mode === "harness") {
        check(matching("pane run")[1][3].includes("harness\\ worker\\ ready"), "incorrect Harness readiness marker");
        check(matching("pane rename").some(a => a[2] === "new-status" && a[3] === "harness-status"), "missing status pane label");
        check(matching("pane run")[3][3].includes("harness-status.ts new-brain "), "status pane did not observe the new worker pane");
        check(matching("pane wait-output").some(a => a[2] === "new-status" && a.includes("Harness Worker status")), "did not wait for status output");
      }
      check(matching("tab create")[0].includes("--no-focus"), "focused before startup succeeded");
      check(calls.at(-1).join(" ") === "tab focus new-tab", "did not focus after readiness");
      if (mode === "fresh") check(matching("pane send-keys").length === 0, "interrupted a new shell");
      if (mode.startsWith("replace")) {
        const oldPaneCount = mode === "replace-harness" ? 4 : 3;
        check(matching("pane send-keys").length === oldPaneCount && matching("pane close").length === oldPaneCount, "old panes were not stopped and closed");
        check(calls.findLastIndex(a => a[1] === "close") < calls.findIndex(a => a[0] === "tab" && a[1] === "create"), "created layout before retiring old panels");
      }
    }
    console.log(mode + ": passed");
  ' "$fixture_dir/$scenario" "$scenario" "$status"
done

mkdir -p "$fixture_dir/monitor"
PATH="$fixture_dir/bin:$PATH" MOCK_MODE=monitor MOCK_STATE="$fixture_dir/monitor" MOCK_ROOT="$fixture_dir/repo" \
  bun "$fixture_dir/repo/scripts/harness-status.ts" monitor-worker "$fixture_dir/repo" > "$fixture_dir/monitor/output"
bun -e '
  const output = await Bun.file(process.argv[1]).text();
  for (const text of [
    "Now        Waiting for NATS · PID 301",
    "Now        Running model step · PID 301",
    "Now        Running tool · PID 301",
    "Last input Model → Tool → Waiting for sandbox result · 4s",
    "Last exit  Completed · 2s · PID 304\nLast input Model → Completed · 2s",
    "Last exit  Returned for redelivery · 1s · PID 305",
    "Now        Launcher stopped",
    "Launcher   PID 200",
  ]) {
    if (!output.includes(text)) throw new Error("missing panel output: " + text + "\n" + output);
  }
  if (output.includes("PID 999") || output.includes("private-sandbox-detail")) {
    throw new Error("panel printed a foreign PID or unexpected log details");
  }
' "$fixture_dir/monitor/output"
echo "Harness status refresh: passed"

printf '%s\n' '{"name": "fixture"}' > "$fixture_dir/repo/.run/env.json"
printf '%s\n' '{"phase": "complete"}' > "$fixture_dir/repo/.run/generated/state.json"
printf '%s\n' 'environment_runner: { cloud_harness: true }' > "$fixture_dir/repo/.run/generated/config.yaml"
cat > "$fixture_dir/repo/scripts/generate-go.sh" <<'MOCK'
#!/usr/bin/env bash
[[ "$1" == --cached ]]
MOCK
cat > "$fixture_dir/bin/portless" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$*" == "get oma-fixture-api --no-worktree" ]]
printf '%s\n' http://oma-fixture-api.localhost:1337
MOCK
cat > "$fixture_dir/bin/go" <<'MOCK'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == build && "$2" == -o && "$4" == ./cmd/oma-harness-worker ]]
printf 'build\n' >> "$MOCK_STATE/builds"
cat > "$3" <<'WORKER'
#!/usr/bin/env bash
set -euo pipefail
printf '%s|%s|%s\n' "$$" "$CONFIG_FILE" "$*" >> "$MOCK_STATE/workers"
if [[ "$(wc -l < "$MOCK_STATE/workers")" -eq 1 ]]; then
  exit
fi
trap 'touch "$MOCK_STATE/stopped"; exit' TERM
touch "$MOCK_STATE/listening"
while :; do sleep 0.05; done
WORKER
chmod +x "$3"
MOCK
chmod +x "$fixture_dir/repo/scripts/generate-go.sh" "$fixture_dir/bin/go" "$fixture_dir/bin/portless"

PATH="$fixture_dir/bin:$PATH" MOCK_STATE="$fixture_dir" \
  bash "$fixture_dir/repo/scripts/restart-harness.sh" > "$fixture_dir/launcher-output" 2>&1 &
launcher_pid=$!
for _ in {1..100}; do
  [[ ! -f "$fixture_dir/listening" ]] || break
  if ! kill -0 "$launcher_pid" 2>/dev/null; then break; fi
  sleep 0.05
done
if [[ ! -f "$fixture_dir/listening" ]]; then
  cat "$fixture_dir/launcher-output" >&2
  exit 1
fi
kill -TERM "$launcher_pid"
wait "$launcher_pid"
launcher_pid=
[[ -f "$fixture_dir/stopped" && "$(wc -l < "$fixture_dir/builds")" -eq 1 ]]
{
  IFS='|' read -r first_pid first_config first_args
  IFS='|' read -r next_pid next_config next_args
} < "$fixture_dir/workers"
[[ "$first_pid" != "$next_pid" && "$first_config" == "$fixture_dir/repo/.run/generated/config.yaml" && "$first_config" == "$next_config" ]]
[[ "$first_args" == "-api-url http://oma-fixture-api.localhost:1337" && "$first_args" == "$next_args" ]]
grep -Eq "^harness worker process exited pid=$first_pid status=0 elapsed=[0-9]+s$" "$fixture_dir/launcher-output"
echo "Harness supervisor: passed"
