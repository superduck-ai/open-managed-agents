import { HttpError, type OmaApi } from "./api.ts";
import { ConfigurationError } from "./config.ts";
import { failureDiagnostic, workerDiagnostic } from "./ccrv2/diagnostics.ts";

export type TaskAdmissionStage = "validate_assignment" | "retire_session"
  | "register_worker" | "create_session" | "open_session" | "start_inbound";

export type ReleaseReason = "admission_failed" | "shutdown" | "permanent";

export interface ClaimedTask {
  session_id: string;
  worker_epoch: number;
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
): Promise<void> {
  const startMs = Date.now();
  const task = claimedTask(value);
  const fields: Record<string, unknown> = { session_id: task?.session_id, worker_epoch: task?.worker_epoch };
  let stage = "validate_assignment";
  workerDiagnostic("info", "oma_task_admission_started", fields);
  try {
    await admit(value, (next) => { stage = next; });
    workerDiagnostic("info", "oma_task_admitted", { ...fields, duration_ms: Date.now() - startMs });
  } catch (error) {
    if (task) await releaseClaim(client, task, releaseReason(error, signal));
    if (!signal.aborted) workerDiagnostic("error", "oma_task_admission_failed", {
      ...fields, stage, duration_ms: Date.now() - startMs, ...failureDiagnostic(error),
    });
  }
}

async function probeClaims(client: ClaimClient, signal: AbortSignal): Promise<unknown> {
  for (let failures = 0; ; failures++) {
    try {
      return await client.claim(0, signal);
    } catch (error) {
      if (error instanceof HttpError && (error.status === 401 || error.status === 403))
        throw new ConfigurationError(`Brain claim rejected with HTTP ${error.status}. Check that OMA_BRAIN_CLAIM_TOKEN matches the backend code_session.brain_claim_token_file.`);
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
        if (value !== undefined) await admitClaim(client, signal, value, options.admit);
      } catch (error) {
        if (signal.aborted) break;
        workerDiagnostic("warn", "oma_task_claim_failed", failureDiagnostic(error));
        await sleep(retryDelaysMs[Math.min(failures++, retryDelaysMs.length - 1)]!, signal);
      } finally {
        claiming--;
      }
    }
  };
  await Promise.all(Array.from({ length: options.concurrency }, (_, index) => loop(index === 0 ? initial : undefined)));
}
