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
import { AuthContext } from '../../shared/auth/context';
import { I18nProvider } from '../../shared/i18n';
import { OrganizationContext } from '../../shared/organizations/context';
import { ProtectedConsoleLayout } from './ProtectedConsoleLayout';

const { act, cleanup, render, screen } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

for (const [status, message] of [
  [403, 'You do not have access to this workspace.'],
  [404, 'This workspace does not exist or is archived.'],
  [503, 'Could not load the organization. Please try again.'],
] as const) {
  test(`workspace scope failure renders a useful localized error ${status}`, async () => {
    resetTestDom('https://oma.duck.ai/workspaces/missing/agents');
    Object.assign(globalThis, { scrollTo: () => {} });
    globalThis.fetch = mock(async () => Response.json({ data: [] })) as typeof fetch;
    const root = createRootRoute({ component: ProtectedConsoleLayout });
    const page = createRoute({
      getParentRoute: () => root,
      path: '/workspaces/$workspaceId/agents',
      component: () => <p>Private resource</p>,
    });
    const router = createRouter({
      routeTree: root.addChildren([page]),
      history: createMemoryHistory({ initialEntries: ['/workspaces/missing/agents'] }),
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await act(async () => {
      render(
        <QueryClientProvider client={client}>
          <I18nProvider initialLocale="en">
            <AuthContext.Provider
              value={{
                account: { uuid: 'operator', email_address: 'operator@example.com' },
                status: 'authenticated',
                refresh: async () => undefined,
                logout: async () => {},
              }}
            >
              <OrganizationContext.Provider
                value={{
                  memberships: [],
                  switching: false,
                  error: { status, code: 'workspace_link_unavailable', message: '' },
                  switchOrganization: async () => false,
                  retry: async () => false,
                }}
              >
                <RouterProvider router={router} />
              </OrganizationContext.Provider>
            </AuthContext.Provider>
          </I18nProvider>
        </QueryClientProvider>,
      );
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
    expect(await screen.findByText(message)).toBeTruthy();
    expect(screen.queryByText('Private resource')).toBeNull();
    expect(screen.getByRole('button', { name: '重试' })).toBeTruthy();
  });
}
