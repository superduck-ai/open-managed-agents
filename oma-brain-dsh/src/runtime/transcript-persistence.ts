import { deepFreeze } from "@deepseek-ai/dsh-util-values";
import { Context } from "@deepseek-ai/cordis";
import {
  SessionLogOffset,
  type Session,
  type SessionEvent,
  type SessionHeader,
  type SessionId,
} from "@deepseek-ai/dsh-session";
import SessionPersistence, {
  SessionAlreadyExistsError,
  SessionAlreadyOwnedError,
  SessionOwnershipLostError,
  SessionPersistenceNotFoundError,
  SessionPersistenceRevision,
  assertContiguous,
  materializeAppendBatch,
  type SessionAccess,
  type SessionHandle,
  type SessionPersistenceCreateOptions,
  type SessionPersistenceOpenOptions,
  type SessionPersistenceStatOptions,
  type SessionPersistenceListOptions,
} from "@deepseek-ai/dsh-session-persistence";
import type SessionStore from "@deepseek-ai/dsh-session";
import { TranscriptUploadError } from "./transcript-batch.ts";
import { sameJson } from "./json-equality.ts";
import { transcriptHandle } from "./transcript-handle.ts";
import {
  transcriptHeader,
  validateTranscriptEvents,
} from "./transcript-validation.ts";
import type { DownloadedTranscript } from "./transcript-download.ts";

export interface TranscriptView {
  readonly header: SessionHeader;
  readonly events: readonly SessionEvent[];
}

export interface TranscriptRecord extends TranscriptView {
  events: SessionEvent[];
  session?: Session;
  error?: Error;
  revision: SessionPersistenceRevision;
  prepared?: {
    session: Session;
    projections: SessionStore["messageProjections"];
  };
}

interface ReplicationBinding {
  changed(id: SessionId): void;
  flush(id: SessionId, through: number, signal?: AbortSignal): Promise<void>;
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    transcriptStore: TranscriptPersistence;
  }
}

export default class TranscriptPersistence extends SessionPersistence {
  static inject = ["sessions"];
  private readonly records = new Map<SessionId, TranscriptRecord>();
  private readonly writers = new Map<SessionId, SessionHandle>();
  private readonly forgotten = new WeakSet<Session>();
  private revision = 0;
  private binding?: ReplicationBinding;

  constructor(ctx: Context) {
    super(ctx);
    ctx.provide("transcriptStore", this);
    ctx.on("session/created", (session) => {
      this.captureSession(session);
      this.binding?.changed(session.id);
    });
    ctx.on("session/event", (session, event) => {
      const record = this.records.get(session.id);
      if (record?.session !== session) return;
      try {
        this.append(session.id, record, [event], true);
      } catch (error) {
        record.error = new TranscriptUploadError(
          "Live transcript capture failed",
          { cause: error },
        );
        this.binding?.changed(session.id);
        throw error;
      }
    });
    ctx.on("session/disposed", (session) => {
      const record = this.records.get(session.id);
      if (record?.session === session) record.session = undefined;
    });
    ctx.on("session/flush", async (session) => {
      const record = this.records.get(session.id);
      if (record) await this.confirm(session.id, record);
    });
    ctx.effect(() => () => {
      this.binding = undefined;
      this.records.clear();
      this.writers.clear();
    });
  }

  connect(binding: ReplicationBinding): () => void {
    if (this.binding)
      throw new Error("Transcript replication is already connected");
    this.binding = binding;
    return () => {
      if (this.binding === binding) this.binding = undefined;
    };
  }

  private nextRevision(): SessionPersistenceRevision {
    return SessionPersistenceRevision(String(++this.revision));
  }

  assertRecord(id: SessionId, record: TranscriptRecord): void {
    if (this.records.get(id) !== record)
      throw new SessionOwnershipLostError(id);
    if (record.error) throw record.error;
  }

  owns(
    id: SessionId,
    record: TranscriptRecord,
    handle: SessionHandle,
  ): boolean {
    return this.records.get(id) === record && this.writers.get(id) === handle;
  }

  release(id: SessionId, handle: SessionHandle): void {
    if (this.writers.get(id) === handle) this.writers.delete(id);
  }

  async create(
    header: SessionHeader,
    options?: SessionPersistenceCreateOptions,
  ): Promise<SessionHandle> {
    options?.signal?.throwIfAborted();
    if (this.records.has(header.id))
      throw new SessionAlreadyExistsError(header.id);
    if (options?.inheritedEventCount)
      throw new Error("Forked sessions are not enabled");
    const record = {
      header: transcriptHeader(header),
      events: [],
      revision: this.nextRevision(),
    };
    this.records.set(header.id, record);
    const handle = transcriptHandle(this, header.id, record, "write");
    this.writers.set(header.id, handle);
    return handle;
  }

  async open(
    id: SessionId,
    access: SessionAccess,
    options?: SessionPersistenceOpenOptions,
  ): Promise<SessionHandle> {
    options?.signal?.throwIfAborted();
    const record = this.records.get(id);
    if (!record) throw new SessionPersistenceNotFoundError(id);
    if (access === "write" && this.writers.has(id))
      throw new SessionAlreadyOwnedError(id);
    const handle = transcriptHandle(this, id, record, access);
    if (access === "write") this.writers.set(id, handle);
    return handle;
  }

