import { HttpError, type OmaApi } from "./api.ts";
import { ConfigurationError } from "./config.ts";
import { failureDiagnostic, workerDiagnostic } from "./ccrv2/diagnostics.ts";

export type TaskAdmissionStage = "validate_assignment" | "retire_session"
  | "register_worker" | "create_session" | "open_session" | "start_inbound";

export type ReleaseReason = "admission_failed" | "shutdown" | "permanent" | "idle";

export interface ClaimedTask {
  session_id: string;
  worker_epoch: number;
}

export interface ClaimLoad {
  max_sessions: number;
  busy_sessions: number;
  resident_sessions: number;
}

export interface ClaimRequest extends ClaimLoad {
  max_tasks: number;
  admission_failures: number;
}

export interface ClaimClient {
  claim(request: ClaimRequest, blockMs: number, signal: AbortSignal): Promise<unknown[]>;
  ack(task: ClaimedTask): Promise<void>;
  release(task: ClaimedTask, reason: ReleaseReason): Promise<void>;
}

export interface ClaimLoopOptions {
  concurrency: number;
  blockMs?: number;
  capacity: () => number;
  load: () => ClaimLoad;
  slotFreed: (signal: AbortSignal) => Promise<void>;
  admit: (task: unknown, stage: (stage: TaskAdmissionStage) => void) => Promise<void>;
  ready?: () => void;
}

type AdmissionOutcome = "admitted" | "skipped" | ReleaseReason;

const retryDelaysMs = [1_000, 2_000, 5_000, 10_000, 30_000];

export class CapacityExhaustedError extends Error {
  constructor() {
    super("Worker capacity exhausted");
  }
}

export class PermanentAdmissionError extends Error {}

function releaseReason(error: unknown, signal: AbortSignal): ReleaseReason {
  if (signal.aborted || error instanceof CapacityExhaustedError) return "shutdown";
  return error instanceof PermanentAdmissionError ? "permanent" : "admission_failed";
}

export class BrainClaimClient implements ClaimClient {
  constructor(
    private readonly api: OmaApi,
    private readonly token: string,
    readonly workerId: string,
  ) {}

  async claim(request: ClaimRequest, blockMs: number, signal: AbortSignal): Promise<unknown[]> {
    const response = await this.api.requestResponse(
      `/v1/code/brain/claims?block_ms=${blockMs}`, this.token, "POST", request,
      { "X-Worker-ID": this.workerId }, signal, blockMs + 15_000,
    );
    if (response.status === 204) return [];
    if (!response.ok) throw new HttpError(response.status);
    const body = await response.json() as { tasks?: unknown };
    if (!Array.isArray(body.tasks)) throw new Error("Brain claim response has no task list");
    return body.tasks;
  }

  async ack(task: ClaimedTask): Promise<void> {
    await this.api.request(
      `/v1/code/brain/claims/${encodeURIComponent(task.session_id)}/ack`, this.token, "POST",
      { worker_epoch: task.worker_epoch }, { "X-Worker-ID": this.workerId }, undefined, 5_000,
    );
  }

  async release(task: ClaimedTask, reason: ReleaseReason): Promise<void> {
    await this.api.request(
      `/v1/code/brain/claims/${encodeURIComponent(task.session_id)}/release`, this.token, "POST",
      { worker_epoch: task.worker_epoch, reason }, { "X-Worker-ID": this.workerId }, undefined, 5_000,
    );
  }
}

function claimedTask(value: unknown): ClaimedTask | undefined {
  if (!value || typeof value !== "object" || Array.isArray(value)) return undefined;
  const task = value as Record<string, unknown>;
  return typeof task.session_id === "string" && typeof task.worker_epoch === "number"
    ? { session_id: task.session_id, worker_epoch: task.worker_epoch } : undefined;
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(done, ms);
    function done() {
      clearTimeout(timer);
      signal.removeEventListener("abort", done);
      resolve();
    }
    signal.addEventListener("abort", done, { once: true });
  });
}

export async function releaseClaim(client: ClaimClient, task: ClaimedTask, reason: ReleaseReason): Promise<void> {
  try {
    await client.release(task, reason);
  } catch (error) {
    workerDiagnostic("warn", "oma_task_release_failed", {
      session_id: task.session_id, worker_epoch: task.worker_epoch, reason, ...failureDiagnostic(error),
    });
  }
}

