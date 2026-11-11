import { afterEach, beforeEach, describe, expect, mock, test } from 'bun:test';
import { useMemo, useState, type ReactNode } from 'react';
import { resetTestDom, setTestViewport } from '../../test/setup';
import type { AuthAccount } from '../../shared/auth/api';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { defaultWorkspace, type CreateWorkspaceInput, type Workspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';

const testingLibrary = await import('@testing-library/react');
const { ConsoleShell } = await import('./ConsoleLayout');

const { act, cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

function clearManagementHistory() {
  for (const key of Object.keys(window.sessionStorage)) {
    if (key.startsWith('oma.workspace-management.return:')) window.sessionStorage.removeItem(key);
  }
  for (const key of Object.keys(window.localStorage)) {
    if (key.startsWith('oma.workspace-management.last-page:')) window.localStorage.removeItem(key);
  }
}

const defaultViewport = { width: window.innerWidth, height: window.innerHeight };
beforeEach(() => {
  clearManagementHistory();
  window.sessionStorage.clear();
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
    expect(
      screen
        .getByRole('link', { name: locale === 'zh-CN' ? '文档' : 'Documentation', exact: true })
        .getAttribute('href'),
    ).toBe(`https://oma.mintlifysite.com/docs/${locale === 'zh-CN' ? 'zh' : 'en'}/overview`);
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

  test.each([
    { locale: 'en', resources: 'Resources', skills: 'Skills', channels: 'Channels' },
    { locale: 'zh-CN', resources: '资源', skills: '技能', channels: '渠道' },
  ] as const)(
    'keeps channels after skills in expanded and compact $locale resources',
    async ({ locale, resources, skills, channels }) => {
      const path = '/workspaces/default/channels';
      resetTestDom(`https://oma.duck.ai${path}`);
      renderWithWorkspaces(
        <ConsoleShell currentPath={path} account={testAccount()} onLogout={() => undefined}>
          <div>Channels content</div>
        </ConsoleShell>,
        { locale },
      );
      expect(screen.queryByRole('link', { name: channels, exact: true })).toBeNull();
      const group = screen.getByRole('button', { name: resources, exact: true });
      expect(group.getAttribute('aria-expanded')).toBe('false');
      expect(group.hasAttribute('data-active')).toBe(true);
      fireEvent.click(group);
      const link = screen.getByRole('link', { name: channels, exact: true });
      expect(link.getAttribute('href')).toBe(path);
      expect(link.getAttribute('aria-current')).toBe('page');
      expect(link.closest('li')?.previousElementSibling?.textContent).toBe(skills);
      await act(async () => setTestViewport(950, 850));
      await waitFor(() =>
        expect(document.querySelector('[data-sidebar-state]')?.getAttribute('data-sidebar-state')).toBe('collapsed'),
      );
      fireEvent.click(screen.getByRole('button', { name: resources, exact: true }));
      const compactLink = await screen.findByRole('menuitem', { name: channels, exact: true });
      expect(compactLink.getAttribute('href')).toBe(path);
      const names = screen.getAllByRole('menuitem').map((item) => item.textContent);
      expect(names[names.indexOf(skills) + 1]).toBe(channels);
    },
  );

  test('uses the full-width console layout for workspace channels', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/channels');
    renderWithWorkspaces(
      <ConsoleShell
        currentPath="/workspaces/default/channels"
        account={{ uuid: 'acct_test', email_address: 'test@example.com', display_name: 'test' }}
        onLogout={() => undefined}
      >
        <div>Channels content</div>
      </ConsoleShell>,
    );

    const contentContainer = screen.getByText('Channels content').parentElement;
    expect(contentContainer?.classList.contains('lg:px-8')).toBe(true);
    expect(contentContainer?.classList.contains('max-w-[928px]')).toBe(false);
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

  test('keeps hidden workbench out of the expanded resource directory', () => {
    resetTestDom('https://oma.duck.ai/workbench');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/workbench" account={testAccount()} onLogout={() => undefined}>
        <div>Workbench content</div>
      </ConsoleShell>,
    );

    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
  });

  test('starts resource routes collapsed and opens them on request', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    renderWithWorkspaces(
      <ConsoleShell currentPath="/workspaces/default/files" account={testAccount()} onLogout={() => undefined}>
        <div>Files content</div>
      </ConsoleShell>,
    );

    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Batches' })).toBeNull();
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files' }).getAttribute('href')).toBe('/workspaces/default/files');

    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));

    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Files' })).toBeNull();
  });

  test('keeps resources collapsed when navigation enters a resource route', () => {
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
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    expect(screen.queryByRole('link', { name: 'Files' })).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Open files' }));

    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('false');
    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));
    expect(screen.getByRole('button', { name: 'Resources' }).getAttribute('aria-expanded')).toBe('true');
    expect(screen.getByRole('link', { name: 'Files' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Workbench' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Batches' })).toBeNull();
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

    fireEvent.click(screen.getByRole('button', { name: 'Resources' }));
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
    expect(screen.queryByRole('menuitem', { name: 'Organization settings' })).toBeNull();
    expect(screen.queryByRole('menuitem', { name: 'Profile' })).toBeNull();
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
    expect(screen.getByRole('menuitemradio', { name: '简体中文' })).toBeTruthy();
    expect(screen.queryByRole('menuitemradio', { name: 'Simplified Chinese' })).toBeNull();
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
    expect(screen.getByText('Log out')).toBeTruthy();

    fireEvent.pointerDown(document.body);

    await waitFor(() => expect(screen.queryByText('Log out')).toBeNull());
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
    fireEvent.click(screen.getByRole('link', { name: '设置' }));
    expect(screen.getByRole('link', { name: 'API 密钥' }).getAttribute('href')).toBe(
      '/settings/workspaces/default/keys',
    );

    fireEvent.click(screen.getByRole('button', { name: /test/i }));
    expect(screen.queryByText('组织设置')).toBeNull();
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
    initialWorkspaceId?: string;
    createWorkspace?: (input: CreateWorkspaceInput) => Promise<Workspace>;
    locale?: Locale;
    effectiveRole?: string;
    canManageWorkspaces?: boolean;
  } = {},
) {
  const tree = (
    <WorkspaceHarness
      initialWorkspaceId={options.initialWorkspaceId}
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
  initialWorkspaceId = defaultWorkspace.id,
  createWorkspace,
  canManageWorkspaces = true,
  effectiveRole,
}: {
  children: ReactNode;
  initialWorkspaceId?: string;
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
  const [activeWorkspaceId, setActiveWorkspaceId] = useState(initialWorkspaceId);
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
  expect(container?.classList.contains('console-quickstart-content')).toBe(true);
  expect(container?.parentElement?.classList.contains('console-quickstart-inset')).toBe(true);
});
