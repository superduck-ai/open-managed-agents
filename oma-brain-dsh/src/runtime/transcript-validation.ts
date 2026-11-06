import {
  KNOWN_SESSION_EVENT_TYPES,
  type SessionEvent,
  type SessionHeader,
} from "@deepseek-ai/dsh-session";
import {
  assertVersion,
  materializeCreateHeader,
  validateStoredEvents,
} from "@deepseek-ai/dsh-session-persistence";
import {
  assertV4RowAdmission,
  restoreReleasedV4Artifact,
} from "@deepseek-ai/dsh-session-format-v3-to-v4";
import { deepFreeze } from "@deepseek-ai/dsh-util-values";

export function transcriptHeader(value: SessionHeader): SessionHeader {
  assertVersion(value);
  if (value.isSeeded) throw new Error("Forked sessions are not enabled");
  return deepFreeze(
    materializeCreateHeader({
      ...value,
      delegationDepth: value.delegationDepth ?? 0,
    }),
  );
}

export function validateTranscriptEvents(
  header: SessionHeader,
  events: SessionEvent[],
): SessionEvent[] {
  validateStoredEvents(header, events);
  for (const event of events)
    assertV4RowAdmission(event, KNOWN_SESSION_EVENT_TYPES);
  return events;
}

export function validateTranscriptHistory(
  header: SessionHeader,
  events: SessionEvent[],
): void {
  restoreReleasedV4Artifact(
    { header, events, inheritedEventCount: 0 } as unknown as Parameters<
      typeof restoreReleasedV4Artifact
    >[0],
    KNOWN_SESSION_EVENT_TYPES,
  );
  for (const event of events) deepFreeze(event);
}