async function admitClaim(
  client: ClaimClient, signal: AbortSignal, value: unknown, admit: ClaimLoopOptions["admit"],
): Promise<AdmissionOutcome> {
  const startMs = Date.now();
  const task = claimedTask(value);
  const fields: Record<string, unknown> = { session_id: task?.session_id, worker_epoch: task?.worker_epoch };
  if (task) {
    try {
      await client.ack(task);
    } catch (error) {
      workerDiagnostic("warn", "oma_task_ack_failed", { ...fields, ...failureDiagnostic(error) });
      return "skipped";
    }
  }
  let stage = "validate_assignment";
  workerDiagnostic("info", "oma_task_admission_started", fields);
  try {
    await admit(value, (next) => { stage = next; });
    workerDiagnostic("info", "oma_task_admitted", { ...fields, duration_ms: Date.now() - startMs });
    return "admitted";
  } catch (error) {
    const reason = releaseReason(error, signal);
    if (task) await releaseClaim(client, task, reason);
    if (!signal.aborted) workerDiagnostic("error", "oma_task_admission_failed", {
      ...fields, stage, duration_ms: Date.now() - startMs, ...failureDiagnostic(error),
    });
    return reason;
  }
}

function claimRejection(error: unknown): number | undefined {
  return error instanceof HttpError && (error.status === 401 || error.status === 403) ? error.status : undefined;
}

function claimRejectedMessage(status: number): string {
  return `Brain claim rejected with HTTP ${status}. Check that OMA_BRAIN_CLAIM_TOKEN matches the backend code_session.brain_claim_token_file.`;
}

async function probeClaims(
  client: ClaimClient, signal: AbortSignal, request: () => ClaimRequest,
): Promise<unknown[]> {
  for (let failures = 0; ; failures++) {
    try {
      return await client.claim(request(), 0, signal);
    } catch (error) {
      const rejected = claimRejection(error);
      if (rejected !== undefined) throw new ConfigurationError(claimRejectedMessage(rejected));
      if (signal.aborted) throw error;
      workerDiagnostic("warn", "oma_task_claim_probe_failed", { attempt: failures + 1, ...failureDiagnostic(error) });
      await sleep(retryDelaysMs[Math.min(failures, retryDelaysMs.length - 1)]!, signal);
    }
  }
}

export async function claimTasks(client: ClaimClient, signal: AbortSignal, options: ClaimLoopOptions): Promise<void> {
  const blockMs = options.blockMs ?? 25_000;
  const admissions = new Set<Promise<void>>();
  let admissionFailures = 0;
  const free = () => Math.min(options.concurrency, options.capacity()) - admissions.size;
  const request = (maxTasks: number): ClaimRequest => ({
    ...options.load(), max_tasks: maxTasks, admission_failures: admissionFailures,
  });
  const start = (values: unknown[]) => {
    for (const value of values) {
      const admission: Promise<void> = admitClaim(client, signal, value, options.admit).then((outcome) => {
        if (outcome === "admitted") admissionFailures = 0;
        else if (outcome === "admission_failed") admissionFailures++;
      }).finally(() => { admissions.delete(admission); });
      admissions.add(admission);
    }
  };
  start(await probeClaims(client, signal, () => request(Math.max(free(), 1))));
  options.ready?.();
  let failures = 0;
  let rejectionReported = false;
  while (!signal.aborted) {
    const maxTasks = free();
    if (maxTasks <= 0) {
      await Promise.race([options.slotFreed(signal), ...admissions]);
      continue;
    }
    try {
      const values = await client.claim(request(maxTasks), blockMs, signal);
      failures = 0;
      rejectionReported = false;
      start(values);
    } catch (error) {
      if (signal.aborted) break;
      const rejected = claimRejection(error);
      if (rejected === undefined) workerDiagnostic("warn", "oma_task_claim_failed", failureDiagnostic(error));
      else if (!rejectionReported) {
        rejectionReported = true;
        console.error(claimRejectedMessage(rejected));
      }
      await sleep(retryDelaysMs[Math.min(failures++, retryDelaysMs.length - 1)]!, signal);
    }
  }
  await Promise.allSettled([...admissions]);
}
