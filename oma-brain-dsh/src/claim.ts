import { HttpError, type OmaApi } from "./api.ts";
import { ConfigurationError } from "./config.ts";
import { failureDiagnostic, workerDiagnostic } from "./ccrv2/diagnostics.ts";
import { canonicalTraceParent, parseTraceParent } from "./observability/trace-context.ts";
import { brainMetrics } from "./telemetry.ts";

export type TaskAdmissionStage = "validate_assignment" | "retire_session"
  | "register_worker" | "create_session" | "open_session" | "start_inbound";

export type ReleaseReason = "admission_failed" | "shutdown" | "permanent" | "idle";

export interface ClaimedTask {
  session_id: string;
  worker_epoch: number;
  trace_parent?: string;
}

export interface ClaimClient {
  claim(blockMs: number, signal: AbortSignal): Promise<unknown | undefined>;
  release(task: ClaimedTask, reason: ReleaseReason): Promise<void>;
}

export interface ClaimLoopOptions {
  concurrency: number;
  blockMs?: number;
  capacity: () => number;
  slotFreed: (signal: AbortSignal) => Promise<void>;
  admit: (task: unknown, stage: (stage: TaskAdmissionStage) => void) => Promise<void>;
  ready?: () => void;
}

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

  async claim(blockMs: number, signal: AbortSignal): Promise<unknown | undefined> {
    const response = await this.api.requestResponse(
      `/v1/code/brain/claims?block_ms=${blockMs}`, this.token, "POST", {},
      { "X-Worker-ID": this.workerId }, signal, blockMs + 15_000,
    );
    if (response.status === 204) return undefined;
    if (!response.ok) throw new HttpError(response.status);
    return response.json();
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
  if (typeof task.session_id !== "string" || typeof task.worker_epoch !== "number") return undefined;
  const traceParent = canonicalTraceParent(task.trace_parent);
  return { session_id: task.session_id, worker_epoch: task.worker_epoch, ...(traceParent ? { trace_parent: traceParent } : {}) };
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
    const failure = failureDiagnostic(error);
    workerDiagnostic("warn", "oma_task_release_failed", {
      session_id: task.session_id, worker_epoch: task.worker_epoch, reason, ...failure,
    }, task.trace_parent);
    brainMetrics().claimFailures.add(1, { operation: "release", failure_reason: String(failure.failure_reason) });
  }
}

async function admitClaim(
  client: ClaimClient, signal: AbortSignal, value: unknown, admit: ClaimLoopOptions["admit"],
): Promise<void> {
  const startMs = Date.now();
  const task = claimedTask(value);
  const traceParent = task?.trace_parent;
  const traceId = parseTraceParent({ traceparent: traceParent })?.traceId;
  const fields: Record<string, unknown> = {
    session_id: task?.session_id, worker_epoch: task?.worker_epoch, ...(traceId ? { trace_id: traceId } : {}),
  };
  let stage = "validate_assignment";
  workerDiagnostic("info", "oma_task_admission_started", fields, traceParent);
  try {
    await admit(value, (next) => { stage = next; });
    workerDiagnostic("info", "oma_task_admitted", { ...fields, duration_ms: Date.now() - startMs }, traceParent);
    brainMetrics().taskAdmissions.add(1, { outcome: "admitted" });
  } catch (error) {
    const reason = releaseReason(error, signal);
    if (task) await releaseClaim(client, task, reason);
    if (!signal.aborted) {
      const failure = failureDiagnostic(error);
      workerDiagnostic("error", "oma_task_admission_failed", {
        ...fields, stage, release_reason: reason, duration_ms: Date.now() - startMs, ...failure,
      }, traceParent);
      brainMetrics().taskAdmissions.add(1, {
        outcome: "failed", stage, release_reason: reason, failure_reason: String(failure.failure_reason),
      });
    }
  }
}

function claimRejection(error: unknown): number | undefined {
  return error instanceof HttpError && (error.status === 401 || error.status === 403) ? error.status : undefined;
}

function claimRejectedMessage(status: number): string {
  return `Brain claim rejected with HTTP ${status}. Check that OMA_BRAIN_CLAIM_TOKEN matches the backend code_session.brain_claim_token_file.`;
}

async function probeClaims(client: ClaimClient, signal: AbortSignal): Promise<unknown> {
  for (let failures = 0; ; failures++) {
    try {
      return await client.claim(0, signal);
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
  const initial = await probeClaims(client, signal);
  options.ready?.();
  let claiming = 0;
  let rejectionReported = false;
  const loop = async (first: unknown) => {
    let pending = first;
    let failures = 0;
    while (!signal.aborted) {
      if (pending === undefined && options.capacity() - claiming <= 0) {
        await options.slotFreed(signal);
        continue;
      }
      claiming++;
      try {
        const value = pending ?? await client.claim(blockMs, signal);
        pending = undefined;
        failures = 0;
        rejectionReported = false;
        if (value !== undefined) await admitClaim(client, signal, value, options.admit);
      } catch (error) {
        if (signal.aborted) break;
        const rejected = claimRejection(error);
        const failure = failureDiagnostic(error);
        brainMetrics().claimFailures.add(1, { operation: "claim", failure_reason: String(failure.failure_reason) });
        if (rejected === undefined) workerDiagnostic("warn", "oma_task_claim_failed", failure);
        else if (!rejectionReported) {
          rejectionReported = true;
          console.error(claimRejectedMessage(rejected));
        }
        await sleep(retryDelaysMs[Math.min(failures++, retryDelaysMs.length - 1)]!, signal);
      } finally {
        claiming--;
      }
    }
  };
  await Promise.all(Array.from({ length: options.concurrency }, (_, index) => loop(index === 0 ? initial : undefined)));
}
