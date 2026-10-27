import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import {
  RouterContextProvider,
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from '@tanstack/react-router';
import { afterEach, describe, expect, mock, test } from 'bun:test';
import { useEffect, type ReactNode } from 'react';
import { AuthContext, type AuthContextValue } from '../../shared/auth/context';
import { I18nProvider, type Locale } from '../../shared/i18n';
import { setConsoleRequestContext } from '../../shared/api/client';
import { Toaster } from '../../shared/ui/sonner';
import { defaultWorkspace, type Workspace } from '../../shared/workspaces/api';
import { WorkspaceContext, type WorkspaceContextValue } from '../../shared/workspaces/context';
import { consoleResourceListLimit } from '../../shared/console-list';
import { resetTestDom } from '../../test/setup';
import { BatchesPage, DashboardPage, FilesPage, SkillDetailPage, SkillsPage } from './DashboardPage';

const testingLibrary = await import('@testing-library/react');
const { act, cleanup, fireEvent, render, screen, waitFor, within } = testingLibrary;

const originalFetch = globalThis.fetch;
const originalClipboardDescriptor = Object.getOwnPropertyDescriptor(globalThis.navigator, 'clipboard');
const originalCreateObjectURLDescriptor = Object.getOwnPropertyDescriptor(URL, 'createObjectURL');
const originalRevokeObjectURLDescriptor = Object.getOwnPropertyDescriptor(URL, 'revokeObjectURL');
const originalConfirm = window.confirm;

afterEach(() => {
  cleanup();
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
  window.confirm = originalConfirm;
  restoreClipboard();
  restoreObjectUrl();
});

describe('Dashboard i18n', () => {
  test.each(['en', 'zh-CN'] as const)(
    'prioritizes workspace resource links instead of static financial and model examples in %s',
    (locale) => {
      resetTestDom('https://oma.duck.ai/dashboard');
      renderDashboardPage(<DashboardPage section="dashboard" />, { id: 'wrkspc_team', name: 'Team' }, locale);
      for (const [label, path] of [
        [locale === 'en' ? /^Agents/ : /^智能体/, 'agents'],
        [locale === 'en' ? /^Sessions/ : /^会话/, 'sessions'],
        [locale === 'en' ? /^LLM models/ : /^LLM 模型/, 'llm-models'],
        [locale === 'en' ? /^Environments/ : /^环境/, 'environments'],
        [locale === 'en' ? /^Credential vaults/ : /^凭据保险库/, 'vaults'],
      ] as const) {
        expect(screen.getByRole('link', { name: label }).getAttribute('href')).toBe(`/workspaces/wrkspc_team/${path}`);
      }
      expect(
        screen.getByRole('link', { name: locale === 'en' ? 'Create agent' : '创建智能体' }).getAttribute('href'),
      ).toBe('/workspaces/wrkspc_team/agents/new');
      expect(screen.getByText('Team')).toBeTruthy();
      expect(screen.queryByText('Fable 5')).toBeNull();
      expect(screen.queryByText('$1.00')).toBeNull();
      expect(screen.queryByText(/\$500/)).toBeNull();
      expect(screen.queryByText(locale === 'en' ? 'Credit balance' : '信用余额')).toBeNull();
      expect(screen.queryByText(locale === 'en' ? 'Example models' : '模型示例')).toBeNull();
    },
  );

  test('renders dashboard and playground chrome in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default');
    const dashboard = renderDashboardPage(<DashboardPage section="dashboard" />, undefined, 'zh-CN');

    await waitFor(() => expect(document.documentElement.lang).toBe('zh-CN'));
    expect(screen.getByRole('heading', { name: '概览' })).toBeTruthy();
    expect(screen.getByText('默认')).toBeTruthy();
    expect(screen.queryByText('Default')).toBeNull();
    const getApiKey = screen.getByRole('link', { name: '获取 API 密钥' });
    expect(getApiKey.getAttribute('href')).toBe('/settings/workspaces/default/keys');
    expect(getApiKey.dataset.slot).toBe('button');
    const docsLink = screen.getByRole('link', { name: '查看文档' });
    expect(docsLink.getAttribute('href')).toBe('https://oma.mintlifysite.com/docs/zh/overview');
    expect(screen.getByRole('link', { name: '创建智能体' }).dataset.slot).toBe('button');
    expect(screen.getByRole('heading', { name: '智能体' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '会话' })).toBeTruthy();
    expect(screen.getByRole('heading', { name: '工作区资源' })).toBeTruthy();

    dashboard.unmount();

    renderDashboardPage(<DashboardPage section="playground" />, undefined, 'zh-CN');

    expect(screen.getByRole('heading', { name: 'Playground' })).toBeTruthy();
    expect(screen.getByText('Playground 尚未接入模型服务。')).toBeTruthy();
    expect(screen.queryByRole('combobox')).toBeNull();
    expect(screen.queryByRole('textbox')).toBeNull();
    expect(screen.queryByRole('button', { name: '发送' })).toBeNull();
    expect(screen.getByRole('link', { name: '打开工作台' }).getAttribute('href')).toBe('/workbench');
  });

  test('links the English dashboard to the corresponding OMA documentation pages', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default');
    renderDashboardPage(<DashboardPage section="dashboard" />);

    expect(screen.getByRole('link', { name: 'Explore docs' }).getAttribute('href')).toBe(
      'https://oma.mintlifysite.com/docs/en/overview',
    );
    expect(screen.getByRole('link', { name: /^LLM models/ }).getAttribute('href')).toBe(
      '/workspaces/default/llm-models',
    );
  });

  test('renders files, skills, and batches empty states in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    mockFilesList(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null,
    }));
    const files = renderFilesPage(undefined, 'zh-CN');

    expect(await screen.findByRole('heading', { name: '文件' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '文件列表' })).toBeTruthy();
    expect(await screen.findByText('向默认工作区上传文件后，文件会显示在这里。')).toBeTruthy();
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
        next_page: null,
      };
    });
    const skills = renderSkillsPage(undefined, 'zh-CN');

    expect(await screen.findByRole('heading', { name: '技能' })).toBeTruthy();
    expect(screen.getByRole('button', { name: '创建技能' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '技能列表' })).toBeTruthy();
    expect(await screen.findByText('还没有技能')).toBeTruthy();
    expect(screen.queryByRole('columnheader', { name: 'ID' })).toBeNull();

    skills.unmount();
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    mockMessageBatchesApi(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null,
    }));
    renderBatchesPage(undefined, 'zh-CN');

    expect(await screen.findByRole('heading', { name: '批处理' })).toBeTruthy();
    expect(screen.getByRole('region', { name: '批处理列表' })).toBeTruthy();
    expect(await screen.findByText('默认工作区暂无批处理。')).toBeTruthy();
  });

  test('preserves Claude Code usage in Chinese', () => {
    resetTestDom('https://oma.duck.ai/claude-code/usage');
    renderDashboardPage(<DashboardPage section="claude-code-usage" />, undefined, 'zh-CN');

    expect(screen.getByRole('heading', { name: 'Claude Code 用量' })).toBeTruthy();
    expect(screen.queryByText('席位')).toBeNull();
    expect(screen.getByText('Claude Code 用量尚未接入。')).toBeTruthy();
    expect(screen.queryByText('—')).toBeNull();
    expect(screen.queryByText('0')).toBeNull();
  });

  test.each(['en', 'zh-CN'] as const)('does not simulate saved Claude Code permissions in %s', (locale) => {
    resetTestDom('https://oma.duck.ai/claude-code/settings');
    renderDashboardPage(<DashboardPage section="claude-code-settings" />, undefined, locale);
    const viewMembers = screen.getByRole('link', { name: locale === 'en' ? 'View members' : '查看成员' });
    expect(viewMembers.getAttribute('href')).toBe('/settings/members');
    expect(
      screen.getByText(locale === 'en' ? 'Claude Code settings are not connected.' : 'Claude Code 设置尚未接入。'),
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: locale === 'en' ? 'Configure' : '配置' })).toBeNull();
    expect(screen.queryByRole('button', { name: locale === 'en' ? 'Save' : '保存' })).toBeNull();
    expect(screen.queryByRole('combobox')).toBeNull();
  });

  test.each(['en', 'zh-CN'] as const)('shows unavailable services without ineffective actions in %s', (locale) => {
    for (const [section, message] of [
      ['tags', locale === 'en' ? 'Tag management is not connected.' : '标签管理尚未接入。'],
      ['billing', locale === 'en' ? 'Billing information is not connected.' : '账单信息尚未接入。'],
    ]) {
      resetTestDom(`https://oma.duck.ai/${section}`);
      const page = renderDashboardPage(<DashboardPage section={section} />, undefined, locale);
      expect(screen.getByText(message)).toBeTruthy();
      expect(screen.queryByRole('button')).toBeNull();
      expect(screen.queryByText('0')).toBeNull();
      if (section === 'billing')
        expect(screen.getByRole('heading', { name: locale === 'en' ? 'Billing' : '账单' })).toBeTruthy();
      page.unmount();
    }
  });

  test('directs default workspace member management to organization settings', async () => {
    resetTestDom('https://oma.duck.ai/members');
    const api = mockOrganizationMembersApi();
    renderDashboardPage(<DashboardPage section="members" />);
    expect(await screen.findByRole('heading', { name: 'Members' })).toBeTruthy();
    expect(screen.getByRole('link', { name: 'Go to organization settings' }).getAttribute('href')).toBe(
      '/settings/members',
    );
    expect(screen.queryByRole('button', { name: 'Invite' })).toBeNull();
    expect(api.requests).toEqual([]);
  });

  test('uses the shared workspace webhooks surface on the manage webhooks route', async () => {
    resetTestDom('https://oma.duck.ai/webhooks');
    setConsoleRequestContext({
      organizationUuid: 'org_test_uuid',
      workspaceId: defaultWorkspace.id,
    });
    const api = mockWebhooksList();

    renderDashboardPage(<DashboardPage section="webhooks" />);

    expect(await screen.findByRole('heading', { name: 'Webhooks' })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Add webhook endpoint' }).length).toBeGreaterThan(0);
    expect(await screen.findByText('No webhook endpoints yet')).toBeTruthy();
    expect(
      screen.queryByText('Create an endpoint to receive event notifications from Open Managed Agents.'),
    ).toBeNull();
    expect(api.requests[0]?.url).toBe('/v1/webhooks?beta=true');
    expect(api.requests[0]?.headers.get('anthropic-beta')).toBe('webhooks-2026-03-01');
    expect(api.requests[0]?.headers.get('x-workspace-id')).toBe(defaultWorkspace.id);
  });

  test('uses shared cards for available dashboard resources and an empty state for disconnected usage', () => {
    resetTestDom('https://oma.duck.ai/dashboard');
    const dashboard = renderDashboardPage(<DashboardPage section="dashboard" />);

    for (const label of ['Agents', 'LLM models']) {
      const card = screen.getByText(label).closest('[data-slot="card"]') as HTMLElement | null;
      expect(card).toBeTruthy();
      expect(card?.className.includes('surface-card')).toBe(false);
    }

    dashboard.unmount();
    resetTestDom('https://oma.duck.ai/claude-code/usage');
    renderDashboardPage(<DashboardPage section="claude-code-usage" />);

    expect(screen.queryByText('Seats')).toBeNull();
    expect(document.querySelector('[data-slot=card]')).toBeNull();

    const emptyState = screen
      .getByText('Claude Code usage is not connected.')
      .closest('[data-slot="empty"]') as HTMLElement | null;
    expect(emptyState).toBeTruthy();
    expect(emptyState?.className.includes('surface-card')).toBe(false);
  });
  test.each(['limits', 'service-accounts', 'privacy-controls', 'security'])(
    'retired workspace setting %s cannot expose simulated actions',
    (section) => {
      resetTestDom(`https://oma.duck.ai/${section}`);
      renderDashboardPage(<DashboardPage section={section} />);
      expect(screen.getByText('This page has been retired')).toBeTruthy();
      expect(screen.getByRole('link', { name: 'Back to app' }).getAttribute('href')).toBe('/dashboard');
      expect(screen.queryByRole('button')).toBeNull();
    },
  );
});

