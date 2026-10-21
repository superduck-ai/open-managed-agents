import type { I18nMsg, QuickstartSessionEvent, SessionEventListEntry, SessionExecutionState } from '../types';
import { stringValueFromKeys } from '../utils';
import { sessionExecutionStateDescription } from './sessionExecutionState';

function threadId(event: QuickstartSessionEvent) {
  return stringValueFromKeys(event, ['session_thread_id', 'thread_id']);
}

function startsExecution(event: QuickstartSessionEvent) {
  return (
    event.type === 'span.model_request_start' ||
    (event.type === 'user.message' && typeof event.processed_at === 'string') ||
    event.type === 'session.status_running' ||
    event.type === 'session.thread_status_running' ||
    event.type === 'session.status_rescheduled' ||
    event.type === 'session.thread_status_rescheduled'
  );
}

function interruptScope(events: QuickstartSessionEvent[], index: number) {
  const stop = events[index];
  const stopThread = threadId(stop);
  let before = events.length;
  let settled = false;
  for (let next = index + 1; next < events.length; next++) {
    const event = events[next];
    if (stopThread && threadId(event) && threadId(event) !== stopThread) continue;
    if (startsExecution(event)) {
      before = next;
      break;
    }
    if (event.type === 'session.status_idle' || event.type === 'session.thread_status_idle') settled = true;
  }
  return { index, before, settled: settled && typeof stop.processed_at === 'string', stopThread };
}

export function arrangeSessionTranscriptInterrupts(
  entries: SessionEventListEntry[],
  events: QuickstartSessionEvent[],
  msg?: I18nMsg,
) {
  const scopes = events.flatMap((event, index) =>
    event.type === 'user.interrupt' ? [interruptScope(events, index)] : [],
  );
  if (!scopes.length) return entries;
  const indices = new Map(events.map((event, index) => [event.id, index]));
  const rawIndices = entries.map((entry) =>
    'event' in entry
      ? (indices.get(entry.rawEventId) ?? indices.get(stringValueFromKeys(entry.event, ['parent_event_id'])))
      : undefined,
  );
  const ranks = [...rawIndices];
  let nextIndex = events.length;
  for (let index = ranks.length - 1; index >= 0; index--) {
    const rank = ranks[index];
    if (rank === undefined) ranks[index] = nextIndex - 0.25;
    else nextIndex = rank;
  }
  const ranked = entries.flatMap<{ entry: SessionEventListEntry; rank: number }>((entry, order) => {
    const index = rawIndices[order];
    if (!('event' in entry) || index === undefined) return [{ entry, rank: ranks[order] ?? order }];
    const scope = scopes.find((candidate) => candidate.index === index);
    if (scope) {
      const state: SessionExecutionState = scope.settled ? 'stopped' : 'stopping';
      const preview = sessionExecutionStateDescription(state, msg)!;
      const updated = {
        ...entry,
        traceEntry: { ...entry.traceEntry, state, preview },
        searchText: `${entry.searchText}\n${preview.toLowerCase()}`,
      };
      return [{ entry: updated, rank: scope.before - 0.5 }];
    }
    if (
      entry.traceEntry.state === 'interrupted' &&
      scopes.some(
        (candidate) =>
          candidate.index < index &&
          index < candidate.before &&
          (!candidate.stopThread || candidate.stopThread === threadId(entry.event)),
      )
    ) {
      return [];
    }
    return [{ entry, rank: index }];
  });
  return ranked.sort((left, right) => left.rank - right.rank).map(({ entry }) => entry);
}
