import { afterEach, beforeEach, expect, test } from 'bun:test';
import { useState } from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { resetTestDom, setTestViewport } from '../../test/setup';
import { I18nProvider } from '../../shared/i18n';
import { ConsoleShell } from './ConsoleLayout';
import { WorkspaceContext } from '../../shared/workspaces/context';
import { defaultWorkspace } from '../../shared/workspaces/api';

const { render, cleanup, screen, fireEvent, waitFor, act } = await import('@testing-library/react');
const account = {
  uuid: 'a',
  email_address: 'a@example.com',
  effectiveOrganizationUuid: 'org',
  permissions: ['organization:manage', 'members:manage'],
};
beforeEach(() => {
  resetTestDom('https://oma.duck.ai/dashboard');
  window.sessionStorage.clear();
  setTestViewport(1280, 900);
});
afterEach(() => {
  cleanup();
  setTestViewport(1280, 900);
});
function Harness({ initial = '/dashboard', admin = true }: { initial?: string; admin?: boolean }) {
  const [path, setPath] = useState(initial);
  const [workspaceId, selectWorkspace] = useState('default');
  const workspaces = [defaultWorkspace, { ...defaultWorkspace, id: 'other', name: 'Other' }];
  return (
    <I18nProvider initialLocale="en">
      <QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}>
        <WorkspaceContext.Provider
          value={{
            orgUuid: 'org',
            workspaces,
            activeWorkspace: workspaces.find((item) => item.id === workspaceId)!,
            activeWorkspaceId: workspaceId,
            selectWorkspace,
            canManageWorkspaces: admin,
            isLoading: false,
            error: null,
            createWorkspace: async () => defaultWorkspace,
            refreshWorkspaces: async () => {},
          }}
        >
          <ConsoleShell
            account={{ ...account, permissions: admin ? account.permissions : [] }}
            currentPath={path}
            onLogout={() => {}}
            onNavigate={setPath}
          >
            <div data-testid="page">{path}</div>
          </ConsoleShell>
        </WorkspaceContext.Provider>
      </QueryClientProvider>
    </I18nProvider>
  );
}

test('direct organization routes are denied to ordinary users', () => {
  render(<Harness initial="/settings/members" admin={false} />);
  expect(screen.queryByTestId('page')).toBeNull();
  expect(screen.getByText('Access denied')).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Organization management' })).toBeNull();
});

test('entering settings and returning to the app use matching navigation animations', async () => {
  const style = document.createElement('style');
  style.textContent = await Bun.file(new URL('./console-navigation.css', import.meta.url)).text();
  document.head.append(style);
  try {
    render(<Harness />);
    fireEvent.click(screen.getByRole('link', { name: 'Settings', exact: true }));
    const settingsStyle = getComputedStyle(screen.getByRole('navigation', { name: 'Settings navigation' }));
    expect(settingsStyle.animation).toBe('workspace-navigation-in 240ms ease-out');
    fireEvent.click(screen.getByRole('button', { name: 'Back to app' }));
    const mainStyle = getComputedStyle(screen.getByRole('navigation', { name: 'Console navigation' }));
    expect(mainStyle.animation).toBe('workspace-navigation-out 240ms ease-out');
  } finally {
    style.remove();
  }
});

test('accordion expands without navigating and preserves the selected page and return path', async () => {
  render(<Harness />);
  fireEvent.click(screen.getByRole('link', { name: 'Settings', exact: true }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/llm');
  expect(screen.getByRole('button', { name: 'Models and access' }).getAttribute('aria-expanded')).toBe('true');
  fireEvent.click(screen.getByRole('button', { name: 'Organization management' }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/llm');
  expect(screen.getByRole('button', { name: 'Models and access' }).hasAttribute('data-active')).toBe(true);
  fireEvent.click(screen.getByRole('link', { name: 'Members', exact: true }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/members');
  expect(document.querySelector('.settings-location')?.textContent).toContain('Organization management / Members');
  fireEvent.click(screen.getByRole('link', { name: 'Workspaces', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: 'Models and access' }));
  fireEvent.click(screen.getByRole('button', { name: 'Organization management' }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/workspaces');
  expect(document.querySelectorAll('.settings-accordion [aria-expanded=true]').length).toBe(1);
  fireEvent.click(screen.getByRole('button', { name: 'Back to app' }));
  expect(screen.getByTestId('page').textContent).toBe('/dashboard');
  fireEvent.click(screen.getByRole('link', { name: 'Settings', exact: true }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/workspaces');
});

test('workspace switch preserves organization page and updates workspace-scoped keys', async () => {
  render(<Harness initial="/settings/llm" />);
  fireEvent.click(screen.getByRole('button', { name: 'Default' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Other', exact: true }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/llm');
  await waitFor(() =>
    expect(screen.getByRole('link', { name: 'API keys' }).getAttribute('href')).toBe('/settings/workspaces/other/keys'),
  );
  expect(document.querySelector('.settings-location')?.textContent).toContain('Organization: org');
});

test('sidebar collapses automatically and compact groups remain usable', async () => {
  render(<Harness initial="/settings/llm" />);
  expect(screen.queryByRole('button', { name: 'Collapse', exact: true })).toBeNull();
  await act(async () => setTestViewport(950, 850));
  await waitFor(() =>
    expect(document.querySelector('[data-sidebar-state]')?.getAttribute('data-sidebar-state')).toBe('collapsed'),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Organization management' }));
  fireEvent.click(await screen.findByRole('menuitem', { name: 'Members' }));
  expect(screen.getByTestId('page').textContent).toBe('/settings/members');
});

test('mobile drawer stays open for categories and closes after choosing a page', async () => {
  setTestViewport(390, 844);
  render(<Harness initial="/settings/llm" />);
  fireEvent.click(screen.getByRole('button', { name: 'Toggle Sidebar' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Organization management' }));
  expect(screen.getByRole('dialog', { name: 'Sidebar' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Models and access' }));
  fireEvent.click(await screen.findByRole('link', { name: 'API keys' }));
  await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Sidebar' })).toBeNull());
  expect(screen.getByTestId('page').textContent).toBe('/settings/workspaces/default/keys');
});
