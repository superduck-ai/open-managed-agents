import { afterEach, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../../../shared/i18n';
import { resetTestDom } from '../../../test/setup';
import type { QuickstartSessionEvent, SessionApiResponse } from '../types';
import { SessionQuickView } from './SessionQuickView';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const session: SessionApiResponse = {
  id: 'sesn_preview',
  type: 'session',
  agent: { type: 'agent', id: 'agent_preview' },
  environment_id: 'env_preview',
  archived_at: null,
  created_at: '2026-10-15T03:09:00Z',
  updated_at: '2026-10-15T03:09:00Z',
  resources: [],
  status: 'idle',
};

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function showPreview(events: QuickstartSessionEvent[], failed = false) {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
  const fetchMock = mock(async (input: RequestInfo | URL) => {
    if (failed) return Response.json({ error: { message: 'Preview unavailable' } }, { status: 500 });
    return Response.json(String(input).includes('/events?') ? { data: [...events].reverse() } : session);
  });
  globalThis.fetch = fetchMock as typeof fetch;
  render(
    <I18nProvider initialLocale="en">
      <SessionQuickView session={session} workspaceId="default" onClose={() => {}} />
    </I18nProvider>,
  );
  return fetchMock;
}

test('reports fetch failures without showing an empty conversation and allows refresh', async () => {
  const fetchMock = showPreview([], true);
  expect(await screen.findByText('Could not list session events (500)')).toBeTruthy();
  expect(
    screen.queryByText('No conversation content in the latest 20 events. Open the session for full history.'),
  ).toBeNull();
  fetchMock.mockImplementation(async (input) =>
    Response.json(String(input).includes('/events?') ? { data: [] } : session),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  expect(await screen.findByText('No activity yet. Open the conversation to send the first message.')).toBeTruthy();
});

test('renders readable Markdown and tool summaries, collapses thinking, and hides bookkeeping', async () => {
  showPreview([
    { id: 'user', type: 'user.message', content: [{ type: 'text', text: 'Create **documents**' }] },
    { id: 'model', type: 'span.model_request_start', model: 'kimi-k2.5' },
    { id: 'thinking', type: 'agent.thinking', content: [{ type: 'thinking', thinking: 'Plan the files' }] },
    { id: 'tool', type: 'agent.tool_use', name: 'write', input: { path: 'hello.md', content: '# Hello' } },
    { id: 'result', type: 'agent.tool_result', tool_use_id: 'tool', content: [{ type: 'text', text: 'Written' }] },
    { id: 'reply', type: 'agent.message', content: [{ type: 'text', text: '## Done\n\nCreated the files.' }] },
    { id: 'idle', type: 'session.thread_status_idle', session_thread_id: 'thread' },
    { id: 'usage', type: 'session.usage', usage: { input_tokens: 24, output_tokens: 526 } },
  ]);
  expect(await screen.findByRole('heading', { name: 'Done' })).toBeTruthy();
  expect(screen.getByText('documents').tagName).toBe('STRONG');
  expect(screen.getByText('hello.md')).toBeTruthy();
  expect(screen.queryByText('Plan the files')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Thinking' }));
  expect(await screen.findByText('Plan the files')).toBeTruthy();
  const panel = screen.getByRole('dialog');
  expect(panel.textContent).not.toContain('session.usage');
  expect(panel.textContent).not.toContain('span.model_request_start');
  expect(panel.textContent).not.toContain('session.thread_status_idle');
  expect(panel.textContent).not.toContain('"input"');
  expect(panel.querySelector('.divide-y')).toBeNull();
  expect(screen.getByRole('button', { name: 'Open conversation' })).toBeTruthy();
});

test('keeps errors visible and distinguishes filtered activity from an unused session', async () => {
  const fetchMock = showPreview([{ id: 'error', type: 'session.error', error: { message: 'Tool execution failed' } }]);
  expect(await screen.findByText('Tool execution failed')).toBeTruthy();
  fetchMock.mockImplementation(async (input) =>
    Response.json(String(input).includes('/events?') ? { data: [{ id: 'usage', type: 'session.usage' }] } : session),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Refresh' }));
  await waitFor(() => expect(screen.queryByText('Tool execution failed')).toBeNull());
  expect(
    await screen.findByText('No conversation content in the latest 20 events. Open the session for full history.'),
  ).toBeTruthy();
});

test('shows failed tool outcomes and model errors without dumping payloads', async () => {
  showPreview([
    { id: 'write', type: 'agent.tool_use', name: 'write', input: { path: 'failed.md' } },
    {
      id: 'write-result',
      type: 'agent.tool_result',
      tool_use_id: 'write',
      is_error: true,
      content: [{ type: 'text', text: 'Permission denied' }],
    },
    { id: 'model-error', type: 'span.model_request_end', is_error: true, error: { message: 'Gateway unavailable' } },
  ]);
  expect(await screen.findByText('Failed')).toBeTruthy();
  expect(screen.getByText('failed.md')).toBeTruthy();
  expect(screen.getByRole('dialog').textContent).toContain('Gateway unavailable');
  expect(screen.getByRole('dialog').textContent).not.toContain('"is_error"');
});
