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

test.each([
  { name: 'pending global stop', processed: false, threadId: undefined, errorType: 'cancelled', state: 'failed' },
  {
    name: 'another thread stop',
    processed: true,
    threadId: 'sthr_reporter123456',
    errorType: 'cancelled',
    state: 'failed',
  },
  {
    name: 'provider failure after global stop',
    processed: true,
    threadId: undefined,
    errorType: 'http_error',
    state: 'failed',
  },
  { name: 'processed global stop', processed: true, threadId: undefined, errorType: 'cancelled', state: 'interrupted' },
  {
    name: 'processed child stop',
    processed: true,
    threadId: 'sthr_analyst123456',
    errorType: 'cancelled',
    state: 'interrupted',
  },
])('renders $name through the actual child lane', async ({ processed, threadId, errorType, state }) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  const api = mockManagedResourceApi();
  api.resources.sessions[0].status = 'idle';
  api.resources.sessionEvents.length = 0;
  Object.values(api.resources.sessionThreadEvents).forEach((events) => (events.length = 0));
  api.resources.sessionEvents.push({
    id: 'global-stop',
    type: 'user.interrupt',
    created_at: '2026-10-14T06:45:02Z',
    processed_at: processed ? '2026-10-14T06:45:02Z' : null,
    session_thread_id: threadId,
  });
  api.resources.sessionEvents.unshift({
    id: 'main-start',
    type: 'span.model_request_start',
    processed_at: '2026-10-14T06:45:00Z',
  });
  api.resources.sessionEvents.push({
    id: 'main-end',
    type: 'span.model_request_end',
    model_request_start_id: 'main-start',
    processed_at: '2026-10-14T06:45:03Z',
    is_error: true,
    error: { type: errorType },
  });
  api.resources.sessionThreadEvents.sthr_analyst123456.push(
    {
      session_thread_id: 'sthr_analyst123456',
      id: 'child-start',
      type: 'span.model_request_start',
      processed_at: '2026-10-14T06:45:00Z',
    },
    {
      session_thread_id: 'sthr_analyst123456',
      id: 'child-answer',
      type: 'agent.message',
      processed_at: '2026-10-14T06:45:01Z',
      content: 'Child partial answer',
    },
    {
      id: 'child-end',
      session_thread_id: 'sthr_analyst123456',
      type: 'span.model_request_end',
      model_request_start_id: 'child-start',
      processed_at: '2026-10-14T06:45:03Z',
      is_error: true,
      error: { type: errorType },
    },
    {
      session_thread_id: 'sthr_analyst123456',
      id: 'child-idle',
      type: 'session.thread_status_idle',
      processed_at: '2026-10-14T06:45:04Z',
    },
    {
      session_thread_id: 'sthr_analyst123456',
      id: 'later-start',
      type: 'span.model_request_start',
      processed_at: '2026-10-14T06:45:05Z',
    },
    {
      session_thread_id: 'sthr_analyst123456',
      id: 'later-end',
      type: 'span.model_request_end',
      model_request_start_id: 'later-start',
      processed_at: '2026-10-14T06:45:06Z',
      is_error: true,
      error: { type: 'cancelled' },
    },
  );
  renderManagedAgentsPage('sessions');
  const mainState = processed && !threadId && errorType === 'cancelled' ? 'interrupted' : 'failed';
  await screen.findByTestId('session-trace-list-pane');
  await waitFor(() => {
    if (mainState === 'interrupted') {
      expect(document.querySelector('[data-event-id^="global-stop-"] [data-execution-state="stopping"]')).toBeTruthy();
      expect(document.querySelector('[data-event-id^="main-end-"]')).toBeNull();
    } else {
      expect(document.querySelector(`[data-event-id^="main-end-"] [data-execution-state="${mainState}"]`)).toBeTruthy();
    }
  });
  fireEvent.click(await screen.findByRole('tab', { name: 'analyst' }));
  await screen.findByText('Child partial answer');
  await waitFor(() => {
    const row = document.querySelector('[data-event-id^="child-end-"]');
    if (state === 'interrupted' && threadId === 'sthr_analyst123456') {
      expect(row === null).toBe(true);
      expect(document.querySelector('[data-event-id^="global-stop-"] [data-execution-state="stopped"]')).toBeTruthy();
      return;
    }
    expect(row?.querySelector(`[data-execution-state="${state}"]`)).toBeTruthy();
    expect(row?.querySelector('[data-execution-state="interrupted"]') !== null).toBe(state === 'interrupted');
  });
  expect(document.querySelector('[data-event-id^="later-end-"] [data-execution-state="failed"]')).toBeTruthy();
  expect(document.querySelector('[data-event-id^="global-stop-"]') !== null).toBe(threadId === 'sthr_analyst123456');
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

test('shows a budget pause and disables ordinary messages while retaining the draft', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  const api = mockManagedResourceApi();
  api.resources.sessions[0].status = 'idle';
  api.resources.sessionThreads.length = 0;
  api.resources.sessionEvents.length = 0;
  api.resources.sessionEvents.push({
    id: 'budget-pause',
    type: 'session.status_idle',
    processed_at: new Date().toISOString(),
    stop_reason: { type: 'budget_reached' },
  });
  renderManagedAgentsPage('sessions');
  await screen.findByText('Budget reached; increase or remove the budget to continue.');
  const input = screen.getByRole('textbox', { name: 'Message' }) as HTMLTextAreaElement;
  fireEvent.change(input, { target: { value: 'Keep my draft' } });
  expect(screen.getByRole('button', { name: 'Send message' }).hasAttribute('disabled')).toBe(true);
  fireEvent.submit(screen.getByTestId('session-message-composer'));
  expect(input.value).toBe('Keep my draft');
  expect(api.requests.some((request) => request.method === 'POST' && request.url.includes('/events'))).toBe(false);
});

test('keeps downloadable output cards separate from the completed reply when the inspector is closed', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  const api = mockOutputSession();
  api.resources.sessionThreads.length = 0;
  renderManagedAgentsPage('sessions');
  const output = await screen.findByTestId('session-output-files');
  const message = await screen.findByText('Your result is ready.');
  expect(output.closest('[data-transcript-block]')).toBeNull();
  expect(output.closest('[data-transcript-iteration]')).toBeNull();
  expect(output.previousElementSibling).toBe(message.closest('[data-transcript-block]'));
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

test('shows a late file automatically after completion without visiting the resources inspector', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions/sesn_one123456');
  const api = mockOutputSession();
  api.resources.sessionThreads.length = 0;
  api.resources.sessions[0].updated_at = new Date().toISOString();
  const original = globalThis.fetch;
  let published = false;
  globalThis.fetch = async (input, init) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    return url.pathname === '/v1/files' && url.searchParams.has('scope_id') && !published
      ? Response.json({ data: [], has_more: false })
      : original(input, init);
  };
  renderManagedAgentsPage('sessions');
  await screen.findByText('Your result is ready.');
  expect(screen.queryByTestId('session-output-files')).toBeNull();
  published = true;
  expect(await screen.findByRole('button', { name: 'Download deliverable.csv' }, { timeout: 2500 })).toBeTruthy();
  expect(screen.getByRole('tab', { name: 'Resources', exact: true }).getAttribute('aria-selected')).toBe('false');
});
