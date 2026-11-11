import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { AuthContext } from '../../shared/auth/context';
import { WorkspaceContext } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { ConsoleSearch } from './ConsoleSearch';

const { cleanup, fireEvent, render, screen, waitFor } = await import('@testing-library/react');
const clients: QueryClient[] = [];
afterEach(() => {
  cleanup();
  clients.forEach((client) => client.clear());
});

test('recent search records clear on workspace changes and stay cleared when returning', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  function tree(workspaceId: string) {
    return (
      <I18nProvider initialLocale="en">
        <QueryClientProvider client={client}>
          <WorkspaceContext.Provider
            value={{
              orgUuid: 'org',
              activeWorkspaceId: workspaceId,
              activeWorkspace: { ...defaultWorkspace, id: workspaceId },
              workspaces: [defaultWorkspace],
              isLoading: false,
              error: null,
              selectWorkspace: () => {},
              createWorkspace: async () => defaultWorkspace,
              refreshWorkspaces: async () => {},
            }}
          >
            <ConsoleSearch currentPath="/dashboard" onNavigate={() => {}} />
          </WorkspaceContext.Provider>
        </QueryClientProvider>
      </I18nProvider>
    );
  }
  const view = render(tree('a'));
  fireEvent.click(screen.getByRole('button', { name: /Search console/ }));
  fireEvent.click(await screen.findByRole('option', { name: /Overview/ }));
  await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
  fireEvent.click(screen.getByRole('button', { name: /Search console/ }));
  expect(await screen.findByText('Recent')).toBeTruthy();
  view.rerender(tree('b'));
  await waitFor(() => expect(screen.queryByText('Recent')).toBeNull());
  view.rerender(tree('a'));
  expect(screen.queryByText('Recent')).toBeNull();
});

test('failed resource searches offer retry instead of an empty result', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const originalFetch = globalThis.fetch;
  let calls = 0;
  globalThis.fetch = (async () => {
    calls += 1;
    throw new Error('network down');
  }) as typeof fetch;
  try {
    render(
      <I18nProvider initialLocale="en">
        <QueryClientProvider client={client}>
          <AuthContext.Provider
            value={{
              account: { uuid: 'acct', email_address: 'a@example.com' },
              status: 'authenticated',
              refresh: async () => ({ account: null }),
              logout: async () => undefined,
            }}
          >
            <WorkspaceContext.Provider
              value={{
                orgUuid: 'org',
                activeWorkspaceId: 'default',
                activeWorkspace: defaultWorkspace,
                workspaces: [defaultWorkspace],
                isLoading: false,
                error: null,
                selectWorkspace: () => {},
                createWorkspace: async () => defaultWorkspace,
                refreshWorkspaces: async () => {},
              }}
            >
              <ConsoleSearch currentPath="/dashboard" onNavigate={() => {}} />
            </WorkspaceContext.Provider>
          </AuthContext.Provider>
        </QueryClientProvider>
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: /Search console/ }));
    fireEvent.change(screen.getByPlaceholderText(/Search console/), { target: { value: 'zzz-no-match' } });
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.queryByText('No matching results.')).toBeNull();
    const before = calls;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    await waitFor(() => expect(calls).toBeGreaterThan(before));
    expect(screen.queryByText('No matching results.')).toBeNull();
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('resource rows show their kind instead of the workspace name', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  const originalFetch = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) =>
    String(input).includes('/environments')
      ? Response.json({ data: [{ id: 'env_1', name: '测试' }] })
      : Response.json({ data: [] })) as typeof fetch;
  try {
    render(
      <I18nProvider initialLocale="en">
        <QueryClientProvider client={client}>
          <AuthContext.Provider
            value={{
              account: { uuid: 'acct', email_address: 'a@example.com' },
              status: 'authenticated',
              refresh: async () => ({ account: null }),
              logout: async () => undefined,
            }}
          >
            <WorkspaceContext.Provider
              value={{
                orgUuid: 'org',
                activeWorkspaceId: 'default',
                activeWorkspace: defaultWorkspace,
                workspaces: [defaultWorkspace],
                isLoading: false,
                error: null,
                selectWorkspace: () => {},
                createWorkspace: async () => defaultWorkspace,
                refreshWorkspaces: async () => {},
              }}
            >
              <ConsoleSearch currentPath="/dashboard" onNavigate={() => {}} />
            </WorkspaceContext.Provider>
          </AuthContext.Provider>
        </QueryClientProvider>
      </I18nProvider>,
    );
    fireEvent.click(screen.getByRole('button', { name: /Search console/ }));
    fireEvent.change(screen.getByPlaceholderText(/Search console/), { target: { value: '测试' } });
    const option = await screen.findByRole('option', { name: /测试/ });
    expect(option.textContent).toContain('Environments');
    expect(option.textContent).not.toContain('Default');
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test('search results keep fixed rows and a single non-wrapping keyboard selection', async () => {
  resetTestDom('https://oma.duck.ai/dashboard');
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  clients.push(client);
  render(
    <I18nProvider initialLocale="en">
      <QueryClientProvider client={client}>
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'org',
            activeWorkspaceId: 'default',
            activeWorkspace: defaultWorkspace,
            workspaces: [defaultWorkspace],
            isLoading: false,
            error: null,
            selectWorkspace: () => {},
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          <ConsoleSearch currentPath="/dashboard" onNavigate={() => {}} />
        </WorkspaceContext.Provider>
      </QueryClientProvider>
    </I18nProvider>,
  );
  fireEvent.click(screen.getByRole('button', { name: /Search console/ }));
  const options = await screen.findAllByRole('option');
  expect(options.length).toBeGreaterThan(1);
  for (const option of options) {
    expect(option.className).toContain('h-11');
    expect(option.className).toContain('min-h-11');
  }
  const input = screen.getByPlaceholderText(/Search console/);
  const selected = () => document.querySelector('[data-selected="true"]');
  fireEvent.keyDown(input, { key: 'Home' });
  expect(selected()).toBe(options[0]);
  fireEvent.keyDown(input, { key: 'ArrowUp' });
  expect(selected()).toBe(options[0]);
  fireEvent.keyDown(input, { key: 'ArrowDown' });
  expect(selected()).toBe(options[1]);
  expect(document.querySelectorAll('[data-selected="true"]').length).toBe(1);
});
