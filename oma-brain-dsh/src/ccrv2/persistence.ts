import { createHash } from "node:crypto";
import { mkdir, mkdtemp, open, rename, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import JsonlPersistence from "@deepseek-ai/dsh-session-persistence-jsonl";
import { assertPersistenceFormat } from "./recovery.ts";
import { workerDiagnostic } from "./diagnostics.ts";
import type { DshLogData } from "./persistence-contracts.ts";
import { Context, Service } from "@deepseek-ai/cordis";
import {
  assertContiguous,
  assertVersion,
  assertStoredId,
  materializeCreateHeader,
  validateStoredEvents,
  type SessionPersistenceRevision,
  type SessionHandle,
} from "@deepseek-ai/dsh-session-persistence";
import { SessionId, SessionLogOffset, type SessionEvent, type SessionHeader, type Session } from "@deepseek-ai/dsh-session";
import { CcrHttpError } from "./client.ts";
import { sameJson } from "./json-equality.ts";
import type { CcrBackend } from "./contracts.ts";

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
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    ccrReplication: CcrReplication;
  }
}

export class CcrReplication extends Service {
  static inject = ["sessionPersistence"];
  private readonly parents = new Map<SessionId, SessionId>();
  private readonly records = new Map<SessionId, Stored | undefined>();
  private readonly jobs = new Map<SessionId, Replication>();
  private stopped = false;
  private readonly forgotten = new Set<SessionId>();

  constructor(ctx: Context, readonly config: { backend: CcrBackend }) {
    super(ctx, "ccrReplication");
    ctx.on("session/created", session => {
      if (this.stopped || this.forgotten.has(session.id)) return;
      const previous = this.jobs.get(session.id);
      if (previous?.paused || previous?.terminal || previous?.live === session) return;
      if (previous?.live) {
        previous.captureError = new Error("Cannot replace an active transcript session");
        return;
      }
      const owner = this.job(session.id);
      owner.live = session;
      owner.local = { header: session.header, inherited: session.inheritedEventCount, events: [...session.snapshotEvents()] };
    });
    ctx.on("session/event", (session, event) => {
      const owner = this.jobs.get(session.id);
      if (owner?.live === session && owner.local) {
        if (event.seq === owner.local.events.length) owner.local.events.push(event);
        else if (!sameJson(owner.local.events[event.seq], event)) owner.captureError = new Error("Conflicting live transcript event");
      }
      this.schedule(session.id);
    });
    ctx.on("session/disposed", session => {
      const owner = this.jobs.get(session.id);
      if (owner?.live === session) {
        owner.live = undefined;
        owner.revision = undefined;
      }
    });
    ctx.on("session/flush", session => { this.schedule(session.id); });
    ctx.effect(() => async () => {
      this.stopped = true;
      for (const job of this.jobs.values()) if (job.timer) clearTimeout(job.timer);
      await Promise.all([...this.jobs.values()].map(job => job.chain));
    });
  }

  bindThread(id: string, parent: string): void {
    const previous = this.parents.get(SessionId(id));
    if (previous && previous !== parent) throw new Error("Thread belongs to another session");
    this.parents.set(SessionId(id), SessionId(parent));
  }

  async ensure(value: string): Promise<boolean> {
    const id = SessionId(value);
    this.forgotten.delete(id);
    let hydrated = false;
    await this.serialize(id, async owner => {
      if (owner.acquired) return;
      const snapshot = await this.ctx.sessionPersistence.stat(id);
      if (snapshot?.formatStatus === "migration-required") throw new Error("Local transcript format requires explicit migration");
      const remote = await this.load(id, owner);
      this.assertCurrent(id, owner);
      if (!snapshot) {
        if (remote) {
          await this.hydrate(id, remote, owner);
          hydrated = true;
        }
      } else {
        const handle = await this.ctx.sessionPersistence.open(id, "write");
        try {
          const local = await this.readLocal(handle);
          if (remote && !this.isPrefix(local, remote) && !this.isPrefix(remote, local)) {
            await this.quarantine(id, owner);
            await handle.close();
            await this.hydrate(id, remote, owner);
            hydrated = true;
          } else {
            if (remote && remote.events.length > local.events.length) {
              await handle.append(remote.events.slice(local.events.length));
              await handle.flush();
              for (const event of remote.events.slice(local.events.length)) local.events.push(event);
              hydrated = true;
            }
            owner.local = local;
          }
        } finally { await handle.close(); }
      }
      this.assertCurrent(id, owner);
      owner.confirmed = remote?.events.length ?? 0;
      owner.headerConfirmed = !!remote;
      owner.validated = true;
      owner.acquired = true;
      this.records.delete(id);
    });
    return hydrated;
  }

  private async readLocal(handle: SessionHandle): Promise<Stored> {
    return { header: handle.header, inherited: handle.inheritedEventCount, events: [...(await handle.read()).events] };
  }

