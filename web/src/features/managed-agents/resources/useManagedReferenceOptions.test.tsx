import { afterEach, expect, test } from 'bun:test';
import { resetTestDom } from '../../../test/setup';
import type { ReactNode } from 'react';

const { act, cleanup, renderHook, waitFor } = await import('@testing-library/react');
const { focusManager, QueryClient, QueryClientProvider } = await import('@tanstack/react-query');
const { setConsoleRequestContext } = await import('../../../shared/api/client');
const { setAnthropicClientForTest } = await import('../../../shared/api/anthropic');
const { useManagedReferenceOptions } = await import('./useManagedReferenceOptions');

const originalFetch = globalThis.fetch;
const clients: InstanceType<typeof QueryClient>[] = [];

afterEach(() => {
  cleanup();
  clients.splice(0).forEach((client) => client.clear());
  focusManager.setFocused(undefined);
  globalThis.fetch = originalFetch;
  setAnthropicClientForTest(null);
  setConsoleRequestContext({});
});

function mountReferences() {
  resetTestDom('https://oma.duck.ai/');
  setConsoleRequestContext({ workspaceId: 'workspace_test', organizationUuid: 'org_test' });
  let environments: { id: string; name: string }[] = [];
  let failNext = false;
  globalThis.fetch = (async (input) => {
    if (failNext) {
      failNext = false;
      return new Response('{}', { status: 500, headers: { 'content-type': 'application/json' } });
    }
    const url = input instanceof Request ? input.url : String(input);
    return Response.json({ data: url.includes('/environments') ? environments : [], has_more: false, next_page: null });
  }) as typeof fetch;
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 15_000, refetchOnWindowFocus: false } },
  });
  clients.push(client);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>{children}</QueryClientProvider>
  );
  return {
    mount: () => renderHook(() => useManagedReferenceOptions('workspace_test'), { wrapper }),
    createEnvironment: () => {
      environments = [{ id: 'env_created', name: 'New environment' }];
    },
    failNextFetch: () => {
      failNext = true;
    },
  };
}

const createdEnvironmentOption = { id: 'env_created', label: 'New environment', secondary: 'env_created' };

test('refreshes environment options after creating an environment in another tab', async () => {
  const { mount, createEnvironment } = mountReferences();
  const hook = mount();
  await waitFor(() => expect(hook.result.current.loading).toBe(false));
  expect(hook.result.current.environments).toEqual([]);
  act(() => focusManager.setFocused(false));
  createEnvironment();
  await act(async () => focusManager.setFocused(true));
  await waitFor(() => expect(hook.result.current.environments).toEqual([createdEnvironmentOption]));
});

test('refreshes options when reopening the dialog while the previous cache is still fresh', async () => {
  const { mount, createEnvironment } = mountReferences();
  const initial = mount();
  await waitFor(() => expect(initial.result.current.loading).toBe(false));
  expect(initial.result.current.environments).toEqual([]);
  initial.unmount();
  createEnvironment();
  const reopened = mount();
  await waitFor(() => expect(reopened.result.current.environments).toEqual([createdEnvironmentOption]));
});

test('keeps cached options usable when a background refresh fails', async () => {
  const { mount, createEnvironment, failNextFetch } = mountReferences();
  createEnvironment();
  const hook = mount();
  await waitFor(() => expect(hook.result.current.environments).toEqual([createdEnvironmentOption]));
  expect(hook.result.current.error).toBeNull();
  act(() => focusManager.setFocused(false));
  failNextFetch();
  await act(async () => focusManager.setFocused(true));
  await waitFor(() => expect(hook.result.current.error).toBeNull());
  expect(hook.result.current.environments).toEqual([createdEnvironmentOption]);
});

test('surfaces an error when the first load fails with no cached options', async () => {
  const { mount, failNextFetch } = mountReferences();
  failNextFetch();
  const hook = mount();
  await waitFor(() => expect(hook.result.current.error).toBeTruthy());
  expect(hook.result.current.environments).toEqual([]);
});

