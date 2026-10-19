import { createHash } from "node:crypto";
import type { CcrBackend } from "./contracts.ts";
import type {
  ClientEvent, DeliveryUpdate, InternalEvent,
  RequiresActionDetails, SessionExternalMetadata, SessionState, WorkerEvent, WorkerStateResponse,
} from "./wire.ts";
import { abortable, retryDelay, sleep } from "./retry.ts";
import type { CcrLeaseCheck, CcrLeaseFailureReason, CcrLeaseResult } from "./heartbeat.ts";
import { decodeJwtExpiry } from "./jwt.ts";
import { sse } from "../sse.ts";
import { CcrDeliveryUploader } from "./delivery.ts";
import { CcrBatchUploader, type BatchSchedule } from "./batch-uploader.ts";
import { WorkerPutQueue, type WorkerPutClaim } from "./worker-put.ts";
import { failureDiagnostic, workerDiagnostic } from "./diagnostics.ts";

function leaseFailureReason(error: unknown, status?: number): CcrLeaseFailureReason {
  if (error instanceof MissingWorkerCredentialsError) return "credentials_unavailable";
  switch (status) {
    case 401: case 403: return "authentication_rejected";
    case 409: return "epoch_mismatch";
    case 410: return "lease_expired";
    case 408: return "timeout";
    case 429: return "rate_limited";
  }
  if (status !== undefined) return status >= 500 ? "server_error" : "request_rejected";
  if (error instanceof Error && (error.name === "TimeoutError" ||
    ("code" in error && error.code === "ETIMEDOUT"))) return "timeout";
  return "transport_error";
}

function record(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value))
    throw new Error("Expected a JSON object");
  return value as Record<string, unknown>;
}

export class CcrHttpError extends Error {
  constructor(readonly status: number, readonly retryAfterMs?: number, readonly tokenExpired = false,
    readonly authExhausted = false) {
    super(`OMA HTTP ${status}`);
  }
}

function leaseFailure(error: unknown, workerEpoch?: number): CcrLeaseResult {
  const httpStatus = error instanceof CcrHttpError ? error.status : undefined;
  const expired = error instanceof CcrHttpError && error.tokenExpired;
  return {
    status: httpStatus === 409 || httpStatus === 410 || expired ||
      (error instanceof CcrHttpError && error.authExhausted) ? "rejected" : "unavailable",
    httpStatus,
    reason: expired ? "token_expired" : leaseFailureReason(error, httpStatus),
    workerEpoch,
  };
}

interface WriteObserver {
  workerEpoch: number;
  abort: AbortController;
  rejection?: Error;
  listener(result: CcrLeaseResult): void;
}

interface WorkerClaim {
  epoch: number;
  failures: number;
  sequenceNum: number;
  deliveryFailure?: Error;
}

interface EventQueue<T> {
  claim: WorkerClaim;
  scope?: WriteObserver;
  uploader: CcrBatchUploader<T>;
}

export interface CcrHttpRequest {
  path: string;
  token: string;
  method: string;
  body?: unknown;
  signal?: AbortSignal;
  timeout?: number;
  headers?: Record<string, string>;
}

export interface CcrClientConfig {
  /** Generic HTTP transport. Return the raw response, including HTTP errors. */
  request(options: CcrHttpRequest): Promise<Response>;
  /** Resolve current credentials on every request, including retries. */
  credentials(id: string): { token: string; workerEpoch: number };
  sessionIds(): Iterable<string>;
  /** Injectable clock and randomness for deterministic retry tests. */
  sleep?(ms: number, signal?: AbortSignal): Promise<void>;
  random?(): number;
  now?(): number;
  /** Timer for the 100ms output stream window; independent of retry sleeps. */
  schedule?: BatchSchedule;
}

export type CcrLeaseStatus = CcrLeaseResult["status"];

function stableId(value: unknown): string {
  return createHash("sha256").update(JSON.stringify(value)).digest("hex");
}

/** CCRv2 HTTP paths, wire envelopes, fencing and confirmation rules. */
export class CcrClient implements CcrBackend {
  private readonly writeObservers = new Map<string, Set<WriteObserver>>();
  private readonly workerPuts = new WorkerPutQueue();
  private readonly authStates = new Map<string, WorkerClaim>();
  private readonly deliveries = new Map<string, { epoch: number; uploader: CcrDeliveryUploader }>();
  private readonly internalEvents = new Map<string, EventQueue<WorkerEvent>>();
  private readonly outputEvents = new Map<string, EventQueue<ClientEvent>>();
  constructor(readonly config: CcrClientConfig) {}

