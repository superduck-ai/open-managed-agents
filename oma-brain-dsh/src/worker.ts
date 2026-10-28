import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { type CcrClient, type CcrSession } from "./ccrv2/index.ts";
import { createOmaSession } from "./oma-session.ts";
import { registerWorkerTask, validateBrainTask } from "./worker-registration.ts";
import { claimTasks, releaseClaim, type ClaimClient, type TaskAdmissionStage } from "./claim.ts";
import { SessionCapacity } from "./capacity.ts";
import { failureDiagnostic, workerDiagnostic } from "./ccrv2/diagnostics.ts";
import { SHUTDOWN_SETTLE_MS } from "./config.ts";
import { brainMetrics } from "./telemetry.ts";
import { openWorkerSession } from "./worker-session.ts";

type RetireReason = "superseded" | "capacity" | "inbound_ended" | "admission_failed" | "aborted" | "idle" | "shutdown";

declare module "@deepseek-ai/cordis" {
  interface Events {
    "oma/shutdown-drain"(): Promise<void>;
  }
}

export interface WorkerOptions {
  maxSessions: number;
  claimConcurrency: number;
  shutdownDrainMs?: number;
}

export async function startWorkers(
  ctx: Context, api: OmaApi, client: CcrClient, claims: ClaimClient, options: WorkerOptions,
): Promise<void> {
  const { maxSessions } = options;
  const abort = new AbortController();
  const sessions = new Map<string, CcrSession>();
  const running = new Set<Promise<void>>();
  const retiring = new Map<string, Promise<void>>();
  let slotWaiters: (() => void)[] = [];
  const notifySlot = () => {
    const waiters = slotWaiters;
    slotWaiters = [];
    for (const wake of waiters) wake();
  };
  const slotFreed = (signal: AbortSignal) => new Promise<void>((resolve) => {
    const timer = setTimeout(done, 1_000);
    function done() {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      const index = slotWaiters.indexOf(done);
      if (index >= 0) slotWaiters.splice(index, 1);
      resolve();
    }
    slotWaiters.push(done);
    signal.addEventListener("abort", done, { once: true });
  });
  const watch = (promise: Promise<void>, sessionId?: string) => {
    const task = promise.catch((error) => {
      if (abort.signal.aborted) return;
      console.error("OMA session task stopped");
      const failure = failureDiagnostic(error);
      workerDiagnostic("error", "oma_session_task_failed", { ...(sessionId ? { session_id: sessionId } : {}), ...failure });
      brainMetrics().sessionTasksStopped.add(1, { reason: "task_failed", failure_reason: String(failure.failure_reason) });
    }).finally(() => { running.delete(task); });
    running.add(task);
  };
  const retire = (session: CcrSession, reason: RetireReason): Promise<void> => {
    if (sessions.get(session.id) !== session) return Promise.resolve();
    const existing = retiring.get(session.id);
    if (existing) return existing;
    workerDiagnostic("info", "oma_session_retired", {
      session_id: session.id, worker_epoch: api.assignment(session.id).worker_epoch, reason, busy: session.busy,
    });
    brainMetrics().sessionTasksStopped.add(1, { reason });
    session.stop();
    const result = session.close().finally(() => { void api.telemetry?.close(session.id); }).finally(() => {
      ctx.ccrv2.forget(session.id);
      if (sessions.delete(session.id)) brainMetrics().sessionsActive.add(-1);
      api.assignments.delete(session.id);
      api.modelResponses.delete(session.id);
      retiring.delete(session.id);
      notifySlot();
    });
    retiring.set(session.id, result);
    return result;
  };
  const claimedTask = (session: CcrSession) => {
    const assignment = api.assignments.get(session.id);
    return assignment && { session_id: session.id, worker_epoch: assignment.worker_epoch };
  };
  const retireIdle = async (session: CcrSession, reason: "idle" | "capacity"): Promise<void> => {
    const task = sessions.get(session.id) === session && !retiring.has(session.id) ? claimedTask(session) : undefined;
    await retire(session, reason);
    if (task) await releaseClaim(claims, task, "idle");
  };
  const capacity = new SessionCapacity(maxSessions, sessions, retiring, (idle) => watch(retireIdle(idle, "capacity"), idle.id));
  const admit = async (value: unknown, stage: (stage: TaskAdmissionStage) => void) => {
    abort.signal.throwIfAborted();
    stage("validate_assignment");
    validateBrainTask(value);
    const task = value;
    const existing = sessions.get(task.session_id);
    if (existing) {
      stage("retire_session");
      await retire(existing, "superseded");
    }
    const releaseSlot = capacity.reserve();
    let session: CcrSession;
    try {
      stage("register_worker");
      const assignment = await registerWorkerTask(api, task, abort.signal);
      abort.signal.throwIfAborted();
      stage("create_session");
      session = createOmaSession(ctx, api, assignment);
      sessions.set(session.id, session);
      brainMetrics().sessionsActive.add(1);
    } finally {
      releaseSlot();
    }
    try {
      await openWorkerSession(session, client, (task) => watch(task, session.id), (opened) => retire(opened, "inbound_ended"), stage);
    } catch (error) {
      await retire(session, "admission_failed");
      throw error;
    }
  };
  const timer = setInterval(() => {
    for (const session of sessions.values()) {
      if (session.abort.signal.aborted) watch(retire(session, "aborted"), session.id);
      else if (!session.busy && Date.now() - session.lastIdle > 60_000) watch(retireIdle(session, "idle"), session.id);
    }
  }, 3000);
  let capacityReported = false;
  let draining: Promise<void> | undefined;
  const drainSessions = async () => {
    abort.abort();
    clearInterval(timer);
    const active = [...sessions.values()].filter((session) => !session.abort.signal.aborted && !retiring.has(session.id));
    const drained = await Promise.all(active.map((session) => session.drain(options.shutdownDrainMs ?? 0)));
    await Promise.all(active.filter((_, index) => !drained[index]).map((session) => session.interruptForShutdown(SHUTDOWN_SETTLE_MS)));
  };
  ctx.on("oma/shutdown-drain", () => draining ??= drainSessions());
  ctx.effect(() => async () => {
    if (capacityReported) brainMetrics().sessionsCapacity.add(-maxSessions);
    abort.abort();
    clearInterval(timer);
    const current = [...sessions.values()];
    const handoff = current.filter((session) => session.busy).map(claimedTask).filter((task) => task !== undefined);
    const idle = current.filter((session) => !session.busy && !session.abort.signal.aborted && !retiring.has(session.id))
      .map(claimedTask).filter((task) => task !== undefined);
    for (const session of sessions.values()) session.stop();
    await Promise.allSettled(handoff.map((task) => releaseClaim(claims, task, "shutdown")));
    await Promise.allSettled([...running]);
    await Promise.allSettled([...sessions.values()].map((session) => retire(session, "shutdown")));
    await Promise.allSettled(idle.map((task) => releaseClaim(claims, task, "idle")));
  });
  const ready = Promise.withResolvers<void>();
  const claiming = claimTasks(claims, abort.signal, {
    concurrency: options.claimConcurrency, capacity: () => capacity.available(), slotFreed, admit, ready: () => ready.resolve(),
  });
  watch(claiming.catch((error) => { ready.reject(error); throw error; }));
  await ready.promise;
  console.info("oma-brain-dsh ready");
  brainMetrics().sessionsActive.add(0);
  brainMetrics().sessionsCapacity.add(maxSessions);
  capacityReported = true;
  workerDiagnostic("info", "oma_brain_ready", { max_sessions: maxSessions, claim_concurrency: options.claimConcurrency });
}
