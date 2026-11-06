import { createHash } from "node:crypto";
import { workerDiagnostic } from "../observability/diagnostics.ts";
import type { DshLogData } from "../runtime/transcript-types.ts";
import { Context, Service } from "@deepseek-ai/cordis";
import {
  SessionId,
  type SessionEvent,
  type SessionHeader,
} from "@deepseek-ai/dsh-session";
import {
  withDownloadedTranscript,
  type DownloadedTranscript,
} from "./transcript-download.ts";
import { transcriptBatch, TranscriptUploadError } from "./transcript-batch.ts";
import { ReplicationPump } from "./replication-pump.ts";
import {
  pendingBytes,
  rejectReplication,
  settleReplication,
  updateReady,
  type ReplicationState as Replication,
} from "./replication-state.ts";
export interface RemoteTranscriptSnapshot {
  readonly nextSeq: string;
}

export interface RemoteTranscriptStore {
  read(
    rootSessionId: string,
    sessionId: string,
    signal?: AbortSignal,
    snapshot?: RemoteTranscriptSnapshot | null,
  ): AsyncIterable<readonly DshLogData[]>;
  write(
    rootSessionId: string,
    sessionId: string,
    records: readonly DshLogData[],
    signal?: AbortSignal,
  ): Promise<void>;
  fenced(error: unknown): boolean;
}

interface ReplicationConfig {
  remote: RemoteTranscriptStore;
  highWaterBytes?: number;
  lowWaterBytes?: number;
  drainTimeoutMs?: number;
}

interface CapacityWaiter {
  resolve(): void;
  reject(error: unknown): void;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    sessionReplication: SessionReplication;
  }
}

export class SessionReplication extends Service {
  static inject = ["sessionPersistence", "sessions", "transcriptStore"];
  private readonly parents = new Map<SessionId, SessionId>();
  private readonly jobs = new Map<SessionId, Replication>();
  private stopped = false;
  private readonly pumps = new Map<SessionId, ReplicationPump>();
  private readonly capacityWaiters = new Map<SessionId, Set<CapacityWaiter>>();
  private readonly highWaterBytes: number;
  private readonly lowWaterBytes: number;
  private readonly forgotten = new Set<SessionId>();

  constructor(
    ctx: Context,
    readonly config: ReplicationConfig,
    serviceName = "sessionReplication",
  ) {
    super(ctx, serviceName);
    this.highWaterBytes = config.highWaterBytes ?? 32 * 1024 * 1024;
    this.lowWaterBytes = config.lowWaterBytes ?? 16 * 1024 * 1024;
    if (
      !Number.isSafeInteger(this.lowWaterBytes) ||
      !Number.isSafeInteger(this.highWaterBytes) ||
      !(this.lowWaterBytes >= 0 && this.highWaterBytes > this.lowWaterBytes)
    )
      throw new Error("Invalid transcript backpressure watermarks");
    const disconnect = ctx.transcriptStore.connect({
      changed: (id) => this.schedule(id),
      flush: async (id, through, signal) => {
        const owner = this.jobs.get(id);
        if (owner?.headerConfirmed && owner.confirmed >= through) return;
        await this.flush(id, signal);
      },
    });
    ctx.effect(() => disconnect);
    ctx.on("agent/created", ({ agent }) => {
      agent.ctx.effect(() => async () => {
        const owner = this.jobs.get(agent.id);
        if (!owner || owner.live !== agent.session) return;
        const releasing = owner.chain.then(async () => {
          await this.captureLocal(agent.id, owner);
          if (owner.live === agent.session) {
            owner.live = undefined;
          }
        });
        owner.chain = releasing.catch(() => {});
        await releasing;
      });
      return undefined;
    });
    ctx.on("session/created", (session) => {
      if (this.stopped || this.forgotten.has(session.id)) return;
      const previous = this.jobs.get(session.id);
      if (previous?.paused || previous?.terminal || previous?.live === session)
        return;
      if (previous?.live) {
        previous.captureError = new Error(
          "Cannot replace an active transcript session",
        );
        return;
      }
      const owner = this.job(session.id);
      owner.live = session;
      this.schedule(session.id);
    });
    ctx.on("session/disposed", (session) => {
      const owner = this.jobs.get(session.id);
      if (owner?.live === session) {
        owner.live = undefined;
      }
    });
    ctx.effect(() => async () => {
      this.stopped = true;
      for (const job of this.jobs.values())
        if (job.timer) clearTimeout(job.timer);
      for (const [id, job] of this.jobs)
        this.pause(id, job, new Error("Session replication is stopped"));
      await Promise.all([...this.pumps.values()].map((pump) => pump.idle()));
    });
  }