  private resetClaim(id: string, epoch: number): WorkerClaim {
    const claim = { epoch, failures: 0, sequenceNum: 0 };
    this.authStates.set(id, claim);
    // Set the new identity before closing the old uploader: its late completion
    // and close callback must not stop the replacement, even at the same epoch.
    void this.deliveries.get(id)?.uploader.close(new Error("CCRv2 claim replaced"));
    void this.internalEvents.get(id)?.uploader.close(new Error("CCRv2 claim replaced"));
    const output = this.outputEvents.get(id);
    void output?.uploader.close(output.claim.epoch === epoch ? new Error("CCRv2 claim replaced") : new CcrHttpError(409));
    return claim;
  }

  private claim(id: string, epoch: number): WorkerClaim {
    const current = this.authStates.get(id);
    return current?.epoch === epoch ? current : this.resetClaim(id, epoch);
  }

  private stopDelivery(id: string, claim: WorkerClaim, error: Error): void {
    if (this.authStates.get(id) !== claim) return;
    claim.deliveryFailure ??= error;
    void this.deliveries.get(id)?.uploader.close(claim.deliveryFailure);
    void this.internalEvents.get(id)?.uploader.close(error);
    void this.outputEvents.get(id)?.uploader.close(error);
  }

  async register(id: string, token: string, signal?: AbortSignal): Promise<number> {
    // Registration can advance legacy credentials to a new epoch. Never retry
    // an ambiguous registration inside the client or during SSE reconnects.
    const startMs = Date.now();
    const fields = { session_id: id, method: "POST", path: "/worker/register", worker_id_present: false };
    let stage = "request";
    let response: Response | undefined;
    workerDiagnostic("info", "ccrv2_worker_register_started", fields);
    try {
      response = await this.config.request({
        path: `/v1/code/sessions/${encodeURIComponent(id)}/worker/register`,
        token, method: "POST", body: {}, signal,
      });
      stage = "http_response";
      if (!response.ok) {
        await response.body?.cancel();
        throw new CcrHttpError(response.status);
      }
      stage = "decode_response";
      const value = record(await response.json()).worker_epoch;
      stage = "validate_epoch";
      const epoch = typeof value === "string" && /^[1-9]\d*$/.test(value) ? Number(value) : value;
      if (typeof epoch !== "number" || !Number.isSafeInteger(epoch) || epoch <= 0)
        throw new Error("Invalid registered worker epoch");
      stage = "reset_claim";
      this.resetClaim(id, epoch);
      workerDiagnostic("info", "ccrv2_worker_register_succeeded", {
        ...fields, http_status: response.status, worker_epoch: epoch, duration_ms: Date.now() - startMs,
      });
      return epoch;
    } catch (error) {
      workerDiagnostic("error", "ccrv2_worker_register_failed", {
        ...fields, stage, http_status: response?.status, request_id: response?.headers.get("request-id")
          ?? response?.headers.get("x-request-id") ?? undefined,
        duration_ms: Date.now() - startMs, ...failureDiagnostic(error),
      });
      throw error;
    }
  }

  async *inbound(id: string, signal: AbortSignal, initialSequenceNum?: number): AsyncIterable<{ id: string; payload: Record<string, unknown> }> {
    const epoch = this.config.credentials(id).workerEpoch;
    const receipts = new Set<Promise<void>>();
    const lifetime = new AbortController();
    const owner = AbortSignal.any([signal, lifetime.signal]);
    let failure: unknown;
    try {
      for await (const event of this.readInbound(id, owner, initialSequenceNum)) {
        // Receipt reporting belongs to the client and never holds SSE intake.
        // Keep the stream's original epoch even if credentials rotate mid-read.
        const receipt = this.reportDelivery(id, event.id, "received", owner, epoch)
          .catch((error: unknown) => {
            if (!owner.aborted) { failure = error; lifetime.abort(error); }
          }).finally(() => { receipts.delete(receipt); });
        receipts.add(receipt);
        yield event;
      }
      if (failure !== undefined) throw failure;
    } catch (error) {
      if (!signal.aborted) throw failure ?? error;
    } finally {
      lifetime.abort();
      await Promise.allSettled(receipts);
    }
  }

  inboundSequenceNum(id: string): number {
    return this.claim(id, this.config.credentials(id).workerEpoch).sequenceNum;
  }

