import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { type QuickstartSessionEvent } from '../types';
import { findActiveAwaitingToolCall } from './sessionDetailModel';
import { buildSessionEventEntries, latestRequiresActionEventIDs } from './sessionTraceModel';
import { SessionRequiresActionCard } from './SessionRequiresActionCard';

const { cleanup, render, screen, fireEvent, waitFor } = await import('@testing-library/react');

afterEach(cleanup);

test('child approval remains available while the parent is running', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_approval');
  const events: QuickstartSessionEvent[] = [
    { id: 'parent-running', type: 'session.status_running' },
    {
      id: 'child-write',
      type: 'agent.tool_use',
      name: 'write',
      evaluated_permission: 'ask',
      session_thread_id: 'child',
      input: { file_path: '/tmp/approval', content: 'test' },
    },
    {
      id: 'mirror-write',
      type: 'agent.tool_use',
      name: 'write',
      evaluated_permission: 'ask',
      session_thread_id: 'child',
      input: { file_path: '/tmp/approval', content: 'test' },
    },
    {
      id: 'child-waiting',
      type: 'session.thread_status_idle',
      session_thread_id: 'child',
      stop_reason: { type: 'requires_action', event_ids: ['mirror-write'] },
    },
  ];
  const pending = latestRequiresActionEventIDs(events);
  const toolCall = findActiveAwaitingToolCall(buildSessionEventEntries(events, 'transcript'), pending);
  const confirmations: unknown[] = [];
  render(
    <div>
      {toolCall && (
        <SessionRequiresActionCard
          toolCall={toolCall}
          onConfirm={async (input) => {
            confirmations.push(input);
          }}
        />
      )}
    </div>,
  );
  expect(screen.queryByTestId('tool-allow-button')).not.toBeNull();
  const deny = screen.getByTestId('tool-deny-button');
  fireEvent.click(deny);
  await waitFor(() =>
    expect(confirmations).toEqual([{ toolUseId: 'mirror-write', result: 'deny', sessionThreadId: 'child' }]),
  );
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
