import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { useMemo, type ReactNode } from 'react';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { resetTestDom } from '../../test/setup';
import { OrganizationLLMProviderCard } from './OrganizationLLMProviderCard';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

const originalFetch = globalThis.fetch;
const consoleURL = 'https://ccloud.cvte.com/ai/tenant-management';

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
});

describe('Organization LLM provider card', () => {
  test('sends an administrator to the configured company console and saves the returned key', async () => {
    const api = mockCardApi({ consoleURL, provider: null });
    renderCard('admin');

    const links = await screen.findAllByRole('link', { name: 'Open company console' });
    expect(links).toHaveLength(1);
    expect(links[0]?.getAttribute('href')).toBe(consoleURL);
    expect(links[0]?.getAttribute('target')).toBe('_blank');
    expect(links[0]?.getAttribute('rel')).toContain('noopener');
    expect(screen.getByText('ccloud.cvte.com')).toBeTruthy();
    expect(screen.queryByRole('link', { name: consoleURL })).toBeNull();
    expect((screen.getByLabelText('Provider name') as HTMLInputElement).value).toBe('AI网关');
    expect((screen.getByLabelText('Model gateway address') as HTMLInputElement).value).toBe(
      'https://narrows-gateway.test.seewo.com',
    );

    fireEvent.change(screen.getByLabelText('Provider name'), { target: { value: 'ChatData gateway' } });
    fireEvent.change(screen.getByLabelText('Model gateway address'), {
      target: { value: 'https://narrows.example.com' },
    });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-test-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save organization model' }));

    await waitFor(() => expect(api.puts).toHaveLength(1));
    expect(api.puts[0]).toEqual({
      name: 'ChatData gateway',
      base_url: 'https://narrows.example.com',
      api_key: 'sk-test-secret',
      model_ids: [],
    });
    await waitFor(() => expect(api.syncs).toBe(1));
  });

  test('reuses provider model preview when fetching the organization model list', async () => {
    const api = mockCardApi({ consoleURL, provider: null });
    renderCard('admin');

    fireEvent.change(await screen.findByLabelText('API key'), { target: { value: 'sk-test-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Fetch model list' }));

    expect(await screen.findAllByTestId('llm-model-row')).toHaveLength(2);
    expect(screen.getAllByRole('button', { name: 'Copy ID' }).length).toBeGreaterThan(0);
    fireEvent.click(screen.getByRole('checkbox', { name: 'doubao-seed-1-6' }));
    expect(screen.getByRole('checkbox', { name: 'doubao-seed-1-6' }).getAttribute('aria-checked')).toBe('false');
    expect(screen.queryByRole('button', { name: 'Add model' })).toBeNull();
    expect(api.previews[0]).toEqual({
      base_url: 'https://narrows-gateway.test.seewo.com',
      api_key: 'sk-test-secret',
    });

    fireEvent.click(screen.getByRole('button', { name: 'Save organization model' }));
    await waitFor(() => expect(api.puts).toHaveLength(1));
    expect(api.puts[0]?.model_ids).toEqual(['deepseek-v4-pro']);
    expect(api.syncs).toBe(0);
  });

  test('removes a new organization provider when model sync fails', async () => {
    const api = mockCardApi({ consoleURL, provider: null, syncStatus: 502 });
    renderCard('admin');

    fireEvent.change(await screen.findByLabelText('Provider name'), { target: { value: 'AI网关' } });
    fireEvent.change(screen.getByLabelText('Model gateway address'), {
      target: { value: 'https://narrows-gateway.test.seewo.com' },
    });
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-test-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save organization model' }));

    await waitFor(() => expect(api.deletes).toBe(1));
    expect(api.syncs).toBe(1);
    expect(screen.getByText('Request failed. Please try again.')).toBeTruthy();
    expect(screen.queryByText('Connected')).toBeNull();
    expect((screen.getByLabelText('API key') as HTMLInputElement).value).toBe('sk-test-secret');
  });

  test('keeps an existing organization provider when refreshing its models fails', async () => {
    const api = mockCardApi({
      consoleURL,
      syncStatus: 502,
      provider: {
        name: 'ChatData gateway',
        base_url: 'https://narrows.example.com',
        api_key_last4: 'dad1',
        model_ids: ['deepseek-v4-pro'],
      },
    });
    renderCard('admin');

    fireEvent.click(await screen.findByRole('button', { name: 'Replace key' }));
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-next-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save organization model' }));

    await waitFor(() => expect(api.puts).toHaveLength(1));
    expect(api.syncs).toBe(0);
    expect(api.deletes).toBe(0);
    expect(api.puts[0]?.model_ids).toEqual(['deepseek-v4-pro']);
  });

  test('keeps the saved gateway when an administrator replaces only the key', async () => {
    const api = mockCardApi({
      consoleURL,
      provider: {
        name: 'ChatData gateway',
        base_url: 'https://narrows.example.com',
        api_key_last4: 'dad1',
        model_ids: ['deepseek-v4-pro'],
      },
    });
    renderCard('admin');

    expect(await screen.findByText('ChatData gateway')).toBeTruthy();
    expect(screen.getByText('Connected')).toBeTruthy();
    expect(screen.getByText('Key ending in dad1')).toBeTruthy();
    expect(screen.getAllByRole('link', { name: 'Open company console' })).toHaveLength(1);
    expect(screen.queryByLabelText('Provider name')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Replace key' }));
    fireEvent.change(screen.getByLabelText('API key'), { target: { value: 'sk-next-secret' } });
    fireEvent.click(screen.getByRole('button', { name: 'Save organization model' }));

    await waitFor(() => expect(api.puts).toHaveLength(1));
    expect(api.puts[0]).toEqual({
      name: 'ChatData gateway',
      base_url: 'https://narrows.example.com',
      api_key: 'sk-next-secret',
      model_ids: ['deepseek-v4-pro'],
    });
  });

  test('asks before an administrator removes the organization provider', async () => {
    const api = mockCardApi({
      consoleURL,
      provider: {
        name: 'ChatData gateway',
        base_url: 'https://narrows.example.com',
        api_key_last4: 'dad1',
        model_ids: ['deepseek-v4-pro'],
      },
    });
    renderCard('admin');

    fireEvent.click(await screen.findByRole('button', { name: 'Remove' }));
    expect(api.deletes).toBe(0);
    fireEvent.click(screen.getByRole('button', { name: 'Remove organization model' }));
    await waitFor(() => expect(api.deletes).toBe(1));
  });

  test('shows connection status to other members without actions', async () => {
    mockCardApi({
      consoleURL,
      provider: {
        name: 'ChatData gateway',
        base_url: 'https://narrows.example.com',
        api_key_last4: 'dad1',
        model_ids: ['deepseek-v4-pro'],
      },
    });
    renderCard('user');

    expect(await screen.findByText('ChatData gateway')).toBeTruthy();
    expect(screen.getByText('Key ending in dad1')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Open company console' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Replace key' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Remove' })).toBeNull();
    expect(screen.queryByLabelText('API key')).toBeNull();
  });

  test('tells other members when the organization has no shared model', async () => {
    mockCardApi({ consoleURL, provider: null });
    renderCard('user');

    expect(
      await screen.findByText('This organization does not have a shared model yet. Ask an organization administrator.'),
    ).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Open company console' })).toBeNull();
  });

  test('disables the company console links when the address is not configured', async () => {
    mockCardApi({ consoleURL: '', provider: null });
    renderCard('admin');

    const openButtons = await screen.findAllByRole('button', { name: 'Open company console' });
    expect(openButtons).toHaveLength(1);
    expect(openButtons.every((button) => button.hasAttribute('disabled'))).toBe(true);
    expect(screen.getByText('The company console address is not configured.')).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Open company console' })).toBeNull();
  });
});

function renderCard(role: 'admin' | 'user') {
  resetTestDom('https://oma.duck.ai/settings/organization');
  render(
    <CardHarness role={role}>
      <OrganizationLLMProviderCard />
    </CardHarness>,
  );
}

function CardHarness({ children, role }: { children: ReactNode; role: 'admin' | 'user' }) {
  const queryClient = useMemo(() => new QueryClient({ defaultOptions: { queries: { retry: false } } }), []);
  const authValue = useMemo<AuthContextValue>(
    () => ({
      account: {
        uuid: 'acct_test',
        email_address: 'test@example.com',
        display_name: 'test',
        memberships: [{ organization: { uuid: 'org_test', name: 'default' }, role }],
      },
      status: 'authenticated',
      csrfToken: 'csrf_test',
      refresh: async () => ({ account: { uuid: 'acct_test', email_address: 'test@example.com' } }),
      logout: async () => undefined,
    }),
    [role],
  );
  const workspaceValue = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      workspaces: [defaultWorkspace],
      activeWorkspace: defaultWorkspace,
      activeWorkspaceId: defaultWorkspace.id,
      isLoading: false,
      error: null,
      selectWorkspace: () => undefined,
      createWorkspace: async () => defaultWorkspace,
      refreshWorkspaces: async () => undefined,
    }),
    [],
  );

  return (
    <QueryClientProvider client={queryClient}>
      <AuthContext.Provider value={authValue}>
        <WorkspaceContext.Provider value={workspaceValue}>{children}</WorkspaceContext.Provider>
      </AuthContext.Provider>
    </QueryClientProvider>
  );
}

