import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { useMemo, useState, type ReactNode } from 'react';
import { resetTestDom, setTestViewport } from '../../test/setup';
import type { AuthAccount } from '../../shared/auth/api';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { defaultWorkspace, type CreateWorkspaceInput, type Workspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';

const testingLibrary = await import('@testing-library/react');
const { ConsoleShell, SettingsShell } = await import('./ConsoleLayout');

const { act, cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

function clearManagementHistory() {
  for (const key of Object.keys(window.localStorage)) {
    if (key.startsWith('oma.workspace-management.last-page:')) window.localStorage.removeItem(key);
  }
}

const defaultViewport = { width: window.innerWidth, height: window.innerHeight };
beforeEach(() => {
  clearManagementHistory();
  setTestViewport(1280, 900);
});
afterEach(() => {
  cleanup();
  clearManagementHistory();
  setTestViewport(defaultViewport.width, defaultViewport.height);
});

describe('ConsoleShell', () => {
  test.each(['en', 'zh-CN'] as const)('账号区在 %s 显示用户名和当前组织，不显示角色与工作区', (locale) => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/default/llm-models"
        account={testAccount('user')}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
      { locale, effectiveRole: 'workspace_user' },
    );
    const account = screen.getByRole('button', { name: 'test org_test' });
    expect(within(account).getByText('test')).toBeTruthy();
    expect(within(account).getByText('org_test')).toBeTruthy();
    expect(account.querySelector('.text-xs')?.textContent).toBe('org_test');
  });

  test.each(['/settings/workspaces/default/mcp-tunnels', '/settings/workspaces/default/mcp-tunnels/tunnel_test'])(
    'uses the full-width console layout for %s',
    (currentPath) => {
      resetTestDom(`https://oma.duck.ai${currentPath}`);
      renderWithWorkspaces(
        <ConsoleShell
          currentPath={currentPath}
          account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
          onLogout={() => undefined}
        >
          <div>MCP tunnels content</div>
        </ConsoleShell>,
      );

      const contentContainer = screen.getByText('MCP tunnels content').parentElement;

      expect(contentContainer?.classList.contains('lg:px-8')).toBe(true);
      expect(contentContainer?.classList.contains('max-w-[928px]')).toBe(false);
    },
  );

  test('keeps the resource location selected when the sidebar is collapsed', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/workspaces/default/skills" account={testAccount()} onLogout={() => undefined}>
        <div>Skills content</div>
      </ConsoleShell>,
    );
    expect(screen.getByRole('link', { name: 'Skills', exact: true }).getAttribute('aria-current')).toBe('page');
    const resources = screen.getByRole('button', { name: 'Resources', exact: true });
    expect(resources.hasAttribute('data-active')).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Collapse', exact: true }));
    expect(resources.hasAttribute('data-active')).toBe(true);
    expect(resources.getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'Expand', exact: true }));
    expect(resources.hasAttribute('data-active')).toBe(false);
    fireEvent.click(resources);
    expect(resources.hasAttribute('data-active')).toBe(true);
  });

  test('marks the current primary page and retains full navigation labels', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/environments');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/workspaces/default/environments" account={testAccount()} onLogout={() => undefined}>
        <div>Environment content</div>
      </ConsoleShell>,
    );
    const current = screen.getByRole('link', { name: 'Environments', exact: true });
    expect(current.getAttribute('aria-current')).toBe('page');
    expect(current.getAttribute('title')).toBe('Environments');
    expect(screen.getByRole('link', { name: 'Overview', exact: true }).hasAttribute('aria-current')).toBe(false);
    const workspaceIcon = getWorkspaceMenuButton(/Default/i).querySelector('svg');
    expect(workspaceIcon?.innerHTML).not.toBe(
      screen.getByRole('button', { name: 'Resources' }).querySelector('svg')?.innerHTML,
    );
    expect(workspaceIcon?.style.color).toBe('var(--violet-9)');
  });

  test('preserves manual quickstart search and hash through workspace management', () => {
    const origin = '/workspaces/default/agent-quickstart?mode=api#online-test';
    resetTestDom(`https://oma.duck.ai${origin}`);
    function Harness() {
      const [path, setPath] = useState('/workspaces/default/agent-quickstart');
      return (
        <ConsoleShell
          currentPath={path}
          account={testAccount()}
          onLogout={() => undefined}
          onNavigate={async (next) => setPath(next)}
        >
          <div>{path}</div>
        </ConsoleShell>
      );
    }
    renderWithWorkspaces(<Harness />);
    fireEvent.click(screen.getByRole('link', { name: 'Workspace management' }));
    expect(screen.getByRole('link', { name: 'Back to navigation' }).getAttribute('href')).toBe(origin);
  });

  test.each([
    { origin: '/workspaces/default/agents/agent_one', target: '/workspaces/wrkspc_foo/agents' },
    { origin: '/agents', target: '/workspaces/wrkspc_foo/agents' },
    { origin: '/dashboard', target: '/dashboard' },
  ])('returns from management in the selected workspace after entering from $origin', async ({ origin, target }) => {
    resetTestDom(`https://oma.duck.ai${origin}`);
    function Harness() {
      const [path, setPath] = useState(origin);
      return (
        <ConsoleShell
          currentPath={path}
          account={testAccount()}
          onLogout={() => undefined}
          onNavigate={async (next) => setPath(next)}
        >
          <div data-testid="current-route">{path}</div>
        </ConsoleShell>
      );
    }
    renderWithWorkspaces(<Harness />);
    fireEvent.click(screen.getByRole('link', { name: 'Workspace management' }));
    expect(screen.getByRole('link', { name: 'Back to navigation' }).getAttribute('href')).toBe(origin);
    await act(async () => fireEvent.click(getWorkspaceMenuButton(/Default/i)));
    await act(async () => fireEvent.click(screen.getByRole('menuitem', { name: /foo/i })));
    const back = screen.getByRole('link', { name: 'Back to navigation' });
    expect(back.getAttribute('href')).toBe(target);
    fireEvent.click(back);
    expect(screen.getByTestId('current-route').textContent).toBe(target);
    expect(getWorkspaceMenuButton(/foo/i)).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Agents', exact: true }).getAttribute('href')).toBe(
      '/workspaces/wrkspc_foo/agents',
    );
  });

  test('remembers the last management module after leaving and remounting the console', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers');
    function Harness({ account = testAccount() }: { account?: AuthAccount }) {
      const [path, setPath] = useState('/workspaces/default/mcp-servers');
      return (
        <ConsoleShell
          currentPath={path}
          account={account}
          onLogout={() => undefined}
          onNavigate={async (next) => setPath(next)}
        >
          <div>{path}</div>
        </ConsoleShell>
      );
    }
    renderWithWorkspaces(<Harness />);
    expect(screen.getByRole('link', { name: 'Workspace management' }).getAttribute('href')).toBe(
      '/workspaces/default/llm-models',
    );
    fireEvent.click(screen.getByRole('link', { name: 'Workspace management' }));
    fireEvent.click(screen.getByRole('link', { name: 'Cost', exact: true }));
    fireEvent.click(screen.getByRole('link', { name: 'Back to navigation' }));
    expect(screen.getByRole('link', { name: 'Workspace management' }).getAttribute('href')).toBe(
      '/workspaces/default/cost',
    );
    cleanup();
    renderWithWorkspaces(<Harness />);
    expect(screen.getByRole('link', { name: 'Workspace management' }).getAttribute('href')).toBe(
      '/workspaces/default/cost',
    );
    fireEvent.click(screen.getByRole('link', { name: 'Workspace management' }));
    expect(screen.getByRole('link', { name: 'Cost', exact: true }).hasAttribute('data-active')).toBe(true);
    cleanup();
    renderWithWorkspaces(<Harness account={{ ...testAccount(), uuid: 'another-account' }} />);
    expect(screen.getByRole('link', { name: 'Workspace management' }).getAttribute('href')).toBe(
      '/workspaces/default/llm-models',
    );
  });

  test.each(['/usage/cache', 'https://outside.example', '/not-a-page'])(
    'ignores an unavailable or invalid remembered management module: %s',
    (stored) => {
      resetTestDom('https://oma.duck.ai/dashboard');
      window.localStorage.setItem('oma.workspace-management.last-page:["acct_test","org_test","default"]', stored);
      renderWithWorkspaces(
        <ConsoleShell currentPath="/dashboard" account={testAccount()} onLogout={() => undefined}>
          <div>Dashboard content</div>
        </ConsoleShell>,
      );
      expect(screen.getByRole('link', { name: 'Workspace management' }).getAttribute('href')).toBe(
        '/workspaces/default/llm-models',
      );
    },
  );

  test('keeps the current directory until navigation changes the route', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={testAccount()}
        onLogout={() => undefined}
        onNavigate={async () => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );
    const management = screen.getByRole('link', { name: 'Workspace management' });
    fireEvent.click(management, { ctrlKey: true });
    expect(screen.getByRole('link', { name: 'Overview' })).toBeTruthy();
    fireEvent.click(management);
    expect(screen.getByRole('link', { name: 'Overview' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Back to navigation' })).toBeNull();
  });

  test('keeps primary workflows, resources and every workspace module in one sidebar', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const navigate = mock(async (_path: string) => undefined);
    function Harness() {
      const [path, setPath] = useState('/dashboard');
      return (
        <ConsoleShell
          currentPath={path}
          account={testAccount()}
          onLogout={() => undefined}
          onNavigate={async (next) => {
            void navigate(next);
            setPath(next);
          }}
        >
          <div>Dashboard content</div>
        </ConsoleShell>
      );
    }
    renderWithWorkspaces(<Harness />);
    const account = screen.getByRole('button', { name: 'test org_test' });
    expect(screen.getByRole('link', { name: 'Overview' }).getAttribute('href')).toBe('/dashboard');
    for (const name of ['Quickstart', 'Agents', 'Sessions', 'Deployments', 'Environments'])
      expect(screen.getByRole('link', { name })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Observability' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'LLM models' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(
      Array.from(document.querySelectorAll('[data-resource-navigation] a')).map((link) => link.textContent),
    ).toEqual(['Skills', 'Files', 'Memory stores', 'Credential vaults', 'MCP Servers']);
    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Batches' })).toBeNull();
    fireEvent.click(screen.getByRole('link', { name: 'Workspace management' }));
    expect(
      Array.from(document.querySelectorAll('[data-workspace-section]')).map((section) => ({
        label: section.querySelector('[data-sidebar="group-label"]')?.textContent,
        links: Array.from(section.querySelectorAll('a')).map((link) => link.textContent),
      })),
    ).toEqual([
      { label: 'Models and access', links: ['Members', 'LLM models', 'API keys'] },
      { label: 'Usage and operations', links: ['Usage', 'Cost', 'Observability'] },
      { label: 'Integrations', links: ['MCP tunnels', 'Webhooks', 'OpenTelemetry'] },
    ]);
    expect(screen.queryByText('Preview')).toBeNull();
    for (const name of ['Caching', 'Rate limits', 'Logs'])
      expect(screen.queryByRole('link', { name, exact: true })).toBeNull();
    expect(screen.getByRole('link', { name: 'Documentation' }).closest('[data-console-footer-links]')).toBeTruthy();
    for (const name of [
      'LLM models',
      'Observability',
      'Usage',
      'Cost',
      'Members',
      'API keys',
      'Webhooks',
      'OpenTelemetry',
    ])
      expect(screen.getByRole('link', { name, exact: true })).toBeTruthy();
    expect(screen.getByRole('link', { name: /MCP tunnels/ }).getAttribute('href')).toBe(
      '/settings/workspaces/default/mcp-tunnels',
    );
    expect(document.querySelectorAll('[data-slot="sidebar"]').length).toBe(1);
    expect(screen.getByRole('button', { name: 'test org_test' })).toBe(account);
    expect(screen.getByRole('link', { name: 'Documentation' }).getAttribute('href')).toBe(
      'https://oma.mintlifysite.com/',
    );
    fireEvent.click(screen.getByRole('link', { name: 'Back to navigation' }));
    expect(screen.getByRole('link', { name: 'Agents' })).toBeTruthy();
    expect(navigate).toHaveBeenLastCalledWith('/dashboard');
  });

  test('keeps hidden workbench out of the expanded resource directory', () => {
    resetTestDom('https://oma.duck.ai/workbench');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/workbench" account={testAccount()} onLogout={() => undefined}>
        <div>Workbench content</div>
      </ConsoleShell>,
    );

    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
  });

  test.each([
    { width: 1030, height: 853 },
    { width: 1510, height: 754 },
  ])(
    'collapses resources in a constrained viewport $width x $height while allowing manual expansion',
    ({ width, height }) => {
      resetTestDom('https://oma.duck.ai/dashboard');
      setTestViewport(width, height);
      renderWithWorkspaces(
        <ConsoleShell currentPath="/dashboard" account={testAccount()} onLogout={() => undefined}>
          <div>Dashboard content</div>
        </ConsoleShell>,
      );
      const resource = screen.getByRole('button', { name: 'Resources', exact: true });
      expect(resource.getAttribute('aria-expanded')).toBe('false');
      expect(screen.queryByRole('link', { name: 'Files', exact: true })).toBeNull();
      fireEvent.click(resource);
      expect(resource.getAttribute('aria-expanded')).toBe('true');
      expect(screen.getByRole('link', { name: 'Files', exact: true })).toBeTruthy();
    },
  );

  test('responds to viewport changes and expands resources when entering an active resource route', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    function Harness() {
      const [path, setPath] = useState('/dashboard');
      return (
        <>
          <button onClick={() => setPath('/workspaces/default/files')}>Open files</button>
          <ConsoleShell currentPath={path} account={testAccount()} onLogout={() => undefined}>
            <div>Route content</div>
          </ConsoleShell>
        </>
      );
    }
    renderWithWorkspaces(<Harness />);
    const resource = screen.getByRole('button', { name: 'Resources', exact: true });
    expect(resource.getAttribute('aria-expanded')).toBe('true');
    act(() => setTestViewport(1030, 853));
    await waitFor(() => expect(resource.getAttribute('aria-expanded')).toBe('false'));
    act(() => setTestViewport(1280, 900));
    await waitFor(() => expect(resource.getAttribute('aria-expanded')).toBe('true'));
    act(() => setTestViewport(1030, 853));
    fireEvent.click(screen.getByRole('button', { name: 'Open files', exact: true }));
    expect(resource.getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files', exact: true })).toBeTruthy();
    act(() => setTestViewport(900, 700));
    expect(resource.getAttribute('aria-expanded')).toBe('true');
  });

  test('expands the sidebar group that contains the current route', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/workspaces/default/files" account={testAccount()} onLogout={() => undefined}>
        <div>Files content</div>
      </ConsoleShell>,
    );

    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Batches' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files' }).getAttribute('href')).toBe('/workspaces/default/files');

    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));

    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Files' })).toBeNull();
  });

  test('expands a collapsed group after navigation enters its route', () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    function FilesRouteHarness() {
      const [currentPath, setCurrentPath] = useState('/dashboard');
      return (
        <>
          <button type="button" onClick={() => setCurrentPath('/workspaces/default/files')}>
            Open files
          </button>
          <ConsoleShell currentPath={currentPath} account={testAccount()} onLogout={() => undefined}>
            <div>Route content</div>
          </ConsoleShell>
        </>
      );
    }

    renderWithWorkspaces(<FilesRouteHarness />);

    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Files' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Open files' }));

    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Batches' })).toBeNull();
  });

  test('用户可见 LLM 模型导航', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/llm-models');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/default/llm-models"
        account={testAccount('user')}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    expect(screen.getByRole('link', { name: 'LLM models' })).toBeTruthy();
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
      </ConsoleShell>,
    );

    expect(getWorkspaceMenuButton(/Default/i).closest('[data-sidebar-scroll-area="true"]')).toBeNull();
    const scrollArea = screen
      .getByRole('navigation', { name: /Console navigation/i })
      .closest('[data-sidebar-scroll-area="true"]');
    expect(scrollArea).toBeTruthy();
    expect(scrollArea?.classList.contains('scrollbar-none')).toBe(true);
  });

  test('uses the wide build layout for MCP server routes', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/mcp-servers');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/default/mcp-servers"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>MCP server content</div>
      </ConsoleShell>,
    );

    const content = screen.getByText('MCP server content').parentElement;
    expect(content?.className).toContain('lg:px-8');
    expect(content?.className).not.toContain('max-w-[928px]');
  });

  test.each([
    {
      Shell: ConsoleShell,
      path: '/dashboard',
      locale: 'en' as const,
      collapseLabel: 'Collapse',
      expandLabel: 'Expand',
    },
    {
      Shell: SettingsShell,
      path: '/settings/organization',
      locale: 'zh-CN' as const,
      collapseLabel: '收起',
      expandLabel: '展开',
    },
  ])('toggles the sidebar beside OMA on $path', ({ Shell, path, locale, collapseLabel, expandLabel }) => {
    resetTestDom(`https://oma.duck.ai${path}`);
    const navigate = mock(async () => undefined);
    renderWithWorkspaces(
      <Shell currentPath={path} account={testAccount()} onLogout={() => undefined} onNavigate={navigate}>
        <div>Sidebar toggle content</div>
      </Shell>,
      { locale },
    );

    const sidebar = document.querySelector('[data-slot="sidebar"]');
    const brand = screen.getByRole('link', { name: 'Open Managed Agents' });
    const trigger = screen.getByRole('button', { name: collapseLabel });
    expect(trigger.parentElement).toBe(brand.parentElement);
    expect(trigger.getAttribute('aria-expanded')).toBe('true');

    fireEvent.click(trigger);

    expect(sidebar?.getAttribute('data-state')).toBe('collapsed');
    const expand = screen.getByRole('button', { name: expandLabel });
    expect(expand.getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Open Managed Agents' })).toBeNull();
    expect(expand.parentElement?.querySelectorAll('button').length).toBe(1);
    expect(expand.parentElement?.querySelector('img')).toBeNull();
    expect(document.cookie).toContain('sidebar_state=false');
    expect(navigate).not.toHaveBeenCalled();

    fireEvent.click(expand);

    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
    expect(screen.getByRole('button', { name: collapseLabel }).getAttribute('aria-expanded')).toBe('true');
    expect(document.cookie).toContain('sidebar_state=true');

    fireEvent.click(screen.getByRole('link', { name: 'Open Managed Agents' }));

    expect(navigate).toHaveBeenCalledWith('/dashboard');
    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
  });

  test('collapses and expands the desktop sidebar from the sidebar rail', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    const sidebar = document.querySelector('[data-slot="sidebar"]');
    const main = screen.getByText('Dashboard content').closest('main');

    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('');
    expect(main?.getAttribute('data-slot')).toBe('sidebar-inset');

    const sidebarRail = screen.getByRole('button', { name: 'Toggle Sidebar' });

    fireEvent.click(sidebarRail);

    expect(sidebar?.getAttribute('data-state')).toBe('collapsed');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('icon');
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.getByRole('link', { name: 'Agents' })).toBeTruthy();

    fireEvent.click(sidebarRail);

    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('');
    expect(screen.getByRole('link', { name: 'Agents' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Files' })).toBeTruthy();
  });

  test('keeps mobile navigation labels and resources visible after a desktop collapse', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    document.cookie = 'sidebar_state=false; path=/';
    const matchMedia = window.matchMedia;
    window.matchMedia = (query) => ({
      media: query,
      matches: query === '(max-width: 767px)' || query === '(max-width: 1100px)',
      onchange: null,
      addListener: () => undefined,
      removeListener: () => undefined,
      addEventListener: () => undefined,
      removeEventListener: () => undefined,
      dispatchEvent: () => true,
    });
    try {
      renderWithWorkspaces(
        <ConsoleShell currentPath="/dashboard" account={testAccount()} onLogout={() => undefined}>
          <div>Dashboard content</div>
        </ConsoleShell>,
      );
      fireEvent.click(screen.getByRole('button', { name: 'Expand', exact: true }));
      const sidebar = await screen.findByRole('dialog', { name: 'Sidebar', exact: true });
      expect(within(sidebar).getByText('Default', { exact: true })).toBeTruthy();
      expect(within(sidebar).getByRole('link', { name: 'Agents', exact: true }).textContent).toBe('Agents');
      expect(
        within(sidebar).getByRole('button', { name: 'Resources', exact: true }).getAttribute('aria-expanded'),
      ).toBe('false');
      fireEvent.click(within(sidebar).getByRole('button', { name: 'Resources', exact: true }));
      expect(within(sidebar).getByRole('link', { name: 'Files', exact: true }).textContent).toBe('Files');
      expect(
        within(sidebar).getByRole('button', { name: 'Resources', exact: true }).getAttribute('aria-expanded'),
      ).toBe('true');
    } finally {
      cleanup();
      window.matchMedia = matchMedia;
    }
  });

  test('preserves the desktop sidebar state when entering a session detail route', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');

    function SessionRouteHarness() {
      const [currentPath, setCurrentPath] = useState('/workspaces/default/sessions');
      return (
        <>
          <button type="button" onClick={() => setCurrentPath('/workspaces/default/sessions/sesn_one123456')}>
            Open session
          </button>
          <ConsoleShell
            currentPath={currentPath}
            account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
            onLogout={() => undefined}
          >
            <div>Session content</div>
          </ConsoleShell>
        </>
      );
    }

    renderWithWorkspaces(<SessionRouteHarness />);

    const sidebar = document.querySelector('[data-slot="sidebar"]');
    const sessionsMain = screen.getByText('Session content').closest('main');
    expect(sessionsMain?.classList.contains('session-route-theme')).toBe(false);
    expect(sidebar?.getAttribute('data-state')).toBe('expanded');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('');

    fireEvent.click(screen.getByRole('button', { name: 'Toggle Sidebar' }));
    expect(sidebar?.getAttribute('data-state')).toBe('collapsed');

    fireEvent.click(screen.getByRole('button', { name: 'Open session' }));

    expect(sidebar?.getAttribute('data-state')).toBe('collapsed');
    expect(sidebar?.getAttribute('data-collapsible')).toBe('icon');
    const sessionMain = screen.getByText('Session content').closest('main');
    const workspaceShell = screen.getByText('Session content').parentElement;
    expect(sessionMain?.classList.contains('h-svh')).toBe(true);
    expect(sessionMain?.classList.contains('min-h-0')).toBe(true);
    expect(sessionMain?.classList.contains('overflow-hidden')).toBe(true);
    expect(sessionMain?.classList.contains('session-route-theme')).toBe(true);
    expect(workspaceShell?.hasAttribute('data-session-workspace-shell')).toBe(true);
    expect(workspaceShell?.classList.contains('flex-1')).toBe(true);
    expect(workspaceShell?.classList.contains('min-h-0')).toBe(true);
    expect(workspaceShell?.classList.contains('overflow-hidden')).toBe(true);
    expect(workspaceShell?.classList.contains('px-4')).toBe(false);
    expect(workspaceShell?.classList.contains('lg:px-8')).toBe(false);
    expect(screen.getByRole('link', { name: 'Sessions' })).toBeTruthy();
  });

  test('restores the desktop sidebar state after the console shell remounts', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/sessions');
    const account = { uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' };
    const listView = renderWithWorkspaces(
      <ConsoleShell currentPath="/workspaces/default/sessions" account={account} onLogout={() => undefined}>
        <div>Sessions list</div>
      </ConsoleShell>,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Toggle Sidebar' }));
    expect(document.querySelector('[data-slot="sidebar"]')?.getAttribute('data-state')).toBe('collapsed');
    expect(document.cookie).toContain('sidebar_state=false');

    listView.unmount();
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/default/sessions/sesn_one123456"
        account={account}
        onLogout={() => undefined}
      >
        <div>Session detail</div>
      </ConsoleShell>,
    );

    expect(document.querySelector('[data-slot="sidebar"]')?.getAttribute('data-state')).toBe('collapsed');
    expect(screen.getByText('Session detail').closest('main')?.classList.contains('h-svh')).toBe(true);
  });

  test('keeps document scrolling for non-session console routes', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/dashboard" account={testAccount()} onLogout={() => undefined}>
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    const main = screen.getByText('Dashboard content').closest('main');
    expect(main?.classList.contains('min-h-screen')).toBe(true);
    expect(main?.classList.contains('h-svh')).toBe(false);
    expect(main?.classList.contains('overflow-hidden')).toBe(false);
    expect(main?.classList.contains('session-route-theme')).toBe(false);
    expect(screen.getByText('Dashboard content').parentElement?.hasAttribute('data-session-workspace-shell')).toBe(
      false,
    );
  });

  test('localizes the desktop sidebar rail for Chinese users', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
      { locale: 'zh-CN' },
    );

    expect(screen.getByRole('button', { name: '切换侧边栏' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Toggle Sidebar' })).toBeNull();
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
      </ConsoleShell>,
    );

    fireEvent.click(screen.getByRole('link', { name: 'Overview' }));

    expect(navigate).toHaveBeenCalledWith('/dashboard');
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
      </ConsoleShell>,
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
      </ConsoleShell>,
    );

    fireEvent.click(screen.getByRole('button', { name: /test/i }));

    const menu = screen.getAllByRole('menu')[0];
    expect(menu.closest('[data-sidebar-state]')).toBeNull();
    expect(screen.queryByRole('menuitemradio', { name: /Default API plan/i })).toBeNull();
    expect(screen.getByRole('menuitem', { name: 'Organization settings' })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Profile' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'System' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'Light' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'Dark' })).toBeTruthy();
    expect(screen.queryByRole('menuitem', { name: 'Appearance' })).toBeNull();
    expect(screen.getByRole('menuitem', { name: 'Language' })).toBeTruthy();
    expect(screen.queryByRole('menuitem', { name: 'Feedback' })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: 'Get help' })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: 'Legal center' })).toBeNull();
    expect(screen.queryByText('Theme')).toBeNull();

    fireEvent.click(screen.getByRole('menuitem', { name: /Log out/i }));

    await waitFor(() => expect(logout).toHaveBeenCalled());
  });

  test('opens the language submenu to the right', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /test/i }));
    });
    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: 'Language' }));
    });

    expect(screen.getByRole('menu', { name: 'Language' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: 'English' }).getAttribute('aria-checked')).toBe('true');
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
      </ConsoleShell>,
    );

    fireEvent.click(screen.getByRole('button', { name: /test/i }));
    expect(screen.getByText('Organization settings')).toBeTruthy();

    fireEvent.pointerDown(document.body);

    await waitFor(() => expect(screen.queryByText('Organization settings')).toBeNull());
  });

  test('renders migrated shell text in Chinese and switches language from the account menu', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    function Harness() {
      const [path, setPath] = useState('/dashboard');
      return (
        <ConsoleShell
          currentPath={path}
          account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
          onLogout={() => undefined}
          onNavigate={async (next) => setPath(next)}
        >
          <div>Dashboard content</div>
        </ConsoleShell>
      );
    }
    renderWithWorkspaces(<Harness />, { locale: 'zh-CN' });

    expect(document.documentElement.lang).toBe('zh-CN');
    expect(screen.getByText('概览')).toBeTruthy();
    expect(screen.getByText('资源')).toBeTruthy();
    fireEvent.click(screen.getByRole('link', { name: '工作区管理' }));
    expect(screen.getByRole('link', { name: 'API 密钥' }).getAttribute('href')).toBe(
      '/settings/workspaces/default/keys',
    );

    fireEvent.click(screen.getByRole('button', { name: /test/i }));
    expect(screen.getByText('组织设置')).toBeTruthy();
    expect(screen.getByText('退出登录')).toBeTruthy();

    fireEvent.click(screen.getByRole('menuitem', { name: '语言' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: /English/i }));

    expect(document.documentElement.lang).toBe('en');
    expect(window.localStorage.getItem('oma.locale')).toBe('en');
    expect(screen.getByRole('link', { name: 'LLM models' })).toBeTruthy();
  });

  test('opens the workspace selector with labeled workspace items and create action', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    await act(async () => {
      fireEvent.click(getWorkspaceMenuButton(/Default/i));
    });

    expect(screen.getByText('Workspaces')).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: /Default/i }).getAttribute('aria-current')).toBe('true');
    expect(screen.getByRole('menuitem', { name: /foo/i })).toBeTruthy();
    expect(screen.getByRole('menuitem', { name: 'Create workspace' })).toBeTruthy();
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
      </ConsoleShell>,
    );

    fireEvent.click(getWorkspaceMenuButton(/Default/i));
    expect(screen.getAllByRole('menu').length).toBeGreaterThan(0);

    fireEvent.mouseDown(document.body);
    fireEvent.click(document.body);

    await waitFor(() => expect(screen.queryByRole('menuitem', { name: /Default/i })).toBeNull());
  });

  test('切换工作区不改变账号区的当前组织', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    await act(async () => {
      fireEvent.click(getWorkspaceMenuButton(/Default/i));
    });
    await waitFor(() => expect(screen.getByRole('menuitem', { name: /foo/i })).toBeTruthy());
    await act(async () => {
      fireEvent.click(screen.getByRole('menuitem', { name: /foo/i }));
    });
    expect(getWorkspaceMenuButton(/foo/i)).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole('button', { name: /test/i }));
    });
    const account = screen.getByRole('button', { name: /test/i });
    expect(within(account).getByText('org_test')).toBeTruthy();
    expect(within(account).queryByText('foo')).toBeNull();
  });

  test('账号区显示当前组织而不是首个成员组织', () => {
    resetTestDom('https://oma.duck.ai/dashboard');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{
          uuid: 'acct_test',
          email_address: 'test@example.com',
          display_name: 'test',
          memberships: [
            { role: 'user', organization: { uuid: 'org_other' } },
            { role: 'admin', organization: { uuid: 'org_test' } },
          ],
        }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
    );

    const account = screen.getByRole('button', { name: /test/i });
    expect(within(account).getByText('org_test')).toBeTruthy();
    expect(within(account).queryByText('org_other')).toBeNull();
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
      </ConsoleShell>,
    );

    fireEvent.click(getWorkspaceMenuButton(/Default/i));
    fireEvent.click(screen.getByRole('menuitem', { name: /foo/i }));

    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/workspaces/wrkspc_foo/agents'));
    expect(getWorkspaceMenuButton(/foo/i)).toBeTruthy();
  });

  test.each(['agents', 'sessions', 'environments', 'vaults', 'memory-stores', 'skills', 'mcp-servers'])(
    '从 %s 详情切换工作区后导航到列表',
    async (section) => {
      const path = `/workspaces/default/${section}/old-resource`;
      resetTestDom(`https://oma.duck.ai${path}`);
      const navigate = mock(async () => undefined);
      renderWithWorkspaces(
        <ConsoleShell
          currentPath={path}
          account={{ uuid: 'acct_test', email_address: 'test@example.com' }}
          onLogout={() => undefined}
          onNavigate={navigate}
        >
          <div>Resource detail</div>
        </ConsoleShell>,
      );
      fireEvent.click(getWorkspaceMenuButton(/Default/i));
      fireEvent.click(screen.getByRole('menuitem', { name: /foo/i }));
      await waitFor(() => expect(navigate).toHaveBeenCalledWith(`/workspaces/wrkspc_foo/${section}`));
    },
  );

  test('syncs the workspace selector from workspace-scoped routes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/wrkspc_foo/logs');

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/wrkspc_foo/logs"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Logs content</div>
      </ConsoleShell>,
    );

    await waitFor(() => expect(getWorkspaceMenuButton(/foo/i)).toBeTruthy());
  });

  test.each([false, true])('工作区操作入口遵循管理权限：%s', async (canManageWorkspaces) => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const navigate = mock(async () => undefined);
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com' }}
        onLogout={() => undefined}
        onNavigate={navigate}
      >
        <div>Dashboard</div>
      </ConsoleShell>,
      { canManageWorkspaces, locale: 'zh-CN' },
    );
    fireEvent.click(getWorkspaceMenuButton('默认'));
    if (!canManageWorkspaces) {
      expect(screen.queryByRole('menuitem', { name: '管理工作区' })).toBeNull();
      expect(screen.queryByRole('menuitem', { name: /创建工作区/ })).toBeNull();
      return;
    }
    expect(screen.getByRole('menuitem', { name: /创建工作区/ })).toBeTruthy();
    const manage = screen.getByRole('menuitem', { name: '管理工作区' });
    expect(manage.getAttribute('href')).toBe('/settings/workspaces');
    fireEvent.click(manage);
    await waitFor(() => expect(navigate).toHaveBeenCalledWith('/settings/workspaces'));
    await waitFor(() => expect(screen.queryByRole('menuitem', { name: '管理工作区' })).toBeNull());
  });

  test('creates a workspace with color and US residency', async () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const createWorkspace = mock(async (input: CreateWorkspaceInput) => ({
      id: 'wrkspc_bar',
      type: 'workspace' as const,
      name: input.name,
      display_color: input.display_color,
      color: input.display_color,
      data_residency: input.data_residency,
    }));

    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/dashboard"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Dashboard content</div>
      </ConsoleShell>,
      { createWorkspace },
    );

    fireEvent.click(getWorkspaceMenuButton(/Default/i));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Create workspace' }));

    expect(screen.getByRole('button', { name: 'Create' }).hasAttribute('disabled')).toBe(true);

    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'bar' } });
    fireEvent.click(screen.getByRole('radio', { name: 'Sage' }));
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));

    await waitFor(() => expect(createWorkspace).toHaveBeenCalled());
    expect(createWorkspace.mock.calls[0][0]).toEqual({
      name: 'bar',
      display_color: '#D8D2A6',
      data_residency: {
        workspace_geo: 'us',
      },
    });
    expect(getWorkspaceMenuButton(/bar/i)).toBeTruthy();
  });
});

