import { afterEach, expect, mock, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  RouterProvider,
} from '@tanstack/react-router';
import { resetTestDom } from '../../test/setup';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { WorkspaceContext } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { setConsoleRequestContext } from '../../shared/api/client';
import { FileDetailsPage } from './FileDetailsPage';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const originalScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo');
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.forEach((client) => client.clear());
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  if (originalScrollTo) Object.defineProperty(globalThis, 'scrollTo', originalScrollTo);
  else Reflect.deleteProperty(globalThis, 'scrollTo');
});

async function mount(workspaceId = 'workspace-b', locale: Locale = 'zh-CN') {
  resetTestDom(`https://oma.duck.ai/workspaces/${workspaceId}/files/file_test`);
  Object.assign(globalThis, { scrollTo: () => undefined });
  const requests: { url: string; workspace: string | null }[] = [];
  globalThis.fetch = mock(async (input, init) => {
    requests.push({ url: String(input), workspace: new Headers(init?.headers).get('x-workspace-id') });
    return Response.json({
      id: 'file_test',
      type: 'file',
      filename: 'report.txt',
      mime_type: 'text/plain',
      size_bytes: 10,
      created_at: '2026-11-01T00:00:00Z',
      downloadable: true,
    });
  }) as typeof fetch;
  const root = createRootRoute();
  const detail = createRoute({
    getParentRoute: () => root,
    path: '/workspaces/$workspaceId/files/$fileId',
    component: FileDetailsPage,
  });
  const list = createRoute({
    getParentRoute: () => root,
    path: '/workspaces/$workspaceId/files',
    component: () => <p>File list</p>,
  });
  const router = createRouter({
    routeTree: root.addChildren([detail, list]),
    history: createMemoryHistory({ initialEntries: [window.location.pathname] }),
  });
  await router.load();
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const workspaces = ['workspace-a', 'workspace-b'].map((id) => ({ ...defaultWorkspace, id, name: id }));
  function tree(activeWorkspaceId: string, isLoading = false) {
    setConsoleRequestContext({ organizationUuid: 'org', workspaceId: activeWorkspaceId });
    return (
      <I18nProvider initialLocale={locale}>
        <QueryClientProvider client={client}>
          <WorkspaceContext.Provider
            value={{
              orgUuid: 'org',
              workspaces: isLoading ? [] : workspaces,
              activeWorkspaceId,
              activeWorkspace: workspaces.find((item) => item.id === activeWorkspaceId) ?? defaultWorkspace,
              isLoading,
              error: null,
              selectWorkspace: () => {},
              createWorkspace: async () => defaultWorkspace,
              refreshWorkspaces: async () => {},
            }}
          >
            <RouterProvider router={router} />
          </WorkspaceContext.Provider>
        </QueryClientProvider>
      </I18nProvider>
    );
  }
  const result = render(tree('workspace-a'));
  return {
    requests,
    router,
    selectWorkspace: (id: string, isLoading = false) => result.rerender(tree(id, isLoading)),
  };
}

test('an inaccessible route workspace produces no metadata request', async () => {
  const { requests, selectWorkspace } = await mount('unknown-workspace');
  selectWorkspace('workspace-a', true);
  expect(await screen.findByRole('status')).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  selectWorkspace('workspace-a', false);
  expect((await screen.findByRole('alert')).textContent).toBe('无访问权限');
  expect(requests).toHaveLength(0);
});

test('metadata waits for the route workspace and the return link retains its scope', async () => {
  const { requests, router, selectWorkspace } = await mount();
  expect(await screen.findByRole('status')).toBeTruthy();
  expect(requests).toHaveLength(0);
  expect(screen.queryByRole('button', { name: '下载' })).toBeNull();
  selectWorkspace('workspace-b');
  expect(await screen.findByRole('heading', { name: 'report.txt' })).toBeTruthy();
  expect(screen.getByRole('button', { name: '下载' })).toBeTruthy();
  expect(requests).toHaveLength(1);
  expect(requests[0].workspace).toBe('workspace-b');
  expect(requests[0].url).toContain('/v1/files/file_test');
  fireEvent.click(screen.getByRole('button', { name: '文件' }));
  await waitFor(() => expect(router.state.location.pathname).toBe('/workspaces/workspace-b/files'));
});

test.each(['en', 'zh-CN'] as const)('file creation time uses the %s date and time format', async (locale) => {
  const { selectWorkspace } = await mount('workspace-b', locale);
  selectWorkspace('workspace-b');
  await screen.findByRole('heading', { name: 'report.txt' });
  const date = new Intl.DateTimeFormat(locale, { dateStyle: 'medium', timeStyle: 'short' }).format(
    new Date('2026-11-01T00:00:00Z'),
  );
  expect(screen.getByText(date)).toBeTruthy();
  expect(screen.queryByText('2026-11-01T00:00:00Z')).toBeNull();
});
