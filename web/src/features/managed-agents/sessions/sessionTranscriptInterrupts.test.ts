import { describe, expect, test } from 'bun:test';
import type { QuickstartSessionEvent } from '../types';
import { buildSessionEventEntries, buildSessionTraceEntries } from './sessionTraceModel';
import { buildSessionTranscriptBlocks } from './sessionTranscriptModel';

const start = { id: 'request', type: 'span.model_request_start', processed_at: '2026-10-15T04:54:37Z' };
const thinking = {
  id: 'thinking',
  type: 'agent.thinking',
  model_request_start_id: start.id,
  processed_at: '2026-10-15T04:54:39Z',
  content: [{ type: 'thinking', thinking: 'Planning the article' }],
};
const stop = { id: 'stop', type: 'user.interrupt', processed_at: '2026-10-15T04:54:43.586055Z' };
const answer = {
  id: 'answer',
  type: 'agent.message',
  model_request_start_id: start.id,
  aborted: true,
  processed_at: '2026-10-15T04:54:43.610Z',
  content: [{ type: 'text', text: 'Partial spring article' }],
};
const end = {
  id: 'end',
  type: 'span.model_request_end',
  model_request_start_id: start.id,
  processed_at: '2026-10-15T04:54:43.710294Z',
  is_error: true,
  error: { type: 'cancelled' },
};
const idle = {
  id: 'idle',
  type: 'session.status_idle',
  processed_at: '2026-10-15T04:54:43.800Z',
  stop_reason: { type: 'end_turn' },
};

function transcript(events: QuickstartSessionEvent[]) {
  return buildSessionEventEntries(events, 'transcript', 0, undefined, { platformTranscriptFiltering: true });
}

describe('interrupted transcript', () => {
  test.each([false, true])(
    'attaches partial output arriving after the model end, idle before output: %s',
    (idleFirst) => {
      const lateAnswer = { ...answer, processed_at: '2026-10-15T04:54:43.900Z' };
      const afterIdle = { ...idle, processed_at: '2026-10-15T04:54:43.950Z' };
      const entries = transcript([
        start,
        thinking,
        stop,
        end,
        ...(idleFirst ? [idle, lateAnswer] : [lateAnswer, afterIdle]),
      ]);
      const blocks = buildSessionTranscriptBlocks(entries);
      expect(blocks.map((block) => block.kind)).toEqual(['agent', 'standalone']);
      const agent = blocks[0];
      expect(agent?.kind === 'agent' && agent.iterations).toHaveLength(1);
      const output = entries.find((entry) => 'event' in entry && entry.event.id === 'answer');
      expect(output).toMatchObject({
        bracketId: 'request',
        bracketOpen: false,
        bracketEndMs: Date.parse(end.processed_at) + 0.294,
      });
    },
  );

  test('orders separately loaded thread history before its stop without changing the input', () => {
    const events = [stop, start, thinking, answer, end, idle];
    const entries = transcript(events);
    expect(buildSessionTranscriptBlocks(entries).map((block) => block.kind)).toEqual(['agent', 'standalone']);
    expect(entries.at(-1)).toMatchObject({ event: { id: 'stop' }, traceEntry: { state: 'stopped' } });
    expect(events[0]).toBe(stop);
    expect(buildSessionEventEntries(events, 'debug')[0]).toMatchObject({ event: { id: 'stop' } });
  });

  test('keeps the stopped footer before an idle gap and the next user message', () => {
    const next = { id: 'next', type: 'user.message', processed_at: '2026-10-15T04:56:00Z', content: 'Continue' };
    const entries = transcript([start, thinking, stop, answer, end, idle, next]);
    expect(entries.slice(-3).map((entry) => ('event' in entry ? entry.event.id : entry.kind))).toEqual([
      'stop',
      'idle_gap',
      'next',
    ]);
  });

  test.each([false, true])('keeps late partial output with its thinking, idle before end: %s', (idleFirst) => {
    const events = [start, thinking, stop, answer, ...(idleFirst ? [idle, end] : [end, idle])];
    const original = structuredClone(events);
    const entries = transcript(events);
    const blocks = buildSessionTranscriptBlocks(entries);
    expect(blocks.map((block) => block.kind)).toEqual(['agent', 'standalone']);
    const agent = blocks[0];
    expect(agent?.kind === 'agent' && agent.iterations).toHaveLength(1);
    expect(agent?.kind === 'agent' && agent.iterations[0]?.entries.map((entry) => entry.event?.id)).toEqual([
      'thinking-thinking-0',
      'answer',
    ]);
    expect(entries.at(-1)).toMatchObject({ event: { id: 'stop' }, traceEntry: { state: 'stopped' } });
    expect(entries.some((entry) => 'event' in entry && entry.event.id === 'end')).toBe(false);
    expect(buildSessionTraceEntries(events, 'transcript').find((entry) => entry.event.id === 'end')?.state).toBe(
      'interrupted',
    );
    expect(
      buildSessionEventEntries(events, 'debug').some((entry) => 'event' in entry && entry.event.id === 'end'),
    ).toBe(true);
    expect(events).toEqual(original);
  });

  test.each([null, stop.processed_at])('waits for idle after a stop processed at %s', (processedAt) => {
    const entries = transcript([start, thinking, { ...stop, processed_at: processedAt }, answer, end]);
    expect(entries.at(-1)).toMatchObject({ event: { id: 'stop' }, traceEntry: { state: 'stopping' } });
    if (processedAt === null) expect(entries.some((entry) => entry.isError)).toBe(true);
  });

  test('keeps real failures and the next turn outside the stopped reply', () => {
    const failure = {
      id: 'failure',
      type: 'session.error',
      processed_at: '2026-10-15T04:54:43.750Z',
      error: { message: 'Gateway unavailable' },
    };
    const nextUser = {
      id: 'next-user',
      type: 'user.message',
      processed_at: '2026-10-15T04:55:00Z',
      content: 'Continue',
    };
    const entries = transcript([start, thinking, stop, answer, end, failure, idle, nextUser]);
    expect(entries.find((entry) => 'event' in entry && entry.event.id === 'failure')?.isError).toBe(true);
    expect(entries.slice(-2).map((entry) => 'event' in entry && entry.event.id)).toEqual(['stop', 'next-user']);
  });

  test('does not move a stop into a later run or hide unrelated model errors', () => {
    const nextStart = { ...start, id: 'next-request', processed_at: '2026-10-15T04:54:44Z' };
    const nextEnd = {
      ...end,
      id: 'next-end',
      processed_at: '2026-10-15T04:54:45Z',
      model_request_start_id: nextStart.id,
    };
    const entries = transcript([start, stop, answer, end, nextStart, nextEnd]);
    expect(entries.map((entry) => 'event' in entry && entry.event.id)).toEqual(['answer', 'stop', 'next-end']);
    expect(entries.at(-1)?.isError).toBe(true);
  });
});
