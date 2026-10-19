import { expect, test } from 'bun:test';
import { mergeSessionEventsById } from '../api';
import { type QuickstartSessionEvent } from '../types';
import { buildSessionEventEntries, latestRequiresActionEventIDs } from './sessionTraceModel';
import { findActiveAwaitingToolCall } from './sessionDetailModel';

const child: QuickstartSessionEvent = {
  id: 'sevt_child',
  type: 'agent.tool_use',
  name: 'write',
  session_thread_id: 'sthr_child',
  evaluated_permission: 'ask',
  primary_event_id: 'sevt_mirror',
  input: { file_path: '/tmp/shared.txt', content: 'proof' },
};
const mirror: QuickstartSessionEvent = { ...child, id: 'sevt_mirror', original_event_id: 'sevt_child' };

function calls(events: QuickstartSessionEvent[]) {
  return buildSessionEventEntries(mergeSessionEventsById(events), 'transcript').flatMap((entry) =>
    entry.kind === 'tool_call' ? [entry] : entry.kind === 'tool_batch' ? entry.calls : [],
  );
}

test('a completed child tool and its primary mirror display as one completed call', () => {
  for (const tools of [
    [mirror, child],
    [child, mirror],
  ]) {
    const entries = calls([
      ...tools,
      {
        id: 'sevt_confirmation',
        type: 'user.tool_confirmation',
        session_thread_id: 'sthr_child',
        tool_use_id: 'sevt_mirror',
        result: 'allow',
      },
      {
        id: 'sevt_result',
        type: 'agent.tool_result',
        session_thread_id: 'sthr_child',
        tool_use_id: 'sevt_child',
        content: [{ type: 'text', text: 'written' }],
      },
      {
        id: 'sevt_idle',
        type: 'session.thread_status_idle',
        session_thread_id: 'sthr_child',
        stop_reason: { type: 'end_turn' },
      },
    ]);
    expect(entries).toHaveLength(1);
    expect(entries[0].event.id).toBe('sevt_child');
    expect(entries[0].lifecycle).toBe('completed');
    expect(entries[0].confirmationEvent?.id).toBe('sevt_confirmation');
    expect(entries[0].resultEvent?.id).toBe('sevt_result');
  }
});

test('a pending mirror remains actionable before and after child history arrives', () => {
  const waiting = {
    id: 'sevt_waiting',
    type: 'session.thread_status_idle',
    session_thread_id: 'sthr_child',
    stop_reason: { type: 'requires_action', event_ids: ['sevt_mirror'] },
  };
  for (const tools of [[mirror], [mirror, child]]) {
    const events = mergeSessionEventsById([...tools, waiting]);
    const pending = latestRequiresActionEventIDs(events);
    const entry = findActiveAwaitingToolCall(buildSessionEventEntries(events, 'transcript'), pending);
    expect(entry?.lifecycle).toBe('awaiting_approval');
    expect(entry?.event.id).toBe('sevt_child');
    expect([...pending]).toEqual(['sevt_child']);
    const confirmed = mergeSessionEventsById([
      ...events,
      {
        id: 'sevt_confirm',
        type: 'user.tool_confirmation',
        tool_use_id: 'sevt_mirror',
        session_thread_id: 'sthr_child',
        result: 'deny',
      },
    ]);
    expect([...latestRequiresActionEventIDs(confirmed)]).toEqual([]);
    expect(calls(confirmed)[0].lifecycle).toBe('denied');
  }
});

test('legacy child history can link an unannotated primary mirror', () => {
  const { original_event_id: originalEventId, primary_event_id: primaryEventId, ...legacyMirror } = mirror;
  expect(originalEventId).toBe('sevt_child');
  expect(primaryEventId).toBe('sevt_mirror');
  expect(calls([legacyMirror, child])).toHaveLength(1);
});

test('parallel thread approvals survive another thread and parent changing state', () => {
  const events: QuickstartSessionEvent[] = [
    { type: 'session.status_idle', stop_reason: { type: 'requires_action', event_ids: ['main-tool'] } },
    {
      type: 'session.thread_status_idle',
      session_thread_id: 'child-a',
      stop_reason: { type: 'requires_action', event_ids: ['tool-a'] },
    },
    {
      type: 'session.thread_status_idle',
      session_thread_id: 'child-b',
      stop_reason: { type: 'requires_action', event_ids: ['tool-b'] },
    },
    { type: 'session.status_running' },
    { type: 'user.tool_confirmation', tool_use_id: 'tool-b', session_thread_id: 'child-b', result: 'deny' },
    { type: 'session.thread_status_running', session_thread_id: 'child-b' },
  ];
  expect([...latestRequiresActionEventIDs(events)]).toEqual(['tool-a']);
  events.push({ type: 'agent.tool_result', tool_use_id: 'tool-a', session_thread_id: 'child-a' });
  expect([...latestRequiresActionEventIDs(events)]).toEqual([]);
});

test('terminated sessions and completed child threads clear their pending approvals', () => {
  const waiting: QuickstartSessionEvent = {
    type: 'session.thread_status_idle',
    session_thread_id: 'child',
    stop_reason: { type: 'requires_action', event_ids: ['tool'] },
  };
  for (const type of ['session.status_terminated', 'session.deleted', 'session.thread_status_terminated']) {
    expect([...latestRequiresActionEventIDs([waiting, { type, session_thread_id: 'child' }])]).toEqual([]);
  }
  expect([
    ...latestRequiresActionEventIDs([
      waiting,
      { type: 'session.thread_status_idle', session_thread_id: 'child', stop_reason: { type: 'end_turn' } },
    ]),
  ]).toEqual([]);
});
