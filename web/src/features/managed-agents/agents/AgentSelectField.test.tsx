import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../../test/setup';
import { setAnthropicClientForTest } from '../../../shared/api/anthropic';
import { I18nProvider } from '../../../shared/i18n';
const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const { AgentSelectField } = await import('./AgentSelectField');
const originalFetch = globalThis.fetch;
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.forEach((client) => client.clear());
  clients.length = 0;
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
});
function jsonResponse(body: string, init: ResponseInit = {}) {
  return new Response(body, { ...init, headers: { 'content-type': 'application/json' } });
}
function mountPicker() {
  resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const selected: string[] = [];
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="en">
        <AgentSelectField workspaceId="default" value="" onChange={(id) => selected.push(id)} />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole('combobox', { name: 'Agent' }));
  return selected;
}
const first = { id: 'agent_11111111111111111111', name: 'First agent', archived_at: null };
const last = { id: 'agent_22222222222222222222', name: 'Later agent', archived_at: null };

test('keeps the first page when the next page fails and retries the same cursor', async () => {
  let attempts = 0;
  globalThis.fetch = async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    if (url.searchParams.get('page')) {
      attempts++;
      if (attempts === 1)
        return jsonResponse(JSON.stringify({ error: { type: 'invalid_request_error', message: 'Test failure' } }), {
          status: 400,
        });
      return jsonResponse(JSON.stringify({ data: [last], next_page: null }));
    }
    return jsonResponse(JSON.stringify({ data: [first], next_page: 'next' }));
  };
  mountPicker();
  await screen.findByText('First agent');
  fireEvent.click(await screen.findByRole('button', { name: 'Load more' }));
  await screen.findByRole('alert');
  expect(screen.getByText('First agent')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
  await screen.findByText('Later agent');
  expect(attempts).toBe(2);
});

test('loads a page on bottom scroll, deduplicates overlapping rows and selects a later agent', async () => {
  const pages: (string | null)[] = [];
  globalThis.fetch = async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    const page = url.searchParams.get('page');
    pages.push(page);
    return jsonResponse(
      JSON.stringify(page ? { data: [first, last], next_page: null } : { data: [first], next_page: 'next' }),
    );
  };
  const selected = mountPicker();
  await screen.findByText('First agent');
  expect(pages).toEqual([null]);
  fireEvent.scroll(document.querySelector('[data-slot="command-list"]')!);
  await screen.findByText('Later agent');
  expect(pages).toEqual([null, 'next']);
  expect(screen.getAllByText('First agent')).toHaveLength(1);
  fireEvent.click(screen.getByText('Later agent'));
  expect(selected).toEqual([last.id]);
});

test('searches on the server from the first page and keeps stale results out of the active search', async () => {
  const searches: Record<string, unknown>[] = [];
  globalThis.fetch = async (input, init) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    if (url.pathname.endsWith(':search')) {
      searches.push(JSON.parse(String(init?.body)));
      return jsonResponse(JSON.stringify({ data: [last], next_page: null }));
    }
    return jsonResponse(JSON.stringify({ data: [first], next_page: 'next' }));
  };
  mountPicker();
  await screen.findByText('First agent');
  fireEvent.change(screen.getByPlaceholderText('Search by name or agent ID...'), { target: { value: 'Later' } });
  await screen.findByText('Later agent');
  expect(searches).toEqual([{ name: 'Later', limit: 20, include_archived: false }]);
  expect(screen.queryByText('First agent')).toBeNull();
  await waitFor(() => expect(screen.queryByRole('button', { name: 'Load more' })).toBeNull());
});

test('finds an agent by exact ID without a name search request', async () => {
  const requests: string[] = [];
  globalThis.fetch = async (input) => {
    const url = new URL(String(input), 'https://oma.duck.ai');
    requests.push(url.pathname);
    return jsonResponse(JSON.stringify(url.pathname.endsWith(last.id) ? last : { data: [first], next_page: null }));
  };
  mountPicker();
  await screen.findByText('First agent');
  fireEvent.change(screen.getByPlaceholderText('Search by name or agent ID...'), { target: { value: last.id } });
  await screen.findByText('Later agent');
  expect(requests).toEqual(['/v1/agents', '/v1/agents/' + last.id]);
});

for (const failed of [false, true]) {
  test(`Enter on ${failed ? 'Retry' : 'Load more'} preserves button activation without selecting an agent`, async () => {
    let requests = 0;
    globalThis.fetch = async () => {
      requests++;
      if (failed && requests === 1)
        return jsonResponse(JSON.stringify({ error: { type: 'invalid_request_error', message: 'Test failure' } }), {
          status: 400,
        });
      return jsonResponse(JSON.stringify({ data: [first], next_page: requests === 1 ? 'next' : null }));
    };
    const selected = mountPicker();
    const button = await screen.findByRole('button', { name: failed ? 'Retry' : 'Load more' });
    button.focus();
    expect(fireEvent.keyDown(button, { key: 'Enter', code: 'Enter' })).toBe(true);
    expect(selected).toEqual([]);
    expect(screen.getByPlaceholderText('Search by name or agent ID...')).toBeTruthy();
    fireEvent.click(button);
    await waitFor(() => expect(requests).toBe(2));
    expect(selected).toEqual([]);
  });
}
