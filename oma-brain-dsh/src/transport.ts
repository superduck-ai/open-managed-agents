import { connect, type NatsConnection } from "@nats-io/transport-node";
import { jetstream, jetstreamManager, AckPolicy } from "@nats-io/jetstream";
import { HttpError } from "./api.ts";
import { CcrHttpError } from "./ccrv2/client.ts";
import { ConfigurationError } from "./config.ts";
import { failureDiagnostic, workerDiagnostic } from "./ccrv2/diagnostics.ts";

export async function connectNats(url: string): Promise<NatsConnection> {
  return connect({ servers: url.split(",").map((s) => s.trim()), name: "oma-brain-dsh" });
}

export type TaskAdmissionStage = "validate_assignment" | "retire_session" | "check_capacity"
  | "register_worker" | "create_session" | "open_session" | "start_inbound";

export function shouldTerminateTask(error: unknown, stage: string, deliveries: number): boolean {
  if (stage === "decode_task" || stage === "validate_assignment") return true;
  if (error instanceof HttpError || error instanceof CcrHttpError) {
    if ([400, 401, 403, 404, 409, 410, 422].includes(error.status)) return true;
  }
  return deliveries >= 10;
}

export async function consumeTasks(
  nc: NatsConnection, signal: AbortSignal,
  receive: (task: unknown, stage: (stage: TaskAdmissionStage) => void) => Promise<void>, ready?: () => void,
): Promise<void> {
  const manager = await jetstreamManager(nc);
  try {
    await manager.consumers.info("OMA_BRAIN_TASKS", "oma_brain");
  } catch (error) {
    if ((error as { code?: number }).code === 10059)
      throw new ConfigurationError("NATS stream OMA_BRAIN_TASKS is missing (10059). Start the Brain-enabled OMA backend to initialize it and ensure both services use the same NATS server/account.");
    if ((error as { code?: number }).code !== 10014) throw error;
    await manager.consumers.add("OMA_BRAIN_TASKS", {
      durable_name: "oma_brain", filter_subject: "oma.brain.tasks.v1",
      ack_policy: AckPolicy.Explicit, ack_wait: 60_000_000_000,
      max_deliver: -1,
    });
  }
  const consumer = await jetstream(nc).consumers.get("OMA_BRAIN_TASKS", "oma_brain");
  const messages = await consumer.consume({ max_messages: 1 });
  ready?.();
  const stop = () => messages.stop();
  signal.addEventListener("abort", stop, { once: true });
  if (signal.aborted) stop();
  try {
    for await (const message of messages) {
      const renewal = setInterval(() => message.working(), 15_000);
      const startMs = Date.now();
      let stage: string = "decode_task";
      const fields: Record<string, unknown> = {
        stream_seq: message.seq, delivery_count: message.info.deliveryCount,
      };
      try {
        const task: unknown = JSON.parse(new TextDecoder().decode(message.data));
        if (task && typeof task === "object" && !Array.isArray(task)) {
          const envelope = task as Record<string, unknown>;
          if (typeof envelope.session_id === "string") fields.session_id = envelope.session_id;
          if (typeof envelope.worker_epoch === "number") fields.worker_epoch = envelope.worker_epoch;
        }
        workerDiagnostic("info", "oma_task_admission_started", fields);
        stage = "validate_assignment";
        await receive(task, (next) => { stage = next; });
        stage = "ack_task";
        message.ack();
        workerDiagnostic("info", "oma_task_admitted", { ...fields, duration_ms: Date.now() - startMs });
      } catch (error) {
        const terminated = !signal.aborted && shouldTerminateTask(error, stage, message.info.deliveryCount);
        if (terminated) message.term();
        else message.nak(5000);
        if (!signal.aborted) workerDiagnostic("error", "oma_task_admission_failed", {
          ...fields, stage, terminated, duration_ms: Date.now() - startMs, ...failureDiagnostic(error),
        });
      } finally { clearInterval(renewal); }
    }
  } finally {
    signal.removeEventListener("abort", stop);
    messages.stop();
  }
}