  private async *readInbound(id: string, signal: AbortSignal, initialSequenceNum?: number): AsyncIterable<{ id: string; payload: Record<string, unknown> }> {
    const epoch = this.config.credentials(id).workerEpoch;
    const claim = this.claim(id, epoch);
    if (initialSequenceNum !== undefined) {
      if (!Number.isSafeInteger(initialSequenceNum) || initialSequenceNum < 0)
        throw new Error("Invalid initial worker stream sequence");
      claim.sequenceNum = Math.max(claim.sequenceNum, initialSequenceNum);
    }
    let lastSequenceNum = claim.sequenceNum;
    let failures = 0;
    let reconnectStartedAt: number | undefined;
    const now = this.config.now ?? Date.now;
    while (!signal.aborted) {
      const connection = new AbortController();
      const combined = AbortSignal.any([signal, connection.signal]);
      let watchdog: ReturnType<typeof setTimeout>;
      const touch = () => {
        clearTimeout(watchdog);
        watchdog = setTimeout(() => connection.abort(new DOMException("Worker stream idle", "TimeoutError")), 45_000);
      };
      try {
        const cursor = lastSequenceNum > 0 ? `&from_sequence_num=${lastSequenceNum}` : "";
        const headers: Record<string, string> = {
          Accept: "text/event-stream", "anthropic-version": "2023-06-01", "User-Agent": "oma-brain-dsh",
        };
        if (lastSequenceNum > 0) headers["Last-Event-ID"] = String(lastSequenceNum);
        const credentials = this.config.credentials(id);
        if (credentials.workerEpoch !== epoch) throw new CcrHttpError(409);
        const response = await this.config.request({
          path: `/v1/code/sessions/${encodeURIComponent(id)}/worker/events/stream?worker_epoch=${epoch}${cursor}`,
          token: credentials.token, method: "GET", signal: combined, timeout: 0, headers,
        });
        if (!response.ok) {
          await response.body?.cancel();
          throw new CcrHttpError(response.status);
        }
        if (!response.body) throw new Error("Missing worker SSE stream");
        failures = 0;
        reconnectStartedAt = undefined;
        touch();
        for await (const frame of sse(response.body, combined, touch)) {
          const sequenceNum = frame.id ? parseInt(frame.id, 10) : NaN;
          if (sequenceNum > lastSequenceNum) {
            lastSequenceNum = sequenceNum;
            if (this.authStates.get(id) === claim) claim.sequenceNum = sequenceNum;
          }
          if (!frame.data) continue;
          if (frame.event !== "client_event") {
            workerDiagnostic("warn", "cli_sse_unexpected_event_type", { session_id: id });
            continue;
          }
          let event: Record<string, unknown>;
          try { event = record(JSON.parse(frame.data)); }
          catch {
            workerDiagnostic("warn", "cli_sse_invalid_event_data", { session_id: id });
            continue;
          }
          if (event.session_id !== undefined && event.session_id !== id)
            throw new InboundProtocolError("Invalid worker event session");
          if (typeof event.event_id !== "string" || !event.event_id || !event.payload
            || typeof event.payload !== "object" || Array.isArray(event.payload) || !("type" in event.payload)) {
            workerDiagnostic("warn", "cli_sse_invalid_event_envelope", { session_id: id });
            continue;
          }
          yield { id: event.event_id, payload: event.payload as Record<string, unknown> };
        }
      } catch (error) {
        if (signal.aborted) return;
        if (error instanceof InboundProtocolError || this.config.credentials(id).workerEpoch !== epoch
          || (error instanceof CcrHttpError && [401, 403, 404].includes(error.status))) throw error;
      } finally {
        clearTimeout(watchdog!);
        connection.abort();
      }
      if (signal.aborted) return;
      reconnectStartedAt ??= now();
      if (now() - reconnectStartedAt >= 600_000) throw new Error("Worker SSE reconnection budget exhausted");
      const baseDelay = Math.min(1000 * 2 ** failures++, 30_000);
      const delay = Math.max(0, baseDelay + baseDelay * 0.25 * (2 * (this.config.random ?? Math.random)() - 1));
      await abortable((this.config.sleep ?? sleep)(delay, signal), signal);
    }
  }

