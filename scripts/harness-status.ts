interface ForegroundProcess {
  pid: number;
  cwd?: string;
  cmdline?: string;
  argv?: string[];
}

interface ProcessInfo {
  result?: {
    process_info?: {
      foreground_processes?: ForegroundProcess[];
    };
  };
}

interface WorkerPhase {
  label: string;
  task?: "Model" | "Tool";
  settled?: boolean;
}

const starting: WorkerPhase = { label: "Starting Worker" };

function readHerdr(args: string[]): string | undefined {
  try {
    const result = Bun.spawnSync(["herdr", ...args], { stderr: "ignore" });
    if (result.success) return result.stdout.toString();
  } catch {
    return undefined;
  }
}

function foregroundProcesses(
  pane: string,
  root: string,
): ForegroundProcess[] | undefined {
  try {
    const output = readHerdr(["pane", "process-info", "--pane", pane]);
    if (output === undefined) return undefined;
    const info: ProcessInfo = JSON.parse(output);
    return info.result?.process_info?.foreground_processes?.filter(
      (entry) => entry.cwd === root,
    );
  } catch {
    return undefined;
  }
}

function readWorkerPhase(line: string, phase: WorkerPhase): WorkerPhase {
  const log =
    /^\d{2}:\d{2}:\d{2}\.\d+ (?:INFO|ERROR) (?:\[harness_worker\] )?(.+)$/.exec(
      line,
    )?.[1];
  if (!log) return phase;
  const task = /\btask_kind=(model|tool|subagent)\b/.exec(log)?.[1];
  if (log === "harness worker ready") return { label: "Waiting for NATS" };
  if (log === "no harness input available")
    return { label: "No input before timeout", settled: true };
  if (log.startsWith("harness turn claimed ")) {
    if (task === "model") return { label: "Running model step", task: "Model" };
    if (task === "tool") return { label: "Running tool", task: "Tool" };
    if (task === "subagent")
      return { label: "Running subagent model", task: "Model" };
    return { label: "Input claimed" };
  }
  if (log.startsWith("harness turn acknowledged ")) {
    const state = /\bstate=(\w+)\b/.exec(log)?.[1];
    const labels: Record<string, string> = {
      failed: "Failed",
      interrupted: "Interrupted",
      waiting_choice: "Waiting for user choice",
      waiting_tool: "Waiting for sandbox result",
    };
    const label = labels[state ?? ""] ?? "Completed";
    return { ...phase, label, settled: true };
  }
  if (log.startsWith("harness settled delivery acknowledged ")) {
    return { label: "Acknowledged settled delivery", settled: true };
  }
  if (
    log.startsWith("harness worker stopped ") &&
    log.includes("needs redelivery")
  ) {
    return { ...phase, label: "Returned for redelivery", settled: true };
  }
  return phase;
}

function summarizeWorkerLogs(output: string) {
  let phase = starting;
  let stages: string[] = [];
  let lastExit: string | undefined;
  let lastInput: string | undefined;
  for (const raw of output.split("\n")) {
    const line = raw.trimEnd();
    const exit =
      /^harness worker process exited pid=(\d+) status=(\d+) elapsed=(\d+s)$/.exec(
        line,
      );
    if (!exit) {
      const next = readWorkerPhase(line, phase);
      if (next !== phase && next.task && !next.settled) stages.push(next.task);
      phase = next;
      continue;
    }
    const [, pid, code, duration] = exit;
    const outcome = phase.settled
      ? phase.label
      : code === "0"
        ? "Exited normally"
        : "Exit code " + code;
    lastExit = `${outcome} · ${duration} · PID ${pid}`;
    if (stages.length > 0) {
      const recent = stages.length > 5 ? ["…", ...stages.slice(-5)] : stages;
      lastInput = `${recent.join(" → ")} → ${outcome} · ${duration}`;
    }
    phase = starting;
    stages = [];
  }
  return { phase, lastExit, lastInput };
}

async function watchWorker(pane: string, root: string) {
  const workerPath = root + "/tmp/dev-harness/exe/oma-harness-worker";
  const launcherCommands = new Set([
    "bash ./scripts/restart-harness.sh",
    "bash " + root + "/scripts/restart-harness.sh",
  ]);
  let lastExit = "—";
  let lastInput = "—";

  while (true) {
    const processes = foregroundProcesses(pane, root);
    const launcher = processes?.find((entry) =>
      launcherCommands.has(entry.cmdline ?? entry.argv?.join(" ") ?? ""),
    );
    const worker = processes?.find(
      (entry) =>
        entry.argv?.[0] === workerPath ||
        entry.cmdline === workerPath ||
        entry.cmdline?.startsWith(workerPath + " "),
    );
    const logs = readHerdr([
      "pane",
      "read",
      pane,
      "--source",
      "recent-unwrapped",
      "--lines",
      "120",
      "--raw",
    ]);
    const activity = summarizeWorkerLogs(logs ?? "");
    lastExit = activity.lastExit ?? lastExit;
    lastInput = activity.lastInput ?? lastInput;
    const status =
      processes === undefined
        ? "Worker pane unavailable"
        : worker
          ? `${logs === undefined ? "Worker logs unavailable" : activity.phase.label} · PID ${worker.pid}`
          : launcher
            ? "Waiting for a Worker process"
            : "Launcher stopped";
    const updated = new Date().toLocaleTimeString("en-GB");
    const lines = [
      "Harness Worker status",
      `Now        ${status}`,
      `Last exit  ${lastExit}`,
      `Last input ${lastInput}`,
      `Launcher   PID ${launcher?.pid ?? "—"} · ${updated}`,
    ];
    const clearScreen = process.stdout.isTTY ? "\x1b[H\x1b[2J" : "";
    process.stdout.write(clearScreen + lines.join("\n") + "\n");
    if (!launcher && !worker) return;
    await Bun.sleep(1000);
  }
}

const [workerPane, worktreeRoot] = process.argv.slice(2);
if (!workerPane || !worktreeRoot) {
  console.error(
    "Usage: bun scripts/harness-status.ts <worker-pane> <worktree-root>",
  );
  process.exit(2);
}
await watchWorker(workerPane, worktreeRoot);
