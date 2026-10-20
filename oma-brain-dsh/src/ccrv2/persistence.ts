import type { Context } from "@deepseek-ai/cordis";
import {
  SessionPersistence,
  SessionPersistenceRevision,
  SessionAlreadyExistsError,
  SessionAlreadyOwnedError,
  SessionPersistenceNotFoundError,
  SessionHandleClosedError,
  SessionReadOnlyError,
  assertContiguous,
  assertVersion,
  assertStoredId,
  materializeCreateHeader,
  materializeAppendBatch,
  validateStoredEvents,
  type SessionHandle,
  type SessionAccess,
  type SessionPersistenceSnapshot,
  type SessionPersistenceCreateOptions,
} from "@deepseek-ai/dsh-session-persistence";
import {
  SessionLogOffset,
  type SessionEvent,
  type SessionHeader,
  SessionId,
} from "@deepseek-ai/dsh-session";
import type { CcrBackend } from "./contracts.ts";

interface Stored {
  header: SessionHeader;
  events: SessionEvent[];
  inherited: number;
  durable: boolean;
}

class RemoteHandle implements SessionHandle {
  readonly inheritedEventCount;
  private closed = false;
  private chain: Promise<unknown> = Promise.resolve();
  private buffered: SessionEvent[] = [];
  private closing?: Promise<void>;
  constructor(
    readonly store: CcrPersistence,
    readonly id: SessionId,
    readonly access: SessionAccess,
    readonly record: Stored,
  ) {
    this.inheritedEventCount = SessionLogOffset(record.inherited);
  }
  get header() {
    return this.record.header;
  }
  check(write = false) {
    if (this.closed) throw new SessionHandleClosedError(this.id, "access");
    if (write && this.access !== "write")
      throw new SessionReadOnlyError(this.id, "write");
  }
  enqueue(event: SessionEvent) {
    this.check(true);
    this.buffered.push(structuredClone(event));
  }
  async read(
    offset = 0,
    length = Number.MAX_SAFE_INTEGER,
    options?: { signal?: AbortSignal },
  ) {
    this.check();
    options?.signal?.throwIfAborted();
    if (
      !Number.isSafeInteger(offset) ||
      offset < 0 ||
      !Number.isSafeInteger(length) ||
      length < 0
    )
      throw new Error("Invalid log slice");
    return {
      eventState: "detached" as const,
      events: structuredClone(
        this.record.events.slice(offset, offset + length),
      ),
    };
  }
  serialize(job: () => Promise<void>) {
    const result = this.chain.then(job);
    this.chain = result.catch(() => {});
    return result;
  }
  append(events: readonly SessionEvent[], options?: { signal?: AbortSignal }) {
    this.check(true);
    const batch = materializeAppendBatch(events);
    return this.serialize(async () => {
      options?.signal?.throwIfAborted();
      assertContiguous(this.id, batch, this.record.events.length);
      if (!batch.length) return;
      await this.store.persist(this.id, {
        header: this.header,
        inherited: this.record.inherited,
        events: batch,
      });
      this.record.durable = true;
      this.record.events.push(...structuredClone(batch));
    });
  }
  flush(options?: { signal?: AbortSignal }) {
    this.check(true);
    return this.serialize(async () => {
      options?.signal?.throwIfAborted();
      const batch = this.buffered.slice();
      assertContiguous(this.id, batch, this.record.events.length);
      if (!batch.length && this.record.durable) return;
      await this.store.persist(this.id, {
        header: this.header,
        inherited: this.record.inherited,
        events: batch,
      });
      this.record.durable = true;
      this.record.events.push(...structuredClone(batch));
      this.buffered.splice(0, batch.length);
    });
  }
  close() {
    return (this.closing ??= (async () => {
      try {
        if (this.access === "write") await this.flush();
      } finally {
        this.closed = true;
        if (this.store.writers.get(this.id) === this)
          this.store.writers.delete(this.id);
      }
    })());
  }
  [Symbol.asyncDispose]() {
    return this.close();
  }
}

