import { describe, expect, test } from 'bun:test';
import {
  type DisplayEventEntry,
  type SessionEventListEntry,
  type SessionEventUsage,
  type ToolBatchEntry,
  type ToolCallEntry,
} from '../types';
import { buildSessionTranscriptBlocks, filterSessionTranscriptBlocks } from './sessionTranscriptModel';
import {
  applyModelRequestBrackets,
  buildSessionEventEntries,
  h,
  latestOpenModelRequest,
  mergeSessionEvents,
  sessionEventElapsedTime,
  sessionTimestampMs,
  sessionTraceEntryFromEvent,
} from './sessionTraceModel';

const EMPTY_USAGE: SessionEventUsage = {
  input: 0,
  output: 0,
  input_tokens: 0,
  output_tokens: 0,
  cache_read_input_tokens: 0,
  cache_creation_input_tokens: 0,
};

describe('sessionTranscriptModel', () => {
  test.each([false, true])('closes a model bracket only after a stop is processed: %s', (processed) => {
    const at = (seconds: number) => `2026-10-14T06:45:0${seconds}Z`;
    const events = [
      { id: 'request', type: 'span.model_request_start', processed_at: at(0) },
      { id: 'answer', type: 'agent.message', processed_at: at(1), content: 'Partial answer' },
      { id: 'stop', type: 'user.interrupt', created_at: at(2), processed_at: processed ? at(2) : null },
      { id: 'error', type: 'session.error', processed_at: at(3), error: { message: 'Agent execution failed.' } },
    ];
    expect(latestOpenModelRequest(events)?.id ?? null).toBe(processed ? null : 'request');
    const entries = buildSessionEventEntries(events, 'transcript', 0, undefined, { platformTranscriptFiltering: true });
    const answer = entries.find((entry) => 'event' in entry && entry.event.id === 'answer');
    expect(answer?.bracketOpen).toBe(!processed);
    if (processed) expect(answer?.bracketEndMs).toBe(Date.parse(at(2)));
    expect(entries.some((entry) => 'event' in entry && entry.event.id === 'error')).toBe(true);
  });

  test('keeps distinct user messages with the same text', () => {
    const first = {
      id: 'sevt_first',
      type: 'user.message',
      processed_at: '2026-08-26T13:13:00Z',
      content: [{ type: 'text', text: 'same' }],
    };
    const second = {
      id: 'sevt_second',
      type: 'user.message',
      processed_at: '2026-08-26T13:13:01Z',
      content: [{ type: 'text', text: 'same' }],
    };

    expect(mergeSessionEvents([first], [second]).map((event) => event.id)).toEqual(['sevt_first', 'sevt_second']);
  });

  test('preserves RFC3339 precision beyond milliseconds', () => {
    expect(sessionTimestampMs('2026-01-01T08:00:00.123456789Z')).toBeCloseTo(
      Date.parse('2026-01-01T08:00:00.123Z') + 0.456789,
      6,
    );
  });

  test('missing processed_at stays unset instead of falling back to event index', () => {
    const streaming = {
      id: 'preview',
      type: 'agent.message',
      processed_at: null,
      is_streaming: true,
      content: 'partial',
    };
    const traceEntry = sessionTraceEntryFromEvent(streaming, 3);
    const displayEvent = h(streaming, 0, undefined, traceEntry);

    expect(traceEntry.createdAtMs).toBe(0);
    expect(traceEntry.relativeTime).toBe('now');
    expect(displayEvent.createdAtMs).toBe(0);
    expect(displayEvent.processedAtMs).toBe(0);
    expect(displayEvent.relativeTime).toBe('now');
    expect(sessionEventElapsedTime(streaming, Date.parse('2026-01-01T08:00:00Z'))).toBe('now');
  });

  test('keeps only the latest model request that has not ended or crossed a turn boundary', () => {
    const open = { id: 'span-open', type: 'span.model_request_start' };

    expect(
      latestOpenModelRequest([
        { id: 'span-closed', type: 'span.model_request_start' },
        { id: 'span-closed-end', type: 'span.model_request_end', model_request_start_id: 'span-closed' },
        open,
      ]),
    ).toBe(open);
    expect(latestOpenModelRequest([open, { id: 'idle', type: 'session.status_idle' }])).toBeNull();
  });

  test('keeps agent entries without a reliable bracket as standalone blocks', () => {
    const message = displayEntry('agent-without-bracket', 'agent');

    const blocks = buildSessionTranscriptBlocks([message]);

    expect(blocks).toHaveLength(1);
    expect(blocks[0]).toMatchObject({
      id: 'standalone-agent-without-bracket',
      kind: 'standalone',
      entry: { id: 'agent-without-bracket' },
    });
  });

  test('does not assign a mixed-bracket tool batch to an iteration', () => {
    const mixedBatch = toolBatchEntry('mixed-batch', [
      toolEntry('mixed-a', 'bracket-a'),
      toolEntry('mixed-b', 'bracket-b'),
    ]);

    const blocks = buildSessionTranscriptBlocks([mixedBatch]);

    expect(blocks).toEqual([{ id: 'standalone-mixed-batch', kind: 'standalone', entry: mixedBatch }]);
  });

  test('builds speaker turns and bracket iterations before user and status boundaries', () => {
    const entries: SessionEventListEntry[] = [
      displayEntry('thinking-1', 'thinking'),
      displayEntry('answer-1', 'agent', { bracketId: 'bracket-1', label: 'Researcher' }),
      toolEntry('tool-1', 'bracket-1'),
      toolBatchEntry('batch-2', [toolEntry('tool-2a', 'bracket-2'), toolEntry('tool-2b', 'bracket-2')]),
      displayEntry('answer-2', 'agent', { bracketId: 'bracket-2', label: 'Researcher' }),
      displayEntry('you-1', 'user'),
      displayEntry('running', 'status_running', { kind: 'status' }),
      displayEntry('answer-3', 'agent', { bracketId: 'bracket-3', label: 'Researcher' }),
    ];

    const blocks = buildSessionTranscriptBlocks(entries);

    expect(
      blocks.map((block) =>
        block.kind === 'agent'
          ? {
              kind: block.kind,
              speaker: block.speakerLabel,
              iterations: block.iterations.map((iteration) => ({
                bracketId: iteration.bracketId,
                entries: iteration.entries.map((entry) => entry.id),
              })),
            }
          : { kind: block.kind, entry: block.entry.id },
      ),
    ).toEqual([
      {
        kind: 'agent',
        speaker: 'Researcher',
        iterations: [
          { bracketId: 'bracket-1', entries: ['thinking-1', 'answer-1', 'tool-1'] },
          { bracketId: 'bracket-2', entries: ['batch-2', 'answer-2'] },
        ],
      },
      { kind: 'user', entry: 'you-1' },
      { kind: 'standalone', entry: 'running' },
      {
        kind: 'agent',
        speaker: 'Researcher',
        iterations: [{ bracketId: 'bracket-3', entries: ['answer-3'] }],
      },
    ]);
  });

  test('preserves original turn boundaries when filtering hides their user separator', () => {
    const firstAnswer = displayEntry('answer-before-user', 'agent', { bracketId: 'bracket-before' });
    const user = displayEntry('hidden-user', 'user');
    const secondAnswer = displayEntry('answer-after-user', 'agent', { bracketId: 'bracket-after' });

    const blocks = filterSessionTranscriptBlocks(
      buildSessionTranscriptBlocks([firstAnswer, user, secondAnswer]),
      (entry) => entry.id !== user.id,
    );

    expect(blocks.map((block) => block.id)).toEqual(['agent-turn-answer-before-user', 'agent-turn-answer-after-user']);
    expect(blocks.every((block) => block.kind === 'agent')).toBe(true);
  });

  test('uses the model request processed time as the thinking start', () => {
    const thinking = displayEntry('thinking', 'thinking');
    thinking.event.parent_event_id = 'event-message';
    thinking.event.processed_at = '2026-01-01T08:00:04.500Z';
    thinking.processedAtMs = Date.parse(thinking.event.processed_at);

    applyModelRequestBrackets(
      [thinking],
      [
        {
          id: 'model-start',
          type: 'span.model_request_start',
          created_at: '2026-01-01T08:00:00.000Z',
          processed_at: '2026-01-01T08:00:01.000Z',
        },
        { id: 'event-message', type: 'agent.message', processed_at: thinking.event.processed_at },
        {
          id: 'model-end',
          type: 'span.model_request_end',
          model_request_start_id: 'model-start',
          processed_at: '2026-01-01T08:00:05.000Z',
        },
      ],
    );

    expect(thinking.bracketStartMs).toBe(Date.parse('2026-01-01T08:00:01.000Z'));
  });

  test('keeps a model bracket available when idle arrives before its final message', () => {
    const entries = buildSessionEventEntries(
      [
        {
          id: 'model-start',
          type: 'span.model_request_start',
          processed_at: '2026-01-01T08:00:01.000Z',
        },
        {
          id: 'idle',
          type: 'session.status_idle',
          processed_at: '2026-01-01T08:00:04.000Z',
        },
        {
          id: 'answer',
          type: 'agent.message',
          processed_at: '2026-01-01T08:00:04.500Z',
          content: [{ type: 'text', text: 'Final answer' }],
        },
        {
          id: 'model-end',
          type: 'span.model_request_end',
          model_request_start_id: 'model-start',
          processed_at: '2026-01-01T08:00:05.000Z',
          usage: { input_tokens: 12, output_tokens: 4 },
        },
      ],
      'transcript',
      Date.parse('2026-01-01T08:00:01.000Z'),
      undefined,
      { platformTranscriptFiltering: true },
    );
    const answer = entries.find((entry) => entry.kind === 'message');

    expect(answer).toMatchObject({
      bracketId: 'model-start',
      inferenceMs: 4_000,
      usage: { input_tokens: 12, output_tokens: 4 },
    });
  });
});

