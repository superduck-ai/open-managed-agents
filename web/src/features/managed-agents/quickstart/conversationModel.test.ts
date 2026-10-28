import { expect, test } from 'bun:test';
import { quickstartConversationState } from './conversationModel';
import type { QuickstartSessionEvent } from '../types';

function events(...types: string[]): QuickstartSessionEvent[] {
  return types.map((type) => ({ type }));
}

test('accepted input and queue status keep sending locked without claiming execution', () => {
  const waiting = quickstartConversationState(events('user.message'), 'idle', false);
  expect(waiting.running).toBe(true);
  expect(waiting.waiting).toBe(true);
  const queued = quickstartConversationState(events('user.message', 'session.status_rescheduled'), 'idle', false);
  expect(queued.running).toBe(true);
  expect(queued.waiting).toBe(true);
});

test('execution and confirmation are distinguished from a queue', () => {
  const running = quickstartConversationState(events('user.message', 'session.status_running'), 'running', false);
  expect(running.running).toBe(true);
  expect(running.waiting).toBe(false);
  const action = quickstartConversationState(events('user.message', 'session.status_idle'), 'idle', true);
  expect(action.waiting).toBe(false);
});

const secondTurn: QuickstartSessionEvent[] = [
  {
    type: 'session.status_idle',
    processed_at: '2026-10-28T05:39:44.637858Z',
    stop_reason: { type: 'end_turn' },
  },
  {
    type: 'session.status_running',
    processed_at: '2026-10-28T05:40:21.079610Z',
  },
  {
    type: 'session.thread_status_running',
    processed_at: '2026-10-28T05:40:21.079610Z',
  },
  { type: 'user.message', processed_at: '2026-10-28T05:40:21.202055Z' },
  {
    type: 'span.model_request_start',
    id: 'request_second',
    processed_at: '2026-10-28T05:40:21.350653Z',
  },
];

test('R1 actual second-turn ordering is executing after its model request starts', () => {
  const state = quickstartConversationState(secondTurn, 'running', false);
  expect(state.running).toBe(true);
  expect(state.complete).toBe(false);
  expect(state.waiting).toBe(false);
});

const input: QuickstartSessionEvent = { type: 'user.message' };
const start: QuickstartSessionEvent = {
  type: 'span.model_request_start',
  id: 'current_request',
};
const end: QuickstartSessionEvent = {
  type: 'span.model_request_end',
  model_request_start_id: 'current_request',
};
const queue: QuickstartSessionEvent = { type: 'session.status_rescheduled' };
const reply: QuickstartSessionEvent = {
  type: 'agent.message',
  content: [{ type: 'text', text: 'Reply' }],
};
const idle: QuickstartSessionEvent = {
  type: 'session.status_idle',
  stop_reason: { type: 'end_turn' },
};

test('R1 requeue after a request start wins, then a later request start proves execution', () => {
  expect(quickstartConversationState([input, start, end, queue], 'running', false).waiting).toBe(true);
  expect(quickstartConversationState([input, start, end, queue, start], 'running', false).waiting).toBe(false);
});

test('R1 completed request still waits for turn convergence instead of returning to queued', () => {
  const state = quickstartConversationState([input, start, end], 'idle', false);
  expect(state.waiting).toBe(false);
  expect(state.running).toBe(true);
  expect(state.complete).toBe(false);
});

test('R1 old spans and old reply text do not start a new input or resumed tool turn', () => {
  const previous = [input, start, end, reply, idle];
  expect(quickstartConversationState([...previous, input], 'running', false).waiting).toBe(true);
  expect(quickstartConversationState([...previous, { type: 'user.tool_confirmation' }], 'running', false).waiting).toBe(
    true,
  );
  expect(quickstartConversationState([input, queue, reply], 'running', false).waiting).toBe(true);
});

test('R1 confirmation and final states win over execution evidence', () => {
  const confirmation = quickstartConversationState(
    [input, start, end, { type: 'session.status_idle', stop_reason: { type: 'requires_action' } }],
    'idle',
    true,
  );
  expect(confirmation.waiting).toBe(false);
  expect(confirmation.complete).toBe(false);
  const completed = quickstartConversationState([input, start, end, reply, idle], 'idle', false);
  expect(completed.waiting).toBe(false);
  expect(completed.running).toBe(false);
  expect(completed.complete).toBe(true);
  const failed = quickstartConversationState(
    [input, start, end, { type: 'session.status_idle', stop_reason: { type: 'error' } }],
    'idle',
    false,
  );
  expect(failed.failedTurn).toBe(true);
  expect(failed.waiting).toBe(false);
  expect(failed.complete).toBe(false);
  const interrupted = quickstartConversationState([input, start, { type: 'user.interrupt' }, idle], 'idle', false);
  expect(interrupted.stopped).toBe(true);
  expect(interrupted.waiting).toBe(false);
  expect(interrupted.complete).toBe(false);
  const terminated = quickstartConversationState([input, start], 'terminated', false);
  expect(terminated.running).toBe(false);
  expect(terminated.waiting).toBe(false);
});
