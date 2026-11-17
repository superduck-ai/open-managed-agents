import {
  type I18nMsg,
  type QuickstartSessionEvent,
  type SessionDetailLane,
  type SessionEventListEntry,
} from '../types';
import { interruptedSessionModelRequestIds } from './sessionExecutionState';
import { buildSessionEventEntries, sessionCanonicalDisplayEvent, sessionEventTimestamp } from './sessionTraceModel';

export function buildSessionEntriesByLane(
  lanes: SessionDetailLane[],
  events: QuickstartSessionEvent[],
  eventsByLaneId: Map<string, QuickstartSessionEvent[]>,
  traceStartMs: number,
  msg: I18nMsg,
) {
  const interruptionEvents = events
    .map(sessionCanonicalDisplayEvent)
    .sort((left, right) => sessionEventTimestamp(left) - sessionEventTimestamp(right));
  const interruptedModelRequestIds = interruptedSessionModelRequestIds(interruptionEvents);
  const entriesByLaneId = new Map<string, SessionEventListEntry[]>();
  lanes.forEach((lane) => {
    entriesByLaneId.set(
      lane.id,
      buildSessionEventEntries(eventsByLaneId.get(lane.id) ?? [], 'transcript', traceStartMs, msg, {
        platformTranscriptFiltering: true,
        interruptedModelRequestIds,
      }),
    );
  });
  return entriesByLaneId;
}