function displayEntry(
  id: string,
  type: DisplayEventEntry['displayEvent']['type'],
  options: { bracketId?: string; kind?: DisplayEventEntry['kind']; lane?: string; label?: string } = {},
): DisplayEventEntry {
  const event = { id: `event-${id}`, session_thread_id: options.lane ?? 'main' };
  const kind = options.kind ?? 'message';
  return {
    id,
    kind,
    displayEvent: {
      id: `display-${id}`,
      type,
      rawType: `${type}.message`,
      label: options.label ?? (type === 'user' ? 'You' : 'Agent'),
      content: `${id} content`,
      event,
      isStreaming: false,
      isError: false,
      createdAtMs: 1,
      processedAtMs: 1,
      relativeTime: '0:01',
    },
    traceEntry: {
      id: `trace-${id}`,
      type: `${type}.message`,
      family: type === 'user' ? 'user' : 'agent',
      label: options.label ?? (type === 'user' ? 'You' : 'Agent'),
      preview: `${id} content`,
      displayText: `${id} content`,
      displayKind: type === 'thinking' ? 'thinking' : 'prose',
      event,
      createdAtMs: 1,
      relativeTime: '0:01',
      rawEventId: `event-${id}`,
      searchText: id,
      isError: false,
    },
    event,
    type: `${type}.message`,
    rawEventId: `event-${id}`,
    createdAtMs: 1,
    processedAtMs: 1,
    relativeTime: '0:01',
    searchText: id,
    isError: false,
    usage: EMPTY_USAGE,
    inferenceMs: 100,
    executionMs: 0,
    bracketId: options.bracketId,
  };
}

function toolEntry(id: string, bracketId: string, lane = 'main'): ToolCallEntry {
  const base = displayEntry(id, 'tool_use', { lane });
  return {
    ...base,
    kind: 'tool_call',
    name: `tool-${id}`,
    inputPreview: `${id} input`,
    lifecycle: 'completed',
    bracketId,
  } as ToolCallEntry;
}

function toolBatchEntry(id: string, calls: ToolCallEntry[]): ToolBatchEntry {
  const first = calls[0];
  return {
    ...first,
    id,
    kind: 'tool_batch',
    calls,
    toolCounts: calls.map((call) => ({ name: call.name, count: 1 })),
  } as ToolBatchEntry;
}
