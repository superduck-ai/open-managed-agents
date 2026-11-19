import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { IdentityDirectoryRow } from './api';

const { cleanup, fireEvent, render, screen } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { I18nProvider } = await import('../../../shared/i18n');
const { IdentityTable } = await import('./IdentityTable');
const originalFetch = globalThis.fetch;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

test('展开身份后可以加载后续 Agent，并保留已加载项', async () => {
  resetTestDom('/');
  const pages: (string | null)[] = [];
  globalThis.fetch = (async (input) => {
    const page = new URL(String(input), window.location.origin).searchParams.get('page');
    pages.push(page);
    return Response.json(
      page === '1'
        ? { data: [{ agent_id: 'agent-first', sessions: 1, channels: [] }], next_page: 2 }
        : { data: [{ agent_id: 'agent-last', sessions: 1, channels: [] }] },
    );
  }) as typeof fetch;
  const row: IdentityDirectoryRow = {
    id: 'idn_test',
    namespace: 'test',
    external_subject_id: 'user-test',
    display_name: '验收身份',
    enabled: true,
    created_at: '2026-11-18T07:00:00Z',
    active: true,
    agents: 101,
    sessions: 101,
    last_used_at: null,
  };
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale="zh-CN">
        <IdentityTable
          rows={[row]}
          loading={false}
          filtered={false}
          error={null}
          onRetry={() => undefined}
          orgUuid="org-test"
          workspaceId="workspace-test"
          agentOptions={[]}
          onOpen={() => undefined}
        />
      </I18nProvider>
    </QueryClientProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: '展开 Agent' }));
  await screen.findByText('agent-first');
  fireEvent.click(screen.getByRole('button', { name: '加载更多 Agent' }));
  await screen.findByText('agent-last');
  expect(screen.getByText('agent-first')).toBeTruthy();
  expect(screen.queryByRole('button', { name: '加载更多 Agent' })).toBeNull();
  expect(pages).toEqual(['1', '2']);
});
