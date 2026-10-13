import { afterEach, expect, mock, test } from 'bun:test';
import {
  ManagedAgentsPage,
  fireEvent,
  mockAgentsApi,
  render,
  resetManagedAgentsTestState,
  resetTestDom,
  screen,
  serverAgent,
  waitFor,
  within,
} from '../ManagedAgentsPage.test-utils';

afterEach(resetManagedAgentsTestState);

test('quick view reports retrieval failures and retries without leaving the list', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents');
  mockAgentsApi([serverAgent]);
  const baseFetch = globalThis.fetch;
  let fail = true;
  globalThis.fetch = mock(async (input, init) => {
    if (fail && String(input).includes(`/v1/agents/${serverAgent.id}?`)) {
      return new Response(JSON.stringify({ error: { message: 'Agent unavailable' } }), { status: 404 });
    }
    return baseFetch(input, init);
  }) as typeof fetch;
  render(<ManagedAgentsPage section="agents" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Quick view Server agent' }));
  const panel = screen.getByRole('dialog', { name: 'Agent' });
  expect(await within(panel).findByText('Agent unavailable')).toBeTruthy();
  fail = false;
  fireEvent.click(within(panel).getByRole('button', { name: 'Retry' }));
  expect(await within(panel).findByRole('heading', { name: 'Server agent' })).toBeTruthy();
  expect(window.location.pathname).toBe('/workspaces/default/agents');
});

test('failed session creation preserves title and selected agent for retry', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents');
  mockAgentsApi([serverAgent]);
  const baseFetch = globalThis.fetch;
  globalThis.fetch = mock(async (input, init) => {
    if (String(input).includes('/v1/sessions?') && init?.method === 'POST') {
      return new Response(JSON.stringify({ error: { message: 'Session unavailable' } }), { status: 500 });
    }
    return baseFetch(input, init);
  }) as typeof fetch;
  render(<ManagedAgentsPage section="agents" />);
  await screen.findByRole('link', { name: 'Server agent' });
  fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Start conversation' }));
  const dialog = screen.getByRole('dialog', { name: 'Create session' });
  fireEvent.change(within(dialog).getByLabelText(/Title/), { target: { value: 'Retry run' } });
  const submit = within(dialog).getByRole('button', { name: 'Create session' });
  await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false));
  fireEvent.click(submit);
  expect((await within(dialog).findByRole('alert')).textContent).toContain('Session unavailable');
  expect((within(dialog).getByLabelText(/Title/) as HTMLInputElement).value).toBe('Retry run');
  expect(window.location.pathname).toBe('/workspaces/default/agents');
  expect(submit.hasAttribute('disabled')).toBe(false);
});

test('quick view switches between agents and keeps full detail links available', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents');
  mockAgentsApi([
    { ...serverAgent, description: 'Extract structured data.', system: 'Read the supplied schema.', version: 2 },
    { ...serverAgent, id: 'agent_second123456', name: 'Second agent', system: 'Second prompt' },
  ]);
  render(<ManagedAgentsPage section="agents" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Quick view Server agent' }));
  const panel = screen.getByRole('dialog', { name: 'Agent' });
  expect(await within(panel).findByText('Read the supplied schema.')).toBeTruthy();
  expect(within(panel).getByText('V2')).toBeTruthy();
  expect(within(panel).getByRole('link', { name: 'Open' }).getAttribute('href')).toBe(
    '/workspaces/default/agents/agent_server123456',
  );
  expect(within(panel).getByRole('button', { name: 'Previous agent' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(within(panel).getByRole('button', { name: 'Next agent' }));
  expect(await within(panel).findByText('Second prompt')).toBeTruthy();
  expect(within(panel).queryByText('Read the supplied schema.')).toBeNull();
  expect(within(panel).getByRole('button', { name: 'Next agent' }).hasAttribute('disabled')).toBe(true);
  fireEvent.click(within(panel).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
});

test('starting a conversation from quick view creates for that agent and opens the interactive session', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents');
  const api = mockAgentsApi([serverAgent]);
  render(<ManagedAgentsPage section="agents" />);
  fireEvent.click(await screen.findByRole('button', { name: 'Quick view Server agent' }));
  const panel = screen.getByRole('dialog', { name: 'Agent' });
  fireEvent.click(await within(panel).findByRole('button', { name: 'Start conversation' }));
  const dialog = screen.getByRole('dialog', { name: 'Create session' });
  fireEvent.change(within(dialog).getByLabelText(/Title/), { target: { value: '  Extraction run  ' } });
  const submit = within(dialog).getByRole('button', { name: 'Create session' });
  await waitFor(() => expect(submit.hasAttribute('disabled')).toBe(false));
  expect(within(dialog).getByRole('link', { name: 'Manage environments (opens in new tab)' })).toBeTruthy();
  fireEvent.click(submit);
  await waitFor(() => expect(window.location.pathname).toBe('/workspaces/default/sessions/sesn_created123456'));
  expect(window.location.search).toBe('?interactive=true');
  const request = api.requests.find((request) => request.method === 'POST' && request.url.startsWith('/v1/sessions?'));
  expect(request?.body?.agent).toEqual({ type: 'agent', id: serverAgent.id });
  expect(request?.body?.title).toBe('Extraction run');
  expect(request?.body?.environment_id).toBe('env_option123456');
});

test('archived agents can be previewed but cannot start a conversation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents');
  mockAgentsApi([{ ...serverAgent, archived_at: '2026-10-01T00:00:00Z' }]);
  render(<ManagedAgentsPage section="agents" />);
  fireEvent.click(screen.getByRole('button', { name: 'Status Active' }));
  fireEvent.click(await screen.findByRole('menuitemradio', { name: 'All' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Quick view Server agent' }));
  const panel = screen.getByRole('dialog', { name: 'Agent' });
  expect((await within(panel).findByRole('button', { name: 'Start conversation' })).hasAttribute('disabled')).toBe(
    true,
  );
  fireEvent.click(within(panel).getByRole('button', { name: 'Close' }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
  expect((await screen.findByRole('menuitem', { name: 'Start conversation' })).getAttribute('aria-disabled')).toBe(
    'true',
  );
});
