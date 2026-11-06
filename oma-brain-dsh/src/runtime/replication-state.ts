import type { Session } from "@deepseek-ai/dsh-session";
import type { TranscriptView } from "./transcript-persistence.ts";
import { transcriptEventBytes } from "./transcript-batch.ts";

export interface ReplicationWaiter {
  through?: number;
  resolve(): void;
  reject(error: unknown): void;
}

export interface ReplicationState {
  chain: Promise<void>;
  timer?: ReturnType<typeof setTimeout>;
  failures: number;
  terminal?: Error;
  paused?: boolean;
  acquired?: boolean;
  live?: Session;
  validated?: boolean;
  confirmed: number;
  readyNext: number;
  headerReady?: boolean;
  headerBytes: number;
  requestedNext: number;
  headerConfirmed: boolean;
  captureError?: Error;
  waiters: Set<ReplicationWaiter>;
  byteBase: number;
  byteEnds: number[];
  pendingSince?: number;
  inFlight: boolean;
  abort: AbortController;
}

export function updateReady(
  state: ReplicationState,
  transcript: TranscriptView,
): void {
  const { events } = transcript;
  const through = events.length;
  if (!state.headerReady && !state.headerConfirmed)
    state.headerBytes = transcriptEventBytes(transcript.header);
  for (
    let index = state.byteBase + state.byteEnds.length - 1;
    index < through;
    index++
  )
    state.byteEnds.push(
      state.byteEnds[index - state.byteBase]! +
        transcriptEventBytes(events[index]),
    );
  state.readyNext = through;
  state.headerReady = true;
  if (through > state.confirmed || !state.headerConfirmed)
    state.pendingSince ??= Date.now();
}

export function pendingBytes(state: ReplicationState): number {
  const events = Math.max(
    0,
    (state.byteEnds[state.readyNext - state.byteBase] ?? 0) -
      (state.byteEnds[state.confirmed - state.byteBase] ?? 0),
  );
  return (
    events +
    (!state.headerConfirmed && state.headerReady ? state.headerBytes : 0)
  );
}

export function settleReplication(state: ReplicationState): void {
  if (state.headerConfirmed && state.confirmed >= state.readyNext)
    state.pendingSince = undefined;
  for (const waiter of state.waiters) {
    if (
      waiter.through !== undefined &&
      state.headerConfirmed &&
      state.confirmed >= waiter.through
    ) {
      state.waiters.delete(waiter);
      waiter.resolve();
    }
  }
}

export function rejectReplication(
  state: ReplicationState,
  error: unknown,
): void {
  for (const waiter of state.waiters) waiter.reject(error);
  state.waiters.clear();
}