  bindThread(id: string, parent: string): void {
    const previous = this.parents.get(SessionId(id));
    if (previous && previous !== parent)
      throw new Error("Thread belongs to another session");
    this.parents.set(SessionId(id), SessionId(parent));
  }

  async ensure(
    value: string,
    snapshot?: RemoteTranscriptSnapshot | null,
  ): Promise<boolean> {
    const id = SessionId(value);
    this.forgotten.delete(id);
    let hydrated = false;
    await this.serialize(id, async (owner) => {
      if (owner.acquired) return;
      if (owner.live)
        throw new Error("Cannot restore an active transcript session");
      await this.withRemote(
        id,
        owner,
        async (remote) => {
          if (
            snapshot &&
            (!remote || BigInt(remote.count) !== BigInt(snapshot.nextSeq))
          )
            throw new TranscriptUploadError(
              "Transcript does not match directory watermark",
            );
          this.ctx.transcriptStore.replace(id, remote, () =>
            this.assertCurrent(id, owner),
          );
          hydrated = !!remote;
          owner.readyNext = remote?.count ?? 0;
          owner.requestedNext = owner.readyNext;
          owner.byteBase = owner.readyNext;
          owner.byteEnds = [0];
          owner.headerReady = false;
          owner.pendingSince = undefined;
          this.assertCurrent(id, owner);
          owner.confirmed = remote?.count ?? 0;
          owner.headerConfirmed = !!remote;
          owner.validated = true;
          owner.acquired = true;
        },
        snapshot,
      );
    });
    return hydrated;
  }

  private async captureLocal(id: SessionId, owner: Replication): Promise<void> {
    if (owner.live) await this.ctx.transcriptStore.capture(owner.live);
    this.assertCurrent(id, owner);
    const record = this.ctx.transcriptStore.view(id);
    if (record) updateReady(owner, record);
  }

  async flush(value: string, signal?: AbortSignal): Promise<void> {
    signal?.throwIfAborted();
    const id = SessionId(value);
    this.forgotten.delete(id);
    const members = this.members(id);
    for (const [key, owner] of members) this.assertCurrent(key, owner);
    const promises = members.map(([key, owner]) => {
      const through = this.ctx.transcriptStore.eventCount(key);
      return new Promise<void>((resolve, reject) => {
        const waiter = {
          through,
          resolve: () => finish(),
          reject: (error: unknown) => finish(error),
        };
        const cancel = () =>
          finish(signal?.reason ?? new Error("Transcript flush cancelled"));
        const finish = (error?: unknown) => {
          owner.waiters.delete(waiter);
          signal?.removeEventListener("abort", cancel);
          if (error !== undefined) reject(error);
          else resolve();
        };
        owner.waiters.add(waiter);
        signal?.addEventListener("abort", cancel, { once: true });
        if (signal?.aborted) cancel();
        else this.schedule(key, 0, true);
      });
    });
    await Promise.all(promises);
  }

  status(value: string) {
    const jobs = this.members(SessionId(value)).map(([, job]) => job);
    return {
      pendingEvents: jobs.reduce(
        (total, job) => total + Math.max(0, job.readyNext - job.confirmed),
        0,
      ),
      pendingBytes: jobs.reduce((total, job) => total + pendingBytes(job), 0),
      inFlight: jobs.some((job) => job.inFlight),
      oldestPendingMs: jobs.reduce(
        (age, job) =>
          Math.max(age, job.pendingSince ? Date.now() - job.pendingSince : 0),
        0,
      ),
    };
  }

