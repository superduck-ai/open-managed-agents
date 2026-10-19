import type { Context } from "@deepseek-ai/cordis";
import { OmaApi } from "./api.ts";
import { type CcrClient, type CcrSession } from "./ccrv2/index.ts";
import { createOmaSession } from "./oma-session.ts";
import { registerWorkerTask, validateBrainTask } from "./worker-registration.ts";
import { connectNats, consumeTasks, type TaskAdmissionStage } from "./transport.ts";
import { required } from "./config.ts";
import { openWorkerSession } from "./worker-session.ts";

export async function startWorkers(ctx: Context, api: OmaApi, client: CcrClient, maxSessions: number): Promise<void> {
  const nc = await connectNats(required("NATS_URL"));
  const abort = new AbortController();
  const sessions = new Map<string, CcrSession>();
  const running = new Set<Promise<void>>();
  const retiring = new Map<string, Promise<void>>();
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
      retiring.delete(session.id);
    });
    retiring.set(session.id, result);
    return result;
  };
  const admit = async (value: unknown, stage: (stage: TaskAdmissionStage) => void) => {
    abort.signal.throwIfAborted();
    stage("validate_assignment");
    validateBrainTask(value);
    const task = value;
    const existing = sessions.get(task.session_id);
    if (existing) {
      const epoch = api.assignment(existing.id).worker_epoch;
      if (task.worker_epoch <= epoch && !existing.abort.signal.aborted) return;
      stage("retire_session");
      await retire(existing);
    }
    stage("check_capacity");
    if (sessions.size >= maxSessions) {
      const idle = [...sessions.values()].filter((session) => !session.busy)
        .sort((a, b) => a.lastIdle - b.lastIdle)[0];
      if (!idle) throw new Error("Worker capacity exhausted");
      stage("retire_session");
      await retire(idle);
    }
    stage("register_worker");
    const assignment = await registerWorkerTask(api, task, abort.signal);
    abort.signal.throwIfAborted();
    stage("create_session");
    const session = createOmaSession(ctx, api, assignment);
    sessions.set(session.id, session);
    try {
      await openWorkerSession(session, client, watch, retire, stage);
    } catch (error) {
      await retire(session);
      throw error;
    }
  };
  const timer = setInterval(() => {
    for (const session of sessions.values()) {
      if (session.abort.signal.aborted || (!session.busy && Date.now() - session.lastIdle > 60_000))
        watch(retire(session));
    }
  }, 3000);
  ctx.effect(() => async () => {
    abort.abort();
    clearInterval(timer);
    for (const session of sessions.values()) session.stop();
    await Promise.allSettled([...running]);
    await Promise.allSettled([...sessions.values()].map(retire));
    await nc.close();
  });
  const ready = Promise.withResolvers<void>();
  const consuming = consumeTasks(nc, abort.signal, admit, () => ready.resolve());
  watch(consuming.catch((error) => { ready.reject(error); throw error; }));
  await ready.promise;
  console.info("oma-brain-dsh ready");
}