  private async worker(
    id: string,
    path: string,
    method: string,
    body?: Record<string, unknown>,
    signal?: AbortSignal,
    timeout?: number,
    workerEpoch?: number,
    observeResult = true,
  ): Promise<Response> {
    signal?.throwIfAborted();
    const credentials = this.config.credentials(id);
    const auth = this.claim(id, credentials.workerEpoch);
    // Capture subscriptions before sending so a late response cannot affect a
    // replacement session, even if the host reuses the same epoch.
    const observers = method !== "GET" && observeResult
      ? [...this.writeObservers.get(id) ?? []].filter((observer) => observer.workerEpoch === (workerEpoch ?? credentials.workerEpoch))
      : [];
    if (workerEpoch !== undefined && credentials.workerEpoch !== workerEpoch) {
      // Refreshing a token must never let an old request take over a new claim.
      const error = new CcrHttpError(409);
      this.notifyWrites(id, observers, leaseFailure(error, workerEpoch));
      throw error;
    }
    if (path === "/events/delivery" && auth.deliveryFailure) throw auth.deliveryFailure;
    // Like superduck's empty-auth-header path, wait for refreshed credentials
    // without issuing HTTP or consuming/resetting the authentication budget.
    if (method !== "GET" && !credentials.token) throw new MissingWorkerCredentialsError();
    let response: Response;
    try {
      response = await this.config.request({
        path: `/v1/code/sessions/${encodeURIComponent(id)}/worker${path}`,
        token: credentials.token,
        method,
        body: body
          ? { ...body, session_id: id, worker_epoch: credentials.workerEpoch }
          : undefined,
        signal,
        timeout,
        headers: { "anthropic-version": "2023-06-01", "User-Agent": "oma-brain-dsh" },
      });
    } catch (error) {
      // Diagnose each actual attempt, excluding cancellation and local checks.
      if (method === "POST" && (path === "/internal-events" || path === "/events") && !signal?.aborted)
        this.logBatchFailure(id, path, error, credentials.workerEpoch, Array.isArray(body?.events) ? body.events.length : 0);
      throw error;
    }
    if (signal?.aborted) {
      await response.body?.cancel();
      signal.throwIfAborted();
    }
    if (!response.ok) {
      const raw = response.headers.get("Retry-After");
      const seconds = raw === null ? NaN : parseInt(raw, 10);
      const retryAfterMs = response.status === 429 && Number.isFinite(seconds) && seconds >= 0
        ? seconds * 1000 : undefined;
      // Inspect the token actually sent by this session, never a global token.
      const expiry = response.status === 401 || response.status === 403
        ? decodeJwtExpiry(credentials.token) : null;
      let authExhausted = false;
      if (method !== "GET" && auth && this.authStates.get(id) === auth &&
        (response.status === 401 || response.status === 403)) {
        auth.failures++;
        // Bound sessions retain the scheduler's configurable shared budget.
        // A standalone client must also stop after ten authentication failures.
        const bound = [...this.writeObservers.get(id) ?? []].some((observer) => observer.workerEpoch === auth.epoch);
        authExhausted = !bound && auth.failures >= 10;
      }
      const error = new CcrHttpError(response.status, retryAfterMs,
        expiry !== null && expiry * 1000 < Date.now(), authExhausted);
      // Record the actual response before stopping the claim aborts its queues.
      // A late response to an already-cancelled request exits above instead.
      if (path === "/events/delivery")
        this.logBatchFailure(id, path, error, credentials.workerEpoch, Array.isArray(body?.updates) ? body.updates.length : 0);
      if (method === "POST" && (path === "/internal-events" || path === "/events"))
        this.logBatchFailure(id, path, error, credentials.workerEpoch, Array.isArray(body?.events) ? body.events.length : 0);
      if (leaseFailure(error, credentials.workerEpoch).status === "rejected")
        this.stopDelivery(id, auth, error);
      this.notifyWrites(id, observers, leaseFailure(error, credentials.workerEpoch), error);
      // Cancellation triggered by lease rejection must not replace its HTTP
      // error with a response-cleanup error before delivery can diagnose it.
      await response.body?.cancel().catch(() => {});
      throw error;
    }
    if (method !== "GET" && auth && this.authStates.get(id) === auth) auth.failures = 0;
    this.notifyWrites(id, observers, { status: "confirmed" });
    return response;
  }

  private notifyWrites(id: string, observers: WriteObserver[], result: CcrLeaseResult, error?: Error): void {
    for (const observer of observers) {
      if (!this.writeObservers.get(id)?.has(observer)) continue;
      observer.rejection = error;
      try {
        observer.listener(result);
      } catch {
        // A host callback must not change a write's confirmation or retry it.
        console.error("CCRv2 lease-loss handler failed");
      } finally {
        observer.rejection = undefined;
      }
    }
  }

  private async retry<T>(job: () => Promise<T>, signal?: AbortSignal): Promise<T> {
    for (let attempt = 0; ; attempt++) {
      signal?.throwIfAborted();
      try {
        return await job();
      } catch (error) {
        signal?.throwIfAborted();
        if (attempt === 2 || (error instanceof CcrHttpError &&
          error.status < 500 && ![408, 429].includes(error.status))) throw error;
        const waiting = (this.config.sleep ?? sleep)(retryDelay(
          attempt + 1,
          error instanceof CcrHttpError ? error.retryAfterMs : undefined,
          this.config.random ?? Math.random,
        ), signal);
        await (signal ? abortable(waiting, signal) : waiting);
      }
    }
  }