export class CcrPersistence extends SessionPersistence {
  readonly writers = new Map<SessionId, RemoteHandle>();
  private readonly records = new Map<SessionId, Stored>();
  readonly backend: CcrBackend;
  private readonly parents = new Map<SessionId, SessionId>();
  bindThread(id: string, parent: string): void {
    const previous = this.parents.get(SessionId(id));
    if (previous && previous !== parent) throw new Error("Thread belongs to another session");
    this.parents.set(SessionId(id), SessionId(parent));
  }
  persist(id: SessionId, data: unknown): Promise<void> {
    const parent = this.parents.get(id);
    return this.backend.persist(parent ?? id, "dsh_log", data, parent ? id : undefined);
  }
  constructor(ctx: Context, config: { backend: CcrBackend }) {
    super(ctx);
    this.backend = config.backend;
    ctx.on("session/event", (session, event) => {
      this.writers.get(session.id)?.enqueue(event);
    });
    ctx.on("session/flush", (session) => this.writers.get(session.id)?.flush());
    ctx.effect(() => async () => {
      await this.flush();
    });
  }
  async load(id: SessionId): Promise<Stored | undefined> {
    if (this.records.has(id)) return this.records.get(id);
    const parent = this.parents.get(id);
    // Older versions stored child logs in the foreground stream. Read that
    // prefix first, then the separately scoped child stream used by CCRv2.
    const payloads = await this.backend.privateEvents(parent ?? id);
    if (parent) payloads.push(...await this.backend.privateEvents(parent, { subagents: true }));
    let record: Stored | undefined;
    for (const payload of payloads) {
      if (payload.subtype !== "dsh_log") continue;
      if (payload.schema_version !== 1)
        throw new Error("Unsupported dsh log envelope");
      const data = payload.data;
      if (data.header?.id !== id) continue;
      assertStoredId(id, data.header);
      assertVersion(data.header);
      record ??= {
        header: materializeCreateHeader(data.header),
        inherited: data.inherited,
        events: [],
        durable: true,
      };
      if (JSON.stringify(record.header) !== JSON.stringify(data.header))
        throw new Error("Log header changed");
      if (!Array.isArray(data.events)) throw new Error("Invalid dsh log batch");
      const batch = validateStoredEvents(
        record.header,
        structuredClone(data.events),
      );
      for (const event of batch) {
        if (event.seq < record.events.length) {
          if (
            JSON.stringify(record.events[event.seq]) !== JSON.stringify(event)
          )
            throw new Error("Conflicting dsh log event");
        } else {
          assertContiguous(id, [event], record.events.length);
          record.events.push(event);
        }
      }
    }
    if (record) this.records.set(id, record);
    return record;
  }
  async create(
    header: SessionHeader,
    options?: SessionPersistenceCreateOptions,
  ): Promise<SessionHandle> {
    options?.signal?.throwIfAborted();
    assertVersion(header);
    if (this.writers.has(header.id))
      throw new SessionAlreadyOwnedError(header.id);
    if (await this.load(header.id))
      throw new SessionAlreadyExistsError(header.id);
    if (header.isSeeded || options?.inheritedEventCount)
      throw new Error("Forked sessions are not enabled");
    const record = {
      header: materializeCreateHeader(header),
      inherited: 0,
      events: [],
      durable: false,
    };
    this.records.set(header.id, record);
    const handle = new RemoteHandle(this, header.id, "write", record);
    this.writers.set(header.id, handle);
    try {
      await handle.flush();
    } catch (error) {
      this.writers.delete(header.id);
      this.records.delete(header.id);
      throw error;
    }
    return handle;
  }
  async open(
    id: SessionId,
    access: SessionAccess,
    options?: { signal?: AbortSignal },
  ): Promise<SessionHandle> {
    options?.signal?.throwIfAborted();
    if (access === "write" && this.writers.has(id))
      throw new SessionAlreadyOwnedError(id);
    const record = await this.load(id);
    if (!record) throw new SessionPersistenceNotFoundError(id);
    const handle = new RemoteHandle(this, id, access, record);
    if (access === "write") this.writers.set(id, handle);
    return handle;
  }
  async flush() {
    await Promise.all([...this.writers.values()].map((h) => h.flush()));
  }
  async stat(id: SessionId): Promise<SessionPersistenceSnapshot | undefined> {
    const record = await this.load(id);
    return record
      ? {
          header: structuredClone(record.header),
          revision: SessionPersistenceRevision(String(record.events.length)),
          eventCount: record.events.length,
        }
      : undefined;
  }
  async list(): Promise<SessionPersistenceSnapshot[]> {
    const results = await Promise.all(
      [...this.backend.sessionIds()].map((id) => this.stat(id as SessionId)),
    );
    return results.filter((r): r is SessionPersistenceSnapshot => !!r);
  }
  forget(id: SessionId): void {
    if (this.writers.has(id))
      throw new Error("Cannot forget an active session");
    this.records.delete(id);
    for (const [child, parent] of this.parents) {
      if (parent !== id) continue;
      if (this.writers.has(child)) throw new Error("Cannot forget an active thread");
      this.records.delete(child);
      this.parents.delete(child);
    }
  }
}