  private isPrefix(prefix: Stored, log: Stored): boolean {
    return sameJson(prefix.header, log.header) && prefix.inherited === log.inherited
      && prefix.events.length <= log.events.length
      && prefix.events.every((event, index) => sameJson(event, log.events[index]));
  }

  private async hydrate(id: SessionId, record: Stored, owner: Replication): Promise<void> {
    this.assertCurrent(id, owner);
    const handle = await this.ctx.sessionPersistence.create(record.header, { inheritedEventCount: SessionLogOffset(record.inherited) });
    try {
      await handle.append(record.events);
      await handle.flush();
      owner.local = { header: handle.header, inherited: record.inherited, events: [...record.events] };
    } finally { await handle.close(); }
  }

  private async quarantine(id: SessionId, owner: Replication): Promise<void> {
    this.assertCurrent(id, owner);
    const persistence = this.ctx.sessionPersistence;
    if (!(persistence instanceof JsonlPersistence)) throw new Error("Transcript quarantine requires native JSONL persistence");
    const path = await persistence.resolveCurrentLog(id);
    if (!path) throw new Error("Cannot locate divergent transcript");
    const source = dirname(path);
    const quarantine = `${resolve(persistence.config.root)}.quarantine`;
    await mkdir(quarantine, { recursive: true, mode: 0o700 });
    this.assertCurrent(id, owner);
    const destination = join(quarantine, `${encodeURIComponent(id)}-${crypto.randomUUID()}`);
    await rename(source, destination);
    if (process.platform !== "win32") {
      for (const directory of [dirname(source), quarantine, dirname(quarantine)]) {
        const handle = await open(directory, "r");
        try { await handle.sync(); } finally { await handle.close(); }
      }
    }
  }

  async flush(value: string): Promise<void> {
    const id = SessionId(value);
    if (this.stopped) throw new Error("CCRv2 replication is stopped");
    this.forgotten.delete(id);
    this.assertCurrent(id, this.job(id));
    const ids = [id, ...[...this.parents].filter(([, parent]) => parent === id).map(([child]) => child)];
    const targets = new Map(ids.map(key => [key, this.jobs.get(key)?.live ? this.jobs.get(key)?.local?.events.length : undefined]));
    await this.ctx.sessionPersistence.flush();
    for (const key of ids) await this.serialize(key, owner => this.synchronize(key, owner, targets.get(key)));
  }

  async drain(value: string): Promise<void> {
    const id = SessionId(value);
    const jobs = [...this.jobs].filter(([child]) => child === id || this.parents.get(child) === id).map(([, job]) => job);
    for (const job of jobs) {
      job.paused = true;
      if (job.timer) clearTimeout(job.timer);
      job.timer = undefined;
    }
    await Promise.all(jobs.map(job => job.chain));
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
    this.records.delete(id);
    this.forgotten.add(id);
  }

  private job(id: SessionId): Replication {
    let job = this.jobs.get(id);
    if (!job) {
      const parent = this.parents.get(id);
      const inherited = parent ? this.jobs.get(parent) : undefined;
      job = { chain: Promise.resolve(), failures: 0, terminal: inherited?.terminal, paused: inherited?.paused, confirmed: 0, headerConfirmed: false };
      this.jobs.set(id, job);
    }
    return job;
  }

