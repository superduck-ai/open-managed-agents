import { abortable } from "./retry.ts";

export interface WorkerPutClaim {
  epoch: number;
  signal?: AbortSignal;
}

interface Queue extends WorkerPutClaim {
  tail: Promise<void>;
  confirmedStatus?: string;
}

/** FIFO confirmation for status and metadata, without merging retry payloads. */
export class WorkerPutQueue {
  private readonly sessions = new Map<string, Queue>();

  async put(
    id: string,
    claim: WorkerPutClaim,
    body: Record<string, unknown>,
    deduplicate: boolean,
    validate: () => void,
    send: (snapshot: Record<string, unknown>) => Promise<void>,
  ): Promise<void> {
    // Capture before waiting, so callers cannot mutate a queued patch.
    const snapshot: Record<string, unknown> = JSON.parse(JSON.stringify(body));
    const previous = this.sessions.get(id);
    const queue: Queue = previous?.epoch === claim.epoch && previous.signal === claim.signal
      ? previous : { ...claim, tail: previous?.tail ?? Promise.resolve() };
    this.sessions.set(id, queue);
    const signature = JSON.stringify(snapshot);
    const ready = claim.signal ? abortable(queue.tail, claim.signal) : queue.tail;
    const pending = ready.then(async () => {
      claim.signal?.throwIfAborted();
      validate();
      if (deduplicate && queue.confirmedStatus === signature) return;
      const metadata = snapshot.external_metadata;
      // An ambiguous failure may have changed server state. A metadata patch
      // may also require the next idle/running status to clear it again.
      if (snapshot.worker_status !== undefined || (metadata && typeof metadata === "object" &&
        ("pending_action" in metadata || "task_summary" in metadata))) queue.confirmedStatus = undefined;
      await send(snapshot);
      if (snapshot.worker_status !== undefined) queue.confirmedStatus = signature;
    });
    // Cancellation can reject a waiting caller before an old transport ends.
    // A replacement claim still waits for that transport before sending.
    queue.tail = Promise.allSettled([queue.tail, pending]).then(() => {});
    const tail = queue.tail;
    void tail.then(() => {
      if (claim.signal?.aborted && queue.tail === tail && this.sessions.get(id) === queue)
        this.sessions.delete(id);
    });
    await pending;
  }
}