function getWorkspaceMenuButton(name: RegExp | string) {
  const matches = screen.getAllByRole('button', { name });
  const match = matches.find((button) => matchesName(button.getAttribute('aria-label'), name));
  if (!match) {
    throw new Error(`Workspace menu button ${String(name)} was not found.`);
  }
  return match;
}

function matchesName(value: string | null, expected: RegExp | string) {
  if (!value) {
    return false;
  }
  return typeof expected === 'string' ? value === expected : expected.test(value);
}

function testAccount(role = 'admin'): AuthAccount {
  return {
    uuid: 'acct_test',
    email_address: 'test@example.com',
    display_name: 'test',
    memberships: [{ role, organization: { uuid: 'org_test' } }],
  };
}

function renderWithWorkspaces(
  children: ReactNode,
  options: {
    createWorkspace?: (input: CreateWorkspaceInput) => Promise<Workspace>;
    locale?: Locale;
    effectiveRole?: string;
    canManageWorkspaces?: boolean;
  } = {},
) {
  const tree = (
    <WorkspaceHarness
      createWorkspace={options.createWorkspace}
      canManageWorkspaces={options.canManageWorkspaces}
      effectiveRole={options.effectiveRole}
    >
      {children}
    </WorkspaceHarness>
  );
  return render(options.locale ? <I18nProvider initialLocale={options.locale}>{tree}</I18nProvider> : tree);
}

