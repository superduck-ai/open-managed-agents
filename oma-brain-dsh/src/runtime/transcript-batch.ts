import type { SessionEvent } from "@deepseek-ai/dsh-session";

export const TRANSCRIPT_BATCH_EVENTS = 200;
export const TRANSCRIPT_BATCH_BYTES = 4 * 1024 * 1024;
export const TRANSCRIPT_EVENT_BYTES = 15 * 1024 * 1024;

export class TranscriptUploadError extends Error {}

export function transcriptEventBytes(event: unknown): number {
  try {
    const bytes = Buffer.byteLength(JSON.stringify(event));
    if (bytes > TRANSCRIPT_EVENT_BYTES)
      throw new TranscriptUploadError("Transcript record exceeds 15 MiB");
    return bytes;
  } catch (error) {
    if (error instanceof TranscriptUploadError) throw error;
    throw new TranscriptUploadError("Transcript record cannot be serialized", {
      cause: error,
    });
  }
}

export function transcriptBatch(
  events: readonly SessionEvent[],
  start = 0,
  through = events.length,
): SessionEvent[] {
  const batch: SessionEvent[] = [];
  let bytes = Buffer.byteLength('{"events":[]}');
  const end = Math.min(through, events.length, start + TRANSCRIPT_BATCH_EVENTS);
  for (let index = start; index < end; index++) {
    const event = events[index]!;
    const size = transcriptEventBytes(event) + (batch.length ? 1 : 0);
    if (batch.length && bytes + size > TRANSCRIPT_BATCH_BYTES) break;
    bytes += size;
    batch.push(event);
  }
  return batch;
}