describe('Files page', () => {
  test('reports upload failures and restores the upload action', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    const requests = mockFilesList((url, _headers, method) => {
      if (url === '/v1/files?beta=true' && method === 'POST') {
        return new Response(
          JSON.stringify({
            type: 'error',
            error: { type: 'api_error', message: 'storage unavailable' },
          }),
          {
            status: 500,
            headers: { 'Content-Type': 'application/json' },
          },
        );
      }
      return {
        data: [],
        has_more: false,
        first_id: null,
        last_id: null,
      };
    });

    renderFilesPage();

    expect(await screen.findByText('Upload files to the Default workspace to see them here.')).toBeTruthy();
    const file = new File(['session input'], 'session-input.txt', { type: 'text/plain' });
    fireEvent.change(await screen.findByLabelText('Choose files to upload'), { target: { files: [file] } });

    expect(await screen.findByText('File upload failed')).toBeTruthy();
    expect(requests.some((request) => request.method === 'POST')).toBe(true);
    await waitFor(() =>
      expect((screen.getByRole('button', { name: 'Upload files' }) as HTMLButtonElement).disabled).toBe(false),
    );
  });

  test('uploads a file and refreshes the workspace file list', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    let uploaded = false;
    const requests = mockFilesList((url, _headers, method) => {
      if (url === '/v1/files?beta=true' && method === 'POST') {
        uploaded = true;
        return {
          id: 'file_uploaded123456',
          type: 'file',
          filename: 'session-input.txt',
          mime_type: 'text/plain',
          size_bytes: 13,
          created_at: new Date().toISOString(),
          downloadable: false,
        };
      }
      return {
        data: uploaded
          ? [
              {
                id: 'file_uploaded123456',
                type: 'file',
                filename: 'session-input.txt',
                mime_type: 'text/plain',
                size_bytes: 13,
                created_at: new Date().toISOString(),
                downloadable: false,
              },
            ]
          : [],
        has_more: false,
        first_id: uploaded ? 'file_uploaded123456' : null,
        last_id: uploaded ? 'file_uploaded123456' : null,
      };
    });

    renderFilesPage();

    expect(await screen.findByText('Upload files to the Default workspace to see them here.')).toBeTruthy();
    const file = new File(['session input'], 'session-input.txt', { type: 'text/plain' });
    fireEvent.change(await screen.findByLabelText('Choose files to upload'), { target: { files: [file] } });

    await waitFor(() =>
      expect(requests.map((request) => `${request.method} ${request.url}`)).toContain('POST /v1/files?beta=true'),
    );
    expect(await screen.findByText('session-input.txt')).toBeTruthy();
    const uploadRequest = requests.find(
      (request) => request.url === '/v1/files?beta=true' && request.method === 'POST',
    );
    expect(uploadRequest?.body).toBeInstanceOf(FormData);
    const uploadedFile = (uploadRequest?.body as FormData).get('file') as File;
    expect(uploadedFile.name).toBe('session-input.txt');
    expect(await uploadedFile.text()).toBe('session input');
    expect(uploadRequest?.headers.get('x-workspace-id')).toBe('default');
  });

  test('returns to the first page and refreshes successful files after a partial upload failure', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    let uploaded = false;
    const requests = mockFilesList((url, _headers, method, body) => {
      if (url === '/v1/files?beta=true' && method === 'POST') {
        const selectedFile = (body as FormData).get('file') as File;
        if (selectedFile.name === 'failed.txt') {
          return new Response(
            JSON.stringify({
              type: 'error',
              error: { type: 'api_error', message: 'storage unavailable' },
            }),
            {
              status: 500,
              headers: { 'Content-Type': 'application/json' },
            },
          );
        }
        uploaded = true;
        return {
          id: 'file_partial_success',
          type: 'file',
          filename: 'partial-success.txt',
          mime_type: 'text/plain',
          size_bytes: 7,
          created_at: new Date().toISOString(),
          downloadable: false,
        };
      }
      if (url.includes('after_id=file_page_one')) {
        return {
          data: [
            {
              id: 'file_page_two',
              type: 'file',
              filename: 'second-page.txt',
              mime_type: 'text/plain',
              size_bytes: 11,
              created_at: new Date(Date.now() - 240_000).toISOString(),
              downloadable: false,
            },
          ],
          has_more: false,
          first_id: 'file_page_two',
          last_id: 'file_page_two',
        };
      }
      return {
        data: [
          ...(uploaded
            ? [
                {
                  id: 'file_partial_success',
                  type: 'file',
                  filename: 'partial-success.txt',
                  mime_type: 'text/plain',
                  size_bytes: 7,
                  created_at: new Date().toISOString(),
                  downloadable: false,
                },
              ]
            : []),
          {
            id: 'file_page_one',
            type: 'file',
            filename: 'first-page.txt',
            mime_type: 'text/plain',
            size_bytes: 10,
            created_at: new Date(Date.now() - 120_000).toISOString(),
            downloadable: false,
          },
        ],
        has_more: true,
        first_id: uploaded ? 'file_partial_success' : 'file_page_one',
        last_id: 'file_page_one',
      };
    });

    renderFilesPage();

    expect(await screen.findByText('first-page.txt')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(await screen.findByText('second-page.txt')).toBeTruthy();

    const succeeded = new File(['success'], 'partial-success.txt', { type: 'text/plain' });
    const failed = new File(['failure'], 'failed.txt', { type: 'text/plain' });
    fireEvent.change(await screen.findByLabelText('Choose files to upload'), {
      target: { files: [succeeded, failed] },
    });

    expect(await screen.findByText('File upload failed')).toBeTruthy();
    expect(await screen.findByText('File uploaded')).toBeTruthy();
    expect(await screen.findByText('partial-success.txt')).toBeTruthy();
    expect(screen.queryByText('second-page.txt')).toBeNull();
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
    expect(
      requests.filter(
        (request) =>
          request.method === 'GET' && request.url === `/v1/files?beta=true&limit=${consoleResourceListLimit}`,
      ).length,
    ).toBeGreaterThanOrEqual(2);
  });

  test('renders files in the platform table shape with copy and disabled download actions', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    const clipboardWrite = mock(async (_value: string) => undefined);
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { writeText: clipboardWrite },
      configurable: true,
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
          downloadable: false,
        },
      ],
      has_more: false,
      first_id: 'file_abc123456789',
      last_id: 'file_abc123456789',
    }));

    renderFilesPage();

    expect(await screen.findByRole('heading', { name: 'Files' })).toBeTruthy();
    expect(await screen.findByText('report.json')).toBeTruthy();
    for (const heading of ['ID', 'Name', 'Size', 'Created']) {
      expect(screen.getByText(heading)).toBeTruthy();
    }
    expect(screen.getByText('321 B')).toBeTruthy();
    expect(screen.getByText('file_...23456789')).toBeTruthy();
    expect(screen.queryByRole('link', { name: /View Docs/i })).toBeNull();
    expect(screen.queryByText('Copy the template below to upload your first file:')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Copy file_abc123456789' }));

    await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith('file_abc123456789'));
    expect((screen.getByRole('button', { name: 'Download report.json' }) as HTMLButtonElement).disabled).toBe(true);
    expect(requests[0]?.url).toBe(`/v1/files?beta=true&limit=${consoleResourceListLimit}`);
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
            downloadable: false,
          },
        ],
        has_more: false,
        first_id: workspaceId === 'wrkspc_alt' ? 'file_alt' : 'file_default',
        last_id: workspaceId === 'wrkspc_alt' ? 'file_alt' : 'file_default',
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
          downloadable: false,
        },
      ],
      has_more: false,
      first_id: 'file_route',
      last_id: 'file_route',
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
              downloadable: false,
            },
          ],
          has_more: false,
          first_id: 'file_page_two',
          last_id: 'file_page_two',
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
            downloadable: false,
          },
        ],
        has_more: true,
        first_id: 'file_page_one',
        last_id: 'file_page_one',
      };
    });

    renderFilesPage();

    expect(await screen.findByText('first.md')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByText('second.md')).toBeTruthy();
    expect(
      requests.some(
        (request) => request.url === `/v1/files?beta=true&limit=${consoleResourceListLimit}&after_id=file_page_one`,
      ),
    ).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);
  });

  test('renders an empty files table without the old upload template card', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    mockFilesList(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null,
    }));

    renderFilesPage();

    expect(await screen.findByText('Upload files to the Default workspace to see them here.')).toBeTruthy();
    expect(screen.queryByText('import anthropic')).toBeNull();
    expect(screen.queryByRole('columnheader')).toBeNull();
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement).disabled).toBe(true);
  });

  test('refreshes the workspace name in the empty state when only the route changes', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/asdf/files');
    mockFilesList(() => ({
      data: [],
      has_more: false,
      first_id: null,
      last_id: null,
    }));

    renderFilesPage();

    // `asdf` is not a known workspace, so the name falls back to the route id.
    expect(await screen.findByText('Upload files to the asdf workspace to see them here.')).toBeTruthy();

    // Navigating to the default workspace must update the name even though the
    // active workspace context is unchanged — this is the regression for the
    // useLocation-driven scope.
    window.history.pushState(null, '', 'https://oma.duck.ai/workspaces/default/files');

    expect(await screen.findByText('Upload files to the Default workspace to see them here.')).toBeTruthy();
    expect(screen.queryByText('Upload files to the asdf workspace to see them here.')).toBeNull();
  });

  test('renders the standardized files error row and retries successfully', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/files');
    let attempt = 0;
    const requests = mockFilesList(() => {
      attempt += 1;
      if (attempt === 1) {
        return new Response(
          JSON.stringify({ error: { type: 'api_error', message: 'Files service is unavailable.' } }),
          {
            status: 503,
            headers: { 'Content-Type': 'application/json' },
          },
        );
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
            downloadable: false,
          },
        ],
        has_more: false,
        first_id: 'file_retry_success',
        last_id: 'file_retry_success',
      };
    });

    renderFilesPage();

    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByText('Files could not be loaded.')).toBeTruthy();
    expect(screen.getByText('Files service is unavailable.')).toBeTruthy();
    expect(screen.queryByRole('columnheader')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByText('recovered.txt')).toBeTruthy();
    expect(screen.getByRole('columnheader', { name: 'Name' })).toBeTruthy();
    expect(screen.queryByText('Files service is unavailable.')).toBeNull();
    expect(requests).toHaveLength(2);
  });
});