  private assertCurrent(id: SessionId, job: Replication): void {
    if (job.terminal) throw job.terminal;
    if (this.stopped || job.paused || this.jobs.get(id) !== job)
      throw new Error("CCRv2 replication is stopped");
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

  private serialize(id: SessionId, operation: (job: Replication) => Promise<void>, owner = this.job(id)): Promise<void> {
    const result = owner.chain.then(async () => {
      this.assertCurrent(id, owner);
      try {
        await operation(owner);
      } catch (error) {
        if (error instanceof CcrHttpError && ([409, 410].includes(error.status) || error.tokenExpired || error.authExhausted)) {
          this.rejectLease(id, owner, error);
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
      void this.serialize(id, async owner => {
        const through = owner.live ? owner.local?.events.length : undefined;
        await this.ctx.sessionPersistence.flush();
        await this.synchronize(id, owner, through);
      }, job).then(() => {
        job.failures = 0;
      }, () => {
        job.failures++;
        if (!this.stopped && !job.paused && !job.terminal && this.jobs.get(id) === job) this.schedule(id, Math.min(30_000, 100 * 2 ** Math.min(job.failures - 1, 9)));
      });
    }, delay);
    job.timer.unref();
  }

  private async localRecord(id: SessionId, owner: Replication): Promise<Stored | undefined> {
    if (owner.live && owner.local) return owner.local;
    const snapshot = await this.ctx.sessionPersistence.stat(id);
    if (!snapshot) return undefined;
    if (owner.local && owner.revision === snapshot.revision) return owner.local;
    const handle = await this.ctx.sessionPersistence.open(id, "read");
    try {
      const events = (await handle.read(owner.local?.events.length ?? 0)).events;
      if (owner.local) for (const event of events) owner.local.events.push(event);
      else owner.local = { header: handle.header, inherited: handle.inheritedEventCount, events: [...events] };
      owner.revision = snapshot.revision;
      return owner.local;
    } finally { await handle.close(); }
  }

  private async synchronize(id: SessionId, owner: Replication, target?: number): Promise<void> {
    this.assertCurrent(id, owner);
    if (owner.captureError) throw owner.captureError;
    const local = await this.localRecord(id, owner);
    if (!local) return;
    const through = target ?? local.events.length;
    if (!owner.validated) {
      const remote = await this.load(id, owner);
      if (remote && !this.isPrefix(remote, local)) throw new Error("Local and remote log histories diverged");
      owner.confirmed = remote?.events.length ?? 0;
      owner.headerConfirmed = !!remote;
      owner.validated = true;
      this.records.delete(id);
    }
    if (local.header.isSeeded || local.inherited) throw new Error("Forked sessions are not enabled");
    if (!owner.headerConfirmed) {
      await this.persistRecord(id, local.header, owner);
      owner.headerConfirmed = true;
    }
    const events = local.events.slice(owner.confirmed, through);
    const results = await Promise.allSettled(events.map(event => this.persistRecord(id, event, owner)));
    for (const result of results) {
      if (result.status === "rejected") throw result.reason;
      owner.confirmed++;
    }
  }

  private async persistRecord(id: SessionId, data: DshLogData, owner: Replication): Promise<void> {
    this.assertCurrent(id, owner);
    const parent = this.parents.get(id);
    const message = "seq" in data && data.type === "tool/result" ? data.data.message : undefined;
    const text = message?.content.filter(block => block.type === "text").map(block => block.text).join("\n");
    const diagnostic = message && text !== undefined ? {
      session_ref: createHash("sha256").update(parent ?? id).digest("hex"),
      session_thread_ref: createHash("sha256").update(id).digest("hex"),
      call_ref: createHash("sha256").update(message.toolCallId).digest("hex"),
      text_utf16_units: text.length, has_nul: text.includes(String.fromCharCode(0)),
    } : undefined;
    if (diagnostic) workerDiagnostic("info", "ccrv2_tool_result_durability", { ...diagnostic, phase: "start" });
    try { await this.config.backend.persist(parent ?? id, "dsh_log", data, parent ? id : undefined); }
    catch (error) {
      if (error instanceof CcrHttpError && ([409, 410].includes(error.status) || error.tokenExpired || error.authExhausted))
        this.rejectLease(id, owner, error);
      throw error;
    }
    if (diagnostic) workerDiagnostic("info", "ccrv2_tool_result_durability", { ...diagnostic, phase: "confirmed" });
  }

  private async validateRemote(record: Stored): Promise<void> {
    const root = await mkdtemp(join(tmpdir(), "oma-brain-remote-validation-"));
    const ctx = new Context();
    try {
      await ctx.plugin(JsonlPersistence, { root, compression: "none" });
      const handle = await ctx.sessionPersistence.create(record.header);
      try {
        await handle.append(record.events);
        await handle.flush();
      } finally { await handle.close(); }
      const reader = await ctx.sessionPersistence.open(record.header.id, "read");
      try { await reader.read(); } finally { await reader.close(); }
    } finally {
      try { await ctx.fiber.dispose(); }
      finally { await rm(root, { recursive: true, force: true }); }
    }
  }

  private async load(id: SessionId, owner: Replication): Promise<Stored | undefined> {
    if (this.records.has(id)) return this.records.get(id);
    const parent = this.parents.get(id);
    const payloads = await this.config.backend.privateEvents(parent ?? id, { agentId: parent ? id : undefined });
    this.assertCurrent(id, owner);
    let record: Stored | undefined;
    for (const payload of payloads) {
      assertPersistenceFormat(payload, parent ?? id);
      if (payload.type !== "dsh_log") continue;
      const data = payload.data;
      if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("Invalid dsh log record");
      if (!("seq" in data)) {
        assertStoredId(id, data);
        assertVersion(data);
        if (data.isSeeded) throw new Error("Forked sessions are not enabled");
        const header = materializeCreateHeader({ ...data, delegationDepth: data.delegationDepth ?? 0 });
        if (record && !sameJson(record.header, header)) throw new Error("Log header changed");
        record ??= { header, inherited: 0, events: [] };
        continue;
      }
      if (!record) throw new Error("Missing dsh log header");
      const [event] = validateStoredEvents(record.header, [structuredClone(data)]);
      if (event.seq < record.events.length) {
        if (!sameJson(record.events[event.seq], event)) throw new Error("Conflicting dsh log event");
      } else {
        assertContiguous(id, [event], record.events.length);
        record.events.push(event);
      }
    }
    if (record) await this.validateRemote(record);
    this.assertCurrent(id, owner);
    if (this.jobs.get(id) === owner) this.records.set(id, record);
    return record;
  }
}
