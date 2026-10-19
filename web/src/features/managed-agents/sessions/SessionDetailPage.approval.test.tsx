import { afterEach, expect, test } from 'bun:test';
import {
  cleanup,
  fireEvent,
  mockManagedResourceApi,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  waitFor,
  within,
} from '../ManagedAgentsPage.test-utils';

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('approval follows the selected child and does not fall back to another pending child', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456?lane=sthr_reporter123456');
  const api = mockManagedResourceApi();
  api.resources.sessionThreads.splice(2);
  api.resources.sessionThreads.forEach((thread, index) => {
    thread.role = 'Alpha';
    thread.created_at = new Date(Date.now() - 2000 + index).toISOString();
  });
  api.resources.sessionEvents.length = 0;
  for (const [index, thread] of api.resources.sessionThreads.entries()) {
    const toolId = `write-${index}`;
    const createdAt = new Date(Date.now() - 1000 + index).toISOString();
    const tool = {
      id: toolId,
      primary_event_id: `mirror-${toolId}`,
      type: 'agent.tool_use',
      name: 'Write',
      evaluated_permission: 'ask',
      session_thread_id: thread.id,
      input: { file_path: index === 0 ? '/tmp/bb435-rejected.txt' : '/tmp/other-child.txt', content: 'test' },
      created_at: createdAt,
      processed_at: createdAt,
    };
    const waiting = {
      id: `waiting-${index}`,
      type: 'session.thread_status_idle',
      session_thread_id: thread.id,
      stop_reason: { type: 'requires_action', event_ids: [`mirror-${toolId}`] },
      created_at: createdAt,
      processed_at: createdAt,
    };
    api.resources.sessionEvents.push({ ...tool, id: `mirror-${toolId}`, original_event_id: toolId }, waiting);
    api.resources.sessionThreadEvents[thread.id] = [tool, waiting];
  }

  renderManagedAgentsPage('sessions');
  await screen.findByTestId('session-tool-approval-card');
  fireEvent.click(screen.getByRole('tab', { name: 'Alpha 2' }));
  await waitFor(() => expect(window.location.search).toContain('lane=sthr_analyst123456'));
  await waitFor(() => expect(document.body.textContent?.includes('other-child.txt')).toBe(true));
  fireEvent.click(screen.getByRole('tab', { name: 'Alpha' }));
  await waitFor(() => expect(window.location.search).toContain('lane=sthr_reporter123456'));
  const firstCard = screen.getByTestId('session-tool-approval-card');
  expect(within(firstCard).getByText('Alpha')).toBeTruthy();
  expect(within(firstCard).getByText('/tmp/bb435-rejected.txt')).toBeTruthy();
  fireEvent.click(screen.getByTestId('tool-deny-button'));
  await waitFor(() =>
    expect(api.requests.filter((request) => request.method === 'POST').map((request) => request.body)).toEqual([
      {
        events: [
          {
            type: 'user.tool_confirmation',
            tool_use_id: 'write-0',
            result: 'deny',
            session_thread_id: 'sthr_reporter123456',
          },
        ],
      },
    ]),
  );
  await waitFor(() => expect(Boolean(screen.queryByTestId('session-tool-approval-card'))).toBe(false));

  fireEvent.click(screen.getByRole('tab', { name: 'Orchestrator' }));
  expect(within(await screen.findByTestId('session-tool-approval-card')).getByText('Alpha 2')).toBeTruthy();
  fireEvent.click(screen.getByRole('tab', { name: 'Alpha 2' }));
  const card = await screen.findByTestId('session-tool-approval-card');
  expect(within(card).getByText('Alpha 2')).toBeTruthy();
  expect(within(card).getByText('Write')).toBeTruthy();
  expect(within(card).getByText('/tmp/other-child.txt')).toBeTruthy();
  fireEvent.click(within(card).getByTestId('tool-allow-button'));
  await waitFor(() =>
    expect(api.requests.filter((request) => request.method === 'POST').at(-1)?.body).toEqual({
      events: [
        {
          type: 'user.tool_confirmation',
          tool_use_id: 'write-1',
          result: 'allow',
          session_thread_id: 'sthr_analyst123456',
        },
      ],
    }),
  );
});