function mockCardApi({
  consoleURL: configuredURL,
  provider,
  syncStatus = 200,
}: {
  consoleURL: string;
  provider: { name: string; base_url: string; api_key_last4: string; model_ids: string[] } | null;
  syncStatus?: number;
}) {
  let current = provider;
  const puts: Record<string, unknown>[] = [];
  const previews: Record<string, unknown>[] = [];
  let syncs = 0;
  let deletes = 0;
  const fetchMock = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const method = init?.method ?? 'GET';
    if (url.includes('/preview_models') && method === 'POST') {
      previews.push(JSON.parse(String(init?.body)) as Record<string, unknown>);
      return jsonResponse({ model_ids: ['deepseek-v4-pro', 'doubao-seed-1-6'] });
    }
    if (url.endsWith('/llm_console') && method === 'GET') {
      return jsonResponse({
        console_url: configuredURL,
        base_url: 'https://narrows-gateway.test.seewo.com',
      });
    }
    if (url.endsWith('/llm_provider') && method === 'GET') {
      return current ? jsonResponse(providerBody(current)) : jsonResponse({ error: 'not found' }, 404);
    }
    if (url.endsWith('/llm_provider') && method === 'PUT') {
      const body = JSON.parse(String(init?.body)) as Record<string, unknown>;
      puts.push(body);
      current = {
        name: String(body.name),
        base_url: String(body.base_url),
        api_key_last4: 'cret',
        model_ids: body.model_ids as string[],
      };
      return jsonResponse(providerBody(current), current ? 200 : 201);
    }
    if (url.endsWith('/llm_provider/models/sync') && method === 'POST') {
      syncs += 1;
      if (syncStatus >= 400) {
        return jsonResponse({ code: 'upstream_unavailable', message: 'failed to list models' }, syncStatus);
      }
      return jsonResponse(providerBody(current ?? { name: '', base_url: '', api_key_last4: '', model_ids: [] }));
    }
    if (url.endsWith('/llm_provider') && method === 'DELETE') {
      deletes += 1;
      current = null;
      return new Response(null, { status: 204 });
    }
    return jsonResponse({ error: 'not found' }, 404);
  });
  globalThis.fetch = fetchMock as unknown as typeof fetch;
  return {
    puts,
    previews,
    get syncs() {
      return syncs;
    },
    get deletes() {
      return deletes;
    },
  };
}

function providerBody(provider: { name: string; base_url: string; api_key_last4: string; model_ids: string[] }) {
  return {
    type: 'llm_provider',
    id: 'llmprov_test',
    name: provider.name,
    base_url: provider.base_url,
    has_api_key: true,
    api_key_last4: provider.api_key_last4,
    model_ids: provider.model_ids,
    scope: 'organization',
    created_at: '2026-10-13T00:00:00Z',
    updated_at: '2026-10-13T00:00:00Z',
  };
}

function jsonResponse(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  });
}