function WorkspaceHarness({
  children,
  createWorkspace,
  canManageWorkspaces = true,
  effectiveRole,
}: {
  children: ReactNode;
  createWorkspace?: (input: CreateWorkspaceInput) => Promise<Workspace>;
  canManageWorkspaces?: boolean;
  effectiveRole?: string;
}) {
  const [workspaces, setWorkspaces] = useState<Workspace[]>([
    { ...defaultWorkspace, effective_role: effectiveRole },
    {
      id: 'wrkspc_foo',
      type: 'workspace',
      name: 'foo',
      display_color: '#9B87F5',
      color: '#9B87F5',
    },
  ]);
  const [activeWorkspaceId, setActiveWorkspaceId] = useState(defaultWorkspace.id);
  const activeWorkspace = workspaces.find((workspace) => workspace.id === activeWorkspaceId) ?? defaultWorkspace;

  const value = useMemo<WorkspaceContextValue>(
    () => ({
      orgUuid: 'org_test',
      canManageWorkspaces,
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
              data_residency: input.data_residency,
            };
        setWorkspaces((current) => [...current, created]);
        setActiveWorkspaceId(created.id);
        return created;
      },
      refreshWorkspaces: async () => undefined,
    }),
    [activeWorkspace, activeWorkspaceId, canManageWorkspaces, createWorkspace, workspaces],
  );

  return <WorkspaceContext.Provider value={value}>{children}</WorkspaceContext.Provider>;
}

test.each([
  '/quickstart',
  '/quickstart/',
  '/workspaces/default/agent-quickstart',
  '/workspaces/default/agent-quickstart/',
])('keeps quickstart content bounded at %s', (currentPath) => {
  resetTestDom(`https://oma.duck.ai${currentPath}`);
  renderWithWorkspaces(
    <ConsoleShell currentPath={currentPath} account={testAccount()} onLogout={() => undefined}>
      <div>Quickstart route content</div>
    </ConsoleShell>,
  );
  const container = screen.getByText('Quickstart route content').parentElement;
  expect(container?.classList.contains('flex-1')).toBe(true);
  expect(container?.classList.contains('overflow-hidden')).toBe(true);
  expect(container?.parentElement?.classList.contains('h-dvh')).toBe(true);
});