  async waitForCapacity(value: string, signal?: AbortSignal): Promise<void> {
    signal?.throwIfAborted();
    const id = this.root(SessionId(value));
    const members = this.members(id);
    for (const [key, owner] of members) {
      this.assertCurrent(key, owner);
      await this.captureLocal(key, owner);
    }
    signal?.throwIfAborted();
    for (const [key, owner] of members) this.assertCurrent(key, owner);
    for (const [key, owner] of this.members(id)) this.assertCurrent(key, owner);
    if (this.status(id).pendingBytes < this.highWaterBytes) return;
    const started = Date.now();
    workerDiagnostic("warn", "dsr_transcript_backpressure", {
      phase: "waiting",
      pending_bytes: this.status(id).pendingBytes,
      pending_events: this.status(id).pendingEvents,
    });
    await new Promise<void>((resolve, reject) => {
      const waiters = this.capacityWaiters.get(id) ?? new Set<CapacityWaiter>();
      this.capacityWaiters.set(id, waiters);
      const finish = (error?: unknown) => {
        waiters.delete(waiter);
        if (!waiters.size) this.capacityWaiters.delete(id);
        signal?.removeEventListener("abort", cancel);
        if (error !== undefined) reject(error);
        else resolve();
      };
      const waiter = {
        resolve: () => finish(),
        reject: (error: unknown) => finish(error),
      };
      const cancel = () =>
        finish(
          signal?.reason ?? new Error("Transcript capacity wait cancelled"),
        );
      waiters.add(waiter);
      signal?.addEventListener("abort", cancel, { once: true });
      if (signal?.aborted) cancel();
      else for (const [key] of this.members(id)) this.schedule(key);
    });
    workerDiagnostic("info", "dsr_transcript_backpressure", {
      phase: "resumed",
      wait_ms: Date.now() - started,
      pending_bytes: this.status(id).pendingBytes,
    });
  }

  async drain(
    value: string,
    timeoutMs = this.config.drainTimeoutMs ?? 5000,
  ): Promise<void> {
    const id = SessionId(value);
    const abort = new AbortController();
    const timer = setTimeout(
      () => abort.abort(new Error("Transcript drain timed out")),
      timeoutMs,
    );
    try {
      await this.flush(id, abort.signal);
    } catch (error) {
      const status = this.status(id);
      workerDiagnostic("warn", "dsr_transcript_drain_failed", {
        pending_events: status.pendingEvents,
        pending_bytes: status.pendingBytes,
        in_flight: status.inFlight,
      });
      throw error;
    } finally {
      clearTimeout(timer);
      for (const [key, job] of this.members(id))
        this.pause(
          key,
          job,
          abort.signal.reason ?? new Error("Session replication is stopped"),
        );
    }
  }

  cancel(
    value: string,
    error = new Error("Transcript replication cancelled"),
  ): void {
    for (const [id, job] of this.members(SessionId(value)))
      this.pause(id, job, error);
  }

  private root(id: SessionId): SessionId {
    return this.parents.get(id) ?? id;
  }

  private members(id: SessionId): [SessionId, Replication][] {
    this.job(id);
    return [...this.jobs].filter(
      ([key]) => key === id || this.parents.get(key) === id,
    );
  }

  private pump(id: SessionId): ReplicationPump {
    const root = this.root(id);
    let pump = this.pumps.get(root);
    if (!pump) this.pumps.set(root, (pump = new ReplicationPump()));
    return pump;
  }

  private capacityChanged(id: SessionId, error?: unknown): void {
    const root = this.root(id);
    if (
      error === undefined &&
      this.status(root).pendingBytes > this.lowWaterBytes
    )
      return;
    for (const waiter of this.capacityWaiters.get(root) ?? []) {
      if (error !== undefined) waiter.reject(error);
      else waiter.resolve();
    }
  }

  private pause(id: SessionId, job: Replication, error: Error): void {
    job.paused = true;
    if (job.timer) clearTimeout(job.timer);
    job.timer = undefined;
    this.pump(id).remove(id);
    job.abort.abort(error);
    rejectReplication(job, error);
    this.capacityChanged(id, error);
  }

