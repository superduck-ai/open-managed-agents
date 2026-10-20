import type { Payload } from "./protocol.ts";
import type { DeliveryUpdate, RequiresActionDetails, SessionState, SessionExternalMetadata, WorkerStateReadResult } from "./wire.ts";

/**
 * CCRv2 operations. persist resolves only after durable confirmation; events
 * resolves after publication is confirmed. The backend retains epoch fencing,
 * write deduplication and delivery ACK ownership.
 */
export interface CcrBackend {
  getSubagentThread?(id: string, threadId: string, signal?: AbortSignal): Promise<{id: string; status: string; archived: boolean}>;
  getWorkerState(id: string, workerEpoch?: number, signal?: AbortSignal): Promise<WorkerStateReadResult>;
  isToolConfirmationPending(id: string, requestId: string, signal?: AbortSignal): Promise<boolean>;
  privateEvents(id: string, options?: { subagents?: boolean }): Promise<Payload[]>;
  persist(id: string, subtype: string, data: unknown, agentId?: string): Promise<void>;
  /** Honour cancellation for activity keepalives; durable callers omit signal. */
  events(id: string, payloads: Payload[], signal?: AbortSignal): Promise<void>;
  acknowledge(id: string, inputId: string, signal?: AbortSignal): Promise<void>;
  /** Resolves on HTTP confirmation. Progress callers enqueue in the background;
   * terminal task ACKs await confirmation after durable publication.
   * The signal owns this session's delivery queue, including retry waits. */
  reportDelivery(id: string, inputId: string, status: DeliveryUpdate["status"], signal?: AbortSignal): Promise<void>;
  setStatus(id: string, status: SessionState, details?: RequiresActionDetails): Promise<void>;
  reportMetadata(id: string, metadata: SessionExternalMetadata & Record<string, unknown>): Promise<void>;
  sessionIds(): Iterable<string>;
}
