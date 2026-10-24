import { appendPluginRecord, type Session } from "@deepseek-ai/dsh-session";
import { emptyDelivery, type DeliveryState, type Payload } from "./protocol.ts";
import { CcrRecoveryError, readDelivery } from "./recovery.ts";

export interface DeliveryDelta {
  version: 2;
  revision: number;
  inputId: string;
  logOffsets: Record<string, number>;
  completedAdded: string[];
  queued: Payload[];
  sent: string[];
}

export type DeliveryRecord = { version: 1; delivery: DeliveryState } | DeliveryDelta;

declare module "@deepseek-ai/dsh-session" {
  interface PluginRecordMap {
    "plugin:oma/delivery": DeliveryRecord;
  }
}

function object(value: unknown): value is Record<string, unknown> {
  return value !== null && typeof value === "object" && !Array.isArray(value);
}

function readDelta(value: Record<string, unknown>, sessionId: string): DeliveryDelta {
  const delivery = readDelivery({
    revision: value.revision, inputId: value.inputId, logOffsets: value.logOffsets,
    completed: value.completedAdded, outbox: value.queued,
  }, sessionId);
  if (!Array.isArray(value.sent) || value.sent.some(id => typeof id !== "string" || !id))
    throw new CcrRecoveryError(sessionId, "invalid_delivery", "sent");
  return { version: 2, revision: delivery.revision, inputId: delivery.inputId, logOffsets: delivery.logOffsets,
    completedAdded: delivery.completed, queued: delivery.outbox, sent: [...value.sent] };
}

export class DeliveryJournal {
  private readonly completedIds = new Set<string>();
  private completedOffset = 0;
  private metadata = JSON.stringify({ inputId: "", logOffsets: {} });
  private readonly outbox = new Map<string, string>();
  readonly restoredThreadIds = new Set<string>();
  state: DeliveryState = emptyDelivery();

  constructor(readonly sessionId: string) {}

  restore(value: unknown): DeliveryState {
    if (!object(value)) throw new CcrRecoveryError(this.sessionId, "invalid_delivery", "record");
    if (value.version === 1) {
      const delivery = readDelivery(value.delivery, this.sessionId);
      this.retainThreads(delivery.outbox);
      if (delivery.revision > this.state.revision) {
        this.state = delivery;
        this.remember(delivery);
      }
      return this.state;
    }
    if (value.version !== 2) throw new Error("Unsupported CCRv2 delivery record");
    const delta = readDelta(value, this.sessionId);
    this.retainThreads(delta.queued);
    if (delta.revision <= this.state.revision) return this.state;
    if (delta.revision !== this.state.revision + 1)
      throw new CcrRecoveryError(this.sessionId, "invalid_delivery", "revision gap");
    for (const id of delta.completedAdded) {
      if (this.completedIds.has(id)) continue;
      this.completedIds.add(id);
      this.state.completed.push(id);
    }
    for (const id of delta.sent) this.outbox.delete(id);
    for (const payload of delta.queued) this.outbox.set(payload.uuid, JSON.stringify(payload));
    this.state.revision = delta.revision;
    this.state.inputId = delta.inputId;
    this.state.logOffsets = delta.logOffsets;
    this.state.outbox = [...this.outbox.values()].map(value => JSON.parse(value) as Payload);
    this.completedOffset = this.state.completed.length;
    this.metadata = JSON.stringify({ inputId: delta.inputId, logOffsets: delta.logOffsets });
    return this.state;
  }

  append(session: Session, delivery: DeliveryState): boolean {
    if (delivery.completed.length < this.completedOffset)
      throw new CcrRecoveryError(this.sessionId, "invalid_delivery", "completed removed");
    const completedAdded: string[] = [];
    const additions = new Set<string>();
    for (let index = this.completedOffset; index < delivery.completed.length; index++) {
      const id = delivery.completed[index]!;
      if (typeof id !== "string") throw new CcrRecoveryError(this.sessionId, "invalid_delivery", "completed");
      if (!this.completedIds.has(id) && !additions.has(id)) {
        additions.add(id);
        completedAdded.push(id);
      }
    }
    const current = new Map<string, string>();
    const queued: Payload[] = [];
    for (const payload of delivery.outbox) {
      const encoded = JSON.stringify(payload);
      current.set(payload.uuid, encoded);
      if (this.outbox.get(payload.uuid) !== encoded) queued.push(JSON.parse(encoded) as Payload);
    }
    const sent = [...this.outbox.keys()].filter(id => !current.has(id));
    const metadata = JSON.stringify({ inputId: delivery.inputId, logOffsets: delivery.logOffsets });
    if (metadata === this.metadata && !completedAdded.length && !queued.length && !sent.length) {
      this.completedOffset = delivery.completed.length;
      return false;
    }
    const delta = readDelta({ version: 2, revision: delivery.revision + 1, inputId: delivery.inputId,
      logOffsets: delivery.logOffsets, completedAdded, queued, sent }, this.sessionId);
    appendPluginRecord(session, "plugin:oma/delivery", delta);
    delivery.revision = delta.revision;
    for (const id of completedAdded) this.completedIds.add(id);
    this.completedOffset = delivery.completed.length;
    this.metadata = metadata;
    this.outbox.clear();
    for (const [id, value] of current) this.outbox.set(id, value);
    this.retainThreads(queued);
    return true;
  }

  private remember(delivery: DeliveryState): void {
    this.completedIds.clear();
    for (const id of delivery.completed) this.completedIds.add(id);
    delivery.completed = [...this.completedIds];
    this.completedOffset = delivery.completed.length;
    this.metadata = JSON.stringify({ inputId: delivery.inputId, logOffsets: delivery.logOffsets });
    this.outbox.clear();
    for (const payload of delivery.outbox) this.outbox.set(payload.uuid, JSON.stringify(payload));
  }

  private retainThreads(payloads: readonly Payload[]): void {
    for (const payload of payloads) {
      if (payload.type !== "session.thread_created") continue;
      if (typeof payload.session_thread_id !== "string" || !payload.session_thread_id)
        throw new Error("Invalid retained subagent identity");
      this.restoredThreadIds.add(payload.session_thread_id);
    }
  }
}
