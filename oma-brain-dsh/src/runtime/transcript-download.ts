import {
  type SessionEvent,
  type SessionHeader,
  type SessionId,
} from "@deepseek-ai/dsh-session";
import {
  assertContiguous,
  assertStoredId,
} from "@deepseek-ai/dsh-session-persistence";
import { sameJson } from "./json-equality.ts";
import {
  transcriptHeader,
  validateTranscriptEvents,
  validateTranscriptHistory,
} from "./transcript-validation.ts";
import type { DshLogData } from "./transcript-types.ts";

export interface DownloadedTranscript {
  header: SessionHeader;
  events: SessionEvent[];
  count: number;
}

export async function withDownloadedTranscript<T>(
  id: SessionId,
  source: AsyncIterable<readonly DshLogData[]>,
  assertCurrent: () => void,
  consume: (transcript: DownloadedTranscript | undefined) => Promise<T>,
): Promise<T> {
  let header: SessionHeader | undefined;
  const events: SessionEvent[] = [];
  for await (const batch of source) {
    assertCurrent();
    for (const data of batch) {
      if (!data || typeof data !== "object" || Array.isArray(data))
        throw new Error("Invalid dsh log record");
      if (!("seq" in data)) {
        assertStoredId(id, data);
        const candidate = transcriptHeader(data);
        if (header && !sameJson(header, candidate))
          throw new Error("Log header changed");
        header ??= candidate;
      } else {
        if (!header) throw new Error("Missing dsh log header");
        const [event] = validateTranscriptEvents(header, [
          structuredClone(data),
        ]);
        if (event.seq < events.length) {
          if (!sameJson(events[event.seq], event))
            throw new Error("Conflicting dsh log event");
        } else {
          assertContiguous(id, [event], events.length);
          events.push(event);
        }
      }
    }
    assertCurrent();
  }
  if (header) validateTranscriptHistory(header, events);
  assertCurrent();
  return consume(
    header
      ? {
          header,
          events,
          count: events.length,
        }
      : undefined,
  );
}