test('skips credential-vaults and memory-stores fetches when disabled by runtime capabilities', async () => {
  const { WorkspaceContext } = await import('../../../shared/workspaces/context');
  const { defaultWorkspace } = await import('../../../shared/workspaces/api');
  const { defaultNativeCapabilities } = await import('../../../shared/workspaces/runtime');
  resetTestDom('https://oma.duck.ai/');
  setConsoleRequestContext({ workspaceId: 'wrkspc_qoder', organizationUuid: 'org_test' });
  const requestedUrls: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = input instanceof Request ? input.url : String(input);
    requestedUrls.push(url);
    if (url.includes('/environments')) {
      return Response.json({
        data: [{ id: 'env_qdr_1', name: 'Qoder Env' }],
        has_more: false,
        next_page: null,
      });
    }
    return new Response(JSON.stringify({ error: { type: 'unsupported_capability_error' } }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
  }) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const qoderWorkspace = {
    ...defaultWorkspace,
    id: 'wrkspc_qoder',
    runtime: {
      profile_uuid: '00000000-0000-4000-8000-000000000099',
      profile_id: 'rprof_qoder',
      profile_name: 'Qoder Cloud',
      provider_kind: 'qoder_cloud',
      capabilities: {
        ...defaultNativeCapabilities,
        credential_vaults: false,
        memory_stores: false,
      },
    },
  };
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <WorkspaceContext.Provider
        value={{
          orgUuid: 'org_test',
          workspaces: [qoderWorkspace],
          activeWorkspace: qoderWorkspace,
          activeWorkspaceId: qoderWorkspace.id,
          isLoading: false,
          error: null,
          selectWorkspace: () => undefined,
          createWorkspace: async () => qoderWorkspace,
          refreshWorkspaces: async () => undefined,
        }}
      >
        {children}
      </WorkspaceContext.Provider>
    </QueryClientProvider>
  );
  const hook = renderHook(() => useManagedReferenceOptions('wrkspc_qoder'), { wrapper });
  await waitFor(() => expect(hook.result.current.loading).toBe(false));
  expect(hook.result.current.error).toBeNull();
  expect(hook.result.current.environments).toEqual([{ id: 'env_qdr_1', label: 'Qoder Env', secondary: 'env_qdr_1' }]);
  expect(hook.result.current.vaults).toEqual([]);
  expect(hook.result.current.memoryStores).toEqual([]);
  expect(requestedUrls.some((url) => url.includes('/vaults') || url.includes('/memory_stores'))).toBe(false);
});

test('loads memory stores while skipping credential vaults when memory_stores is enabled on external runtime', async () => {
  const { WorkspaceContext } = await import('../../../shared/workspaces/context');
  const { defaultWorkspace } = await import('../../../shared/workspaces/api');
  const { defaultNativeCapabilities } = await import('../../../shared/workspaces/runtime');
  resetTestDom('https://oma.duck.ai/');
  setConsoleRequestContext({ workspaceId: 'wrkspc_qoder_mem', organizationUuid: 'org_test' });
  const requestedUrls: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = input instanceof Request ? input.url : String(input);
    requestedUrls.push(url);
    if (url.includes('/environments')) {
      return Response.json({
        data: [{ id: 'env_qdr_mem', name: 'Qoder Memory Env' }],
        has_more: false,
        next_page: null,
      });
    }
    if (url.includes('/memory_stores')) {
      return Response.json({
        data: [{ id: 'memstore_qdr_1', name: 'Project Knowledge' }],
        has_more: false,
        next_page: null,
      });
    }
    return new Response(JSON.stringify({ error: { type: 'unsupported_capability_error' } }), {
      status: 400,
      headers: { 'content-type': 'application/json' },
    });
  }) as typeof fetch;
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const qoderMemoryWorkspace = {
    ...defaultWorkspace,
    id: 'wrkspc_qoder_mem',
    runtime: {
      profile_uuid: '00000000-0000-4000-8000-000000000099',
      profile_id: 'rprof_qoder',
      profile_name: 'Qoder Cloud',
      provider_kind: 'qoder_cloud',
      capabilities: {
        ...defaultNativeCapabilities,
        byok_llm_providers: false,
        skills: false,
        deployments: false,
        mcp_servers: false,
        credential_vaults: false,
        memory_stores: true,
      },
    },
  };
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={client}>
      <WorkspaceContext.Provider
        value={{
          orgUuid: 'org_test',
          workspaces: [qoderMemoryWorkspace],
          activeWorkspace: qoderMemoryWorkspace,
          activeWorkspaceId: qoderMemoryWorkspace.id,
          isLoading: false,
          error: null,
          selectWorkspace: () => undefined,
          createWorkspace: async () => qoderMemoryWorkspace,
          refreshWorkspaces: async () => undefined,
        }}
      >
        {children}
      </WorkspaceContext.Provider>
    </QueryClientProvider>
  );
  const hook = renderHook(() => useManagedReferenceOptions('wrkspc_qoder_mem'), { wrapper });
  await waitFor(() => expect(hook.result.current.loading).toBe(false));
  expect(hook.result.current.error).toBeNull();
  expect(hook.result.current.environments).toEqual([
    { id: 'env_qdr_mem', label: 'Qoder Memory Env', secondary: 'env_qdr_mem' },
  ]);
  expect(hook.result.current.vaults).toEqual([]);
  expect(hook.result.current.memoryStores).toEqual([
    { id: 'memstore_qdr_1', label: 'Project Knowledge', secondary: 'memstore_qdr_1' },
  ]);
  expect(requestedUrls.some((url) => url.includes('/vaults'))).toBe(false);
  expect(requestedUrls.some((url) => url.includes('/memory_stores'))).toBe(true);
});
