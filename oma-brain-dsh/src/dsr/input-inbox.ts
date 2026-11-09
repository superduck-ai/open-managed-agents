import { DsrError } from "./http.ts";
import type { Delivery, Input } from "./wire.ts";

type PendingReceipt = {
  update: Delivery;
  completion: ReturnType<typeof Promise.withResolvers<void>>;
};

export class DsrReceipts {
  private readonly pending = new Map<string, PendingReceipt>();
  private pumping?: Promise<void>;
  private failure?: unknown;

  constructor(
    private readonly send: (updates: Delivery[]) => Promise<unknown>,
  ) {}

  enqueue(update: Delivery): Promise<void> {
    if (this.failure) return Promise.reject(this.failure);
    const key = `${update.id}:${update.status}`;
    const existing = this.pending.get(key);
    if (existing) return existing.completion.promise;
    if (this.pending.size >= 4096)
      return Promise.reject(new DsrError(0, "input_receipt_capacity"));
    const completion = Promise.withResolvers<void>();
    this.pending.set(key, { update, completion });
    this.pumping ??= Promise.resolve().then(() => this.pump());
    return completion.promise;
  }

  private async pump(): Promise<void> {
    try {
      while (this.pending.size) {
        const batch: [string, PendingReceipt][] = [];
        const ids = new Set<string>();
        for (const entry of this.pending) {
          const id = entry[1].update.id;
          if (ids.has(id)) continue;
          ids.add(id);
          batch.push(entry);
          if (batch.length === 64) break;
        }
        await this.send(batch.map(([, item]) => item.update));
        for (const [key, item] of batch) {
          this.pending.delete(key);
          item.completion.resolve();
        }
      }
    } catch (error) {
      this.failure = error;
      for (const item of this.pending.values()) item.completion.reject(error);
      this.pending.clear();
    } finally {
      this.pumping = undefined;
    }
  }
}

type Task = Extract<Input, { lane: "task" }>;
type Control = Extract<Input, { lane: "control" }>;

export class DsrInputInbox {
  readonly unsettled = new Set<string>();
  private readonly tasks = new Map<string, number>();
  private readonly controls = new Map<string, Delivery>();
  private taskBytes = 0;
  private taskQueue: Promise<void> = Promise.resolve();
  private controlQueue: Promise<void> = Promise.resolve();

  constructor(
    readonly receipts: DsrReceipts,
    private readonly handlers: {
      task: (input: Task) => Promise<boolean>;
      control: (
        input: Control,
        report: (update: Delivery) => Promise<void>,
      ) => Promise<void>;
      fail: (error: unknown) => void;
    },
  ) {}

  accept(input: Input): boolean {
    if (input.lane === "control") return this.acceptControl(input);
    if (this.tasks.has(input.id)) {
      this.received(input.id);
      return true;
    }
    const bytes = new TextEncoder().encode(JSON.stringify(input)).byteLength;
    if (this.tasks.size >= 64 || this.taskBytes + bytes > 16 * 1024 * 1024)
      return false;
    this.tasks.set(input.id, bytes);
    this.taskBytes += bytes;
    this.unsettled.add(input.id);
    const received = this.received(input.id);
    this.taskQueue = this.taskQueue.then(async () => {
      await received;
      const completed = await this.handlers.task(input);
      if (completed) {
        this.unsettled.delete(input.id);
        this.tasks.delete(input.id);
        this.taskBytes -= bytes;
      }
    });
    void this.taskQueue.catch(this.handlers.fail);
    return true;
  }

  private acceptControl(input: Control): boolean {
    if (this.controls.has(input.id)) {
      if (this.unsettled.has(input.id)) this.received(input.id);
      else
        void this.receipts
          .enqueue(this.controls.get(input.id)!)
          .catch(this.handlers.fail);
      return true;
    }
    if (this.controls.size >= 4096) {
      this.handlers.fail(new DsrError(0, "input_control_capacity"));
      return false;
    }
    this.controls.set(input.id, { id: input.id, status: "processed" });
    this.unsettled.add(input.id);
    const received = this.received(input.id);
    const apply = () =>
      this.handlers.control(input, (update) => {
        this.controls.set(input.id, update);
        return this.receipts.enqueue(update);
      });
    const application =
      input.kind === "user.interrupt" ? apply() : received.then(apply);
    const applied = application.then(() => {
      this.unsettled.delete(input.id);
    });
    void applied.catch(this.handlers.fail);
    this.controlQueue = Promise.allSettled([this.controlQueue, applied]).then(
      () => {},
    );
    return true;
  }

  private received(id: string): Promise<void> {
    const receipt = this.receipts.enqueue({ id, status: "received" });
    void receipt.catch(this.handlers.fail);
    return receipt;
  }

  async reportReceived(): Promise<void> {
    await Promise.all([...this.unsettled].map((id) => this.received(id)));
  }

  async drain(): Promise<void> {
    await Promise.allSettled([this.taskQueue, this.controlQueue]);
  }
}
