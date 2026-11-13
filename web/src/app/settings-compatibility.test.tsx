import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { createMemoryHistory, createRouter, RouterProvider } from '@tanstack/react-router';
import { AuthContext } from '../shared/auth/context';
import { defaultWorkspace } from '../shared/workspaces/api';
import { WorkspaceContext } from '../shared/workspaces/context';
import { resetTestDom } from '../test/setup';
import { router as appRouter } from './router';
import type { LLMProvider } from '../features/llm-providers/api';
import {
  readWorkspaceManagementOrigin,
  saveWorkspaceManagementOrigin,
} from './layout/use-workspace-management-history';

const { act, cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const originalFetch = globalThis.fetch;
const originalScrollTo = Object.getOwnPropertyDescriptor(globalThis, 'scrollTo');
let queryClient: QueryClient | undefined;

afterEach(() => {
  cleanup();
  window.sessionStorage.clear();
  queryClient?.clear();
  globalThis.fetch = originalFetch;
  if (originalScrollTo) Object.defineProperty(globalThis, 'scrollTo', originalScrollTo);
  else Reflect.deleteProperty(globalThis, 'scrollTo');
});

test.each([
  {
    path: '/llm-models?from=notice#model-config',
    destination: '/settings/workspaces/default/llm',
    heading: 'LLM models',
    hash: 'model-config',
  },
  {
    path: '/llm-models?from=notice#model%20config',
    destination: '/settings/workspaces/default/llm',
    heading: 'LLM models',
    hash: 'model config',
  },
  {
    path: '/llm-models?from=notice#model%25config',
    destination: '/settings/workspaces/default/llm',
    heading: 'LLM models',
    hash: 'model%25config',
  },
  {
    path: '/workspaces/default/llm-models?from=notice#model-config',
    destination: '/settings/workspaces/default/llm',
    heading: 'LLM models',
    hash: 'model-config',
  },
  {
    path: '/settings/workspaces/default/telemetry?from=notice#destinations',
    destination: '/settings/telemetry',
    heading: 'OpenTelemetry',
    hash: 'destinations',
  },
  {
    path: '/members?from=notice#member-list',
    destination: '/settings/members',
    heading: 'Members',
    hash: 'member-list',
  },
  { path: '/settings/llm/organization', destination: '/settings/llm/organization', heading: 'LLM models', hash: '' },
  {
    path: '/workspaces/other/llm-models?returnTo=%2Fworkspaces%2Fother%2Fagent-quickstart%3Fmode%3Dapi#model%25config',
    destination: '/settings/workspaces/other/llm',
    heading: 'LLM models',
    hash: 'model%25config',
  },
  { path: '/settings/billing', destination: '/billing', heading: 'Billing', hash: '' },
  { path: '/settings/llm', destination: '/settings/workspaces/default/llm', heading: 'LLM models', hash: '' },
])('settings route $path reaches $destination', async ({ path, destination, heading, hash }) => {
  const { router, history, requested, applicationOrigin } = await renderSettingsRoute(path);

  expect(await screen.findByRole('heading', { name: heading })).toBeTruthy();
  await waitFor(() => expect(router.state.location.pathname).toBe(destination));
  expect(screen.queryByText('This page has been retired')).toBeNull();
  expect(history.length).toBe(1);
  if (destination.startsWith('/settings/workspaces/')) {
    const workspaceId = destination.split('/')[3];
    expect(requested.some((url) => url.includes(`/workspaces/${workspaceId}/llm_providers`))).toBe(true);
  }
  const original = new URL(path, 'https://oma.duck.ai');
  expect(router.state.location.hash).toBe(hash);
  expect(router.state.location.searchStr).toBe(original.search);
  expect(readWorkspaceManagementOrigin('acct_test', 'org_test')?.href).toBe(applicationOrigin);
  fireEvent.click(screen.getByRole('button', { name: 'Back to app' }));
  await waitFor(() => expect(router.state.location.href).toBe(applicationOrigin));
});

const workspaceProvider: LLMProvider = {
  id: 'provider_workspace',
  type: 'llm_provider',
  api_backend: 'messages',
  name: 'Seeded workspace gateway',
  slug: 'primary',
  base_url: 'https://models.example.test',
  has_api_key: true,
  api_key_last4: 'test',
  models: [{ id: 'seeded-model' }],
  created_at: '',
  updated_at: '',
};

async function renderSettingsRoute(path: string, organizationProviders: LLMProvider[] = []) {
  resetTestDom(`https://oma.duck.ai${path}`);
  window.sessionStorage.clear();
  const applicationOrigin = '/workspaces/default/agents?status=ready#list';
  saveWorkspaceManagementOrigin('acct_test', 'org_test', { href: applicationOrigin, workspaceId: defaultWorkspace.id });
  Object.assign(globalThis, { scrollTo: () => undefined });
  const requested: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    requested.push(String(input));
    if (/\/workspaces\/[^/]+\/llm_providers$/.test(String(input))) return Response.json([workspaceProvider]);
    if (/\/organizations\/[^/]+\/llm_providers$/.test(String(input))) return Response.json(organizationProviders);
    return /\/(llm_providers|members|invites)(\?|$)/.test(String(input))
      ? Response.json([])
      : Response.json({ data: [] });
  }) as typeof fetch;
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const history = createMemoryHistory({ initialEntries: [path] });
  const router = createRouter({ routeTree: appRouter.options.routeTree, history });
  await router.load();

  render(
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider
        value={{
          account: {
            uuid: 'acct_test',
            email_address: 'test@example.com',
            effectiveOrganizationUuid: 'org_test',
            permissions: ['organization:manage', 'members:manage', 'members:view'],
            memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role: 'admin' }],
          },
          status: 'authenticated',
          refresh: async () => ({ account: null }),
          logout: async () => undefined,
        }}
      >
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'org_test',
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

  return { router, history, requested, applicationOrigin };
}