  append(
    id: SessionId,
    record: TranscriptRecord,
    events: readonly SessionEvent[],
    shared = false,
  ): void {
    this.assertRecord(id, record);
    if (!shared && events.length && this.ctx.sessions.get(id))
      throw new Error(
        "Cannot append transcript directly while a live Session owns it",
      );
    const batch = validateTranscriptEvents(
      record.header,
      shared ? [...events] : [...materializeAppendBatch(events)],
    );
    assertContiguous(id, batch, record.events.length);
    for (const event of batch) record.events.push(deepFreeze(event));
    if (batch.length) {
      record.revision = this.nextRevision();
      record.prepared = undefined;
    }
    this.binding?.changed(id);
  }

  async confirm(
    id: SessionId,
    record: TranscriptRecord,
    signal?: AbortSignal,
  ): Promise<void> {
    this.assertRecord(id, record);
    signal?.throwIfAborted();
    if (!this.binding)
      throw new Error("Transcript replication is not connected");
    await this.binding.flush(id, record.events.length, signal);
    this.assertRecord(id, record);
  }

  async capture(session: Session): Promise<void> {
    this.captureSession(session);
  }

  private captureSession(session: Session): void {
    if (this.forgotten.has(session))
      throw new SessionOwnershipLostError(session.id);
    if (session.inheritedEventCount)
      throw new TranscriptUploadError("Forked sessions are not enabled");
    let record = this.records.get(session.id);
    if (!record) {
      record = {
        header: transcriptHeader(session.header),
        events: [],
        revision: this.nextRevision(),
      };
      this.records.set(session.id, record);
    }
    this.assertRecord(session.id, record);
    if (record.session !== session) {
      const snapshot = session.snapshotEvents();
      if (
        record.session ||
        record.events.length > snapshot.length ||
        !sameJson(record.header, transcriptHeader(session.header)) ||
        !record.events.every(
          (event, index) =>
            event === snapshot[index] || sameJson(event, snapshot[index]),
        )
      )
        throw new TranscriptUploadError(
          "Session history conflicts with transcript",
        );
      record.session = session;
    }
    const events = session.snapshotEvents(
      SessionLogOffset(record.events.length),
    );
    if (events.length) this.append(session.id, record, events, true);
  }

  replace(
    id: SessionId,
    remote: DownloadedTranscript | undefined,
    assertCurrent: () => void,
  ): void {
    if (this.writers.has(id) || this.records.get(id)?.session)
      throw new SessionAlreadyOwnedError(id);
    assertCurrent();
    if (!remote) {
      this.records.delete(id);
      return;
    }
    const session = this.ctx.sessions.prepare(id, {
      seed: remote.events,
      meta: remote.header,
      inheritedEventCount: SessionLogOffset(0),
      eventState: "shared-frozen",
    });
    assertCurrent();
    this.records.set(id, {
      header: remote.header,
      events: remote.events,
      revision: this.nextRevision(),
      prepared: {
        session,
        projections: [...this.ctx.sessions.messageProjections],
      },
    });
  }

  takePrepared(
    id: SessionId,
    options: Parameters<SessionStore["prepare"]>[1],
    projections: SessionStore["messageProjections"],
  ): Session | undefined {
    const record = this.records.get(id);
    const prepared = record?.prepared;
    if (!record || !prepared) return;
    record.prepared = undefined;
    if (
      options?.eventState !== "shared-frozen" ||
      options.inheritedEventCount !== 0 ||
      !sameJson(options.meta, record.header) ||
      options.seed.length !== record.events.length ||
      !options.seed.every((event, index) => event === record.events[index]) ||
      projections.length !== prepared.projections.length ||
      !projections.every(
        (projection, index) => projection === prepared.projections[index],
      )
    )
      return;
    return prepared.session;
  }

  forget(id: SessionId): void {
    const session = this.records.get(id)?.session;
    if (session) this.forgotten.add(session);
    this.records.delete(id);
    this.writers.delete(id);
  }

  view(id: SessionId): TranscriptView | undefined {
    const record = this.records.get(id);
    if (record?.error) throw record.error;
    return record;
  }

  eventCount(id: SessionId): number | undefined {
    return this.records.get(id)?.events.length;
  }

  async flush(): Promise<void> {
    await Promise.all(
      [...this.writers].map(([id]) => this.confirm(id, this.records.get(id)!)),
    );
  }

  async stat(id: SessionId, options?: SessionPersistenceStatOptions) {
    options?.signal?.throwIfAborted();
    const record = this.records.get(id);
    return record
      ? {
          header: record.header,
          eventCount: record.events.length,
          revision: record.revision,
        }
      : undefined;
  }

  async list(options?: SessionPersistenceListOptions) {
    options?.signal?.throwIfAborted();
    return [...this.records.keys()].map((id) => {
      const record = this.records.get(id)!;
      return {
        header: record.header,
        eventCount: record.events.length,
        revision: record.revision,
      };
    });
  }
}
