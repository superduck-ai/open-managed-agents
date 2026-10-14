import { describe, expect, test } from 'bun:test';
import type { QuickstartSessionEvent } from '../types';
import { buildSessionEventEntries, sessionToolLifecycle } from './sessionTraceModel';
import { buildInspectorToolRows, buildInspectorToolTotals } from './sessionInspectorModel';
import {
  interruptedSessionModelRequestIds,
  latestSessionIdlePresentationState,
  sessionEventPresentationState,
  sessionToolResultWasInterrupted,
} from './sessionExecutionState';

const processedAt = '2026-10-14T06:45:48.000Z';
const start = { id: 'request', type: 'span.model_request_start', processed_at: processedAt };
const interrupt = { id: 'interrupt', type: 'user.interrupt', processed_at: processedAt };
const end = {
  id: 'end',
  type: 'span.model_request_end',
  model_request_start_id: start.id,
  processed_at: processedAt,
  is_error: true,
  error: { type: 'cancelled' },
};
const idle = { id: 'idle', type: 'session.status_idle', processed_at: processedAt, stop_reason: { type: 'end_turn' } };

describe('session execution state', () => {
  test.each([
    { name: 'processed user stop', events: [start, interrupt, end], stopped: true },
    { name: 'idle before end', events: [start, interrupt, idle, end], stopped: true },
    { name: 'cancelled without user stop', events: [start, end], stopped: false },
    { name: 'stop not processed', events: [start, { ...interrupt, processed_at: null }, end], stopped: false },
    { name: 'different thread', events: [start, { ...interrupt, session_thread_id: 'child' }, end], stopped: false },
    {
      name: 'global stop affects child',
      events: [{ ...start, session_thread_id: 'child' }, interrupt, { ...end, session_thread_id: 'child' }],
      stopped: true,
    },
    { name: 'new request after stop', events: [interrupt, start, end], stopped: false },
    {
      name: 'missing request reference',
      events: [start, interrupt, { ...end, model_request_start_id: '' }],
      stopped: false,
    },
    {
      name: 'provider failure after stop',
      events: [start, interrupt, { ...end, error: { type: 'http_error' } }],
      stopped: false,
    },
    {
      name: 'previous turn stopped',
      events: [
        start,
        interrupt,
        end,
        idle,
        { id: 'new-turn', type: 'session.status_running' },
        { ...start, id: 'new-request' },
        { ...end, id: 'new-end', model_request_start_id: 'new-request' },
      ],
      stopped: false,
      endId: 'new-end',
    },
  ])('$name', ({ events, stopped, endId = 'end' }) => {
    expect(interruptedSessionModelRequestIds(events).has(endId)).toBe(stopped);
  });

  test('normalizes presentation without changing raw diagnostic events', () => {
    const events = [start, interrupt, end, idle];
    const entries = buildSessionEventEntries(events, 'transcript', 0, undefined, { platformTranscriptFiltering: true });
    const requestEnd = entries.find((entry) => 'event' in entry && entry.event.id === end.id);
    expect(requestEnd?.isError).toBe(false);
    expect(requestEnd && 'traceEntry' in requestEnd && requestEnd.traceEntry.state).toBe('interrupted');
    expect(end.is_error).toBe(true);
    expect(end.error.type).toBe('cancelled');
    const debug = buildSessionEventEntries(events, 'debug').find(
      (entry) => 'event' in entry && entry.event.id === end.id,
    );
    expect(debug?.isError).toBe(true);
  });

  test.each([
    [{ ...interrupt, processed_at: null }, 'stopping'],
    [interrupt, 'stopped'],
    [{ id: 'queued-message', type: 'user.message', processed_at: null }, 'queued'],
    [{ ...idle, stop_reason: { type: 'requires_action' } }, 'awaiting_action'],
    [{ ...idle, stop_reason: { type: 'budget_reached' } }, 'budget_reached'],
    [{ ...idle, stop_reason: { type: 'retries_exhausted' } }, 'failed'],
    [{ type: 'session.status_rescheduled' }, 'rescheduled'],
    [{ type: 'session.status_terminated' }, 'terminated'],
    [{ ...end, error: undefined, is_error: false }, 'completed'],
    [{ ...end, error: { type: 'observation_limit' }, is_error: false }, 'completed'],
    [end, 'failed'],
  ] as const)('maps %j to %s', (event, state) => {
    expect(sessionEventPresentationState(event as QuickstartSessionEvent, new Set())).toBe(state);
  });

  test('end_turn alone is not user stop and resumed sessions clear the idle reason', () => {
    expect(sessionEventPresentationState(idle, new Set())).toBeUndefined();
    expect(
      latestSessionIdlePresentationState([
        { ...idle, stop_reason: { type: 'budget_reached' } },
        { type: 'session.status_running' },
      ]),
    ).toBeUndefined();
  });

  test.each([
    'Tool execution was interrupted before completion. Please retry.',
    'Tool execution was interrupted by the user.',
  ])('recognizes the Claude interruption result %s', (text) => {
    const result = {
      id: 'result',
      type: 'agent.tool_result',
      tool_use_id: 'tool',
      is_error: true,
      processed_at: processedAt,
      content: [{ type: 'text', text }],
    };
    const tool = {
      id: 'tool',
      type: 'agent.tool_use',
      name: 'Bash',
      processed_at: processedAt,
      input: { command: 'pwd' },
    };
    expect(sessionToolResultWasInterrupted(result)).toBe(true);
    expect(sessionToolLifecycle(tool, result)).toBe('interrupted');
    const rows = buildInspectorToolRows([tool, result], null);
    expect(rows[0]?.failed).toBe(0);
    expect(rows[0]?.calls[0]?.isError).toBe(false);
    expect(buildInspectorToolTotals(rows)).toMatchObject({ interrupted: 1, completed: 0, failed: 0, inFlight: 0 });
  });

  test('keeps arbitrary tool failures distinct from interruption and denial', () => {
    const tool = { type: 'agent.tool_use', name: 'Bash', input: {} };
    const result = {
      is_error: true,
      content: [{ type: 'text', text: 'Operation failed after user interrupted another task.' }],
    };
    expect(sessionToolResultWasInterrupted(result)).toBe(false);
    expect(sessionToolLifecycle(tool, result)).toBe('failed');
    expect(sessionToolLifecycle({ ...tool, evaluated_permission: 'deny' }, result)).toBe('denied');
    expect(sessionToolLifecycle({ ...tool, evaluated_permission: 'ask' })).toBe('awaiting_approval');
    expect(
      sessionToolLifecycle({ ...tool, evaluated_permission: 'ask' }, undefined, {
        type: 'user.tool_confirmation',
        result: 'allow',
        processed_at: null,
      }),
    ).toBe('awaiting_approval');
    expect(sessionToolLifecycle({ ...tool, type: 'agent.custom_tool_use' })).toBe('awaiting_result');
    expect(sessionToolLifecycle(tool, { is_error: false, content: 'Done.' })).toBe('completed');
  });
});