test('direct model settings shows seeded workspace models when organization has no providers', async () => {
  const { router } = await renderSettingsRoute('/settings/llm');
  expect(await screen.findAllByText('Seeded workspace gateway')).toBeTruthy();
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/workspaces/default/llm'));
  fireEvent.click(screen.getByRole('button', { name: 'Seeded workspace gateway models' }));
  expect(await screen.findByText('primary/seeded-model')).toBeTruthy();
});

test('scope navigation preserves setup return and clears unsaved provider drafts', async () => {
  const query = '?returnTo=%2Fworkspaces%2Fdefault%2Fagent-quickstart%3Fmode%3Dapi';
  const { router } = await renderSettingsRoute('/settings/llm' + query);
  await screen.findAllByText('Seeded workspace gateway');
  fireEvent.click(screen.getByRole('button', { name: 'Add provider', exact: true }));
  expect(await screen.findByRole('dialog')).toBeTruthy();
  await act(async () => {
    await router.navigate({ href: '/settings/llm/organization' + query });
  });
  expect(await screen.findByText('No providers yet')).toBeTruthy();
  expect(screen.queryByRole('dialog')).toBeNull();
  const workspaceLink = screen.getByRole('link', { name: 'Workspace models', exact: true });
  expect(workspaceLink.getAttribute('href')).toBe('/settings/workspaces/default/llm' + query);
  fireEvent.click(workspaceLink);
  expect(await screen.findAllByText('Seeded workspace gateway')).toBeTruthy();
  expect(screen.queryByRole('dialog')).toBeNull();
});

test('inherited provider edit link opens explicit organization scope', async () => {
  const { router } = await renderSettingsRoute('/settings/workspaces/default/llm', [
    { ...workspaceProvider, id: 'provider_org', name: 'Shared gateway', slug: 'shared', scope: 'organization' },
  ]);
  const link = await screen.findByRole('link', { name: 'Manage organization models' });
  expect(link.getAttribute('href')).toBe('/settings/llm/organization');
  fireEvent.click(link);
  await waitFor(() => expect(router.state.location.pathname).toBe('/settings/llm/organization'));
  expect(await screen.findAllByText('Shared gateway')).toBeTruthy();
  expect(screen.queryByText('Seeded workspace gateway')).toBeNull();
});

test('changing workspace catalog discards the open provider draft', async () => {
  const { router, requested } = await renderSettingsRoute('/settings/workspaces/default/llm');
  await screen.findAllByText('Seeded workspace gateway');
  fireEvent.click(screen.getByRole('button', { name: 'Add provider', exact: true }));
  expect(await screen.findByRole('dialog')).toBeTruthy();
  await act(async () => {
    await router.navigate({ href: '/settings/workspaces/other/llm' });
  });
  await screen.findAllByText('Seeded workspace gateway');
  expect(screen.queryByRole('dialog')).toBeNull();
  expect(requested.some((url) => url.includes('/workspaces/other/llm_providers'))).toBe(true);
});
