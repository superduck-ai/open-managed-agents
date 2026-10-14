import type { I18nMsg, QuickstartSessionEvent, SessionExecutionState } from '../types';
import { stringValueFromKeys, toRecord } from '../utils';

const interruptedToolMessages = new Set([
  'Tool execution was interrupted before completion. Please retry.',
  'Tool execution was interrupted by the user.',
]);

export function sessionToolResultWasInterrupted(event?: QuickstartSessionEvent) {
  if (!event || event.is_error !== true) return false;
  const content = event.content;
  const block = Array.isArray(content) && content.length === 1 ? toRecord(content[0]) : null;
  const text = typeof content === 'string' ? content : block?.type === 'text' ? block.text : undefined;
  return typeof text === 'string' && interruptedToolMessages.has(text);
}

function eventThreadId(event: QuickstartSessionEvent) {
  return stringValueFromKeys(event, ['session_thread_id', 'thread_id']);
}

function eventWasProcessed(event: QuickstartSessionEvent) {
  return typeof event.processed_at === 'string';
}

function startsTurn(event: QuickstartSessionEvent) {
  return (
    (event.type === 'user.message' && eventWasProcessed(event)) ||
    event.type === 'session.status_running' ||
    event.type === 'session.status_rescheduled' ||
    event.type === 'session.thread_status_running' ||
    event.type === 'session.thread_status_rescheduled'
  );
}

type ModelRequestScope = { threadId: string; turn: number; index: number };

export function interruptedSessionModelRequestIds(events: QuickstartSessionEvent[]) {
  const threads = new Set(events.map(eventThreadId));
  const turns = new Map<string, number>();
  const requests = new Map<string, ModelRequestScope>();
  const interrupts = new Map<string, ModelRequestScope[]>();
  const ends: Array<{ event: QuickstartSessionEvent; index: number }> = [];
  events.forEach((event, index) => {
    const threadId = eventThreadId(event);
    if (startsTurn(event)) turns.set(threadId, (turns.get(threadId) ?? 0) + 1);
    const scope = { threadId, turn: turns.get(threadId) ?? 0, index };
    if (event.type === 'span.model_request_start' && typeof event.id === 'string') requests.set(event.id, scope);
    if (event.type === 'span.model_request_end') ends.push({ event, index });
    if (event.type === 'user.interrupt' && eventWasProcessed(event)) {
      const affectedThreads = threadId ? [threadId] : threads;
      for (const affectedThread of affectedThreads) {
        const list = interrupts.get(affectedThread) ?? [];
        list.push({ threadId: affectedThread, turn: turns.get(affectedThread) ?? 0, index });
        interrupts.set(affectedThread, list);
      }
    }
  });
  return new Set(
    ends
      .filter(({ event, index }) => {
        if (typeof event.id !== 'string') return false;
        if (toRecord(event.error)?.type !== 'cancelled') return false;
        const startId = stringValueFromKeys(event, ['model_request_start_id', 'start_event_id', 'parent_event_id']);
        const request = requests.get(startId);
        return (
          request !== undefined &&
          eventThreadId(event) === request.threadId &&
          interrupts
            .get(request.threadId)
            ?.some(
              (interrupt) =>
                interrupt.turn === request.turn && interrupt.index > request.index && interrupt.index <= index,
            )
        );
      })
      .map(({ event }) => event.id as string),
  );
}

export function sessionIdlePresentationState(event: QuickstartSessionEvent): SessionExecutionState | undefined {
  switch (toRecord(event.stop_reason)?.type) {
    case 'requires_action':
      return 'awaiting_action';
    case 'budget_reached':
      return 'budget_reached';
    case 'retries_exhausted':
      return 'failed';
    default:
      return undefined;
  }
}

export function sessionEventPresentationState(
  event: QuickstartSessionEvent,
  interruptedModelIds: ReadonlySet<string>,
): SessionExecutionState | undefined {
  switch (event.type) {
    case 'user.interrupt':
      return eventWasProcessed(event) ? 'stopped' : 'stopping';
    case 'user.message':
    case 'system.message':
      return eventWasProcessed(event) ? undefined : 'queued';
    case 'span.model_request_end':
      if (typeof event.id === 'string' && interruptedModelIds.has(event.id)) return 'interrupted';
      return event.is_error === true || (event.is_error !== false && Boolean(toRecord(event.error)))
        ? 'failed'
        : 'completed';
    case 'session.status_idle':
    case 'session.thread_status_idle':
      return sessionIdlePresentationState(event);
    case 'session.status_rescheduled':
    case 'session.thread_status_rescheduled':
      return 'rescheduled';
    case 'session.status_running':
    case 'session.thread_status_running':
      return 'running';
    case 'session.status_terminated':
    case 'session.thread_status_terminated':
      return 'terminated';
    default:
      return undefined;
  }
}

export function latestSessionIdlePresentationState(events: QuickstartSessionEvent[]) {
  for (let index = events.length - 1; index >= 0; index--) {
    const event = events[index];
    if (typeof event.type === 'string' && event.type.startsWith('session.status_')) {
      return event.type === 'session.status_idle' ? sessionIdlePresentationState(event) : undefined;
    }
  }
  return undefined;
}

const stateLabels: Record<SessionExecutionState, string> = {
  running: 'Executing',
  awaiting_approval: 'Awaiting approval',
  awaiting_result: 'Awaiting result',
  awaiting_action: 'Awaiting action',
  completed: 'Completed',
  denied: 'Denied',
  failed: 'Failed',
  interrupted: 'Interrupted',
  stopped: 'Stopped',
  stopping: 'Stopping',
  queued: 'Queued',
  rescheduled: 'Rescheduled',
  budget_reached: 'Budget reached',
  terminated: 'Terminated',
};

export function sessionExecutionStateLabel(state: SessionExecutionState, msg: I18nMsg) {
  return msg(`managedAgents.sessions.state.${state}`, stateLabels[state]);
}

const stateDescriptions: Partial<Record<SessionExecutionState, string>> = {
  stopped: 'Stopped by the user.',
  stopping: 'Stop requested; waiting for the agent to stop.',
  interrupted: 'Model request interrupted by the user.',
  failed: 'Execution failed.',
  awaiting_action: 'Waiting for a tool confirmation or result.',
  budget_reached: 'Budget reached; increase or remove the budget to continue.',
  rescheduled: 'Session queued; waiting to resume.',
  terminated: 'Session terminated.',
};

export function sessionExecutionStateDescription(state: SessionExecutionState, msg?: I18nMsg) {
  const description = stateDescriptions[state];
  return description && msg ? msg(`managedAgents.sessions.stateDescription.${state}`, description) : description;
}