  private async writeResponse(id: string, path: string, method: string, body: Record<string, unknown>, requestSignal?: AbortSignal,
    claim?: WorkerPutClaim): Promise<Response> {
    // Snapshot once, before sending. Retries keep the same payload and UUIDs;
    // invalid JSON cannot leave a claimed session in an endless retry loop.
    const snapshot = record(JSON.parse(JSON.stringify(body)));
    const scope = this.writeObservers.get(id)?.values().next().value;
    const workerEpoch = claim?.epoch ?? scope?.workerEpoch ?? this.config.credentials(id).workerEpoch;
    const ownerSignal = claim ? claim.signal : scope?.abort.signal;
    const signal = ownerSignal && requestSignal ? AbortSignal.any([ownerSignal, requestSignal])
      : ownerSignal ?? requestSignal;
    const send = async () => {
      try {
        return await this.worker(id, path, method, snapshot, signal, 10_000, workerEpoch);
      } catch (error) {
        if (method === "PUT" && path === "" && (!signal?.aborted || error instanceof CcrHttpError)) {
          const failure = leaseFailure(error, workerEpoch);
          if (failure.status !== "confirmed") {
            workerDiagnostic("warn", "ccrv2_worker_put_failed", {
              event: "ccrv2_worker_put_failed", method, path: "/worker", session_id: id,
              worker_epoch: workerEpoch, http_status: failure.httpStatus ?? null,
              failure_reason: failure.reason,
            });
          }
        }
        throw error;
      }
    };
    // Detached clients and final flushes after session.stop retain a bounded
    // drain. Active claims wait for confirmation, cancellation or lease loss.
    if (!signal || (claim ? !claim.signal : !scope)) return this.retry(send, signal);
    for (let attempt = 1; ; attempt++) {
      signal.throwIfAborted();
      try {
        const response = await send();
        if (signal.aborted) {
          await response.body?.cancel();
          signal.throwIfAborted();
        }
        return response;
      } catch (error) {
        if (signal.aborted || leaseFailure(error, workerEpoch).status === "rejected") throw error;
        await abortable((this.config.sleep ?? sleep)(retryDelay(
          attempt, error instanceof CcrHttpError ? error.retryAfterMs : undefined,
          this.config.random ?? Math.random,
        ), signal), signal);
      }
    }
  }

  /** Read external metadata; ordinary GET failures return null after ten attempts. */
  async getWorkerState(id: string, workerEpoch?: number, signal?: AbortSignal): Promise<{
    metadata: Record<string, unknown> | null;
    durationMs: number;
  }> {
    const startMs = Date.now();
    signal?.throwIfAborted();
    if (workerEpoch !== undefined && (!Number.isSafeInteger(workerEpoch) || workerEpoch <= 0))
      throw new Error("Invalid worker epoch");
    const credentials = this.config.credentials(id);
    if (!credentials.token) return { metadata: null, durationMs: 0 };
    // Keep this read attached to its original claim even when the optional
    // server-side epoch query is omitted. Tokens still refresh on each attempt.
    const epoch = workerEpoch ?? credentials.workerEpoch;
    const observers = [...this.writeObservers.get(id) ?? []].filter((observer) => observer.workerEpoch === epoch);
    const scope = observers[0];
    if (scope) signal = signal ? AbortSignal.any([scope.abort.signal, signal]) : scope.abort.signal;
    const path = workerEpoch === undefined ? "" : `?worker_epoch=${workerEpoch}`;
    const report = (level: "warn" | "error" | "info", event: string, fields: Record<string, unknown>) => {
      try {
        console[level](JSON.stringify({
          event, context: "worker_state", session_id: id, worker_epoch: epoch, ...fields,
        }));
      } catch { /* Diagnostics must not change read or retry behavior. */ }
    };
    // Adapted from superduck-code CCRClient.getWithRetry. Keep this policy
    // separate from durable writes and internal-event history reads.
    for (let attempt = 1; attempt <= 10; attempt++) {
      signal?.throwIfAborted();
      let data: unknown;
      try {
        const response = await this.worker(id, path, "GET", undefined, signal, 30_000, epoch);
        data = await response.json();
        signal?.throwIfAborted();
      } catch (error) {
        signal?.throwIfAborted();
        const failure = leaseFailure(error, epoch);
        report("warn", "ccrv2_worker_get_failed", {
          attempt, max_attempts: 10, http_status: failure.status === "confirmed" ? undefined : failure.httpStatus,
          failure_reason: error instanceof SyntaxError ? "invalid_response"
            : failure.status === "confirmed" ? undefined : failure.reason,
        });
        // GETs do not reset or increment the shared auth counter. Terminal
        // ownership failures still stop the original session immediately.
        if (failure.status === "rejected") {
          this.notifyWrites(id, observers, failure);
          throw error;
        }
        if (error instanceof SyntaxError) throw error;
        if (attempt < 10) {
          const waiting = (this.config.sleep ?? sleep)(retryDelay(
            attempt, error instanceof CcrHttpError ? error.retryAfterMs : undefined,
            this.config.random ?? Math.random,
          ), signal);
          await (signal ? abortable(waiting, signal) : waiting);
        }
        continue;
      }
      const state: WorkerStateResponse = record(data);
      const worker = state.worker == null ? undefined : record(state.worker);
      const metadata = worker?.external_metadata == null ? null : record(worker.external_metadata);
      const durationMs = Date.now() - startMs;
      // Reading is not restoration. Initialization must finish before a host
      // can report that it has applied this metadata.
      report("info", "ccrv2_worker_state_read", { duration_ms: durationMs, had_state: metadata !== null });
      return { metadata, durationMs };
    }
    const durationMs = Date.now() - startMs;
    report("error", "ccrv2_worker_get_retries_exhausted", { attempts: 10, duration_ms: durationMs });
    return { metadata: null, durationMs };
  }