describe('Skills page', () => {
  for (const kind of ['skill', 'version'] as const) {
    test(`protects pending ${kind} deletion and preserves the failed confirmation through cancellation`, async () => {
      resetTestDom('https://oma.duck.ai/workspaces/default/skills');
      const skill = {
        id: 'skill_confirm',
        type: 'skill',
        display_title: 'Research delete target',
        latest_version: '1',
        source: 'custom',
        created_at: '2026-10-25T00:00:00Z',
        updated_at: '2026-10-25T00:00:00Z',
      };
      mockSkillsApi((url) =>
        url.includes('/versions?')
          ? {
              data: [
                {
                  id: 'version_confirm',
                  type: 'skill_version',
                  skill_id: skill.id,
                  version: '1',
                  name: skill.display_title,
                  directory: 'research',
                  description: 'Research notes',
                  created_at: skill.created_at,
                },
              ],
              has_more: false,
            }
          : url.includes('/skill_confirm?')
            ? skill
            : { data: [skill], has_more: false },
      );
      const fetchApi = globalThis.fetch;
      let deleteCalls = 0;
      let finishDelete: (response: Response) => void = () => {};
      globalThis.fetch = ((input, init) => {
        if (init?.method === 'DELETE') {
          deleteCalls++;
          return new Promise<Response>((resolve) => {
            finishDelete = resolve;
          });
        }
        return fetchApi(input, init);
      }) as typeof fetch;
      if (kind === 'version') {
        renderSkillDetailPage(skill.id);
        fireEvent.click(await screen.findByRole('button', { name: 'Delete version 1', exact: true }));
      } else {
        renderSkillsPage();
        fireEvent.click(await screen.findByRole('button', { name: 'Actions', exact: true }));
        fireEvent.click(await screen.findByRole('menuitem', { name: 'Delete', exact: true }));
      }
      const dialog = await screen.findByRole('alertdialog', {
        name: kind === 'skill' ? 'Delete skill?' : 'Delete version?',
      });
      fireEvent.click(within(dialog).getByRole('button', { name: 'Delete', exact: true }));
      await waitFor(() => expect(deleteCalls).toBe(1));
      expect(within(dialog).getByRole('button', { name: 'Cancel', exact: true })).toHaveProperty('disabled', true);
      fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
      expect(screen.getByRole('alertdialog')).toBe(dialog);
      await act(async () =>
        finishDelete(Response.json({ error: { message: 'Delete could not complete' } }, { status: 503 })),
      );
      await within(dialog).findByText('Delete could not complete');
      expect(document.activeElement).toBe(within(dialog).getByRole('alert'));
      expect(within(dialog).getByText(kind === 'skill' ? skill.display_title : '1', { exact: true })).toBeTruthy();
      fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel', exact: true }));
      expect(dialog.hasAttribute('data-closed')).toBe(true);
      expect(dialog.textContent).toContain('Delete could not complete');
      await waitFor(() => expect(screen.queryByRole('alertdialog')).toBeNull());
      expect(deleteCalls).toBe(1);
    });
  }

  test('renders skills in the platform list shape and opens the query-param drawer', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const clipboardWrite = mock(async (_value: string) => undefined);
    Object.defineProperty(globalThis.navigator, 'clipboard', {
      value: { writeText: clipboardWrite },
      configurable: true,
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
          updated_at: new Date(Date.now() - 60_000).toISOString(),
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
              created_at: new Date(Date.now() - 60_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
        };
      }
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 60_000).toISOString(),
            },
            {
              id: 'xlsx',
              type: 'skill',
              display_title: 'xlsx',
              latest_version: '20260203',
              source: 'anthropic',
              created_at: new Date(Date.now() - 240_000).toISOString(),
              updated_at: new Date(Date.now() - 240_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillsPage();

    expect(await screen.findByRole('heading', { name: 'Skills' })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Create skill/i })).toBeTruthy();
    expect(await screen.findByRole('columnheader', { name: 'ID' })).toBeTruthy();
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
    expect(requests[0]?.url).toBe(`/v1/skills?beta=true&limit=${consoleResourceListLimit}`);
    expect(requests[0]?.headers.get('anthropic-beta')).toBe('skills-2025-10-02');
    expect(requests[0]?.headers.get('x-workspace-id')).toBe('default');

    const copyButton = screen.getByRole('button', { name: 'Copy skill_emoji' });
    fireEvent.mouseEnter(copyButton);
    expect(screen.queryByText('Copy')).toBeNull();
    fireEvent.click(copyButton);

    await waitFor(() => expect(clipboardWrite).toHaveBeenCalledWith('skill_emoji'));
    expect(new URL(window.location.href).searchParams.get('skill')).toBeNull();

    fireEvent.click(screen.getByText('emoji-translator'));

    expect(
      await screen.findByText('This skill should be used when the user asks to turn this into emojis.'),
    ).toBeTruthy();
    expect(new URL(window.location.href).searchParams.get('skill')).toBe('skill_emoji');
    expect(
      requests.find((request) => request.url === '/v1/skills/skill_emoji?beta=true')?.headers.get('x-workspace-id'),
    ).toBe('default');
    expect(
      requests
        .find((request) => request.url === '/v1/skills/skill_emoji/versions?beta=true&limit=50')
        ?.headers.get('x-workspace-id'),
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
            updated_at: new Date(Date.now() - 120_000).toISOString(),
          },
        ],
        has_more: false,
        next_page: null,
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
              updated_at: new Date(Date.now() - 240_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
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
            updated_at: new Date(Date.now() - 120_000).toISOString(),
          },
        ],
        has_more: true,
        next_page: 'cursor_two',
      };
    });

    renderSkillsPage();

    expect((await screen.findAllByText('frontend-design')).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByLabelText('Page 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect((await screen.findAllByText('product-self-knowledge')).length).toBeGreaterThanOrEqual(1);
    expect(screen.getByLabelText('Page 2')).toBeTruthy();
    expect(
      requests.some(
        (request) => request.url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}&page=cursor_two`,
      ),
    ).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));

    expect((await screen.findAllByText('frontend-design')).length).toBeGreaterThanOrEqual(1);
    await waitFor(() => expect(screen.queryAllByText('product-self-knowledge')).toHaveLength(0));
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
          updated_at: new Date(Date.now() - 120_000).toISOString(),
        },
      ],
      has_more: true,
      next_page: null,
    }));

    renderSkillsPage();

    expect((await screen.findAllByText('frontend-design')).length).toBeGreaterThanOrEqual(1);
    const next = screen.getByRole('button', { name: 'Next page' }) as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(next);
    expect(requests).toHaveLength(1);
  });

  test('blocks dismissal during a skill upload and keeps the selection after failure', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    mockSkillsApi(() => ({ data: [], has_more: false, next_page: null }));
    const mockedFetch = globalThis.fetch;
    let uploadCalls = 0;
    let finishRequest: (response: Response) => void = () => {};
    globalThis.fetch = (input, init) =>
      init?.method === 'POST'
        ? new Promise<Response>((resolve) => {
            uploadCalls++;
            finishRequest = resolve;
          })
        : mockedFetch(input, init);
    renderSkillsPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Create skill' }));
    const dialog = await screen.findByRole('dialog', { name: 'Create skill' });
    const file = new File(['example archive bytes'], 'research.zip', { type: 'application/zip' });
    fireEvent.change(dialog.querySelector('input[type="file"]')!, { target: { files: [file] } });
    await within(dialog).findByText('research.zip');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create skill', exact: true }));
    await waitFor(() =>
      expect(within(dialog).getByRole('button', { name: 'Close' }).hasAttribute('disabled')).toBe(true),
    );
    expect(within(dialog).getByRole('button', { name: 'Cancel' }).hasAttribute('disabled')).toBe(true);
    expect(within(dialog).getByRole('button', { name: 'Remove upload' }).hasAttribute('disabled')).toBe(true);
    fireEvent.keyDown(dialog, { key: 'Escape', code: 'Escape' });
    expect(screen.getByRole('dialog')).toBe(dialog);
    finishRequest(Response.json({ error: { type: 'api_error', message: 'Example upload failed' } }, { status: 500 }));
    await within(dialog).findByText('Example upload failed');
    expect(within(dialog).getByText('research.zip')).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Create skill', exact: true })).toHaveProperty('disabled', false);
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create skill', exact: true }));
    await waitFor(() => expect(uploadCalls).toBe(2));
    finishRequest(Response.json({ error: { type: 'api_error', message: 'Second upload failed' } }, { status: 500 }));
    await within(dialog).findByText('Second upload failed');
    expect(document.activeElement).toBe(within(dialog).getByRole('alert'));
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel' }));
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: 'Create skill' }));
    expect(within(await screen.findByRole('dialog')).queryByText('research.zip')).toBeNull();
  });

  test('keeps a selected update package through cancellation and clears it before reopening', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    mockSkillsApi(() => ({
      data: [
        {
          id: 'skill_cancel',
          type: 'skill',
          display_title: 'Research',
          latest_version: '1',
          source: 'custom',
          created_at: '2026-10-25T00:00:00Z',
          updated_at: '2026-10-25T00:00:00Z',
        },
      ],
      has_more: false,
    }));
    renderSkillsPage();
    fireEvent.click(await screen.findByRole('button', { name: 'Actions', exact: true }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Update', exact: true }));
    const dialog = await screen.findByRole('dialog');
    fireEvent.change(dialog.querySelector('input[type="file"]')!, {
      target: { files: [new File(['zip bytes'], 'research.zip', { type: 'application/zip' })] },
    });
    await within(dialog).findByText('research.zip');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Cancel', exact: true }));
    expect(dialog.hasAttribute('data-closed')).toBe(true);
    expect(dialog.textContent).toContain('research.zip');
    await waitFor(() => expect(screen.queryByRole('dialog')).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: 'Actions', exact: true }));
    fireEvent.click(await screen.findByRole('menuitem', { name: 'Update', exact: true }));
    expect(within(await screen.findByRole('dialog')).queryByText('research.zip')).toBeNull();
  });

  test('retries a failed version directory without showing a false empty description', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills/skill_versions');
    let attempts = 0;
    const skill = {
      id: 'skill_versions',
      type: 'skill',
      display_title: 'Research',
      latest_version: '1',
      source: 'custom',
      created_at: '2026-10-25T00:00:00Z',
      updated_at: '2026-10-25T00:00:00Z',
    };
    const requests = mockSkillsApi((url) => {
      if (url.includes('/versions?')) {
        attempts++;
        return attempts === 1
          ? Response.json({ error: { message: 'Version directory unavailable' } }, { status: 503 })
          : {
              data: [
                {
                  id: 'version_1',
                  type: 'skill_version',
                  skill_id: skill.id,
                  version: '1',
                  name: 'Research',
                  directory: 'research',
                  description: 'Recovered version description',
                  created_at: skill.created_at,
                },
              ],
              has_more: false,
            };
      }
      return url.includes('/skill_versions?') ? skill : { data: [skill], has_more: false };
    });
    renderSkillDetailPage(skill.id);
    await screen.findByText('Version directory unavailable');
    expect(screen.queryByText('No description provided.')).toBeNull();
    expect(screen.getByRole('button', { name: 'Close', exact: true })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry', exact: true }));
    await screen.findByText('Recovered version description');
    expect(attempts).toBe(2);
    expect(requests.every((request) => request.method === 'GET')).toBe(true);
  });

  test('creates a skill from a single archive upload', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return { data: [], has_more: false, next_page: null };
      }
      if (url === '/v1/skills?beta=true') {
        return {
          id: 'skill_created',
          display_title: 'emoji-translator',
          latest_version: '20260708',
          source: 'custom',
          created_at: new Date(Date.now() - 10_000).toISOString(),
          updated_at: new Date(Date.now() - 10_000).toISOString(),
        };
      }
      if (url === '/v1/skills/skill_created?beta=true') {
        return {
          id: 'skill_created',
          type: 'skill',
          display_title: 'emoji-translator',
          latest_version: '20260708',
          source: 'custom',
          created_at: new Date(Date.now() - 120_000).toISOString(),
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
              created_at: new Date(Date.now() - 10_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
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
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create skill' }));

    await waitFor(() =>
      expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true')).toBe(
        true,
      ),
    );
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
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
        };
      }
      if (url === '/v1/skills?beta=true') {
        return new Response(
          JSON.stringify({
            error: {
              message:
                'A custom skill named "emoji-translator" already exists. Use Update from that skill\'s actions menu to upload a new version.',
              type: 'invalid_request_error',
            },
            type: 'error',
          }),
          {
            status: 400,
            headers: { 'Content-Type': 'application/json' },
          },
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
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Create skill' }));

    await waitFor(() =>
      expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true')).toBe(
        true,
      ),
    );
    expect(
      requests.some(
        (request) => request.method === 'POST' && request.url === '/v1/skills/skill_emoji/versions?beta=true',
      ),
    ).toBe(false);
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
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [],
          has_more: false,
          next_page: null,
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
    expect(
      (within(screen.getByRole('dialog')).getByRole('button', { name: 'Create skill' }) as HTMLButtonElement).disabled,
    ).toBe(true);
    expect(requests.some((request) => request.method === 'POST' && request.url === '/v1/skills?beta=true')).toBe(false);
  });

  test('updates a custom skill from the action menu', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
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
          created_at: new Date(Date.now() - 10_000).toISOString(),
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
    fireEvent.click(within(screen.getByRole('dialog')).getByRole('button', { name: 'Update' }));

    await waitFor(() =>
      expect(
        requests.some(
          (request) => request.method === 'POST' && request.url === '/v1/skills/skill_emoji/versions?beta=true',
        ),
      ).toBe(true),
    );
    await waitFor(() => expect(screen.queryByText('Update Skill')).toBeNull());
    expect(screen.queryByText('emoji-translator.zip')).toBeNull();
  });

  test('deletes a custom skill atomically from the skill endpoint', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills');
    const requests = mockSkillsApi((url) => {
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [
            {
              id: 'skill_emoji',
              type: 'skill',
              display_title: 'emoji-translator',
              latest_version: '20260708',
              source: 'custom',
              created_at: new Date(Date.now() - 120_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
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
    expect(await screen.findByRole('alertdialog', { name: 'Delete skill?' })).toBeTruthy();
    const deleteButtons = screen.getAllByRole('button', { name: 'Delete' });
    fireEvent.click(deleteButtons[deleteButtons.length - 1]);

    await waitFor(() =>
      expect(
        requests.some((request) => request.method === 'DELETE' && request.url === '/v1/skills/skill_emoji?beta=true'),
      ).toBe(true),
    );
    const deleteOrder = requests.map((request) => (request.method === 'DELETE' ? request.url : '')).filter(Boolean);
    expect(deleteOrder).toEqual(['/v1/skills/skill_emoji?beta=true']);
  });
});

describe('Skill detail page', () => {
  test('renders the selected skill in the query-param drawer', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills/frontend-design');
    const requests = mockSkillsApi((url) => {
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [
            {
              id: 'frontend-design',
              type: 'skill',
              display_title: 'frontend-design',
              latest_version: '20260708',
              source: 'anthropic',
              created_at: new Date(Date.now() - 360_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
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
          updated_at: new Date(Date.now() - 120_000).toISOString(),
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
              created_at: new Date(Date.now() - 360_000).toISOString(),
            },
            {
              id: 'skillver_frontend-design-2',
              type: 'skill_version',
              description: 'v2 description',
              directory: 'frontend-design',
              name: 'frontend-design v2',
              skill_id: 'frontend-design',
              version: '20260708',
              created_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
        };
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderSkillDetailPage('frontend-design');

    expect(await screen.findByText('v2 description')).toBeTruthy();
    expect(screen.getByText('Latest')).toBeTruthy();
    expect(new URL(window.location.href).searchParams.get('skill')).toBe('frontend-design');
    expect(
      requests.find((request) => request.url === '/v1/skills/frontend-design?beta=true')?.headers.get('x-workspace-id'),
    ).toBe('default');
    expect(
      requests
        .find((request) => request.url === '/v1/skills/frontend-design/versions?beta=true&limit=50')
        ?.headers.get('x-workspace-id'),
    ).toBe('default');
  });

  test('renders the standardized skill detail error alert and retries successfully', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/skills/frontend-design');
    let attempt = 0;
    const requests = mockSkillsApi((url) => {
      if (url === `/v1/skills?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [
            {
              id: 'frontend-design',
              type: 'skill',
              display_title: 'frontend-design',
              latest_version: '2',
              source: 'anthropic',
              created_at: new Date(Date.now() - 360_000).toISOString(),
              updated_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
        };
      }
      if (url === '/v1/skills/frontend-design?beta=true') {
        attempt += 1;
        if (attempt === 1) {
          return new Response(
            JSON.stringify({ error: { type: 'api_error', message: 'Skill metadata is unavailable.' } }),
            {
              status: 503,
              headers: { 'Content-Type': 'application/json' },
            },
          );
        }
        return {
          id: 'frontend-design',
          type: 'skill',
          display_title: 'frontend-design',
          latest_version: '2',
          source: 'anthropic',
          created_at: new Date(Date.now() - 360_000).toISOString(),
          updated_at: new Date(Date.now() - 120_000).toISOString(),
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
              created_at: new Date(Date.now() - 120_000).toISOString(),
            },
          ],
          has_more: false,
          next_page: null,
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
  test('keeps first-page loading feedback free of unavailable table controls', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    globalThis.fetch = mock(() => new Promise<Response>(() => {})) as typeof fetch;
    renderBatchesPage();

    expect(screen.getByText('Loading batches...')).toBeTruthy();
    expect(screen.queryByRole('columnheader')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Previous page' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
  });

  test('offers retry without table chrome on a first-page failure and restores columns after recovery', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    let failed = true;
    mockMessageBatchesApi(() =>
      failed
        ? new Response(JSON.stringify({ error: { message: 'Batch service unavailable' } }), {
            status: 503,
            headers: { 'Content-Type': 'application/json' },
          })
        : { data: [makeBatch()], has_more: false },
    );
    renderBatchesPage();

    expect(await screen.findByText('Batch service unavailable')).toBeTruthy();
    expect(screen.queryByRole('columnheader')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Previous page' })).toBeNull();
    failed = false;
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));

    expect(await screen.findByRole('button', { name: 'msgbatch_test' })).toBeTruthy();
    expect(screen.getAllByRole('columnheader')).toHaveLength(4);
  });

  test('omits empty first-page controls while preserving navigation out of an empty later page', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    mockMessageBatchesApi(() => ({ data: [], has_more: false }));
    const empty = renderBatchesPage();
    expect(await screen.findByText('No batches have been created in the Default workspace.')).toBeTruthy();
    expect(screen.queryByRole('columnheader')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Next page' })).toBeNull();
    empty.unmount();

    mockMessageBatchesApi((url) =>
      url.includes('after_id=msgbatch_test')
        ? { data: [], has_more: false }
        : { data: [makeBatch()], has_more: true, last_id: 'msgbatch_test' },
    );
    renderBatchesPage();
    expect(await screen.findByRole('button', { name: 'msgbatch_test' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(await screen.findByText('No batches have been created in the Default workspace.')).toBeTruthy();
    expect(screen.queryByRole('columnheader')).toBeNull();
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);
    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));
    expect(await screen.findByRole('button', { name: 'msgbatch_test' })).toBeTruthy();
    expect(screen.getAllByRole('columnheader')).toHaveLength(4);
  });

  test('renders the standardized batch detail error alert and retries successfully', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches?batch=msgbatch_detail');
    let attempt = 0;
    const batch = makeBatch({
      id: 'msgbatch_detail',
      processing_status: 'ended',
      results_url: 'https://oma.duck.ai/v1/messages/batches/msgbatch_detail/results',
    });
    const requests = mockMessageBatchesApi((url) => {
      if (url === `/v1/messages/batches?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [batch],
          has_more: false,
          first_id: 'msgbatch_detail',
          last_id: 'msgbatch_detail',
        };
      }
      if (url === '/v1/messages/batches/msgbatch_detail?beta=true') {
        attempt += 1;
        if (attempt === 1) {
          return new Response(
            JSON.stringify({ error: { type: 'api_error', message: 'Batch details are unavailable.' } }),
            {
              status: 503,
              headers: { 'Content-Type': 'application/json' },
            },
          );
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
    expect(requests.filter((request) => request.url === '/v1/messages/batches/msgbatch_detail?beta=true')).toHaveLength(
      2,
    );
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
      results_url: 'https://oma.duck.ai/v1/messages/batches/msgbatch_done/results',
    });
    const requests = mockMessageBatchesApi((url) => {
      if (url === `/v1/messages/batches?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [endedBatch],
          has_more: false,
          first_id: 'msgbatch_done',
          last_id: 'msgbatch_done',
        };
      }
      if (url === '/v1/messages/batches/msgbatch_done?beta=true') {
        return endedBatch;
      }
      if (url === '/v1/messages/batches/msgbatch_done/results?beta=true') {
        return new Response(new Blob(['{"custom_id":"one"}\n']), {
          status: 200,
          headers: { 'Content-Type': 'application/x-jsonl' },
        });
      }
      throw new Error(`Unexpected request: ${url}`);
    });

    renderBatchesPage();

    expect(await screen.findByRole('heading', { name: 'Batches' })).toBeTruthy();
    expect(await screen.findByRole('button', { name: 'msgbatch_done' })).toBeTruthy();
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
    expect(requests[0]?.url).toBe(`/v1/messages/batches?beta=true&limit=${consoleResourceListLimit}`);
    expect(requests[0]?.headers.get('anthropic-beta')).toBe('message-batches-2024-09-24');
    expect(requests[0]?.headers.get('anthropic-version')).toBe('2023-06-01');
    expect(requests[0]?.headers.get('x-workspace-id')).toBe('default');
    expect(
      requests
        .find((request) => request.url === '/v1/messages/batches/msgbatch_done?beta=true')
        ?.headers.get('x-workspace-id'),
    ).toBe('default');

    fireEvent.click(screen.getByRole('button', { name: 'Download Results' }));

    await waitFor(() =>
      expect(requests.some((request) => request.url === '/v1/messages/batches/msgbatch_done/results?beta=true')).toBe(
        true,
      ),
    );
    const downloadRequest = requests.find(
      (request) => request.url === '/v1/messages/batches/msgbatch_done/results?beta=true',
    );
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
        last_id: batchId,
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
          last_id: 'msgbatch_page_two',
        };
      }
      return {
        data: [makeBatch({ id: 'msgbatch_page_one', processing_status: 'in_progress' })],
        has_more: true,
        first_id: 'msgbatch_page_one',
        last_id: 'msgbatch_page_one',
      };
    });

    renderBatchesPage();

    expect(await screen.findByRole('button', { name: 'msgbatch_page_one' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByRole('button', { name: 'msgbatch_page_two' })).toBeTruthy();
    expect(
      requests.some(
        (request) =>
          request.url === `/v1/messages/batches?beta=true&limit=${consoleResourceListLimit}&after_id=msgbatch_page_one`,
      ),
    ).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous page' }) as HTMLButtonElement).disabled).toBe(false);
  });

  test('selects a batch and cancels in-progress batches from the inspector', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/batches');
    const inProgressBatch = makeBatch({ id: 'msgbatch_progress', processing_status: 'in_progress' });
    const endedBatch = makeBatch({ id: 'msgbatch_done', processing_status: 'ended' });
    const requests = mockMessageBatchesApi((url, method) => {
      if (url === `/v1/messages/batches?beta=true&limit=${consoleResourceListLimit}`) {
        return {
          data: [inProgressBatch, endedBatch],
          has_more: false,
          first_id: 'msgbatch_progress',
          last_id: 'msgbatch_done',
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
          (request) =>
            request.url === '/v1/messages/batches/msgbatch_progress/cancel?beta=true' && request.method === 'POST',
        ),
      ).toBe(true),
    );
    expect(
      requests
        .find((request) => request.url === '/v1/messages/batches/msgbatch_progress/cancel?beta=true')
        ?.headers.get('x-workspace-id'),
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
        headers: { 'Content-Type': 'application/json' },
      });
    }

    return new Response(JSON.stringify({ error: { message: 'not found' } }), {
      status: 404,
      headers: { 'Content-Type': 'application/json' },
    });
  }) as unknown as typeof fetch;

  return { requests };
}

