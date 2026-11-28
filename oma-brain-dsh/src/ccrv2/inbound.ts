import type { CcrClient } from "./client.ts";
import { isControlInput, isInterruptInput } from "./protocol.ts";

export interface InboundReceiver {
  readonly id: string;
  readonly abort: AbortController;
  receive(inputId: string, payload: unknown): Promise<void>;
  stop(): void;
}

export async function consumeInbound(client: CcrClient, session: InboundReceiver): Promise<void> {
  let tasks = Promise.resolve();
  let controls = Promise.resolve();
  const interrupts = new Set<Promise<void>>();
  let failure: unknown;
  const pending = new Map<string, { repeated: boolean }>();
  const enqueue = (event: { id: string; payload: Record<string, unknown> }) => {
    const duplicate = pending.get(event.id);
    if (duplicate) { duplicate.repeated = true; return; }
    const control = isControlInput(event.payload.type);
    const entry = { repeated: false };
    pending.set(event.id, entry);
    const interrupt = isInterruptInput(event.payload);
    const next = (interrupt ? Promise.resolve() : control ? controls : tasks).then(async () => {
      session.abort.signal.throwIfAborted();
      await session.receive(event.id, event.payload);
      if (entry.repeated) await session.receive(event.id, event.payload);
    }).catch((error: unknown) => {
      failure ??= error;
      session.stop();
    }).finally(() => { pending.delete(event.id); });
    if (interrupt) {
      interrupts.add(next);
      void next.finally(() => interrupts.delete(next));
    } else if (control) controls = next;
    else tasks = next;
  };
  try {
    for await (const event of client.inbound(session.id, session.abort.signal)) {
      enqueue(event);
    }
  } catch (error) {
    if (!session.abort.signal.aborted) failure ??= error;
  } finally {
    session.stop();
    await Promise.all([tasks, controls, ...interrupts]);
  }
  if (failure) throw failure;
}
