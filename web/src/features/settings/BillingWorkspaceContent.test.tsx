import { afterEach, expect, mock, test } from 'bun:test';
import { I18nProvider } from '../../shared/i18n';
import { defaultWorkspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { BillingWorkspaceContent } from './BillingWorkspaceContent';

const { render, screen, cleanup } = await import('@testing-library/react');
afterEach(cleanup);

function renderContent(role: string, path: string, isLoading = false) {
  resetTestDom('https://oma.duck.ai' + path);
  const workspace = { ...defaultWorkspace, effective_role: role };
  const value: WorkspaceContextValue = {
    activeWorkspace: workspace,
    activeWorkspaceId: workspace.id,
    workspaces: [workspace],
    isLoading,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async () => workspace,
    refreshWorkspaces: async () => undefined,
  };
  const mount = mock();
  function ResourcePage() {
    mount();
    return <p>Actual resource page</p>;
  }
  render(
    <I18nProvider initialLocale="en">
      <WorkspaceContext.Provider value={value}>
        <BillingWorkspaceContent currentPath={path}>
          <ResourcePage />
        </BillingWorkspaceContent>
      </WorkspaceContext.Provider>
    </I18nProvider>,
  );
  return mount;
}

test('目标工作区不在可访问列表时不把拒绝伪装为空态', () => {
  renderContent('workspace_billing', '/workspaces/other/agents');
  expect(screen.getByText('Actual resource page')).toBeTruthy();
});

test('权限加载完成前不挂载开发资源请求', () => {
  const mount = renderContent('workspace_billing', '/workspaces/default/agents', true);
  expect(mount).not.toHaveBeenCalled();
  expect(screen.queryByText('No resources to display.')).toBeNull();
});

test('Billing 七类资源页显示空态且不挂载资源请求', () => {
  for (const resource of ['agents', 'files', 'skills', 'sessions', 'environments', 'vaults', 'memory-stores']) {
    const mount = renderContent('workspace_billing', '/workspaces/default/' + resource);
    expect(screen.getByText('No resources to display.')).toBeTruthy();
    expect(mount).not.toHaveBeenCalled();
    cleanup();
  }
});

test('Billing 提权后恢复真实资源页面', () => {
  renderContent('workspace_admin', '/workspaces/default/agents');
  expect(screen.getByText('Actual resource page')).toBeTruthy();
});

test('其他角色与 Billing 管理页面保持原行为', () => {
  for (const [role, path] of [
    ['workspace_user', '/workspaces/default/agents'],
    ['workspace_developer', '/workspaces/default/agents'],
    ['workspace_billing', '/members'],
    ['workspace_billing', '/settings/billing'],
  ]) {
    renderContent(role, path);
    expect(screen.getByText('Actual resource page')).toBeTruthy();
    cleanup();
  }
});

test('Billing 的 Workbench 不被开发资源空态拦截', () => {
  const mount = renderContent('workspace_billing', '/workspaces/default/workbench');
  expect(mount).toHaveBeenCalled();
  expect(screen.getByText('Actual resource page')).toBeTruthy();
});
