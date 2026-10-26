import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { useState } = await import('react');
const { SubagentsEditor } = await import('./subagents-editor');
import type { CreateAgentInput } from '../types';

const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

function Fixture() {
  const [draft, setDraft] = useState<CreateAgentInput>({
    name: 'Test',
    model: 'model',
    tools: [],
    skills: [],
    mcp_servers: [],
  });
  return (
    <>
      <SubagentsEditor draft={draft} workspaceId="default" onChange={setDraft} />
      <output data-testid="draft">{JSON.stringify(draft.multiagent)}</output>
    </>
  );
}

test('shows only Subagents, restores its settings after toggling and warns about an empty restricted roster', async () => {
  resetTestDom();
  globalThis.fetch = (async () => Response.json({ data: [] })) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <Fixture />
    </QueryClientProvider>,
  );
  expect(screen.queryByText('Workflows')).toBeNull();
  expect(screen.queryByText('Advisor')).toBeNull();
  expect(screen.queryByRole('checkbox', { name: 'Can define its own agents' })).toBeNull();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Subagents' }));
  await waitFor(() =>
    expect(screen.getByRole('checkbox', { name: 'Can define its own agents' }).getAttribute('aria-checked')).toBe(
      'true',
    ),
  );
  expect(JSON.parse(screen.getByTestId('draft').textContent ?? '')).toEqual({
    type: 'multiagent_20261001',
    workflows: { type: 'disabled' },
    advisor: { type: 'disabled' },
    subagents: { type: 'enabled', inline_agents: { type: 'enabled' }, predefined_agents: [] },
  });
  fireEvent.click(screen.getByRole('checkbox', { name: 'Can define its own agents' }));
  await waitFor(() =>
    expect(screen.getByRole('alert').textContent).toBe('Add a saved agent, or let it define its own.'),
  );
  fireEvent.click(screen.getByRole('checkbox', { name: 'Subagents' }));
  await waitFor(() => expect(screen.queryByRole('alert')).toBeNull());
  expect(JSON.parse(screen.getByTestId('draft').textContent ?? '')).toBeNull();
  fireEvent.click(screen.getByRole('checkbox', { name: 'Subagents' }));
  await waitFor(() =>
    expect(screen.getByRole('checkbox', { name: 'Can define its own agents' }).getAttribute('aria-checked')).toBe(
      'false',
    ),
  );
  expect(screen.getByRole('combobox', { name: 'Add agent' })).toBeTruthy();
  client.clear();
});