const dashboardTestRootRoute = createRootRoute();
const dashboardTestFallbackRoute = createRoute({
  getParentRoute: () => dashboardTestRootRoute,
  path: '$',
});
const dashboardTestRouteTree = dashboardTestRootRoute.addChildren([dashboardTestFallbackRoute]);

function DashboardTestRouterProvider({
  router,
  children,
}: {
  router: ReturnType<typeof createRouter<typeof dashboardTestRouteTree>>;
  children: ReactNode;
}) {
  useEffect(() => {
    // Mirror the subscription that <RouterProvider>'s Transitioner sets up, so
    // useLocation() reflects window.history.pushState calls in tests.
    const unsubscribe = router.history.subscribe(router.load);
    return () => {
      unsubscribe();
      router.history.destroy();
    };
  }, [router]);
  return <RouterContextProvider router={router}>{children}</RouterContextProvider>;
}

function renderDashboardPage(
  children: ReactNode,
  workspace?: Partial<Workspace>,
  locale: Locale = 'en',
  authValue: AuthContextValue = makeAuthContextValue(),
) {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
        gcTime: 0,
      },
    },
  });
  const router = createRouter({
    history: createBrowserHistory({ window }),
    routeTree: dashboardTestRouteTree,
  });
  const renderWithWorkspace = (nextWorkspace?: Partial<Workspace>) => {
    const value = makeWorkspaceContextValue(nextWorkspace);
    return (
      <DashboardTestRouterProvider router={router}>
        <I18nProvider initialLocale={locale}>
          <AuthContext.Provider value={authValue}>
            <WorkspaceContext.Provider value={value}>
              <QueryClientProvider client={queryClient}>
                <Toaster closeButton toastOptions={{ closeButtonAriaLabel: 'Close' }} />
                {children}
              </QueryClientProvider>
            </WorkspaceContext.Provider>
          </AuthContext.Provider>
        </I18nProvider>
      </DashboardTestRouterProvider>
    );
  };

  const rendered = render(renderWithWorkspace(workspace));
  return {
    ...rendered,
    rerenderWorkspace: (nextWorkspace?: Partial<Workspace>) => rendered.rerender(renderWithWorkspace(nextWorkspace)),
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
      ...accountOverrides,
    },
    status: 'authenticated',
    csrfToken: 'csrf_test',
    refresh: async () => ({ account: null }),
    logout: async () => undefined,
  };
}