  async readInternalEvents(id: string, signal?: AbortSignal): Promise<InternalEvent[] | null> {
    return this.readHistory(id, false, signal);
  }

  async readSubagentInternalEvents(id: string, signal?: AbortSignal): Promise<InternalEvent[] | null> {
    return this.readHistory(id, true, signal);
  }

  private async readHistory(id: string, subagents: boolean, signal?: AbortSignal): Promise<InternalEvent[] | null> {
    signal?.throwIfAborted();
    const credentials = this.config.credentials(id);
    if (!credentials.token) return null;
    const epoch = credentials.workerEpoch;
    const observers = [...this.writeObservers.get(id) ?? []].filter((observer) => observer.workerEpoch === epoch);
    const scope = observers[0];
    if (scope) signal = signal ? AbortSignal.any([scope.abort.signal, signal]) : scope.abort.signal;
    const events: InternalEvent[] = [];
    const cursors = new Set<string>();
    let cursor = "";
    while (true) {
      const query = new URLSearchParams();
      if (subagents) query.set("subagents", "true");
      if (cursor) query.set("cursor", cursor);
      const path = `/internal-events${query.size ? `?${query}` : ""}`;
      let response: Response | undefined;
      // Retry each page independently. Exhaustion discards all partial history.
      for (let attempt = 1; attempt <= 10; attempt++) {
        signal?.throwIfAborted();
        try {
          response = await this.worker(id, path, "GET", undefined, signal, 30_000, epoch);
          break;
        } catch (error) {
          signal?.throwIfAborted();
          const failure = leaseFailure(error, epoch);
          if (failure.status === "rejected") {
            this.notifyWrites(id, observers, failure);
            throw error;
          }
          if (attempt === 10) return null;
          const waiting = (this.config.sleep ?? sleep)(retryDelay(
            attempt, error instanceof CcrHttpError ? error.retryAfterMs : undefined,
            this.config.random ?? Math.random,
          ), signal);
          await (signal ? abortable(waiting, signal) : waiting);
        }
      }
      const json = record(await response!.json());
      signal?.throwIfAborted();
      if (json.data !== undefined && !Array.isArray(json.data)) throw new Error("Invalid private event page");
      const nextCursor = json.next_cursor;
      if (nextCursor !== undefined && nextCursor !== null && typeof nextCursor !== "string")
        throw new Error("Invalid private event cursor");
      for (const entry of json.data ?? []) {
        const event = record(entry);
        record(event.payload);
        events.push(event as InternalEvent);
      }
      if (!nextCursor) return events;
      cursor = nextCursor;
      if (cursors.has(cursor)) throw new Error("Private event cursor did not advance");
      cursors.add(cursor);
    }
  }

  async isToolConfirmationPending(id: string, requestId: string, signal?: AbortSignal): Promise<boolean> {
    const { metadata } = await this.getWorkerState(id, undefined, signal);
    const prefix = "managed_agent_tool_permission_request";
    for (const [key, value] of Object.entries(metadata ?? {})) {
      if (key !== prefix && !key.startsWith(`${prefix}:`)) continue;
      if (value == null) continue;
      const request = record(value);
      if (request.request_id === requestId && typeof request.public_event_id === "string"
        && request.public_event_id && typeof request.provider_tool_use_id === "string"
        && request.provider_tool_use_id && (key === prefix || key === `${prefix}:${request.public_event_id}`))
        return true;
    }
    return false;
  }

  async privateEvents(id: string, options?: { subagents?: boolean }): Promise<Record<string, unknown>[]> {
    const events = await this.readHistory(id, options?.subagents ?? false);
    if (events === null) throw new Error("Private event history unavailable");
    return events.map((event) => event.payload);
  }

  async persist(id: string, subtype: string, data: unknown, agentId?: string): Promise<void> {
    const payload = {
      type: "system",
      uuid: stableId(agentId === undefined ? [id, subtype, data] : [id, agentId, subtype, data]),
      subtype,
      schema_version: 1,
      data,
    };
    const event: WorkerEvent = { payload, is_compaction: false, ...(agentId === undefined ? {} : { agent_id: agentId }) };
    const snapshot = JSON.parse(JSON.stringify(event)) as WorkerEvent;
    await this.eventUploader(id, this.internalEvents, "/internal-events", 200).enqueue(snapshot);
  }