  forget(value: string): void {
    const id = SessionId(value);
    this.forgetOne(id);
    for (const [child, parent] of this.parents) {
      if (parent !== id) continue;
      this.forgetOne(child);
      this.parents.delete(child);
    }
    this.pumps.delete(id);
  }

  private forgetOne(id: SessionId): void {
    const job = this.jobs.get(id);
    if (job) this.pause(id, job, new Error("Transcript owner released"));
    this.jobs.delete(id);
    this.ctx.transcriptStore.forget(id);
    this.forgotten.add(id);
  }

  private job(id: SessionId): Replication {
    let job = this.jobs.get(id);
    if (!job) {
      const parent = this.parents.get(id);
      const inherited = parent ? this.jobs.get(parent) : undefined;
      job = {
        chain: Promise.resolve(),
        failures: 0,
        terminal: inherited?.terminal,
        paused: inherited?.paused,
        confirmed: 0,
        readyNext: 0,
        requestedNext: 0,
        headerConfirmed: false,
        headerBytes: 0,
        waiters: new Set(),
        byteBase: 0,
        byteEnds: [0],
        inFlight: false,
        abort: new AbortController(),
      };
      this.jobs.set(id, job);
    }
    return job;
  }

  private assertCurrent(id: SessionId, job: Replication): void {
    if (job.terminal) throw job.terminal;
    if (this.stopped || job.paused || this.jobs.get(id) !== job)
      throw new Error("Session replication is stopped");
  }

  private rejectLease(id: SessionId, owner: Replication, error: Error): void {
    owner.terminal = error;
    if (this.jobs.get(id) !== owner) return;
    const parent = this.parents.get(id) ?? id;
    this.job(parent).terminal = error;
    for (const [child, job] of this.jobs) {
      if (child !== parent && this.parents.get(child) !== parent) continue;
      job.terminal = error;
      this.pause(child, job, error);
    }
  }

  private serialize(
    id: SessionId,
    operation: (job: Replication) => Promise<void>,
    owner = this.job(id),
  ): Promise<void> {
    const result = owner.chain.then(async () => {
      this.assertCurrent(id, owner);
      try {
        await operation(owner);
      } catch (error) {
        if (this.config.remote.fenced(error)) {
          this.rejectLease(
            id,
            owner,
            error instanceof Error
              ? error
              : new Error("Transcript owner rejected"),
          );
        }
        throw error;
      }
    });
    owner.chain = result.catch(() => {});
    return result;
  }

  private schedule(id: SessionId, delay = 0, immediate = false): void {
    if (this.stopped || this.forgotten.has(id)) return;
    const job = this.job(id);
    if (job.terminal || job.paused) return;
    job.requestedNext = Math.max(
      job.requestedNext,
      this.ctx.transcriptStore.eventCount(id) ?? 0,
    );
    if (job.timer && !immediate) return;
    if (job.timer) clearTimeout(job.timer);
    if (delay > 0) {
      job.timer = setTimeout(() => {
        job.timer = undefined;
        this.schedule(id);
      }, delay);
      job.timer.unref();
      return;
    }
    job.timer = undefined;
    this.pump(id).request(id, () => this.uploadStep(id, job));
  }

  private async uploadStep(id: SessionId, job: Replication): Promise<boolean> {
    if (this.jobs.get(id) !== job || job.paused || job.terminal) return false;
    try {
      let more = false;
      await this.serialize(
        id,
        async (owner) => {
          const parent = this.parents.get(id);
          if (parent && !this.job(parent).headerConfirmed) {
            this.schedule(parent);
            return;
          }
          await this.captureLocal(id, owner);
          await this.synchronize(id, owner);
          more = owner.confirmed < owner.requestedNext;
        },
        job,
      );
      job.failures = 0;
      return more;
    } catch (error) {
      rejectReplication(job, error);
      if (error instanceof TranscriptUploadError)
        this.rejectLease(id, job, error);
      job.failures++;
      this.schedule(
        id,
        Math.min(30_000, 100 * 2 ** Math.min(job.failures - 1, 9)) +
          Math.random() * 100,
      );
      return false;
    }
  }

