import { afterEach, expect, mock, test } from 'bun:test';
import {
  act,
  cleanup,
  fireEvent,
  mockManagedResourceApi,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  waitFor,
} from '../ManagedAgentsPage.test-utils';

const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('sending from older history resumes following the latest session message', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  const api = mockManagedResourceApi();
  api.resources.sessions[0].status = 'idle';
  api.resources.sessionThreads.length = 0;
  api.resources.sessionEvents.length = 0;
  api.resources.sessionEvents.push({
    id: 'evt_earlier',
    type: 'user.message',
    processed_at: new Date(Date.now() - 60_000).toISOString(),
    content: [{ type: 'text', text: 'Earlier message' }],
  });

  renderManagedAgentsPage('sessions');
  const viewport = await screen.findByTestId('session-trace-list-pane');
  await screen.findByText('Earlier message');
  Object.defineProperties(viewport, {
    clientHeight: { configurable: true, value: 200 },
    scrollHeight: { configurable: true, value: 900 },
  });
  const scrollTo = mock(({ top }: ScrollToOptions) => {
    viewport.scrollTop = Number(top);
  });
  Object.defineProperty(viewport, 'scrollTo', { configurable: true, value: scrollTo });
  viewport.scrollTop = 0;
  fireEvent.wheel(viewport, { deltaY: -20 });
  await act(async () => await new Promise<void>((resolve) => requestAnimationFrame(() => resolve())));

  fireEvent.change(screen.getByRole('textbox', { name: 'Message' }), { target: { value: 'New message' } });
  fireEvent.submit(screen.getByTestId('session-message-composer'));
  await screen.findByText('New message');
  await waitFor(() => expect(scrollTo).toHaveBeenCalledWith({ top: 700, behavior: 'auto' }));
});

function mockOutputSession() {
  const api = mockManagedResourceApi();
  api.resources.sessions[0].status = 'idle';
  api.resources.sessionEvents.length = 0;
  api.resources.sessionEvents.push(
    {
      id: 'evt_output_ready',
      type: 'agent.message',
      processed_at: new Date().toISOString(),
      content: [{ type: 'text', text: 'Your result is ready.' }],
    },
    {
      id: 'evt_output_idle',
      type: 'session.status_idle',
      processed_at: new Date().toISOString(),
      stop_reason: { type: 'end_turn' },
    },
  );
  const original = globalThis.fetch;
  globalThis.fetch = async (input, init) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    return url.pathname === '/v1/files' && url.searchParams.get('scope_id') === 'sesn_one123456'
      ? Response.json({
          data: [
            {
              id: 'file_output',
              type: 'file',
              filename: 'deliverable.csv',
              mime_type: 'text/csv',
              size_bytes: 42,
              downloadable: true,
              created_at: new Date().toISOString(),
            },
          ],
          has_more: false,
        })
      : original(input, init);
  };
  return api;
}

test('keeps downloadable output cards after the transcript when the inspector is closed', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  const api = mockOutputSession();
  api.resources.sessionThreads.length = 0;
  renderManagedAgentsPage('sessions');
  const output = await screen.findByTestId('session-output-files');
  const message = await screen.findByText('Your result is ready.');
  expect(output.closest('[data-testid="session-trace-column"]')).toBeTruthy();
  expect(message.compareDocumentPosition(output) & Node.DOCUMENT_POSITION_FOLLOWING).toBeGreaterThan(0);
  fireEvent.click(screen.getByTestId('session-inspector').querySelector('[data-inspector-close]')!);
  expect(screen.getByRole('button', { name: 'Download deliverable.csv' }).hasAttribute('disabled')).toBe(false);
  const search = screen.getByTestId('session-trace-toolbar').querySelector('input')!;
  fireEvent.change(search, { target: { value: 'missing message' } });
  await waitFor(() => expect(screen.queryByTestId('session-output-files')).toBeNull());
  fireEvent.change(search, { target: { value: '' } });
  await screen.findByTestId('session-output-files');
});

test('shows session-wide output cards on the main lane while keeping child transcripts scoped', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  mockOutputSession();
  renderManagedAgentsPage('sessions');
  await screen.findByTestId('session-output-files');
  const main = screen.getByTestId('lane-tab-strip').querySelector('[role="tab"]')!;
  fireEvent.click(screen.getByRole('tab', { name: 'analyst' }));
  await waitFor(() => expect(screen.queryByTestId('session-output-files')).toBeNull());
  fireEvent.click(main);
  await screen.findByTestId('session-output-files');
});
