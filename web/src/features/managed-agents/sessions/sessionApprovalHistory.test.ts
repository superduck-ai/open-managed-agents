import { expect, test } from 'bun:test';
import type { QuickstartSessionEvent } from '../types';
import { buildSessionEventEntries, latestRequiresActionEventIDs } from './sessionTraceModel';
import { findActiveAwaitingToolCall } from './sessionDetailModel';

const tool: QuickstartSessionEvent = {
  id: 'search',
  type: 'agent.tool_use',
  name: 'WebSearch',
  evaluated_permission: 'ask',
  input: { query: 'latest model' },
};
const waiting: QuickstartSessionEvent = {
  id: 'waiting',
  type: 'session.status_idle',
  stop_reason: { type: 'requires_action', event_ids: ['search'] },
};

function transcript(events: QuickstartSessionEvent[]) {
  return buildSessionEventEntries(events, 'transcript', 0, undefined, { platformTranscriptFiltering: true });
}

function waitingRows(events: QuickstartSessionEvent[]) {
  return transcript(events).filter((entry) => 'traceEntry' in entry && entry.traceEntry.state === 'awaiting_action');
}

test('pending approval stays actionable without a duplicate waiting status row', () => {
  const events = [tool, waiting];
  expect(waitingRows(events)).toHaveLength(0);
  expect(findActiveAwaitingToolCall(transcript(events), latestRequiresActionEventIDs(events))?.lifecycle).toBe(
    'awaiting_approval',
  );
});

test.each(['allow', 'deny'])(
  'handled %s approval does not leave a waiting status in conversation history',
  (result) => {
    const events = [
      tool,
      waiting,
      { id: 'confirmation', type: 'user.tool_confirmation', tool_use_id: 'search', result },
      ...(result === 'allow'
        ? [{ id: 'result', type: 'agent.tool_result', tool_use_id: 'search', content: 'Found results' }]
        : []),
      { id: 'running', type: 'session.status_running' },
      { id: 'idle', type: 'session.status_idle', stop_reason: { type: 'end_turn' } },
    ];
    expect(waitingRows(events)).toHaveLength(0);
    const entries = transcript(events);
    const call = entries.find((entry) => entry.kind === 'tool_call');
    expect(call?.kind === 'tool_call' && call.lifecycle).toBe(result === 'allow' ? 'completed' : 'denied');
    expect(latestRequiresActionEventIDs(events).size).toBe(0);
    const debug = buildSessionEventEntries(events, 'debug', 0, undefined, { platformTranscriptFiltering: true });
    expect(debug.some((entry) => 'event' in entry && entry.event.id === 'waiting')).toBe(true);
  },
);

test('budget pause remains visible', () => {
  const entries = transcript([{ ...waiting, stop_reason: { type: 'budget_reached' } }]);
  expect(entries.some((entry) => 'traceEntry' in entry && entry.traceEntry.state === 'budget_reached')).toBe(true);
});