  private async synchronize(id: SessionId, owner: Replication): Promise<void> {
    this.assertCurrent(id, owner);
    if (owner.captureError)
      throw new TranscriptUploadError(owner.captureError.message, {
        cause: owner.captureError,
      });
    const local = this.ctx.transcriptStore.view(id);
    if (!local) {
      if (
        [...this.parents].some(
          ([child, parent]) =>
            parent === id &&
            this.ctx.transcriptStore.eventCount(child) !== undefined,
        )
      )
        throw new TranscriptUploadError("Parent transcript is missing");
      for (const waiter of owner.waiters) waiter.resolve();
      owner.requestedNext = owner.confirmed;
      return;
    }
    updateReady(owner, local);
    owner.requestedNext = Math.max(owner.requestedNext, owner.readyNext);
    for (const waiter of owner.waiters) waiter.through ??= owner.readyNext;
    const through = Math.min(owner.requestedNext, owner.readyNext);
    if (!owner.validated) {
      await this.withRemote(id, owner, async (remote) => {
        if (remote)
          throw new TranscriptUploadError(
            "Transcript must be restored before uploading",
          );
        owner.confirmed = 0;
        owner.headerConfirmed = false;
        owner.validated = true;
      });
    }
    if (local.header.isSeeded)
      throw new TranscriptUploadError("Forked sessions are not enabled");
    if (!owner.headerConfirmed) {
      await this.persistRecords(id, [local.header], owner);
      owner.headerConfirmed = true;
      for (const [child, parent] of this.parents)
        if (parent === id) this.schedule(child);
      settleReplication(owner);
      this.capacityChanged(id);
      return;
    }
    const events = transcriptBatch(local.events, owner.confirmed, through);
    if (events.length) {
      await this.persistRecords(id, events, owner);
      owner.confirmed += events.length;
    }
    settleReplication(owner);
    this.capacityChanged(id);
  }

  private async persistRecords(
    id: SessionId,
    records: readonly DshLogData[],
    owner: Replication,
  ): Promise<void> {
    this.assertCurrent(id, owner);
    const parent = this.parents.get(id);
    const diagnostics = records.flatMap((data) => {
      const message =
        "seq" in data && data.type === "tool/result"
          ? data.data.message
          : undefined;
      const text = message?.content
        .filter((block) => block.type === "text")
        .map((block) => block.text)
        .join("\n");
      return message && text !== undefined
        ? [
            {
              session_ref: createHash("sha256")
                .update(parent ?? id)
                .digest("hex"),
              session_thread_ref: createHash("sha256").update(id).digest("hex"),
              call_ref: createHash("sha256")
                .update(message.toolCallId)
                .digest("hex"),
              text_utf16_units: text.length,
              has_nul: text.includes(String.fromCharCode(0)),
            },
          ]
        : [];
    });
    for (const diagnostic of diagnostics)
      workerDiagnostic("info", "dsr_tool_result_durability", {
        ...diagnostic,
        phase: "start",
      });
    owner.inFlight = true;
    try {
      await this.config.remote.write(
        parent ?? id,
        id,
        records,
        owner.abort.signal,
      );
      this.assertCurrent(id, owner);
    } catch (error) {
      if (this.config.remote.fenced(error))
        this.rejectLease(
          id,
          owner,
          error instanceof Error
            ? error
            : new Error("Transcript owner rejected"),
        );
      throw error;
    } finally {
      owner.inFlight = false;
    }
    for (const diagnostic of diagnostics)
      workerDiagnostic("info", "dsr_tool_result_durability", {
        ...diagnostic,
        phase: "confirmed",
      });
  }

  private withRemote<T>(
    id: SessionId,
    owner: Replication,
    consume: (record: DownloadedTranscript | undefined) => Promise<T>,
    snapshot?: RemoteTranscriptSnapshot | null,
  ): Promise<T> {
    const parent = this.parents.get(id);
    return withDownloadedTranscript(
      id,
      this.config.remote.read(parent ?? id, id, owner.abort.signal, snapshot),
      () => this.assertCurrent(id, owner),
      consume,
    );
  }
}