  private eventUploader<T>(id: string, queues: Map<string, EventQueue<T>>, path: "/events" | "/internal-events",
    maxQueueSize: number): CcrBatchUploader<T> {
    const claim = this.claim(id, this.config.credentials(id).workerEpoch);
    const scope = this.writeObservers.get(id)?.values().next().value;
    let queue = queues.get(id);
    if (queue && (queue.claim !== claim || queue.scope !== scope)) {
      void queue.uploader.close(new Error("CCRv2 claim replaced"));
      queue = undefined;
    }
    if (!queue) {
      const binding: WorkerPutClaim = { epoch: scope?.workerEpoch ?? claim.epoch, signal: scope?.abort.signal };
      const uploader = new CcrBatchUploader<T>(async (events, signal) => {
        const response = await this.writeResponse(id, path, "POST", { events }, signal, binding);
        // A 2xx confirms the batch; response cleanup cannot fail it.
        await response.body?.cancel().catch(() => {});
      }, () => {
        if (queues.get(id)?.uploader === uploader) queues.delete(id);
      }, undefined, { maxBatchSize: 100, maxQueueSize, maxBatchBytes: 10 * 1024 * 1024 },
      path === "/events" ? { delayMs: 100, schedule: this.config.schedule } : undefined);
      queue = { claim, scope, uploader };
      queues.set(id, queue);
      if (scope) uploader.bind(scope.abort.signal);
    }
    return queue.uploader;
  }

  async events(id: string, payloads: Record<string, unknown>[], signal?: AbortSignal): Promise<void> {
    signal?.throwIfAborted();
    const events: ClientEvent[] = payloads.map((payload) => {
      if (typeof payload.type !== "string") throw new Error("Missing client event type");
      return {
        ephemeral: ["event_start", "event_delta", "event_stop"].includes(payload.type),
        payload: {
          ...payload,
          type: payload.type,
          uuid: typeof payload.uuid === "string" ? payload.uuid : crypto.randomUUID(),
        },
      };
    });
    // Snapshot the entire call before enqueueing or waiting for capacity.
    const snapshot = JSON.parse(JSON.stringify(events)) as ClientEvent[];
    if (!snapshot.length) return;
    await this.eventUploader(id, this.outputEvents, "/events", 100_000)
      .enqueueMany(snapshot, signal, snapshot.every((event) => event.ephemeral === true));
  }

  async acknowledge(id: string, inputId: string, signal?: AbortSignal): Promise<void> {
    await this.reportDelivery(id, inputId, "processed", signal);
  }

  private logBatchFailure(id: string, path: "/events/delivery" | "/internal-events" | "/events", error: unknown, epoch: number, batchSize: number): void {
    const httpStatus = error instanceof CcrHttpError ? error.status : undefined;
    const code = error instanceof Error && "code" in error ? error.code : undefined;
    const failure = leaseFailure(error, epoch);
    workerDiagnostic("warn", httpStatus === undefined ? "cli_worker_request_error" : "cli_worker_request_failed", {
      method: "post", path: `/worker${path}`, session_ref: createHash("sha256").update(id).digest("hex"), worker_epoch: epoch,
      batch_size: batchSize, status: httpStatus,
      failure_reason: failure.status === "confirmed" ? undefined : failure.reason,
      error_code: typeof code === "string" && ["ECONNRESET", "ECONNREFUSED", "ETIMEDOUT", "ENOTFOUND", "EAI_AGAIN"].includes(code)
        ? code : undefined,
    });
  }

  /** Confirm a delivery update; signal, when supplied, owns the session's queue. */
  async reportDelivery(id: string, eventId: string, status: DeliveryUpdate["status"], signal?: AbortSignal,
    workerEpoch?: number): Promise<void> {
    signal?.throwIfAborted();
    if (typeof eventId !== "string" || !eventId.trim()) throw new Error("Missing delivery event ID");
    if (!["received", "processing", "processed"].includes(status)) throw new Error("Invalid delivery status");
    const epoch = this.config.credentials(id).workerEpoch;
    if (workerEpoch !== undefined && workerEpoch !== epoch) throw new CcrHttpError(409);
    const claim = this.claim(id, epoch);
    if (claim.deliveryFailure) throw claim.deliveryFailure;
    let delivery = this.deliveries.get(id);
    if (delivery && delivery.epoch !== epoch) {
      void delivery.uploader.close(new CcrHttpError(409));
      delivery = undefined;
    }
    if (!delivery) {
      const uploader = new CcrDeliveryUploader(async (batch, requestSignal) => {
        try {
          const response = await this.worker(id, "/events/delivery", "POST", {
            updates: batch.map((update) => ({ event_id: update.eventId, status: update.status })),
          }, requestSignal, 10_000, epoch);
          // Like superduck, any 2xx confirms the batch. ignored updates are valid:
          // a lost reply or an expired ACK mapping must not poison later receipts.
          await response.body?.cancel().catch(() => {});
        } catch (error) {
          // HTTP failures were logged before lease observers could cancel us.
          if (!requestSignal.aborted && !(error instanceof CcrHttpError) && !(error instanceof MissingWorkerCredentialsError))
            this.logBatchFailure(id, "/events/delivery", error, epoch, batch.length);
          throw error;
        }
      }, (error) => {
        if (this.deliveries.get(id)?.uploader !== uploader) return;
        this.deliveries.delete(id);
        if (this.authStates.get(id) === claim)
          claim.deliveryFailure ??= error instanceof Error ? error : new Error("CCRv2 delivery stopped");
      }, {
        allowed: (error) => leaseFailure(error, epoch).status !== "rejected",
        wait: (error, attempt, requestSignal) => abortable((this.config.sleep ?? sleep)(retryDelay(
          attempt, error instanceof CcrHttpError ? error.retryAfterMs : undefined,
          this.config.random ?? Math.random,
        ), requestSignal), requestSignal),
      });
      delivery = { epoch, uploader };
      this.deliveries.set(id, delivery);
    }
    const scope = [...this.writeObservers.get(id) ?? []].find((observer) => observer.workerEpoch === epoch);
    if (scope) delivery.uploader.bind(scope.abort.signal);
    if (signal) delivery.uploader.bind(signal);
    await delivery.uploader.enqueue({ eventId, status });
  }

