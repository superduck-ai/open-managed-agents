import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import { type QuickstartSessionEvent } from '../types';
import { mergeSessionEventsById } from '../api';
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
      primary_event_id: 'mirror-write',
      type: 'agent.tool_use',
      name: 'write',
      evaluated_permission: 'ask',
      session_thread_id: 'child',
      input: { file_path: '/tmp/approval', content: 'test' },
    },
    {
      id: 'mirror-write',
      original_event_id: 'child-write',
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
  const projected = mergeSessionEventsById(events);
  const pending = latestRequiresActionEventIDs(projected);
  const toolCall = findActiveAwaitingToolCall(buildSessionEventEntries(projected, 'transcript'), pending);
  const confirmations: unknown[] = [];
  render(
    <div>
      {toolCall && (
        <SessionRequiresActionCard
          toolCall={toolCall}
          threadLabel="Alpha R"
          onConfirm={async (input) => {
            confirmations.push(input);
          }}
        />
      )}
    </div>,
  );
  expect(screen.queryByTestId('tool-allow-button')).not.toBeNull();
  expect(screen.getByText('Alpha R')).not.toBeNull();
  expect(screen.getByTestId('session-tool-approval-card').textContent).toContain('/tmp/approval');
  const deny = screen.getByTestId('tool-deny-button');
  fireEvent.click(deny);
  await waitFor(() =>
    expect(confirmations).toEqual([{ toolUseId: 'child-write', result: 'deny', sessionThreadId: 'child' }]),
  );
});
