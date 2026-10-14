import { afterEach, expect, mock, spyOn, test } from 'bun:test';
import {
  ManagedAgentsPage,
  fireEvent,
  mockAgentsApi,
  render,
  resetManagedAgentsTestState,
  resetTestDom,
  screen,
  waitFor,
  within,
} from '../ManagedAgentsPage.test-utils';

afterEach(resetManagedAgentsTestState);

test('successful agent creation navigates directly to its configuration without an intermediate list entry', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents/new');
  mockAgentsApi([]);
  render(<ManagedAgentsPage section="agents" />);
  const page = await screen.findByRole('main', { name: 'Create agent' });
  await within(page).findByRole('textbox', { name: 'Name' });
  const pushState = spyOn(window.history, 'pushState');
  try {
    fireEvent.click(within(page).getByRole('button', { name: 'Create agent' }));
    await waitFor(() => expect(window.location.pathname).toBe('/workspaces/default/agents/agent_created123456'));
    expect(window.location.search).toBe('?tab=config');
    expect(pushState.mock.calls.map((call) => call[2])).toEqual([
      '/workspaces/default/agents/agent_created123456?tab=config',
    ]);
  } finally {
    pushState.mockRestore();
  }
});

test('failed agent creation preserves the standalone page and edited draft', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents/new');
  mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  globalThis.fetch = mock(async (input, init) => {
    if (String(input).includes('/v1/agents?') && init?.method === 'POST') {
      return Response.json({ error: { message: 'Agent creation failed' } }, { status: 500 });
    }
    return baseFetch(input, init);
  }) as typeof fetch;
  render(<ManagedAgentsPage section="agents" />);
  const page = await screen.findByRole('main', { name: 'Create agent' });
  const name = within(page).getByRole('textbox', { name: 'Name' }) as HTMLInputElement;
  fireEvent.change(name, { target: { value: 'Keep this draft' } });
  fireEvent.click(within(page).getByRole('button', { name: 'Create agent' }));
  await waitFor(() => expect(page.textContent?.includes('Agent creation failed')).toBe(true));
  expect(name.value).toBe('Keep this draft');
  expect(window.location.pathname).toBe('/workspaces/default/agents/new');
  expect(within(page).getByRole('button', { name: 'Create agent' }).hasAttribute('disabled')).toBe(false);
});

test('direct creation routes show a page with optional sections initially collapsed', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agents/new');
  mockAgentsApi([]);
  render(<ManagedAgentsPage section="agents" />);
  const page = await screen.findByRole('main', { name: 'Create agent' });
  expect(within(page).getByRole('link', { name: 'Agents' }).getAttribute('href')).toBe('/workspaces/default/agents');
  expect(within(page).getByRole('heading', { name: 'Create agent' })).toBeTruthy();
  expect(within(page).getByRole('combobox', { name: 'Model' })).toBeTruthy();
  expect(screen.queryByRole('dialog')).toBeNull();
  for (const section of ['Tools', 'Skills', 'Multiagent']) {
    expect(
      within(page)
        .getByRole('button', { name: `${section} (optional)` })
        .getAttribute('aria-expanded'),
    ).toBe('false');
  }
  expect(within(page).queryByRole('combobox', { name: 'Add MCP server' })).toBeNull();
  fireEvent.click(within(page).getByRole('button', { name: 'Tools (optional)' }));
  expect(within(page).getByRole('combobox', { name: 'Add MCP server' })).toBeTruthy();
});
