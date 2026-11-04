import type { SessionEvent, SessionHeader } from "@deepseek-ai/dsh-session";
import { DsrError, decimal, object, request, type Transport } from "./http.ts";
import type { Claim, Registration, WorkerState, WorkerPatch, Finish, PermissionRequest, Permission, Delivery, DeliveryResult, Transcript, TranscriptPage, PublicEvent } from "./wire.ts";

export class DsrClient {
  readonly root: string;
  readonly claim: Readonly<Claim>;
  private registration?: Promise<Registration>;
  private readonly cancelled = new Set<string>();

  constructor(readonly transport: Transport, claim: Readonly<Claim>) {
    this.claim = Object.freeze({ ...claim });
    decimal(claim.worker_epoch, true);
    this.root = `/v1/dsr/sessions/${encodeURIComponent(claim.code_session_id)}`;
  }

  async register(signal?: AbortSignal): Promise<Registration> {
    if (this.registration) return this.registration;
    this.registration = this.registerOnce(signal).finally(() => { this.registration = undefined; });
    return this.registration;
  }

  private async registerOnce(signal?: AbortSignal): Promise<Registration> {
    const response = await request(this.transport, { path: `${this.root}/register`, token: this.claim.session_token, method: "POST", body: {}, signal });
    const value = object(await response.json());
    const lease = object(value.lease);
    if (value.epoch !== this.claim.worker_epoch || value.protocol !== "1" || typeof value.observability_enabled !== "boolean"
      || ![lease.ttl_ms, lease.grace_ms, lease.heartbeat_interval_ms].every(value => typeof value === "number" && Number.isSafeInteger(value) && value > 0)
      || (lease.heartbeat_interval_ms as number) * 1.1 + 5000 >= (lease.ttl_ms as number)) throw new DsrError(0, "invalid_response");
    return value as unknown as Registration;
  }

  async ready(): Promise<void> { await this.registration; }

  private async json(path: string, method = "GET", body?: unknown, signal?: AbortSignal, headers?: Record<string, string>): Promise<Record<string, unknown>> {
    await this.ready();
    const send = () => request(this.transport, { path: `${this.root}${path}`, method, body, token: this.claim.session_token, signal, headers });
    let response: Response;
    try { response = await send(); }
    catch (error) {
      if (!(error instanceof DsrError) || error.code !== "lease_expired") throw error;
      await this.register(signal);
      response = await send();
    }
    return object(await response.json());
  }

  async heartbeat(signal?: AbortSignal): Promise<string> {
    const response = await this.json("/heartbeat", "POST", {}, signal);
    if (typeof response.lease_expires_at !== "string" || !Number.isFinite(Date.parse(response.lease_expires_at))) throw new DsrError(0, "invalid_response");
    return response.lease_expires_at;
  }

  async context(threadId?: string, signal?: AbortSignal): Promise<Record<string, unknown>> {
    return object((await this.json("/context", "GET", undefined, signal, threadId ? { "DSR-Thread-ID": threadId } : undefined)).context);
  }

  async workerState(signal?: AbortSignal): Promise<WorkerState> {
    return this.parseState(await this.json("/worker-state", "GET", undefined, signal));
  }

  async patchWorkerState(patch: WorkerPatch, signal?: AbortSignal): Promise<WorkerState> {
    return this.parseState(await this.json("/worker-state", "PUT", patch, signal));
  }

  private parseState(value: Record<string, unknown>): WorkerState {
    if (Object.hasOwn(value, "status") || value.model !== null && typeof value.model !== "string"
      || value.trace_turns !== null && !Array.isArray(value.trace_turns)) throw new DsrError(0, "invalid_response");
    if (value.model_control_sequence !== null && BigInt(decimal(value.model_control_sequence, true)) > 18446744073709551615n) throw new DsrError(0, "invalid_response");
    return value as unknown as WorkerState;
  }

  async delivery(updates: Delivery[], signal?: AbortSignal): Promise<DeliveryResult> {
    const response = await this.json("/inputs/delivery", "POST", { updates }, signal);
    if (!Number.isSafeInteger(response.applied) || !Number.isSafeInteger(response.ignored) || !Number.isSafeInteger(response.scheduled) || !Array.isArray(response.results)
      || response.results.length !== updates.length) throw new DsrError(0, "invalid_response");
    for (const [index, raw] of response.results.entries()) {
      const item = object(raw);
      if (item.id !== updates[index]!.id || item.status !== updates[index]!.status || !["applied", "ignored", "scheduled"].includes(String(item.outcome))) throw new DsrError(0, "invalid_response");
    }
    return response as unknown as DeliveryResult;
  }

  async processing(id: string, signal?: AbortSignal): Promise<"running" | "completed"> {
    const response = await this.delivery([{ id, status: "processing" }], signal);
    const state = response.results[0]?.execution_status;
    if (state !== "running" && state !== "completed") throw new DsrError(0, "processing_unconfirmed");
    const cancelledAt = response.results[0]?.cancel_requested_at;
    if (cancelledAt !== undefined) {
      if (typeof cancelledAt !== "string" || !Number.isFinite(Date.parse(cancelledAt))) throw new DsrError(0, "invalid_response");
      this.cancelled.add(id);
    }
    return state;
  }

