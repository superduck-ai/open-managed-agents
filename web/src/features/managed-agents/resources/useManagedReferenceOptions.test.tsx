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
  globalThis.fetch = (async (input) => {
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
