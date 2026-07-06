import { afterEach, describe, expect, mock, test } from 'bun:test';
import { useMemo, useState, type ReactNode } from 'react';
import { resetTestDom } from '../../test/setup';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { defaultWorkspace, type CreateWorkspaceInput, type Workspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';

const testingLibrary = await import('@testing-library/react');
const { ConsoleShell } = await import('./ConsoleLayout');

const { act, cleanup, fireEvent, render, screen, waitFor } = testingLibrary;

afterEach(() => {
  cleanup();
});

describe('ConsoleShell', () => {
  test('renders the complete Open Managed Agents sidebar', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    expect(screen.getByText('Open Managed Agents')).toBeTruthy();
    expect(screen.getByRole('combobox', { name: /Default/i })).toBeTruthy();
    expect(screen.getByText('Dashboard')).toBeTruthy();
    expect(screen.getByText('API keys')).toBeTruthy();
    expect(screen.getByText('Build')).toBeTruthy();
    expect(screen.getByText('Managed Agents')).toBeTruthy();
    expect(screen.getByText('Analytics')).toBeTruthy();
    expect(screen.getByText('Claude Code')).toBeTruthy();
    expect(screen.getByText('Manage')).toBeTruthy();
    expect(screen.getByText('Documentation')).toBeTruthy();
    expect(screen.getByText('Deployments')).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Files' }).getAttribute('href')).toBe('/workspaces/default/files');
    expect(screen.getByRole('link', { name: 'Skills' }).getAttribute('href')).toBe('/workspaces/default/skills');
    expect(screen.getByRole('link', { name: 'Batches' }).getAttribute('href')).toBe('/workspaces/default/batches');
    expect(screen.getByRole('link', { name: 'Caching' }).getAttribute('href')).toBe('/usage/cache');
    expect(screen.getByRole('link', { name: 'Rate limits' }).getAttribute('href')).toBe('/usage/limits');
    expect(screen.getByRole('link', { name: 'Quickstart' }).getAttribute('href')).toBe(
      '/workspaces/default/agent-quickstart'
    );
    expect(screen.queryByRole('link', { name: /Playground/i })).toBeNull();
    expect(screen.queryByRole('link', { name: /Dreams/i })).toBeNull();
    expect(screen.queryByRole('link', { name: /MCP tunnels/i })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Tags' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Feedback' })).toBeNull();
  });

  test('keeps the workspace selector outside the sidebar scroll area', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    expect(screen.getByRole('combobox', { name: /Default/i }).closest('[data-sidebar-scroll-area="true"]')).toBeNull();
    const scrollArea = screen.getByRole('navigation', { name: /Console navigation/i }).closest('[data-sidebar-scroll-area="true"]');
    expect(scrollArea).toBeTruthy();
    expect(scrollArea?.classList.contains('sidebar-scroll-area')).toBe(true);
  });

  test('collapses and expands the desktop sidebar from the header button', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    const sidebar = document.querySelector('[data-slot="sidebar"]');
    const main = screen.getByText('Dashboard content').closest('main');

    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('');
    expect(main?.getAttribute('data-slot')).toBe('sidebar-inset');

    fireEvent.click(screen.getByRole('button', { name: 'Collapse' }));

    expect(sidebar?.getAttribute('data-state')).toBe('collapsed');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('icon');
    expect(screen.getByRole('button', { name: 'Expand' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Build' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Files' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Expand' }));

    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('');
    expect(screen.getByRole('link', { name: 'Files' })).toBeTruthy();
  });

  test('uses client navigation for sidebar links when available', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const navigate = mock(async () => undefined);

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
        onNavigate={navigate}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    fireEvent.click(screen.getByRole('link', { name: 'Workbench' }));

    expect(navigate).toHaveBeenCalledWith('/workbench');
  });

  test('uses workspace scoped client navigation for build links', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const navigate = mock(async () => undefined);

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
        onNavigate={navigate}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    fireEvent.click(screen.getByRole('link', { name: 'Files' }));

    expect(navigate).toHaveBeenCalledWith('/workspaces/default/files');
  });

  test('opens the account menu and calls logout', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const logout = mock(async () => undefined);

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={logout}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    fireEvent.click(screen.getByRole('button', { name: /test/i }));

    const menu = screen.getAllByRole('menu')[0];
    expect(menu.closest('[data-sidebar-state]')).toBeNull();
    expect(screen.getByRole('menuitemradio', { name: /Default API plan/i }).getAttribute('aria-checked')).toBe('true');
    expect(screen.getByRole('menuitem', { name: 'Organization settings' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Feedback' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Get help' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Language' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Legal center' })).toBeTruthy();
    expect(screen.queryByText('Theme')).toBeNull();

    fireEvent.click(screen.getByRole('menuitem', { name: /Log out/i }));

    await waitFor(() => expect(logout).toHaveBeenCalled());
  });

  test('opens account menu submenus to the right', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /test/i }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: 'Language' }));
    });

    expect(screen.getByRole('menu', { name: 'Language' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'English' }).getAttribute('aria-checked')).toBe('true');

    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: 'Legal center' }));
    });

    expect(screen.getByRole('menu', { name: 'Legal center' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Commercial Terms' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Usage Policy' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Privacy Policy' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Data retention' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Your Privacy Choices' })).toBeTruthy();
  });

  test('closes the account menu when clicking outside', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    fireEvent.click(screen.getByRole('button', { name: /test/i }));
    expect(screen.getByText('Organization settings')).toBeTruthy();

    fireEvent.pointerDown(document.body);

    await waitFor(() => expect(screen.queryByText('Organization settings')).toBeNull());
  });

  test('renders migrated shell text in Chinese and switches language from the account menu', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
      { locale: 'zh-CN' }
    );

    expect(document.documentElement.lang).toBe('zh-CN');
    expect(screen.getByText('仪表盘')).toBeTruthy();
    expect(screen.getByText('API 密钥')).toBeTruthy();
    expect(screen.getByText('构建')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /test/i }));
    expect(screen.getByText('组织设置')).toBeTruthy();
    expect(screen.getByText('退出登录')).toBeTruthy();

    fireEvent.click(screen.getByRole('menuitem', { name: '语言' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /English/i }));

    expect(document.documentElement.lang).toBe('en');
    expect(window.localStorage.getItem('oma.locale')).toBe('en');
    expect(screen.getByText('Dashboard')).toBeTruthy();
  });

  test('opens the workspace selector with disabled all workspaces and selectable workspaces', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    await act(async () => {
      fireEvent.click(screen.getByRole('combobox', { name: /Default/i }));
    });

    expect(screen.getByText('All workspaces')).toBeTruthy();
    expect(screen.getByText('Only available on Cost and Logs')).toBeTruthy();
    expect(screen.getByRole('option', { name: /Default/i }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('option', { name: /foo/i })).toBeTruthy();
    expect(screen.getByText('Create workspace')).toBeTruthy();
  });

  test('closes the workspace selector when clicking outside', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    fireEvent.click(screen.getByRole('combobox', { name: /Default/i }));
    expect(screen.getByRole('listbox')).toBeTruthy();

    fireEvent.mouseDown(document.body);
    fireEvent.click(document.body);

    await waitFor(() => expect(screen.queryByRole('listbox')).toBeNull());
  });

  test('selects a workspace and updates the account subtitle', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>
    );

    await act(async () => {
      fireEvent.click(screen.getByRole('combobox', { name: /Default/i }));
    });
    await waitFor(() => expect(screen.getByRole('option', { name: /foo/i })).toBeTruthy());
    await act(async () => {
      fireEvent.click(screen.getByRole('option', { name: /foo/i }));
    });
    expect(screen.getByRole('combobox', { name: /foo/i })).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /test/i }));
    });
    expect(screen.getByText('Admin · foo')).toBeTruthy();
  });

  test('uses client navigation when selecting a workspace on managed routes', async () => {
    resetTestDom('https://oma.duck.ai/agents');
    const navigate = mock(async () => undefined);

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/agents"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
        onNavigate={navigate}
      >
        <div>Agents content</div>
      </ConsoleShell>
    );

    fireEvent.click(screen.getByRole('combobox', { name: /Default/i }));
    fireEvent.click(screen.getByRole('option', { name: /foo/i }));

    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/workspaces/wrkspc_foo/agents'));
    expect(screen.getByRole('combobox', { name: /foo/i })).toBeTruthy();
  });

  test('syncs the workspace selector from workspace-scoped routes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/wrkspc_foo/logs');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/wrkspc_foo/logs"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Logs content</div>
      </ConsoleShell>
    );

    await waitFor(() => expect(screen.getByRole('combobox', { name: /foo/i })).toBeTruthy());
  });

  test('creates a workspace with color and US residency', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const createWorkspace = mock(async (input: CreateWorkspaceInput) => ({
      id: 'wrkspc_bar',
      type: 'workspace' as const,
      name: input.name,
      display_color: input.display_color,
      color: input.display_color,
      data_residency: input.data_residency
    }));

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
      { createWorkspace }
    );

    fireEvent.click(screen.getByRole('combobox', { name: /Default/i }));
    fireEvent.click(screen.getByText('Create workspace'));

    expect(screen.getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'bar' } });
    fireEvent.click(screen.getByRole('radio', { name: 'Sage' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));

    await waitFor(() => expect(createWorkspace).toHaveBeenCalled());
    expect(createWorkspace.mock.calls[0][0]).toEqual({
      name: 'bar',
      display_color: '#D8D2A6',
      data_residency: {
        workspace_geo: 'us'
      }
    });
    expect(screen.getByRole('combobox', { name: /bar/i })).toBeTruthy();
  });
});

