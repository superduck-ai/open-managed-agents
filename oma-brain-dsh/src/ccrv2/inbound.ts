import type { CcrClient } from "./client.ts";
import { isControlInput } from "./protocol.ts";

export interface InboundReceiver {
  readonly id: string;
  readonly abort: AbortController;
  receive(inputId: string, payload: unknown): Promise<void>;
  stop(): void;
  queueInput?(inputId: string, payload: unknown): boolean;
}

export async function consumeInbound(client: CcrClient, session: InboundReceiver): Promise<void> {
  let tasks = Promise.resolve();
  let controls = Promise.resolve();
  let failure: unknown;
  const pending = new Map<string, { repeated: boolean; control: boolean; steered: boolean }>();
  const enqueue = (event: { id: string; payload: Record<string, unknown> }) => {
    const duplicate = pending.get(event.id);
    if (duplicate) { duplicate.repeated = true; return; }
    const control = isControlInput(event.payload.type);
    const waiting = [...pending.values()].filter((entry) => !entry.control && !entry.steered).length;
    const steered = !control && waiting <= 1 && (session.queueInput?.(event.id, event.payload) ?? false);
    const entry = { repeated: false, control, steered };
    pending.set(event.id, entry);
    const next = (control ? controls : tasks).then(async () => {
      session.abort.signal.throwIfAborted();
      await session.receive(event.id, event.payload);
      if (entry.repeated) await session.receive(event.id, event.payload);
    }).catch((error: unknown) => {
      failure ??= error;
      session.stop();
    }).finally(() => { pending.delete(event.id); });
    if (control) controls = next;
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
    await Promise.all([tasks, controls]);
  }
  if (failure) throw failure;
}
