import { createHash } from "node:crypto";
import { workerDiagnostic } from "../observability/diagnostics.ts";
import type { DshLogData } from "../runtime/transcript-types.ts";
import { Context, Service } from "@deepseek-ai/cordis";
import {
  type SessionPersistenceRevision,
  type SessionHandle,
} from "@deepseek-ai/dsh-session-persistence";
import {
  SessionId,
  SessionLogOffset,
  type SessionEvent,
  type SessionHeader,
  type Session,
} from "@deepseek-ai/dsh-session";
import {
  withDownloadedTranscript,
  type DownloadedTranscript,
} from "./transcript-download.ts";
import { sameJson } from "../runtime/json-equality.ts";
export class TranscriptBatchError extends Error {
  constructor(
    readonly failure: unknown,
    readonly fence: Error,
  ) {
    super("Transcript batch rejected");
  }
}

export interface RemoteTranscriptStore {
  read(
    rootSessionId: string,
    sessionId: string,
  ): AsyncIterable<readonly DshLogData[]>;
  write(
    rootSessionId: string,
    sessionId: string,
    records: readonly DshLogData[],
  ): Promise<void>;
  fenced(error: unknown): boolean;
}

interface Stored {
  header: SessionHeader;
  events: SessionEvent[];
  inherited: number;
}

interface Replication {
  chain: Promise<void>;
  timer?: ReturnType<typeof setTimeout>;
  failures: number;
  terminal?: Error;
  paused?: boolean;
  acquired?: boolean;
  local?: Stored;
  live?: Session;
  revision?: SessionPersistenceRevision;
  validated?: boolean;
  confirmed: number;
  headerConfirmed: boolean;
  captureError?: Error;
  flushing?: boolean;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    sessionReplication: SessionReplication;
  }
}

export class SessionReplication extends Service {
  static inject = ["sessionPersistence", "sessions"];
  private readonly parents = new Map<SessionId, SessionId>();
  private readonly jobs = new Map<SessionId, Replication>();
  private stopped = false;
  private readonly forgotten = new Set<SessionId>();

