import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import type { ReactNode } from 'react';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { setConsoleRequestContext } from '../../shared/api/client';
import { defaultWorkspace, type Workspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { resetTestDom } from '../../test/setup';
import { BatchesPage, DashboardPage, FilesPage, SkillDetailPage, SkillsPage } from './DashboardPage';

const testingLibrary = await import('@testing-library/react');
const { cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

const originalFetch = globalThis.fetch;
const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard');
const originalCreateObjectURLDescriptor = Object.getOwnPropertyDescriptor(URL, 'createObjectURL');
const originalRevokeObjectURLDescriptor = Object.getOwnPropertyDescriptor(URL, 'revokeObjectURL');
const originalConfirm = window.confirm;

function selectOption(name: string) {
  const option = screen.getByRole('option', { name });
  fireEvent.pointerDown(option);
  fireEvent.mouseDown(option);
  fireEvent.pointerUp(option);
  fireEvent.mouseUp(option);
  fireEvent.click(option);
}

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.confirm = originalConfirm;
  restoreClipboard();
  restoreObjectUrl();
});

describe('Dashboard i18n', () => {
  test('renders dashboard and playground chrome in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default');
    const dashboard = renderDashboardPage(<DashboardPage section="dashboard" />, undefined, 'zh-CN');

    await waitFor(() => expect(document.documentElement.lang).toBe('zh-CN'));
    expect(screen.getByRole('heading', { name: '早上好，test' })).toBeTruthy();
    const getApiKey = screen.getByRole('link', { name: '获取 API 密钥' });
    expect(getApiKey.getAttribute('href')).toBe('/settings/workspaces/default/keys');
    expect(getApiKey.dataset.slot).toBe('button');
    const docsLink = screen.getByRole('link', { name: '查看文档' });
    expect(docsLink.getAttribute('href')).toBe('https://docs.anthropic.com/');
    expect(screen.getByRole('link', { name: '构建 Agent' }).dataset.slot).toBe('button');
    expect(screen.getByText('本月支出')).toBeTruthy();
    expect(screen.getByRole('heading', { name: '模型' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '资源' })).toBeTruthy();

    dashboard.unmount();

    renderDashboardPage(<DashboardPage section="playground" />, undefined, 'zh-CN');

    expect(screen.getByRole('heading', { name: 'Playground' })).toBeTruthy();
    expect(screen.getByText('配置')).toBeTruthy();
    expect(screen.getByText('开始对话以预览 Claude 的响应。')).toBeTruthy();
    const messageInput = screen.getByLabelText('消息');
    expect(messageInput).toBeTruthy();
    expect((messageInput as HTMLElement).dataset.slot).toBe('input-group-control');
    const messageComposer = (messageInput as HTMLElement).closest('[data-slot="input-group"]');
    expect(messageComposer).toBeTruthy();
    expect(screen.getByPlaceholderText('向 Claude 发送消息...')).toBeTruthy();
    expect(screen.getByRole('button', { name: '发送' })).toBeTruthy();
  });

  test('renders files, skills, and batches empty states in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    mockFilesList(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null
    }));
    const files = renderFilesPage(undefined, 'zh-CN');

    expect(await screen.findByRole('heading', { name: '文件' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '文件列表' })).toBeTruthy();
    expect(await screen.findByText('Default 工作区还没有上传文件。')).toBeTruthy();
    expect(screen.getByRole('button', { name: '上一页' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '下一页' })).toBeTruthy();

    files.unmount();
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    mockSkillsApi((url) => {
      if (url.includes('/versions/latest')) {
        throw new Error(`Unexpected request: ${url}`);
      }
      return {
        data: [],
        has_more: false,
        next_page: null
      };
    });
    const skills = renderSkillsPage(undefined, 'zh-CN');

    expect(await screen.findByRole('heading', { name: '技能' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '创建技能' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '技能列表' })).toBeTruthy();
    expect(await screen.findByText('Default 工作区还没有创建技能。')).toBeTruthy();

    skills.unmount();
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    mockMessageBatchesApi(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null
    }));
    renderBatchesPage(undefined, 'zh-CN');

    expect(await screen.findByRole('heading', { name: '批处理' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '批处理列表' })).toBeTruthy();
    expect(await screen.findByText('Default 工作区还没有创建批处理。')).toBeTruthy();
  });

  test('renders Claude Code and manage sections in Chinese', () => {
    resetTestDom('https://oma.duck.ai/claude-code/usage');
    const claudeCode = renderDashboardPage(<DashboardPage section="claude-code-usage" />, undefined, 'zh-CN');

    expect(screen.getByRole('heading', { name: 'Claude Code 用量' })).toBeTruthy();
    expect(screen.getByText('席位')).toBeTruthy();
    expect(screen.getByText('暂无 Claude Code 用量')).toBeTruthy();

    claudeCode.unmount();
    resetTestDom('https://oma.duck.ai/settings/service-accounts');
    const serviceAccounts = renderDashboardPage(<DashboardPage section="service-accounts" />, undefined, 'zh-CN');

    expect(screen.getByRole('heading', { name: '服务账号' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '创建服务账号' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '还没有服务账号' })).toBeTruthy();

    serviceAccounts.unmount();
    resetTestDom('https://oma.duck.ai/security');
    renderDashboardPage(<DashboardPage section="security" />, undefined, 'zh-CN');

    expect(screen.getByRole('heading', { name: '安全' })).toBeTruthy();
    expect(screen.getByRole('switch', { name: '管理员多因素认证' })).toBeTruthy();
    expect(screen.queryByText('暂无安全警报')).toBeNull();
  });

  test('uses standard Claude Code settings actions', async () => {
    resetTestDom('https://oma.duck.ai/claude-code/settings');

    renderDashboardPage(<DashboardPage section="claude-code-settings" />);

    expect(screen.getByRole('heading', { name: 'Claude Code settings' })).toBeTruthy();
    const manage = screen.getByRole('link', { name: 'Manage' });
    expect(manage.getAttribute('href')).toBe('/settings/members');
    expect(manage.dataset.slot).toBe('button');
    expect(screen.getByText('Current default: Members with a Claude Code seat')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Configure' }));

    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Configure workspace permissions' })).toBeTruthy();

    fireEvent.click(screen.getByRole('combobox', { name: 'Default access' }));
    selectOption('Disabled by default');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Disabled by default')).toBeTruthy();
  });

  test('uses standard rate limit notice actions', () => {
    resetTestDom('https://oma.duck.ai/limits');

    renderDashboardPage(<DashboardPage section="limits" />);

    expect(screen.getByRole('heading', { name: 'Rate limits' })).toBeTruthy();
    const monitorUsage = screen.getByRole('link', { name: 'Monitor rate limit usage' });
    expect(monitorUsage.getAttribute('href')).toBe('/usage/limits');
    expect(monitorUsage.dataset.slot).toBe('button');
    const contactSales = screen.getByRole('link', { name: 'Contact sales' });
    expect(contactSales.getAttribute('href')).toBe('https://www.anthropic.com/contact-sales');
    expect(contactSales.getAttribute('target')).toBe('_blank');
    expect(contactSales.getAttribute('rel')).toBe('noreferrer');
    expect(contactSales.dataset.slot).toBe('button');
  });

  test('uses shared button links on security actions', () => {
    resetTestDom('https://oma.duck.ai/security');

    renderDashboardPage(<DashboardPage section="security" />);

    const viewLogs = screen.getByRole('link', { name: 'View logs' });
    expect(viewLogs.getAttribute('href')).toBe('/logs');
    expect(viewLogs.dataset.slot).toBe('button');
    const manageMembers = screen.getByRole('link', { name: 'Manage' });
    expect(manageMembers.getAttribute('href')).toBe('/members');
    expect(manageMembers.dataset.slot).toBe('button');
  });

  test('uses the shared organization members surface on the manage members route', async () => {
    resetTestDom('https://oma.duck.ai/members');
    const api = mockOrganizationMembersApi();

    renderDashboardPage(
      <DashboardPage section="members" />,
      undefined,
      'en',
      makeAuthContextValue({
        memberships: [{ role: 'admin' }]
      })
    );

    expect(await screen.findByRole('heading', { name: 'Members 2' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Invite' })).toBeTruthy();
    expect(screen.queryByRole('link', { name: 'Invite' })).toBeNull();
    expect(screen.getByRole('button', { name: 'More actions' })).toBeTruthy();
    expect(screen.getByText('pending@example.com')).toBeTruthy();
    expect(screen.getByText('admin@example.local')).toBeTruthy();
    expect(api.requests).toContain('/api/console/organizations/org_test_uuid/members');
    expect(api.requests).toContain('/api/console/organizations/org_test_uuid/invites?status=pending');
  });

  test('uses the shared workspace webhooks surface on the manage webhooks route', async () => {
    resetTestDom('https://oma.duck.ai/webhooks');
    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: defaultWorkspace.id
    });
    const api = mockWebhooksList();

    renderDashboardPage(<DashboardPage section="webhooks" />);

    expect(await screen.findByRole('heading', { name: 'Webhooks' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Add webhook endpoint' })).toBeTruthy();
    expect(await screen.findByText('No webhook endpoints have been created for Default.')).toBeTruthy();
    expect(screen.queryByText('Create an endpoint to receive event notifications from Open Managed Agents.')).toBeNull();
    expect(api.requests[0]?.url).toBe('/v1/webhooks?beta=true');
    expect(api.requests[0]?.headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(api.requests[0]?.headers.get('x-workspace-id')).toBe(defaultWorkspace.id);
  });

  test('uses standard service account dialog, tabs, and menu actions', async () => {
    resetTestDom('https://oma.duck.ai/service-accounts');

    renderDashboardPage(<DashboardPage section="service-accounts" />);

    expect(screen.getByRole('heading', { name: 'Service accounts' })).toBeTruthy();
    expect(screen.getByRole('tablist')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Create service account' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'No service accounts yet' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Create service account' }));

    expect(screen.getByRole('dialog')).toBeTruthy();
    fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'CI deploy bot' } });
    fireEvent.change(screen.getByLabelText('Description'), {
      target: { value: 'Publishes production builds' }
    });
    fireEvent.click(screen.getByRole('button', { name: 'Create' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    const serviceAccountsTable = screen.getByRole('table', { name: 'Service accounts' });
    expect(serviceAccountsTable).toBeTruthy();
    expect(serviceAccountsTable.closest('[data-slot="card"]')).toBeNull();
    expect(screen.getByText('CI deploy bot')).toBeTruthy();
    expect(screen.getByText('svcacc_local_0001')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive' }));

    const archiveDialog = screen.getByRole('alertdialog');
    fireEvent.click(within(archiveDialog).getByRole('button', { name: 'Archive' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
    expect(screen.getByRole('heading', { name: 'No service accounts yet' })).toBeTruthy();

    fireEvent.click(screen.getByRole('tab', { name: 'Archived' }));
    expect(screen.getByText('CI deploy bot')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Delete' }));

    const deleteDialog = screen.getByRole('alertdialog');
    fireEvent.click(within(deleteDialog).getByRole('button', { name: 'Delete' }));

    await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
    expect(screen.getByRole('heading', { name: 'No archived service accounts' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Show active service accounts' })).toBeTruthy();
  });

  test('uses standard privacy controls switches, tooltips, and configure dialogs', async () => {
    resetTestDom('https://oma.duck.ai/privacy-controls');

    renderDashboardPage(<DashboardPage section="privacy-controls" />);

    expect(screen.getByRole('heading', { name: 'Privacy controls' })).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Training data' })).toBeTruthy();
    expect(screen.getByRole('switch', { name: 'Sensitive metadata redaction' })).toBeTruthy();
    expect(screen.getByText('Current default: Excluded from training')).toBeTruthy();
    expect(screen.getByText('Current default: Admins only')).toBeTruthy();

    fireEvent.click(screen.getByRole('switch', { name: 'Training data' }));
    expect(screen.getByText('Current default: Eligible for training')).toBeTruthy();

    expect(
      screen.getByRole('button', {
        name: 'Applies to new prompts, attachments, and outputs created after this default changes.'
      })
    ).toBeTruthy();

    const configureButtons = () => screen.getAllByRole('button', { name: 'Configure' });

    fireEvent.click(configureButtons()[0]);

    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Configure activity retention' })).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox', { name: 'Retention window' }));
    selectOption('1 year');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: 1 year')).toBeTruthy();

    fireEvent.click(configureButtons()[1]);
    expect(screen.getByRole('heading', { name: 'Configure export access' })).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox', { name: 'Export access' }));
    selectOption('Disabled');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Admins only')).toBeTruthy();

    fireEvent.click(configureButtons()[1]);
    fireEvent.click(screen.getByRole('combobox', { name: 'Export access' }));
    selectOption('Disabled');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Disabled')).toBeTruthy();
  });

  test('uses standard security controls switches, links, and configure dialogs', async () => {
    resetTestDom('https://oma.duck.ai/security');

    renderDashboardPage(<DashboardPage section="security" />);

    expect(screen.getByRole('heading', { name: 'Security' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'View logs' }).getAttribute('href')).toBe('/logs');
    expect(screen.getByRole('link', { name: 'Manage' }).getAttribute('href')).toBe('/members');
    expect(screen.getByRole('switch', { name: 'Admin multi-factor authentication' })).toBeTruthy();
    expect(screen.getByText('Current default: Required for admins and billing members')).toBeTruthy();
    expect(screen.getByText('Current default: Every 24 hours')).toBeTruthy();
    expect(screen.getByText('Current default: Admins only')).toBeTruthy();

    fireEvent.click(screen.getByRole('switch', { name: 'Admin multi-factor authentication' }));
    expect(screen.getByText('Current default: Optional for admins and billing members')).toBeTruthy();

    const configureButtons = () => screen.getAllByRole('button', { name: 'Configure' });

    fireEvent.click(configureButtons()[0]);

    expect(screen.getByRole('dialog')).toBeTruthy();
    expect(screen.getByRole('heading', { name: 'Configure session reauthentication' })).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox', { name: 'Reauthentication window' }));
    selectOption('Every 7 days');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Every 7 days')).toBeTruthy();

    fireEvent.click(configureButtons()[1]);
    expect(screen.getByRole('heading', { name: 'Configure security activity visibility' })).toBeTruthy();
    fireEvent.click(screen.getByRole('combobox', { name: 'Visibility' }));
    selectOption('All members');
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Admins only')).toBeTruthy();

    fireEvent.click(configureButtons()[1]);
    fireEvent.click(screen.getByRole('combobox', { name: 'Visibility' }));
    selectOption('Admins and billing');
    fireEvent.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    expect(screen.getByText('Current default: Admins and billing')).toBeTruthy();
  });

  test('uses shared shadcn card chrome for dashboard home and Claude Code usage', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const dashboard = renderDashboardPage(<DashboardPage section="dashboard" />);

    for (const label of ['Fable 5', 'Advisor tool']) {
      const card = screen.getByText(label).closest('[data-slot="card"]') as HTMLElement | null;
      expect(card).toBeTruthy();
      expect(card?.className.includes('surface-card')).toBe(false);
    }

    dashboard.unmount();
    resetTestDom('https://oma.duck.ai/claude-code/usage');
    renderDashboardPage(<DashboardPage section="claude-code-usage" />);

    const seatsCard = screen.getByText('Seats').closest('[data-slot="card"]') as HTMLElement | null;
    expect(seatsCard).toBeTruthy();
    expect(seatsCard?.className.includes('surface-card')).toBe(false);

    const emptyState = screen.getByText('No Claude Code usage yet').closest('[data-slot="empty"]') as HTMLElement | null;
    expect(emptyState).toBeTruthy();
    expect(emptyState?.className.includes('surface-card')).toBe(false);
    expect(emptyState?.className.includes('border-dashed')).toBe(true);
  });
});

describe('Files page', () => {
  test('renders files in the platform table shape with copy and disabled download actions', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    const clipboardWrite = mock(async (_value: string) => undefined);
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { writeText: clipboardWrite },
      configurable: true
    });
    const requests = mockFilesList(() => ({
      data: [
        {
          id: 'file_abc123456789',
          type: 'file',
          filename: 'report.json',
          mime_type: 'application/json',
          size_bytes: 321,
          created_at: new Date(Date.now() - 120_000).toISOString(),
          downloadable: false
        }
      ],
      has_more: false,
      first_id: 'file_abc123456789',
      last_id: 'file_abc123456789'
    }));

    renderFilesPage();

    expect(await screen.findByRole('heading', { name: 'Files' })).toBeTruthy();
    for (const heading of ['ID', 'Name', 'Size', 'Created']) {
      expect(screen.getByText(heading)).toBeTruthy();
    }
    expect(await screen.findByText('report.json')).toBeTruthy();
    expect(screen.getByText('321 B')).toBeTruthy();
    expect(screen.getByText('file_...23456789')).toBeTruthy();
    expect(screen.queryByRole('link', { name: /View Docs/i })).toBeNull();
    expect(screen.queryByText('Copy the template below to upload your first file:')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Copy file_abc123456789' }));

    await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith('file_abc123456789'));
    expect((screen.getByRole('button', { name: 'Download report.json' }) as HTMLButtonElement).disabled).toBe(true);
    expect(requests[0]?.url).toBe('/v1/files?beta=true&limit=20');
    expect(requests[0]?.headers.get('anthropic-beta')).toBe('files-api-2025-04-14');
    expect(requests[0]?.headers.get('x-workspace-id')).toBe('default');
  });

  test('refetches files when the active workspace changes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    const requests = mockFilesList((_url, headers) => {
      const workspaceId = headers.get('x-workspace-id');
      return {
        data: [
          {
            id: workspaceId === 'wrkspc_alt' ? 'file_alt' : 'file_default',
            type: 'file',
            filename: workspaceId === 'wrkspc_alt' ? 'alt-workspace.txt' : 'default-workspace.txt',
            mime_type: 'text/plain',
            size_bytes: 12,
            created_at: new Date(Date.now() - 120_000).toISOString(),
            downloadable: false
          }
        ],
        has_more: false,
        first_id: workspaceId === 'wrkspc_alt' ? 'file_alt' : 'file_default',
        last_id: workspaceId === 'wrkspc_alt' ? 'file_alt' : 'file_default'
      };
    });

    const rendered = renderFilesPage({ id: 'default', name: 'Default' });

    expect(await screen.findByText('default-workspace.txt')).toBeTruthy();

    window.history.pushState(null, '', 'https://oma.duck.ai/workspaces/wrkspc_alt/files');
    rendered.rerenderWorkspace({ id: 'wrkspc_alt', name: 'Alt' });

    expect(await screen.findByText('alt-workspace.txt')).toBeTruthy();
    expect(screen.queryByText('default-workspace.txt')).toBeNull();
    expect(requests.map((request) => request.headers.get('x-workspace-id'))).toContain('default');
    expect(requests.map((request) => request.headers.get('x-workspace-id'))).toContain('wrkspc_alt');
  });

  test('uses the route workspace before workspace context catches up', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/wrkspc_route/files');
    const requests = mockFilesList(() => ({
      data: [
        {
          id: 'file_route',
          type: 'file',
          filename: 'route-workspace.txt',
          mime_type: 'text/plain',
          size_bytes: 12,
          created_at: new Date(Date.now() - 120_000).toISOString(),
          downloadable: false
        }
      ],
      has_more: false,
      first_id: 'file_route',
      last_id: 'file_route'
    }));

    renderFilesPage();

    expect(await screen.findByText('route-workspace.txt')).toBeTruthy();
    expect(requests[0]?.headers.get('x-workspace-id')).toBe('wrkspc_route');
  });

  test('uses the list cursor when moving to the next page', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    const requests = mockFilesList((url) => {
      if (url.includes('after_id=file_page_one')) {
        return {
          data: [
            {
              id: 'file_page_two',
              type: 'file',
              filename: 'second.md',
              mime_type: 'text/markdown',
              size_bytes: 22,
              created_at: new Date(Date.now() - 240_000).toISOString(),
              downloadable: false
            }
          ],
          has_more: false,
          first_id: 'file_page_two',
          last_id: 'file_page_two'
        };
      }
      return {
        data: [
          {
            id: 'file_page_one',
            type: 'file',
            filename: 'first.md',
            mime_type: 'text/markdown',
            size_bytes: 11,
            created_at: new Date(Date.now() - 120_000).toISOString(),
            downloadable: false
          }
        ],
        has_more: true,
        first_id: 'file_page_one',
        last_id: 'file_page_one'
      };
    });

    renderFilesPage();

    expect(await screen.findByText('first.md')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByText('second.md')).toBeTruthy();
    expect(requests.some((request) => request.url === '/v1/files?beta=true&limit=20&after_id=file_page_one')).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);
  });

  test('renders an empty files table without the old upload template card', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    mockFilesList(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null
    }));

    renderFilesPage();

    expect(await screen.findByText('No files have been uploaded to the Default workspace.')).toBeTruthy();
    expect(screen.queryByText('import anthropic')).toBeNull();
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('renders the standardized files error row and retries successfully', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    let attempt = 0;
    const requests = mockFilesList(() => {
      attempt += 1;
      if (attempt === 1) {
        return new Response(JSON.stringify({ error: { type: 'api_error', message: 'Files service is unavailable.' } }), {
          status: 503,
          headers: { 'Content-Type': 'application/json' }
        });
      }
      return {
        data: [
          {
            id: 'file_retry_success',
            type: 'file',
            filename: 'recovered.txt',
            mime_type: 'text/plain',
            size_bytes: 42,
            created_at: new Date(Date.now() - 120_000).toISOString(),
            downloadable: false
          }
        ],
        has_more: false,
        first_id: 'file_retry_success',
        last_id: 'file_retry_success'
      };
    });

    renderFilesPage();

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Files could not be loaded.')).toBeTruthy();
    expect(screen.getByText('Files service is unavailable.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByText('recovered.txt')).toBeTruthy();
    expect(screen.queryByText('Files service is unavailable.')).toBeNull();
    expect(requests).toHaveLength(2);
  });
});

