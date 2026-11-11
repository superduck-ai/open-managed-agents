import { afterEach, expect, test } from 'bun:test';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
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
