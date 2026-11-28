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

interface WorkerActivity {
  pid?: number;
  activeLoops?: number;
  label: string;
  loops: Map<string, { phase: WorkerPhase; stages: string[] }>;
}

const starting: WorkerPhase = { label: "Starting Worker" };
const waiting: WorkerPhase = { label: "Waiting for NATS" };

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

function readWorkerPhase(
  event: string,
  fields: string,
  phase: WorkerPhase,
): WorkerPhase {
  if (event === "harness turn claimed") {
    const task = /\btask_kind=(model|tool|subagent)\b/.exec(fields)?.[1];
    if (task === "model") return { label: "Running model step", task: "Model" };
    if (task === "tool") return { label: "Running tool", task: "Tool" };
    if (task === "subagent")
      return { label: "Running subagent model", task: "Model" };
    return { label: "Input claimed" };
  }
  const state = /\bstate=(\w+)\b/.exec(fields)?.[1];
  const labels: Record<string, string> = {
    failed: "Failed",
    interrupted: "Interrupted",
    waiting_choice: "Waiting for user choice",
    waiting_tool: "Waiting for sandbox result",
  };
  const label = labels[state ?? ""] ?? "Completed";
  return { ...phase, label, settled: true };
}

function workerActivity(pid?: number): WorkerActivity {
  return { pid, label: starting.label, loops: new Map() };
}

function summarizeInput(
  phase: WorkerPhase,
  stages: string[],
  result: string,
  elapsed: string,
) {
  const outcome = phase.settled
    ? phase.label
    : result === "redelivery"
      ? "Returned for redelivery"
      : result === "failed"
        ? "Failed"
        : "Completed";
  const recent = stages.length > 5 ? ["…", ...stages.slice(-5)] : stages;
  return [...recent, outcome].join(" → ") + " · " + elapsed;
}

function summarizeWorkerLogs(output: string) {
  let activity = workerActivity();
  let lastExit: string | undefined;
  let lastInput: string | undefined;
  for (const raw of output.split("\n")) {
    const line = raw.trimEnd();
    const start = /^harness worker process started pid=(\d+)$/.exec(line);
    if (start) {
      activity = workerActivity(Number(start[1]));
      activity.activeLoops = 0;
      continue;
    }
    const exit =
      /^harness worker process exited pid=(\d+) status=(\d+) elapsed=(\d+s)$/.exec(
        line,
      );
    if (exit) {
      const [, pid, code, duration] = exit;
      const outcome = code === "0" ? "Exited normally" : "Exit code " + code;
      lastExit = `${outcome} · ${duration} · PID ${pid}`;
      activity = workerActivity();
      continue;
    }
    const log =
      /^\d{2}:\d{2}:\d{2}\.\d+ (?:INFO|WARN) (?:\[harness_worker\] )?(.+)$/.exec(
        line,
      )?.[1];
    if (!log) continue;
    if (log === "harness worker ready") {
      activity.label = waiting.label;
      continue;
    }
    const loopLog =
      /^(harness (?:turn claimed|turn acknowledged|settled delivery acknowledged|loop finished)) pid=(\d+) loop_id=(\d+) (.*)$/.exec(
        log,
      );
    if (!loopLog) continue;
    const [, event, pid, loopId, fields] = loopLog;
    if (activity.pid !== Number(pid)) activity = workerActivity(Number(pid));
    const progress = activity.loops.get(loopId) ?? {
      phase: starting,
      stages: [],
    };
    if (event === "harness loop finished") {
      const finished =
        /^active_loops=(\d+) elapsed_ms=(\d+) result=(\w+)(?: |$)/.exec(fields);
      if (!finished) continue;
      const [, count, elapsed, result] = finished;
      activity.activeLoops = Number(count);
      activity.loops.delete(loopId);
      activity.label = waiting.label;
      lastInput = summarizeInput(
        progress.phase,
        progress.stages,
        result,
        `${elapsed}ms`,
      );
      continue;
    }
    const phase = readWorkerPhase(event, fields, progress.phase);
    const count = /\bactive_loops=(\d+)$/.exec(fields)?.[1];
    if (count !== undefined) activity.activeLoops = Number(count);
    if (phase.task && !phase.settled) progress.stages.push(phase.task);
    progress.phase = phase;
    activity.loops.set(loopId, progress);
  }
  return { activity, lastExit, lastInput };
}

function activityLabel(activity: WorkerActivity | undefined) {
  if (!activity?.activeLoops) return activity?.label ?? starting.label;
  if (activity.activeLoops > 1) return "Processing inputs";
  for (const { phase } of activity.loops.values()) {
    if (!phase.settled) return phase.label;
  }
  return "Processing input";
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
    const summary = summarizeWorkerLogs(logs ?? "");
    lastExit = summary.lastExit ?? lastExit;
    lastInput = summary.lastInput ?? lastInput;
    const activity =
      worker && summary.activity.pid === worker.pid
        ? summary.activity
        : undefined;
    let activeLoops: number | string = "—";
    if (processes !== undefined) {
      if (!worker) activeLoops = 0;
      else activeLoops = activity?.activeLoops ?? "—";
    }
    const status =
      processes === undefined
        ? "Worker pane unavailable"
        : worker
          ? `${logs === undefined ? "Worker logs unavailable" : activityLabel(activity)} · PID ${worker.pid}`
          : launcher
            ? "Waiting for a Worker process"
            : "Launcher stopped";
    const updated = new Date().toLocaleTimeString("en-GB");
    const lines = [
      "Harness Worker status",
      `Now        ${status}`,
      `Agent loops ${activeLoops}`,
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
