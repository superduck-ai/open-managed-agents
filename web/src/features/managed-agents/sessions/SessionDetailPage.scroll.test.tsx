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
