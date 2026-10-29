import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { type CcrClient, type CcrSession } from "./ccrv2/index.ts";
import { createOmaSession } from "./oma-session.ts";
import { registerWorkerTask, validateBrainTask } from "./worker-registration.ts";
import { claimTasks, releaseClaim, type ClaimClient, type TaskAdmissionStage } from "./claim.ts";
import { SessionCapacity } from "./capacity.ts";
import { SHUTDOWN_SETTLE_MS } from "./config.ts";
import { openWorkerSession } from "./worker-session.ts";

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
  const watch = (promise: Promise<void>) => {
    const task = promise.catch(() => {
      if (!abort.signal.aborted) console.error("OMA session task stopped");
    }).finally(() => { running.delete(task); });
    running.add(task);
  };
  const retire = (session: CcrSession): Promise<void> => {
    if (sessions.get(session.id) !== session) return Promise.resolve();
    const existing = retiring.get(session.id);
    if (existing) return existing;
    session.stop();
    const result = session.close().finally(() => {
      ctx.ccrv2.forget(session.id);
      sessions.delete(session.id);
      api.assignments.delete(session.id);
      api.modelResponses.delete(session.id);
      api.modelRequestIDs.delete(session.id);
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
  const retireIdle = async (session: CcrSession): Promise<void> => {
    const task = sessions.get(session.id) === session && !retiring.has(session.id) ? claimedTask(session) : undefined;
    await retire(session);
    if (task) await releaseClaim(claims, task, "idle");
  };
  const capacity = new SessionCapacity(maxSessions, sessions, retiring, (idle) => watch(retireIdle(idle)));
  const admit = async (value: unknown, stage: (stage: TaskAdmissionStage) => void) => {
    abort.signal.throwIfAborted();
    stage("validate_assignment");
    validateBrainTask(value);
    const task = value;
    const existing = sessions.get(task.session_id);
    if (existing) {
      stage("retire_session");
      await retire(existing);
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
    } finally {
      releaseSlot();
    }
    try {
      await openWorkerSession(session, client, watch, retire, stage);
    } catch (error) {
      await retire(session);
      throw error;
    }
  };
  const timer = setInterval(() => {
    for (const session of sessions.values()) {
      if (session.abort.signal.aborted) watch(retire(session));
      else if (!session.busy && Date.now() - session.lastIdle > 60_000) watch(retireIdle(session));
    }
  }, 3000);
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
    abort.abort();
    clearInterval(timer);
    const current = [...sessions.values()];
    const handoff = current.filter((session) => session.busy).map(claimedTask).filter((task) => task !== undefined);
    const idle = current.filter((session) => !session.busy && !session.abort.signal.aborted && !retiring.has(session.id))
      .map(claimedTask).filter((task) => task !== undefined);
    for (const session of sessions.values()) session.stop();
    await Promise.allSettled(handoff.map((task) => releaseClaim(claims, task, "shutdown")));
    await Promise.allSettled([...running]);
    await Promise.allSettled([...sessions.values()].map(retire));
    await Promise.allSettled(idle.map((task) => releaseClaim(claims, task, "idle")));
  });
  const ready = Promise.withResolvers<void>();
  const claiming = claimTasks(claims, abort.signal, {
    concurrency: options.claimConcurrency, capacity: () => capacity.available(), slotFreed, admit, ready: () => ready.resolve(),
  });
  watch(claiming.catch((error) => { ready.reject(error); throw error; }));
  await ready.promise;
  console.info("oma-brain-dsh ready");
}