describe('Skills page', () => {
  test('renders skills in the platform list shape and opens the query-param drawer', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const clipboardWrite = mock(async (_value: string) => undefined);
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { writeText: clipboardWrite },
      configurable: true
    });
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills/skill_emoji?beta=true') {
        return {
          id: 'skill_emoji',
          type: 'skill',
          display_title: 'emoji-translator',
          latest_version: '20260708',
          source: 'custom',
          created_at: new Date(Date.now() - 120_000).toISOString(),
          updated_at: new Date(Date.now() - 60_000).toISOString()
        };
      }
      if (url === '/v1/skills/skill_emoji/versions?beta=true&limit=50') {
        return {
          data: [
            {
              id: 'skillver_emoji',
              type: 'skill_version',
              description: 'This skill should be used when the user asks to turn this into emojis.',
              directory: 'emoji-translator',
              name: 'emoji-translator',
              skill_id: 'skill_emoji',
              version: '20260708',
              created_at: new Date(Date.now() - 60_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 60_000).toISOString()
            },
            {
              id: 'xlsx',
              type: 'skill',
              display_title: 'xlsx',
              latest_version: '20260203',
              source: 'anthropic',
              created_at: new Date(Date.now() - 240_000).toISOString(),
              updated_at: new Date(Date.now() - 240_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    expect(await screen.findByRole('heading', { name: 'Skills' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Create skill/i })).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'ID' })).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'Source' })).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'Latest version' })).toBeTruthy();
    expect(await screen.findByText('emoji-translator')).toBeTruthy();
    expect(screen.getAllByText('xlsx').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Custom')).toBeTruthy();
    expect(screen.getByText('Anthropic')).toBeTruthy();
    expect(screen.getByText('Jul 8, 2026')).toBeTruthy();
    expect(screen.getByText('Feb 3, 2026')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Actions' })).toHaveLength(1);
    expect(requests[0]?.url).toBe('/v1/skills?beta=true&limit=100');
    expect(requests[0]?.headers.get('anthropic-beta')).toBe('skills-2025-10-02');
    expect(requests[0]?.headers.get('x-workspace-id')).toBe('default');

    const copyButton = screen.getByRole('button', { name: 'Copy skill_emoji' });
    fireEvent.mouseEnter(copyButton);
    expect(screen.queryByText('Copy')).toBeNull();
    fireEvent.click(copyButton);

    await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith('skill_emoji'));
    expect(new URL(window.location.href).searchParams.get('skill')).toBeNull();

    fireEvent.click(screen.getByText('emoji-translator'));

    expect(await screen.findByText('This skill should be used when the user asks to turn this into emojis.')).toBeTruthy();
    expect(new URL(window.location.href).searchParams.get('skill')).toBe('skill_emoji');
    expect(requests.find((request) => request.url === '/v1/skills/skill_emoji?beta=true')?.headers.get('x-workspace-id')).toBe('default');
    expect(
      requests.find((request) => request.url === '/v1/skills/skill_emoji/versions?beta=true&limit=50')?.headers.get('x-workspace-id')
    ).toBe('default');
  });

  test('refetches skills when the active workspace changes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((_url, headers) => {
      const workspaceId = headers.get('x-workspace-id');
      const skillId = workspaceId === 'wrkspc_alt' ? 'alt-skill' : 'default-skill';
      return {
        data: [
          {
            id: skillId,
            type: 'skill',
            display_title: skillId,
            latest_version: '20260708',
            source: 'custom',
            created_at: new Date(Date.now() - 120_000).toISOString(),
            updated_at: new Date(Date.now() - 120_000).toISOString()
          }
        ],
        has_more: false,
        next_page: null
      };
    });

    const rendered = renderSkillsPage({ id: 'default', name: 'Default' });

    expect((await screen.findAllByText('default-skill')).length).toBeGreaterThanOrEqual(1);

    window.history.pushState(null, '', 'https://oma.duck.ai/workspaces/wrkspc_alt/skills');
    rendered.rerenderWorkspace({ id: 'wrkspc_alt', name: 'Alt' });

    expect((await screen.findAllByText('alt-skill')).length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText('default-skill')).toBeNull();
    expect(requests.map((request) => request.headers.get('x-workspace-id'))).toContain('default');
    expect(requests.map((request) => request.headers.get('x-workspace-id'))).toContain('wrkspc_alt');
  });

  test('uses the next_page cursor when moving to the next skills page', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url.includes('page=cursor_two')) {
        return {
          data: [
            {
              id: 'product-self-knowledge',
              type: 'skill',
              display_title: 'product-self-knowledge',
              latest_version: '20260622',
              source: 'anthropic',
              created_at: new Date(Date.now() - 240_000).toISOString(),
              updated_at: new Date(Date.now() - 240_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      return {
        data: [
          {
            id: 'frontend-design',
            type: 'skill',
            display_title: 'frontend-design',
            latest_version: '20260203',
            source: 'anthropic',
            created_at: new Date(Date.now() - 120_000).toISOString(),
            updated_at: new Date(Date.now() - 120_000).toISOString()
          }
        ],
        has_more: true,
        next_page: 'cursor_two'
      };
    });

    renderSkillsPage();

    expect((await screen.findAllByText('frontend-design')).length).toBeGreaterThanOrEqual(1);
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByText('product-self-knowledge')).toBeTruthy();
    expect(requests.some((request) => request.url === '/v1/skills?beta=true&limit=100&page=cursor_two')).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));

    expect((await screen.findAllByText('frontend-design')).length).toBeGreaterThanOrEqual(1);
    await waitFor(() => expect(screen.queryByText('product-self-knowledge')).toBeNull());
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('disables skills next pagination without a next_page cursor', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi(() => ({
      data: [
        {
          id: 'frontend-design',
          type: 'skill',
          display_title: 'frontend-design',
          latest_version: '20260203',
          source: 'anthropic',
          created_at: new Date(Date.now() - 120_000).toISOString(),
          updated_at: new Date(Date.now() - 120_000).toISOString()
        }
      ],
      has_more: true,
      next_page: null
    }));

    renderSkillsPage();

    expect((await screen.findAllByText('frontend-design')).length).toBeGreaterThanOrEqual(1);
    const next = screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(next);
    expect(requests).toHaveLength(1);
  });

  test('creates a skill from a single archive upload', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return { data: [], has_more: false, next_page: null };
      }
      if (url === '/v1/skills?beta=true') {
        return {
          id: 'skill_created',
          display_title: 'emoji-translator',
          latest_version: '20260708',
          source: 'custom',
          created_at: new Date(Date.now() - 10_000).toISOString(),
          updated_at: new Date(Date.now() - 10_000).toISOString()
        };
      }
      if (url === '/v1/skills/skill_created?beta=true') {
        return {
          id: 'skill_created',
          type: 'skill',
          display_title: 'emoji-translator',
          latest_version: '20260708',
          source: 'custom',
          created_at: new Date(Date.now() - 120_000).toISOString()
        };
      }
      if (url === '/v1/skills/skill_created/versions?beta=true&limit=50') {
        return {
          data: [
            {
              id: 'skillver_created',
              type: 'skill_version',
              description: 'created description',
              directory: 'emoji-translator',
              name: 'emoji-translator',
              skill_id: 'skill_created',
              version: '20260708',
              created_at: new Date(Date.now() - 10_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Create skill' }));
    expect(await screen.findByText('Drag and drop a .zip, .skill file, or directory to upload')).toBeTruthy();
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['zip bytes'], 'emoji-translator.zip', { type: 'application/zip' });

    fireEvent.change(input, { target: { files: [file] } });
    expect(await screen.findByText('emoji-translator.zip')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true')).toBe(true));
    const post = requests.find((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true');
    expect(post?.headers.get('x-workspace-id')).toBe('default');
    const body = post?.body as FormData;
    expect(body.get('display_title')).toBe('emoji-translator');
    expect((body.get('files[]') as File).name).toBe('emoji-translator.zip');
    await waitFor(() => expect(screen.queryByText('emoji-translator.zip')).toBeNull());
    expect(new URL(window.location.href).searchParams.get('skill')).toBe('skill_created');
  });

  test('surfaces duplicate create errors without posting a new version', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      if (url === '/v1/skills?beta=true') {
        return new Response(
          JSON.stringify({
            error: {
              message: 'A custom skill named "emoji-translator" already exists. Use Update from that skill\'s actions menu to upload a new version.',
              type: 'invalid_request_error'
            },
            type: 'error'
          }),
          {
            status: 400,
            headers: { 'Content-Type': 'application/json' }
          }
        );
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Create skill' }));
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['zip bytes'], 'emoji-translator.zip', { type: 'application/zip' });

    fireEvent.change(input, { target: { files: [file] } });
    expect(await screen.findByText('emoji-translator.zip')).toBeTruthy();
    expect(screen.queryByText(/already exists/)).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true')).toBe(true));
    expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills/skill_emoji/versions?beta=true')).toBe(false);
    const post = requests.find((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true');
    const body = post?.body as FormData;
    expect(body.get('display_title')).toBe('emoji-translator');
    expect((body.get('files[]') as File).name).toBe('emoji-translator.zip');
    expect(await screen.findByText(/A custom skill named "emoji-translator" already exists/)).toBeTruthy();
    expect(screen.getByText('emoji-translator.zip')).toBeTruthy();
    expect(new URL(window.location.href).searchParams.get('skill')).toBeNull();
  });

  test('blocks empty skill archive uploads before posting', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [],
          has_more: false,
          next_page: null
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Create skill' }));
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File([], 'emoji-translator.zip', { type: 'application/zip' });

    fireEvent.change(input, { target: { files: [file] } });

    expect(await screen.findByText('Skill package files cannot be empty.')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Continue' }) as HTMLButtonElement).disabled).toBe(true);
    expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true')).toBe(false);
  });

  test('updates a custom skill from the action menu', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      if (url === '/v1/skills/skill_emoji/versions?beta=true') {
        return {
          id: 'skillver_emoji_2',
          type: 'skill_version',
          description: 'updated',
          directory: 'emoji-translator',
          name: 'emoji-translator',
          skill_id: 'skill_emoji',
          version: '20260709',
          created_at: new Date(Date.now() - 10_000).toISOString()
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Actions' }));
    fireEvent.click(await screen.findByText('Update'));
    expect(await screen.findByText('Update Skill')).toBeTruthy();
    const input = document.querySelector('input[type="file"]') as HTMLInputElement;
    const file = new File(['zip bytes'], 'emoji-translator.zip', { type: 'application/zip' });
    fireEvent.change(input, { target: { files: [file] } });
    fireEvent.click(screen.getByRole('button', { name: 'Continue' }));

    await waitFor(() => expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills/skill_emoji/versions?beta=true')).toBe(true));
    await waitFor(() => expect(screen.queryByText('Update Skill')).toBeNull());
    expect(screen.queryByText('emoji-translator.zip')).toBeNull();
  });

  test('deletes a custom skill atomically from the skill endpoint', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      if (url === '/v1/skills/skill_emoji?beta=true') {
        return { id: 'skill_emoji', type: 'skill_deleted' };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    fireEvent.click(await screen.findByRole('button', { name: 'Actions' }));
    fireEvent.click(await screen.findByText('Delete'));
    expect(await screen.findByText('Confirm deleting emoji-translator')).toBeTruthy();
    const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
    fireEvent.click(deleteButtons[deleteButtons.length - 1]);

    await waitFor(() =>
      expect(requests.some((request) => request.method === 'DELETE' && request.url === '/v1/skills/skill_emoji?beta=true')).toBe(true)
    );
    const deleteOrder = requests
      .map((request) => request.method === 'DELETE' ? request.url : '')
      .filter(Boolean);
    expect(deleteOrder).toEqual(['/v1/skills/skill_emoji?beta=true']);
  });
});

describe('Skill detail page', () => {
  test('renders the selected skill in the query-param drawer', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills/frontend-design');
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [
            {
              id: 'frontend-design',
              type: 'skill',
              display_title: 'frontend-design',
              latest_version: '20260708',
              source: 'anthropic',
              created_at: new Date(Date.now() - 360_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      if (url === '/v1/skills/frontend-design?beta=true') {
        return {
          id: 'frontend-design',
          type: 'skill',
          display_title: 'frontend-design',
          latest_version: '20260708',
          source: 'anthropic',
          created_at: new Date(Date.now() - 360_000).toISOString(),
          updated_at: new Date(Date.now() - 120_000).toISOString()
        };
      }
      if (url === '/v1/skills/frontend-design/versions?beta=true&limit=50') {
        return {
          data: [
            {
              id: 'skillver_frontend-design-1',
              type: 'skill_version',
              description: 'v1 description',
              directory: 'frontend-design',
              name: 'frontend-design',
              skill_id: 'frontend-design',
              version: '20260707',
              created_at: new Date(Date.now() - 360_000).toISOString()
            },
            {
              id: 'skillver_frontend-design-2',
              type: 'skill_version',
              description: 'v2 description',
              directory: 'frontend-design',
              name: 'frontend-design v2',
              skill_id: 'frontend-design',
              version: '20260708',
              created_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillDetailPage('frontend-design');

    expect(await screen.findByText('v2 description')).toBeTruthy();
    expect(screen.getByText('Latest')).toBeTruthy();
    expect(new URL(window.location.href).searchParams.get('skill')).toBe('frontend-design');
    expect(requests.find((request) => request.url === '/v1/skills/frontend-design?beta=true')?.headers.get('x-workspace-id')).toBe('default');
    expect(
      requests.find((request) => request.url === '/v1/skills/frontend-design/versions?beta=true&limit=50')?.headers.get('x-workspace-id')
    ).toBe('default');
  });

  test('renders the standardized skill detail error alert and retries successfully', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills/frontend-design');
    let attempt = 0;
    const requests = mockSkillsApi((url) => {
      if (url === '/v1/skills?beta=true&limit=100') {
        return {
          data: [
            {
              id: 'frontend-design',
              type: 'skill',
              display_title: 'frontend-design',
              latest_version: '2',
              source: 'anthropic',
              created_at: new Date(Date.now() - 360_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      if (url === '/v1/skills/frontend-design?beta=true') {
        attempt += 1;
        if (attempt === 1) {
          return new Response(JSON.stringify({ error: { type: 'api_error', message: 'Skill metadata is unavailable.' } }), {
            status: 503,
            headers: { 'Content-Type': 'application/json' }
          });
        }
        return {
          id: 'frontend-design',
          type: 'skill',
          display_title: 'frontend-design',
          latest_version: '2',
          source: 'anthropic',
          created_at: new Date(Date.now() - 360_000).toISOString(),
          updated_at: new Date(Date.now() - 120_000).toISOString()
        };
      }
      if (url === '/v1/skills/frontend-design/versions?beta=true&limit=50') {
        return {
          data: [
            {
              id: 'skillver_frontend-design-2',
              type: 'skill_version',
              description: 'Recovered description',
              directory: 'frontend-design',
              name: 'frontend-design v2',
              skill_id: 'frontend-design',
              version: '2',
              created_at: new Date(Date.now() - 120_000).toISOString()
            }
          ],
          has_more: false,
          next_page: null
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillDetailPage('frontend-design');

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Skill could not be loaded.')).toBeTruthy();
    expect(screen.getByText('Skill metadata is unavailable.')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByText('Recovered description')).toBeTruthy();
    expect(screen.queryByText('Skill metadata is unavailable.')).toBeNull();
    expect(requests.filter((request) => request.url === '/v1/skills/frontend-design?beta=true')).toHaveLength(2);
  });
});

describe('Batches page', () => {
  test('renders the standardized batch detail error alert and retries successfully', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches?batch=msgbatch_detail');
    let attempt = 0;
    const batch = makeBatch({
      id: 'msgbatch_detail',
      processing_status: 'ended',
      results_url: 'https://oma.duck.ai/v1/messages/batches/msgbatch_detail/results'
    });
    const requests = mockMessageBatchesApi((url) => {
      if (url === '/v1/messages/batches?beta=true&limit=20') {
        return {
          data: [batch],
          has_more: false,
          first_id: 'msgbatch_detail',
          last_id: 'msgbatch_detail'
        };
      }
      if (url === '/v1/messages/batches/msgbatch_detail?beta=true') {
        attempt += 1;
        if (attempt === 1) {
          return new Response(JSON.stringify({ error: { type: 'api_error', message: 'Batch details are unavailable.' } }), {
            status: 503,
            headers: { 'Content-Type': 'application/json' }
          });
        }
        return batch;
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderBatchesPage();

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Batch could not be loaded.')).toBeTruthy();
    expect(screen.getByText('Batch details are unavailable.')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Close' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('button', { name: 'Download Results' })).toBeTruthy();
    expect(screen.queryByText('Batch details are unavailable.')).toBeNull();
    expect(requests.filter((request) => request.url === '/v1/messages/batches/msgbatch_detail?beta=true')).toHaveLength(2);
  });

  test('renders batches in the platform table shape with query-selected details and result downloads', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches?batch=msgbatch_done');
    const createObjectURL = mock((_blob: Blob) => 'https://oma.duck.ai/downloads/msgbatch_done.jsonl');
    const revokeObjectURL = mock((_url: string) => undefined);
    Object.defineProperty(URL, 'createObjectURL', { value: createObjectURL, configurable: true });
    Object.defineProperty(URL, 'revokeObjectURL', { value: revokeObjectURL, configurable: true });
    const endedBatch = makeBatch({
      id: 'msgbatch_done',
      processing_status: 'ended',
      request_counts: { processing: 0, succeeded: 2, errored: 1, canceled: 0, expired: 0 },
      results_url: 'https://oma.duck.ai/v1/messages/batches/msgbatch_done/results'
    });
    const requests = mockMessageBatchesApi((url) => {
      if (url === '/v1/messages/batches?beta=true&limit=20') {
        return {
          data: [endedBatch],
          has_more: false,
          first_id: 'msgbatch_done',
          last_id: 'msgbatch_done'
        };
      }
      if (url === '/v1/messages/batches/msgbatch_done?beta=true') {
        return endedBatch;
      }
      if (url === '/v1/messages/batches/msgbatch_done/results?beta=true') {
        return new Response(new Blob(['{"custom_id":"one"}\n']), {
          status: 200,
          headers: { 'Content-Type': 'application/x-jsonl' }
        });
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderBatchesPage();

    expect(await screen.findByRole('heading', { name: 'Batches' })).toBeTruthy();
    for (const heading of ['ID', 'Status', 'Requests', 'Created']) {
      expect(screen.getByText(heading)).toBeTruthy();
    }
    expect(screen.queryByText('Expires')).toBeNull();
    await waitFor(() => expect(screen.getAllByText('Ended').length).toBeGreaterThanOrEqual(1));
    expect(screen.getAllByText('2 / 3').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('Batch details')).toBeTruthy();
    expect(screen.getByText('Total requests')).toBeTruthy();
    expect(screen.getAllByText('msgbatch_done').length).toBeGreaterThanOrEqual(1);
    const detailPanel = screen.getByRole('region', { name: 'Batch details' });
    const detailCopyButton = within(detailPanel).getByRole('button', { name: 'Copy msgbatch_done' });
    expect((detailCopyButton as HTMLElement).style.opacity).toBe('1');
    expect(screen.queryByText('Copy the template below to set up your first batch:')).toBeNull();
    expect(requests[0]?.url).toBe('/v1/messages/batches?beta=true&limit=20');
    expect(requests[0]?.headers.get('anthropic-beta')).toBe('message-batches-2024-09-24');
    expect(requests[0]?.headers.get('anthropic-version')).toBe('2023-06-01');
    expect(requests[0]?.headers.get('x-workspace-id')).toBe('default');
    expect(requests.find((request) => request.url === '/v1/messages/batches/msgbatch_done?beta=true')?.headers.get('x-workspace-id')).toBe('default');

    fireEvent.click(screen.getByRole('button', { name: 'Download Results' }));

    await waitFor(() =>
      expect(requests.some((request) => request.url === '/v1/messages/batches/msgbatch_done/results?beta=true')).toBe(true)
    );
    const downloadRequest = requests.find((request) => request.url === '/v1/messages/batches/msgbatch_done/results?beta=true');
    expect(downloadRequest?.headers.get('anthropic-beta')).toBe('message-batches-2024-09-24');
    expect(downloadRequest?.headers.get('x-workspace-id')).toBe('default');
    expect(createObjectURL).toHaveBeenCalled();
    expect(revokeObjectURL).toHaveBeenCalledWith('https://oma.duck.ai/downloads/msgbatch_done.jsonl');
  });

  test('refetches batches when the active workspace changes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    const requests = mockMessageBatchesApi((_url, _method, headers) => {
      const workspaceId = headers.get('x-workspace-id');
      const batchId = workspaceId === 'wrkspc_alt' ? 'msgbatch_alt' : 'msgbatch_default';
      return {
        data: [makeBatch({ id: batchId, processing_status: 'ended' })],
        has_more: false,
        first_id: batchId,
        last_id: batchId
      };
    });

    const rendered = renderBatchesPage({ id: 'default', name: 'Default' });

    const defaultBatchButton = await screen.findByRole('button', { name: 'msgbatch_default' });
    expect(defaultBatchButton).toBeTruthy();
    expect(defaultBatchButton.dataset.slot).toBe('button');

    window.history.pushState(null, '', 'https://oma.duck.ai/workspaces/wrkspc_alt/batches');
    rendered.rerenderWorkspace({ id: 'wrkspc_alt', name: 'Alt' });

    expect(await screen.findByRole('button', { name: 'msgbatch_alt' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'msgbatch_default' })).toBeNull();
    expect(requests.map((request) => request.headers.get('x-workspace-id'))).toContain('default');
    expect(requests.map((request) => request.headers.get('x-workspace-id'))).toContain('wrkspc_alt');
  });

  test('uses the batch cursor when moving to the next page', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    const requests = mockMessageBatchesApi((url) => {
      if (url.includes('after_id=msgbatch_page_one')) {
        return {
          data: [makeBatch({ id: 'msgbatch_page_two', processing_status: 'in_progress' })],
          has_more: false,
          first_id: 'msgbatch_page_two',
          last_id: 'msgbatch_page_two'
        };
      }
      return {
        data: [makeBatch({ id: 'msgbatch_page_one', processing_status: 'in_progress' })],
        has_more: true,
        first_id: 'msgbatch_page_one',
        last_id: 'msgbatch_page_one'
      };
    });

    renderBatchesPage();

    expect(await screen.findByRole('button', { name: 'msgbatch_page_one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByRole('button', { name: 'msgbatch_page_two' })).toBeTruthy();
    expect(requests.some((request) => request.url === '/v1/messages/batches?beta=true&limit=20&after_id=msgbatch_page_one')).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);
  });

  test('selects a batch and cancels in-progress batches from the inspector', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    const inProgressBatch = makeBatch({ id: 'msgbatch_progress', processing_status: 'in_progress' });
    const endedBatch = makeBatch({ id: 'msgbatch_done', processing_status: 'ended' });
    const requests = mockMessageBatchesApi((url, method) => {
      if (url === '/v1/messages/batches?beta=true&limit=20') {
        return {
          data: [inProgressBatch, endedBatch],
          has_more: false,
          first_id: 'msgbatch_progress',
          last_id: 'msgbatch_done'
        };
      }
      if (url === '/v1/messages/batches/msgbatch_progress?beta=true') {
        return inProgressBatch;
      }
      if (url === '/v1/messages/batches/msgbatch_progress/cancel?beta=true' && method === 'POST') {
        return { ...inProgressBatch, processing_status: 'canceling' };
      }
      throw new Error(`Unexpected request: ${method} ${url}`);
    });

    renderBatchesPage();

    const batchButton = await screen.findByRole('button', { name: 'msgbatch_progress' });
    expect(batchButton.dataset.slot).toBe('button');
    fireEvent.click(batchButton);

    expect(window.location.search).toBe('?batch=msgbatch_progress');
    expect(await screen.findByText('Batch details')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'msgbatch_progress' }).getAttribute('aria-expanded')).toBe('true');

    fireEvent.click(screen.getByRole('button', { name: 'Cancel batch' }));

    await waitFor(() =>
      expect(
        requests.some(
          (request) => request.url === '/v1/messages/batches/msgbatch_progress/cancel?beta=true' && request.method === 'POST'
        )
      ).toBe(true)
    );
    expect(
      requests.find((request) => request.url === '/v1/messages/batches/msgbatch_progress/cancel?beta=true')?.headers.get('x-workspace-id')
    ).toBe('default');
  });
});

function renderFilesPage(workspace?: Partial<Workspace>, locale?: Locale) {
  return renderDashboardPage(<FilesPage />, workspace, locale);
}

function renderBatchesPage(workspace?: Partial<Workspace>, locale?: Locale) {
  return renderDashboardPage(<BatchesPage />, workspace, locale);
}

function renderSkillsPage(workspace?: Partial<Workspace>, locale?: Locale) {
  return renderDashboardPage(<SkillsPage />, workspace, locale);
}

function renderSkillDetailPage(skillId: string, workspace?: Partial<Workspace>, locale?: Locale) {
  return renderDashboardPage(<SkillDetailPage skillId={skillId} />, workspace, locale);
}

function mockWebhooksList() {
  const requests: Array<{ url: string; headers: Headers }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url;
    const headers = new Headers(init?.headers);
    requests.push({ url, headers });

    if (url === '/v1/webhooks?beta=true') {
      return new Response(JSON.stringify({ data: [] }), {
        status: 200,
        headers: { 'Content-Type': 'application/json' }
      });
    }

    return new Response(JSON.stringify({ error: { message: 'not found' } }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' }
    });
  }) as unknown as typeof fetch;

  return { requests };
}

function renderDashboardPage(
  children: ReactNode,
  workspace?: Partial<Workspace>,
  locale: Locale = 'en',
  authValue: AuthContextValue = makeAuthContextValue()
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0
      }
    }
  });
  const renderWithWorkspace = (nextWorkspace?: Partial<Workspace>) => {
    const value = makeWorkspaceContextValue(nextWorkspace);
    return (
      <I18nProvider initialLocale={locale}>
        <AuthContext.Provider value={authValue}>
          <WorkspaceContext.Provider value={value}>
            <QueryClientProvider client={queryClient}>
              {children}
            </QueryClientProvider>
          </WorkspaceContext.Provider>
        </AuthContext.Provider>
      </I18nProvider>
    );
  };

  const rendered = render(renderWithWorkspace(workspace));
  return {
    ...rendered,
    rerenderWorkspace: (nextWorkspace?: Partial<Workspace>) => rendered.rerender(renderWithWorkspace(nextWorkspace))
  };
}

function makeAuthContextValue(accountOverrides?: Partial<NonNullable<AuthContextValue['account']>>): AuthContextValue {
  return {
    account: {
      tagged_id: 'user_default',
      uuid: 'user_default',
      email_address: 'admin@example.local',
      full_name: 'Local Admin',
      display_name: 'Local Admin',
      memberships: [{ role: 'member' }],
      ...accountOverrides
    },
    status: 'authenticated',
    csrfToken: 'csrf_test',
    refresh: async () => ({ account: null }),
    logout: async () => undefined
  };
}

function makeWorkspaceContextValue(workspace?: Partial<Workspace>): WorkspaceContextValue {
  const activeWorkspace = {
    ...defaultWorkspace,
    ...workspace,
    type: 'workspace' as const
  };
  const workspaces = activeWorkspace.id === defaultWorkspace.id ? [activeWorkspace] : [defaultWorkspace, activeWorkspace];
  return {
    orgUuid: 'org_test_uuid',
    workspaces,
    activeWorkspace,
    activeWorkspaceId: activeWorkspace.id,
    isLoading: false,
    error: null,
    selectWorkspace: () => undefined,
    createWorkspace: async (input) => ({
      ...defaultWorkspace,
      id: `wrkspc_${input.name.toLowerCase().replace(/[^a-z0-9]+/g, '_')}`,
      name: input.name,
      display_color: input.display_color,
      color: input.display_color,
      data_residency: input.data_residency
    }),
    refreshWorkspaces: async () => undefined
  };
}

function mockFilesList(handler: (url: string, headers: Headers) => unknown) {
  const requests: Array<{ url: string; headers: Headers }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const headers = new Headers(init?.headers);
    requests.push({ url, headers });
    const result = handler(url, headers);
    if (result instanceof Response) {
      return result;
    }
    return new Response(JSON.stringify(result), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  }) as unknown as typeof fetch;
  return requests;
}

function mockSkillsApi(handler: (url: string, headers: Headers) => unknown) {
  const requests: Array<{ url: string; method: string; headers: Headers; body?: BodyInit | null }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    requests.push({ url, method, headers, body: init?.body });
    const result = handler(url, headers);
    if (result instanceof Response) {
      return result;
    }
    return new Response(JSON.stringify(result), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  }) as unknown as typeof fetch;
  return requests;
}

function mockMessageBatchesApi(handler: (url: string, method: string, headers: Headers) => unknown) {
  const requests: Array<{ url: string; method: string; headers: Headers }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    const headers = new Headers(init?.headers);
    requests.push({ url, method, headers });
    const result = handler(url, method, headers);
    if (result instanceof Response) {
      return result;
    }
    return new Response(JSON.stringify(result), {
      status: 200,
      headers: { 'Content-Type': 'application/json' }
    });
  }) as unknown as typeof fetch;
  return requests;
}

function mockOrganizationMembersApi() {
  const requests: string[] = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL) => {
    const url = String(input);
    requests.push(url);
    if (url === '/api/console/organizations/org_test_uuid/members') {
      return new Response(
        JSON.stringify([
          {
            id: 'member_admin',
            type: 'user',
            name: 'Local Admin',
            email: 'admin@example.local',
            role: 'admin'
          }
        ]),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        }
      );
    }
    if (url === '/api/console/organizations/org_test_uuid/invites?status=pending') {
      return new Response(
        JSON.stringify([
          {
            id: 'invite_pending',
            type: 'invite',
            email: 'pending@example.com',
            role: 'user',
            status: 'pending',
            invited_at: '2026-06-24T00:00:00Z',
            expires_at: '2026-07-15T00:00:00Z'
          }
        ]),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' }
        }
      );
    }
    throw new Error(`Unexpected request: ${url}`);
  }) as unknown as typeof fetch;
  return { requests };
}

function makeBatch(overrides: Partial<{
  id: string;
  processing_status: string;
  request_counts: {
    processing: number;
    succeeded: number;
    errored: number;
    canceled: number;
    expired: number;
  };
  results_url: string | null;
}> = {}) {
  return {
    id: overrides.id ?? 'msgbatch_test',
    type: 'message_batch',
    processing_status: overrides.processing_status ?? 'ended',
    request_counts: overrides.request_counts ?? {
      processing: overrides.processing_status === 'in_progress' ? 3 : 0,
      succeeded: overrides.processing_status === 'ended' ? 3 : 0,
      errored: 0,
      canceled: 0,
      expired: 0
    },
    created_at: new Date(Date.now() - 120_000).toISOString(),
    expires_at: new Date(Date.now() + 86_400_000).toISOString(),
    ended_at: overrides.processing_status === 'ended' ? new Date(Date.now() - 60_000).toISOString() : null,
    cancel_initiated_at: null,
    archived_at: null,
    results_url: overrides.results_url ?? (overrides.processing_status === 'ended' ? 'https://oma.duck.ai/v1/messages/batches/msgbatch_test/results' : null)
  };
}

function restoreClipboard() {
  if (originalClipboardDescriptor) {
    Object.defineProperty(globalThis.navigator, 'clipboard', originalClipboardDescriptor);
    return;
  }
  delete (globalThis.navigator as unknown as Record<string, unknown>).clipboard;
}

function restoreObjectUrl() {
  if (originalCreateObjectURLDescriptor) {
    Object.defineProperty(URL, 'createObjectURL', originalCreateObjectURLDescriptor);
  } else {
    delete (URL as unknown as Record<string, unknown>).createObjectURL;
  }
  if (originalRevokeObjectURLDescriptor) {
    Object.defineProperty(URL, 'revokeObjectURL', originalRevokeObjectURLDescriptor);
  } else {
    delete (URL as unknown as Record<string, unknown>).revokeObjectURL;
  }
}