function makeWorkspaceContextValue(workspace?: Partial<Workspace>): WorkspaceContextValue {
  const activeWorkspace = {
    ...defaultWorkspace,
    ...workspace,
    type: 'workspace' as const,
  };
  const workspaces =
    activeWorkspace.id === defaultWorkspace.id ? [activeWorkspace] : [defaultWorkspace, activeWorkspace];
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
      data_residency: input.data_residency,
    }),
    refreshWorkspaces: async () => undefined,
  };
}

function mockFilesList(handler: (url: string, headers: Headers, method: string, body?: BodyInit | null) => unknown) {
  const requests: Array<{ url: string; method: string; headers: Headers; body?: BodyInit | null }> = [];
  globalThis.fetch = mock(async (input: RequestInfo | URL, init?: RequestInit) => {
    const request = input instanceof Request ? input : null;
    const url =
      typeof input === 'string'
        ? input
        : input instanceof URL
          ? input.toString()
          : new URL(input.url).pathname + new URL(input.url).search;
    const method = init?.method ?? request?.method ?? 'GET';
    const headers = new Headers(init?.headers ?? request?.headers);
    const body = init?.body ?? request?.body;
    requests.push({ url, method, headers, body });
    const result = handler(url, headers, method, body);
    if (result instanceof Response) {
      return result;
    }
    return new Response(JSON.stringify(result), {
      status: 200,
      headers: { 'Content-Type': 'application/json' },
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
      headers: { 'Content-Type': 'application/json' },
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
      headers: { 'Content-Type': 'application/json' },
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
            role: 'admin',
          },
        ]),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
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
            expires_at: '2026-07-15T00:00:00Z',
          },
        ]),
        {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        },
      );
    }
    throw new Error(`Unexpected request: ${url}`);
  }) as unknown as typeof fetch;
  return { requests };
}

function makeBatch(
  overrides: Partial<{
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
  }> = {},
) {
  return {
    id: overrides.id ?? 'msgbatch_test',
    type: 'message_batch',
    processing_status: overrides.processing_status ?? 'ended',
    request_counts: overrides.request_counts ?? {
      processing: overrides.processing_status === 'in_progress' ? 3 : 0,
      succeeded: overrides.processing_status === 'ended' ? 3 : 0,
      errored: 0,
      canceled: 0,
      expired: 0,
    },
    created_at: new Date(Date.now() - 120_000).toISOString(),
    expires_at: new Date(Date.now() + 86_400_000).toISOString(),
    ended_at: overrides.processing_status === 'ended' ? new Date(Date.now() - 60_000).toISOString() : null,
    cancel_initiated_at: null,
    archived_at: null,
    results_url:
      overrides.results_url ??
      (overrides.processing_status === 'ended'
        ? 'https://oma.duck.ai/v1/messages/batches/msgbatch_test/results'
        : null),
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
