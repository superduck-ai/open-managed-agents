import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router';
import { AuthContext } from '../shared/auth/context';
import { defaultWorkspace } from '../shared/workspaces/api';
import { WorkspaceContext } from '../shared/workspaces/context';
import { resetTestDom } from '../test/setup';
import { router as appRouter } from './router';

const { cleanup, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const originalScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo');
let queryClient: QueryClient | undefined;

afterEach(() => {
  cleanup();
  queryClient?.clear();
  globalThis.fetch = originalFetch;
  if (originalScrollTo) Object.defineProperty(globalThis, 'scrollTo', originalScrollTo);
  else Reflect.deleteProperty(globalThis, 'scrollTo');
});

test('legacy Workbench credit destination reaches the preserved billing page and replaces the retired URL', async () => {
  resetTestDom('https://oma.duck.ai/settings/billing');
  Object.assign(globalThis, { scrollTo: () => undefined });
  globalThis.fetch = (async () => Response.json({ data: [] })) as typeof fetch;
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const history = createMemoryHistory({ initialEntries: ['/settings/billing'] });
  const router = createRouter({ routeTree: appRouter.options.routeTree, history });
  await router.load();

  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          account: {
            uuid: 'acct_test',
            email_address: 'test@example.com',
            memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
          },
          status: 'authenticated',
          refresh: async () => ({ account: null }),
          logout: async () => undefined,
        }}
      >
        <WorkspaceContext.Provider
          value={{
            activeWorkspace: defaultWorkspace,
            activeWorkspaceId: defaultWorkspace.id,
            workspaces: [defaultWorkspace],
            isLoading: false,
            error: null,
            selectWorkspace: () => undefined,
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => undefined,
          }}
        >
          <RouterProvider router={router} />
        </WorkspaceContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>,
  );

  expect(await screen.findByRole('heading', { name: 'Billing' })).toBeTruthy();
  await waitFor(() => expect(router.state.location.pathname).toBe('/billing'));
  expect(screen.queryByText('This page has been retired')).toBeNull();
  expect(history.length).toBe(1);
});
