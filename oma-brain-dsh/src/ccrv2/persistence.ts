import { Context, Service } from "@deepseek-ai/cordis";
import {
  assertContiguous,
  assertVersion,
  assertStoredId,
  materializeCreateHeader,
  validateStoredEvents,
} from "@deepseek-ai/dsh-session-persistence";
import { SessionId, SessionLogOffset, type SessionEvent, type SessionHeader } from "@deepseek-ai/dsh-session";
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
    ctx.on("session/event", session => this.schedule(session.id));
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

  async ensure(value: string): Promise<void> {
    const id = SessionId(value);
    this.forgotten.delete(id);
    await this.serialize(id, async owner => {
      const local = await this.ctx.sessionPersistence.stat(id);
      if (local?.formatStatus === "migration-required") throw new Error("Local transcript format requires explicit migration");
      if (local) return;
      const record = await this.load(id, owner);
      this.assertCurrent(id, owner);
      if (!record) return;
      const handle = await this.ctx.sessionPersistence.create(record.header, {
        inheritedEventCount: SessionLogOffset(record.inherited),
      });
      try {
        await handle.append(record.events);
        await handle.flush();
      } finally {
        await handle.close();
      }
    });
  }

  async flush(value: string): Promise<void> {
    const id = SessionId(value);
    if (this.stopped) throw new Error("CCRv2 replication is stopped");
    this.forgotten.delete(id);
    this.assertCurrent(id, this.job(id));
    await this.ctx.sessionPersistence.flush();
    await this.serialize(id, owner => this.synchronize(id, owner));
    for (const [child, parent] of this.parents) {
      if (parent === id) await this.serialize(child, owner => this.synchronize(child, owner));
    }
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
      job = { chain: Promise.resolve(), failures: 0, terminal: inherited?.terminal, paused: inherited?.paused };
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
        await this.ctx.sessionPersistence.flush();
        await this.synchronize(id, owner);
      }, job).then(() => {
        job.failures = 0;
      }, () => {
        job.failures++;
        if (!this.stopped && !job.paused && !job.terminal && this.jobs.get(id) === job) this.schedule(id, Math.min(30_000, 100 * 2 ** Math.min(job.failures - 1, 9)));
      });
    }, delay);
    job.timer.unref();
  }

  private async synchronize(id: SessionId, owner: Replication): Promise<void> {
    this.assertCurrent(id, owner);
    if (!await this.ctx.sessionPersistence.stat(id)) return;
    const handle = await this.ctx.sessionPersistence.open(id, "read");
    let local: Stored;
    try {
      local = {
        header: handle.header,
        inherited: handle.inheritedEventCount,
        events: [...(await handle.read()).events],
      };
    } finally {
      await handle.close();
    }
    const remote = await this.load(id, owner);
    if (remote && (!sameJson(remote.header, local.header) || remote.inherited !== local.inherited))
      throw new Error("Local and remote log headers diverged");
    if (remote && (remote.events.length > local.events.length || remote.events.some((event, index) => !sameJson(event, local.events[index]))))
      throw new Error("Local and remote log histories diverged");
    const events = local.events.slice(remote?.events.length ?? 0);
    if (remote && !events.length) return;
    this.assertCurrent(id, owner);
    const parent = this.parents.get(id);
    await this.config.backend.persist(parent ?? id, "dsh_log", {
      header: local.header,
      inherited: local.inherited,
      events,
    }, parent ? id : undefined);
    if (owner && this.jobs.get(id) === owner) this.records.set(id, structuredClone(local));
  }

  private async load(id: SessionId, owner: Replication): Promise<Stored | undefined> {
    if (this.records.has(id)) return this.records.get(id);
    const parent = this.parents.get(id);
    const payloads = await this.config.backend.privateEvents(parent ?? id);
    this.assertCurrent(id, owner);
    if (parent) payloads.push(...await this.config.backend.privateEvents(parent, { subagents: true }));
    this.assertCurrent(id, owner);
    let record: Stored | undefined;
    for (const payload of payloads) {
      if (payload.subtype === "dsh_brain_state" || payload.subtype === "grok_checkpoint")
        throw new Error("Legacy CCRv2 state requires migration before resume");
      if (payload.subtype !== "dsh_log") continue;
      if (payload.schema_version !== 1) throw new Error("Unsupported dsh log envelope");
      const data = payload.data;
      if (data.header?.id !== id) continue;
      assertStoredId(id, data.header);
      assertVersion(data.header);
      if (!Number.isSafeInteger(data.inherited) || data.inherited < 0) throw new Error("Invalid inherited log length");
      const header = materializeCreateHeader({ ...data.header, delegationDepth: data.header.delegationDepth ?? 0 });
      record ??= { header, inherited: data.inherited, events: [] };
      if (!sameJson(record.header, header) || record.inherited !== data.inherited) throw new Error("Log header changed");
      if (!Array.isArray(data.events)) throw new Error("Invalid dsh log batch");
      const batch = validateStoredEvents(record.header, structuredClone(data.events));
      for (const event of batch) {
        if (event.seq < record.events.length) {
          if (!sameJson(record.events[event.seq], event)) throw new Error("Conflicting dsh log event");
        } else {
          assertContiguous(id, [event], record.events.length);
          record.events.push(event);
        }
      }
    }
    if (record && record.inherited > record.events.length) throw new Error("Incomplete inherited log prefix");
    if (owner && this.jobs.get(id) === owner) this.records.set(id, record);
    return record;
  }
}