function renderWithWorkspaces(
  children: ReactNode,
  options: { createWorkspace?: (input: CreateWorkspaceInput) => Promise<Workspace>; locale?: Locale } = {}
) {
  const tree = <WorkspaceHarness createWorkspace={options.createWorkspace}>{children}</WorkspaceHarness>;
  return render(options.locale ? <I18nProvider initialLocale={options.locale}>{tree}</I18nProvider> : tree);
}

function WorkspaceHarness({
  children,
  createWorkspace
}: {
  children: ReactNode;
  createWorkspace?: (input: CreateWorkspaceInput) => Promise<Workspace>;
}) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([
    defaultWorkspace,
    {
      id: 'wrkspc_foo',
      type: 'workspace',
      name: 'foo',
      display_color: '#9B87F5',
      color: '#9B87F5'
    }
  ]);
  const [activeWorkspaceId, setActiveWorkspaceId] = useState(defaultWorkspace.id);
  const activeWorkspace = workspaces.find((workspace) => workspace.id === activeWorkspaceId) ?? defaultWorkspace;

  const value = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      workspaces,
      activeWorkspace,
      activeWorkspaceId,
      isLoading: false,
      error: null,
      selectWorkspace: setActiveWorkspaceId,
      createWorkspace: async (input) => {
        const created = createWorkspace
          ? await createWorkspace(input)
          : {
              id: 'wrkspc_new',
              type: 'workspace' as const,
              name: input.name,
              display_color: input.display_color,
              color: input.display_color,
              data_residency: input.data_residency
            };
        setWorkspaces((current) => [...current, created]);
        setActiveWorkspaceId(created.id);
        return created;
      },
      refreshWorkspaces: async () => undefined
    }),
    [activeWorkspace, activeWorkspaceId, createWorkspace, workspaces]
  );

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}