  constructor(
    ctx: Context,
    readonly config: { remote: RemoteTranscriptStore },
    serviceName = "sessionReplication",
  ) {
    super(ctx, serviceName);
    ctx.on("agent/created", ({ agent }) => {
      agent.ctx.effect(() => async () => {
        const owner = this.jobs.get(agent.id);
        if (!owner || owner.live !== agent.session) return;
        const releasing = owner.chain.then(async () => {
          await this.flushLocal(owner);
          if (owner.live === agent.session) {
            owner.live = undefined;
            owner.revision = undefined;
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
      owner.local = {
        header: session.header,
        inherited: session.inheritedEventCount,
        events: [...session.snapshotEvents()],
      };
    });
    ctx.on("session/event", (session, event) => {
      const owner = this.jobs.get(session.id);
      if (owner?.live === session && owner.local) {
        if (event.seq === owner.local.events.length)
          owner.local.events.push(event);
        else if (!sameJson(owner.local.events[event.seq], event))
          owner.captureError = new Error("Conflicting live transcript event");
      }
      this.schedule(session.id);
    });
    ctx.on("session/disposed", (session) => {
      const owner = this.jobs.get(session.id);
      if (owner?.live === session) {
        owner.live = undefined;
        owner.revision = undefined;
      }
    });
    ctx.on("session/flush", (session) => {
      if (!this.jobs.get(session.id)?.flushing) this.schedule(session.id);
    });
    ctx.effect(() => async () => {
      this.stopped = true;
      for (const job of this.jobs.values())
        if (job.timer) clearTimeout(job.timer);
      await Promise.all([...this.jobs.values()].map((job) => job.chain));
    });
  }

  bindThread(id: string, parent: string): void {
    const previous = this.parents.get(SessionId(id));
    if (previous && previous !== parent)
      throw new Error("Thread belongs to another session");
    this.parents.set(SessionId(id), SessionId(parent));
  }

  async ensure(value: string): Promise<boolean> {
    const id = SessionId(value);
    this.forgotten.delete(id);
    let hydrated = false;
    await this.serialize(id, async (owner) => {
      if (owner.acquired) return;
      const snapshot = await this.ctx.sessionPersistence.stat(id);
      if (snapshot?.formatStatus === "migration-required")
        throw new Error("Local transcript format requires explicit migration");
      await this.withRemote(id, owner, async (remote) => {
        if (!snapshot) {
          if (remote) {
            await this.hydrate(id, remote, owner);
            hydrated = true;
          }
        } else {
          const handle = await this.ctx.sessionPersistence.open(id, "write");
          try {
            const local = await this.readLocal(handle);
            if (remote) {
              await this.reconcile(id, owner, local, remote, handle);
              hydrated = remote.count > local.events.length;
            }
            owner.local = hydrated ? undefined : local;
          } finally {
            await handle.close();
          }
        }
        this.assertCurrent(id, owner);
        owner.confirmed = remote?.count ?? 0;
        owner.headerConfirmed = !!remote;
        owner.validated = true;
        owner.acquired = true;
      });
    });
    return hydrated;
  }

  private async readLocal(handle: SessionHandle): Promise<Stored> {
    const events = [...(await handle.read()).events];
    return {
      header: handle.header,
      inherited: handle.inheritedEventCount,
      events,
    };
  }

  private async reconcile(
    id: SessionId,
    owner: Replication,
    local: Stored,
    remote: DownloadedTranscript,
    handle?: SessionHandle,
  ): Promise<void> {
    if (
      !sameJson(local.header, remote.header) ||
      local.inherited !== 0 ||
      (!handle && remote.count > local.events.length)
    )
      throw new Error("Local and remote log histories diverged");
    for await (const batch of remote.batches()) {
      this.assertCurrent(id, owner);
      const overlap = batch.filter((event) => event.seq < local.events.length);
      if (overlap.some((event) => !sameJson(local.events[event.seq], event)))
        throw new Error("Local and remote log histories diverged");
      const tail = batch.filter((event) => event.seq >= local.events.length);
      if (tail.length && handle) await handle.append(tail);
    }
    if (handle) await handle.flush();
  }

  private async hydrate(
    id: SessionId,
    record: DownloadedTranscript,
    owner: Replication,
  ): Promise<void> {
    this.assertCurrent(id, owner);
    const handle = await this.ctx.sessionPersistence.create(record.header, {
      inheritedEventCount: SessionLogOffset(0),
    });
    try {
      for await (const batch of record.batches()) {
        this.assertCurrent(id, owner);
        await handle.append(batch);
      }
      await handle.flush();
    } finally {
      await handle.close();
    }
  }

  private async flushLocal(owner: Replication): Promise<void> {
    if (!owner.live) return;
    owner.flushing = true;
    try {
      await this.ctx.sessions.flush(owner.live);
    } finally {
      owner.flushing = false;
    }
  }

  async flush(value: string): Promise<void> {
    const id = SessionId(value);
    if (this.stopped) throw new Error("Session replication is stopped");
    this.forgotten.delete(id);
    this.assertCurrent(id, this.job(id));
    const ids = [
      id,
      ...[...this.parents]
        .filter(([, parent]) => parent === id)
        .map(([child]) => child),
    ];
    const targets = new Map(
      ids.map((key) => [
        key,
        this.jobs.get(key)?.live
          ? this.jobs.get(key)?.local?.events.length
          : undefined,
      ]),
    );
    for (const key of ids)
      await this.serialize(key, async (owner) => {
        await this.flushLocal(owner);
        await this.synchronize(key, owner, targets.get(key));
      });
  }

  async drain(value: string): Promise<void> {
    const id = SessionId(value);
    const jobs = [...this.jobs]
      .filter(([child]) => child === id || this.parents.get(child) === id)
      .map(([, job]) => job);
    for (const job of jobs) {
      job.paused = true;
      if (job.timer) clearTimeout(job.timer);
      job.timer = undefined;
    }
    await Promise.all(jobs.map((job) => job.chain));
  }

  forget(value: string): void {
    const id = SessionId(value);
    this.forgetOne(id);
    for (const [child, parent] of this.parents) {
      if (parent !== id) continue;
      this.forgetOne(child);
      this.parents.delete(child);
    }
  }

  private forgetOne(id: SessionId): void {
    const job = this.jobs.get(id);
    if (job?.timer) clearTimeout(job.timer);
    this.jobs.delete(id);
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
        headerConfirmed: false,
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
      if (job.timer) clearTimeout(job.timer);
      job.timer = undefined;
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

  private schedule(id: SessionId, delay = 0): void {
    if (this.stopped || this.forgotten.has(id)) return;
    const job = this.job(id);
    if (job.timer || job.terminal || job.paused) return;
    job.timer = setTimeout(() => {
      job.timer = undefined;
      void this.serialize(
        id,
        async (owner) => {
          const through = owner.live ? owner.local?.events.length : undefined;
          await this.flushLocal(owner);
          await this.synchronize(id, owner, through);
        },
        job,
      ).then(
        () => {
          job.failures = 0;
        },
        () => {
          job.failures++;
          if (
            !this.stopped &&
            !job.paused &&
            !job.terminal &&
            this.jobs.get(id) === job
          )
            this.schedule(
              id,
              Math.min(30_000, 100 * 2 ** Math.min(job.failures - 1, 9)),
            );
        },
      );
    }, delay);
    job.timer.unref();
  }

  private async localRecord(
    id: SessionId,
    owner: Replication,
  ): Promise<Stored | undefined> {
    if (owner.live && owner.local) return owner.local;
    const snapshot = await this.ctx.sessionPersistence.stat(id);
    if (!snapshot) return undefined;
    if (owner.local && owner.revision === snapshot.revision) return owner.local;
    const handle = await this.ctx.sessionPersistence.open(id, "read");
    try {
      const local = owner.local;
      const events = (await handle.read(local?.events.length ?? 0)).events;
      if (owner.local !== local || owner.live) return owner.local;
      if (owner.local)
        for (const event of events) owner.local.events.push(event);
      else
        owner.local = {
          header: handle.header,
          inherited: handle.inheritedEventCount,
          events: [...events],
        };
      owner.revision = snapshot.revision;
      return owner.local;
    } finally {
      await handle.close();
    }
  }

  private async synchronize(
    id: SessionId,
    owner: Replication,
    target?: number,
  ): Promise<void> {
    this.assertCurrent(id, owner);
    if (owner.captureError) throw owner.captureError;
    const local = await this.localRecord(id, owner);
    if (!local) return;
    const through = target ?? local.events.length;
    if (!owner.validated) {
      await this.withRemote(id, owner, async (remote) => {
        if (remote) await this.reconcile(id, owner, local, remote);
        owner.confirmed = remote?.count ?? 0;
        owner.headerConfirmed = !!remote;
        owner.validated = true;
      });
    }
    if (local.header.isSeeded || local.inherited)
      throw new Error("Forked sessions are not enabled");
    if (!owner.headerConfirmed) {
      await this.persistRecords(id, [local.header], owner);
      owner.headerConfirmed = true;
    }
    const events = local.events.slice(owner.confirmed, through);
    if (events.length) {
      await this.persistRecords(id, events, owner);
      owner.confirmed += events.length;
    }
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
    try {
      await this.config.remote.write(parent ?? id, id, records);
    } catch (error) {
      if (error instanceof TranscriptBatchError) {
        this.rejectLease(id, owner, error.fence);
        throw error.failure;
      }
      if (this.config.remote.fenced(error))
        this.rejectLease(
          id,
          owner,
          error instanceof Error
            ? error
            : new Error("Transcript owner rejected"),
        );
      throw error;
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
  ): Promise<T> {
    const parent = this.parents.get(id);
    return withDownloadedTranscript(
      id,
      this.config.remote.read(parent ?? id, id),
      () => this.assertCurrent(id, owner),
      consume,
    );
  }
}