  async setStatus(id: string, status: SessionState, details?: RequiresActionDetails): Promise<void> {
    if (status === "requires_action" && !details)
      throw new Error("Approval details are required for requires_action");
    await this.putWorker(id, {
      worker_status: status,
      requires_action_details: status === "requires_action" && details ? {
        tool_name: details.tool_name,
        action_description: details.action_description,
        request_id: details.request_id,
      } : null,
      external_metadata: { pending_action: details ?? null, ...(status === "idle" ? { task_summary: null } : {}) },
    }, !details);
  }

  async reportMetadata(id: string, metadata: SessionExternalMetadata & Record<string, unknown>): Promise<void> {
    await this.putWorker(id, { external_metadata: record(metadata) });
  }

  private async putWorker(id: string, body: Record<string, unknown>, deduplicate = false): Promise<void> {
    const scope = this.writeObservers.get(id)?.values().next().value;
    const claim: WorkerPutClaim = {
      epoch: scope?.workerEpoch ?? this.config.credentials(id).workerEpoch,
      signal: scope?.abort.signal,
    };
    await this.workerPuts.put(id, claim, body, deduplicate, () => {
      if (this.config.credentials(id).workerEpoch !== claim.epoch) throw new CcrHttpError(409);
    }, async (snapshot) => {
      const response = await this.writeResponse(id, "", "PUT", snapshot, undefined, claim);
      await response.body?.cancel();
    });
  }

  async heartbeat(id: string, signal?: AbortSignal, timeout = 5000, workerEpoch?: number): Promise<void> {
    const response = await this.worker(id, "/heartbeat", "POST", {}, signal, timeout, workerEpoch);
    await response.body?.cancel();
  }

  /** Capture the claim's epoch once; authentication is still resolved per beat. */
  bindLease(id: string): CcrLeaseCheck {
    const workerEpoch = this.config.credentials(id).workerEpoch;
    // The scheduler accepts this check's result itself. Only requests outside
    // the check notify subscribers, so each response updates auth state once.
    const check: CcrLeaseCheck = (signal, timeoutMs) => this.leaseResult(id, signal, timeoutMs, workerEpoch, false);
    check.subscribe = (listener) => {
      const observer: WriteObserver = { workerEpoch, listener, abort: new AbortController() };
      const observers = this.writeObservers.get(id) ?? new Set<WriteObserver>();
      if (![...observers].some((entry) => entry.workerEpoch === workerEpoch))
        this.resetClaim(id, workerEpoch);
      const claim = this.claim(id, workerEpoch);
      observers.add(observer);
      this.writeObservers.set(id, observers);
      return () => {
        observer.abort.abort(observer.rejection ?? new Error("CCRv2 session stopped"));
        observers.delete(observer);
        if (![...observers].some((entry) => entry.workerEpoch === workerEpoch)) {
          // End the binding's auth budget without forgetting that its delivery
          // claim stopped. Later one-shot heartbeats keep their own budget.
          if (this.authStates.get(id) === claim) claim.failures = 0;
          this.stopDelivery(id, claim, new Error("CCRv2 session stopped"));
        }
        if (!observers.size && this.writeObservers.get(id) === observers) this.writeObservers.delete(id);
      };
    };
    return check;
  }

  /** One-shot compatibility API; scheduled checks use the epoch-bound lease. */
  async checkLease(id: string): Promise<CcrLeaseStatus> {
    return (await this.leaseResult(id)).status;
  }

  private async leaseResult(
    id: string, signal?: AbortSignal, timeout = 5000, workerEpoch?: number,
    observeResult = true,
  ): Promise<CcrLeaseResult> {
    try {
      const response = await this.worker(id, "/heartbeat", "POST", {}, signal, timeout, workerEpoch, observeResult);
      await response.body?.cancel();
      return { status: "confirmed" };
    } catch (error) {
      signal?.throwIfAborted();
      return leaseFailure(error, workerEpoch);
    }
  }

  sessionIds(): Iterable<string> {
    return this.config.sessionIds();
  }
}

class InboundProtocolError extends Error {}
class MissingWorkerCredentialsError extends Error {
  constructor() { super("CCRv2 worker credentials unavailable"); }
}