  inputCancelled(id: string): boolean { return this.cancelled.has(id); }

  async finish(id: string, completion: Finish, signal?: AbortSignal): Promise<void> {
    const response = await this.json(`/inputs/${encodeURIComponent(id)}/finish`, "POST", completion, signal);
    if (response.result !== completion.result || response.failure_code !== ("failure_code" in completion ? completion.failure_code : undefined)) throw new DsrError(0, "finish_idempotency_mismatch");
  }

  async putHeader(id: string, header: SessionHeader, parent: string | null, signal?: AbortSignal): Promise<string> {
    const response = await this.json(`/transcripts/${encodeURIComponent(id)}/header`, "PUT", { parent_dsh_session_id: parent, header }, signal);
    if (response.dsh_session_id !== id) throw new DsrError(0, "invalid_response");
    return decimal(response.next_seq);
  }

  async appendEvents(id: string, events: readonly SessionEvent[], signal?: AbortSignal): Promise<string> {
    return decimal((await this.json(`/transcripts/${encodeURIComponent(id)}/events`, "POST", { events }, signal)).next_seq);
  }

  async listTranscripts(signal?: AbortSignal): Promise<Transcript[]> {
    const result: Transcript[] = [];
    let after: string | null = null;
    do {
      const response = await this.json(`/transcripts?limit=200${after === null ? "" : `&after=${encodeURIComponent(after)}`}`, "GET", undefined, signal);
      if (!Array.isArray(response.data) || response.next_after !== null && typeof response.next_after !== "string") throw new DsrError(0, "invalid_response");
      let previous: string | null = after;
      for (const raw of response.data) {
        const row = object(raw);
        decimal(row.next_seq);
        if (typeof row.dsh_session_id !== "string" || !row.dsh_session_id || previous !== null && row.dsh_session_id <= previous) throw new DsrError(0, "invalid_response");
        previous = row.dsh_session_id;
        result.push(row as unknown as Transcript);
      }
      const next = response.next_after as string | null;
      if (next !== null && (response.data.length === 0 || next !== previous || after !== null && next <= after)) throw new DsrError(0, "invalid_response");
      after = next;
    } while (after !== null);
    return result;
  }

  async readTranscript(id: string, from = "0", signal?: AbortSignal): Promise<TranscriptPage> {
    decimal(from);
    const response = await this.json(`/transcripts/${encodeURIComponent(id)}?from_seq=${from}&limit_bytes=4194304`, "GET", undefined, signal);
    if (!Array.isArray(response.events) || response.next_from_seq !== null && BigInt(decimal(response.next_from_seq)) <= BigInt(from)) throw new DsrError(0, "invalid_response");
    for (const [index, raw] of response.events.entries()) {
      const event = object(raw);
      if (!Number.isSafeInteger(event.seq) || BigInt(event.seq as number) !== BigInt(from) + BigInt(index)) throw new DsrError(0, "invalid_response");
    }
    if (response.next_from_seq !== null && BigInt(decimal(response.next_from_seq)) !== BigInt(from) + BigInt(response.events.length)) throw new DsrError(0, "invalid_response");
    if (from === "0" && object(response.header).id !== id || from !== "0" && response.header !== null) throw new DsrError(0, "invalid_response");
    return response as unknown as TranscriptPage;
  }

  async permission(input: PermissionRequest, signal?: AbortSignal): Promise<Permission> {
    return this.parsePermission(await this.json("/permission-requests", "POST", input, signal));
  }

  async cancelPermission(id: string, input: { dsh_session_id: string; tool_use_id: string; input_id?: string }, signal?: AbortSignal): Promise<Permission> {
    return this.parsePermission(await this.json(`/permission-requests/${encodeURIComponent(id)}/cancel`, "POST", input, signal));
  }

  private parsePermission(response: Record<string, unknown>): Permission {
    if (typeof response.request_id !== "string" || !response.request_id || !["pending", "resolved", "cancelled", "closed"].includes(String(response.state))) throw new DsrError(0, "invalid_response");
    if (response.state === "resolved" && !["allow", "deny", "external_result"].includes(String(object(response.decision).behavior))) throw new DsrError(0, "invalid_response");
    return response as unknown as Permission;
  }

  async publish(events: PublicEvent[], signal?: AbortSignal): Promise<void> {
    const response = await this.json("/public-events", "POST", { events }, signal);
    const count = events.filter(event => !event.type.startsWith("preview.")).length;
    if (!Number.isSafeInteger(response.accepted) || !Number.isSafeInteger(response.duplicates)
      || (response.accepted as number) + (response.duplicates as number) !== count) throw new DsrError(0, "invalid_response");
  }
}
