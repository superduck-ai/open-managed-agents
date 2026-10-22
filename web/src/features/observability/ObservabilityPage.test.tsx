import { afterEach, expect, test } from 'bun:test';
import '../../test/setup';

const { cleanup, render, waitFor } = await import('@testing-library/react');
const { QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { WorkspaceContext } = await import('../../shared/workspaces/context');
const { defaultWorkspace } = await import('../../shared/workspaces/api');
const { I18nProvider } = await import('../../shared/i18n');
const { ObservabilityPage } = await import('./ObservabilityPage');
const originalFetch = globalThis.fetch;
const clients: InstanceType<typeof QueryClient>[] = [];

afterEach(() => {
  cleanup();
  for (const client of clients.splice(0)) client.clear();
  globalThis.fetch = originalFetch;
});

function renderPage(orgUuid?: string, activeWorkspaceId = 'ws', locale: 'en' | 'zh-CN' = 'en') {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  return render(
    <QueryClientProvider client={client}>
      <I18nProvider initialLocale={locale}>
        <WorkspaceContext
          value={{
            orgUuid,
            activeWorkspaceId,
            activeWorkspace: defaultWorkspace,
            workspaces: [defaultWorkspace],
            isLoading: false,
            error: null,
            selectWorkspace: () => {},
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          <ObservabilityPage scope={{ kind: 'workspace' }} />
        </WorkspaceContext>
      </I18nProvider>
    </QueryClientProvider>,
  );
}

test('missing organization or workspace shows a placeholder without querying', () => {
  let calls = 0;
  globalThis.fetch = (async () => {
    calls++;
    return Response.json({});
  }) as typeof fetch;
  const orgMissing = renderPage();
  expect(orgMissing.getByRole('status').textContent).toBe('Select a workspace to view trajectories.');
  orgMissing.unmount();
  const workspaceMissing = renderPage('org', '', 'zh-CN');
  expect(workspaceMissing.getByRole('status').textContent).toBe('请选择工作区以查看轨迹。');
  expect(calls).toBe(0);
});

test('storage failures show a localized message without backend implementation details', async () => {
  globalThis.fetch = (async () =>
    Response.json({ error: { message: 'OpenObserve is unavailable' } }, { status: 503 })) as typeof fetch;
  const view = renderPage('org');
  await waitFor(() =>
    expect(view.getByRole('alert').textContent).toBe('Could not load trajectories. Please try again later.'),
  );
  expect(view.container.textContent).not.toContain('OpenObserve');
  view.unmount();
  const chinese = renderPage('org', 'ws', 'zh-CN');
  await waitFor(() => expect(chinese.getByRole('alert').textContent).toBe('暂时无法加载轨迹，请稍后重试。'));
  expect(chinese.container.textContent).not.toContain('OpenObserve');
});
