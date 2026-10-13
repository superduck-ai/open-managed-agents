import { expect, test } from 'bun:test';
import { I18nProvider } from '../../shared/i18n';
import { templateTags } from './agentConfig';
import { TemplateCard } from './components/CodeBlocks';
import {
  ManagedAgentsPage,
  WorkspaceContext,
  fireEvent,
  mockAgentsApi,
  render,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  waitFor,
  within,
  workspaceContextValue,
} from './ManagedAgentsPage.test-utils';
import type { AuthContextValue } from '../../shared/auth/context';

export function registerManagedAgentsQuickstartTests() {
  test('guides quickstart to LLM configuration when no provider exists', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    mockAgentsApi([], { modelsNotConfigured: true });
    render(
      <I18nProvider initialLocale="en">
        <WorkspaceContext.Provider value={workspaceContextValue('default')}>
          <ManagedAgentsPage section="quickstart" />
        </WorkspaceContext.Provider>
      </I18nProvider>,
      undefined,
      { seedModels: false },
    );

    const emptyState = await screen.findByTestId('llm-provider-required');
    expect(emptyState.textContent).toContain('No models configured');
    expect(emptyState.textContent).toContain('Configure a model to get started.');
    expect(within(emptyState).getByRole('button', { name: 'Configure models' })).toBeTruthy();
    expect(within(emptyState).queryByRole('alert')).toBeNull();
  });

  test('does not send non-administrators from quickstart to model configuration', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    mockAgentsApi([], { modelsNotConfigured: true });
    render(
      <I18nProvider initialLocale="en">
        <WorkspaceContext.Provider value={workspaceContextValue('default')}>
          <ManagedAgentsPage section="quickstart" />
        </WorkspaceContext.Provider>
      </I18nProvider>,
      undefined,
      { auth: quickstartAuth('developer'), seedModels: false },
    );

    const emptyState = await screen.findByTestId('llm-provider-required');
    expect(emptyState.textContent).toContain('Contact your organization administrator to configure a model.');
    expect(within(emptyState).queryByRole('button', { name: 'Configure models' })).toBeNull();
  });

  test('keeps quickstart model loading failures distinct from the provider empty state', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    mockAgentsApi([], { modelsErrorOnce: true });
    render(
      <I18nProvider initialLocale="en">
        <WorkspaceContext.Provider value={workspaceContextValue('default')}>
          <ManagedAgentsPage section="quickstart" />
        </WorkspaceContext.Provider>
      </I18nProvider>,
      undefined,
      { seedModels: false },
    );

    const alert = await screen.findByRole('alert');
    expect(alert.textContent).toContain('Retry before creating an agent');
    expect(screen.getByRole('button', { name: 'Retry' })).toBeTruthy();
    expect(screen.queryByTestId('llm-provider-required')).toBeNull();
  });

  test('renders the agents list controls and empty state in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([]);
    renderManagedAgentsPage('agents', 'zh-CN');

    expect(screen.getByRole('heading', { name: 'Agents' })).toBeTruthy();
    expect(screen.getByText('创建并管理自主 Agent。')).toBeTruthy();
    expect(screen.getByRole('button', { name: '创建 Agent' })).toBeTruthy();
    expect(screen.getByPlaceholderText('按名称或精确 ID 搜索')).toBeTruthy();
    expect(screen.getByRole('button', { name: /创建时间\s+全部时间/ })).toBeTruthy();
    expect(screen.getByRole('button', { name: /状态\s+活跃/ })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /状态\s+活跃/ }));

    expect(screen.getByRole('menuitemradio', { name: '活跃' })).toBeTruthy();
    expect(screen.getByRole('menuitemradio', { name: '全部' })).toBeTruthy();
    expect(await screen.findByText('暂无 Agent')).toBeTruthy();
    expect(screen.getByRole('button', { name: '开始使用 Agents' })).toBeTruthy();
    expect(api.requests[0]?.headers['x-workspace-id']).toBe('default');
  });

  test('localizes the create-agent collapsed starting point summary in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    mockAgentsApi([]);
    renderManagedAgentsPage('agents', 'zh-CN');

    await waitFor(() => expect(document.documentElement.lang).toBe('zh-CN'));

    fireEvent.click(screen.getByRole('button', { name: '创建 Agent' }));

    const dialog = screen.getByRole('dialog', { name: '创建 Agent' });
    fireEvent.click(within(dialog).getByRole('tab', { name: '模板' }));
    fireEvent.click(within(dialog).getByRole('button', { name: /深度调研助手/i }));

    expect(within(dialog).getByRole('button', { name: '起点 · 深度调研助手' }).getAttribute('aria-expanded')).toBe(
      'false',
    );
    expect(within(dialog).getByText(/· 深度调研助手/)).toBeTruthy();
  });

  test('renders overflow template tags as a visible count badge', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(
      <I18nProvider initialLocale="en">
        <TemplateCard
          template={{
            id: 'tag-overflow',
            slug: 'tag-overflow',
            title: 'Tag overflow',
            body: 'Template body.',
            prompt: 'Create an agent.',
            tags: [
              templateTags.notion,
              templateTags.slack,
              templateTags.sentry,
              templateTags.linear,
              templateTags.github,
            ],
          }}
          onClick={() => {}}
        />
      </I18nProvider>,
    );

    const templateCard = screen.getByRole('button', { name: /Tag overflow/i });
    expect(within(templateCard).getByTitle('notion')).toBeTruthy();
    expect(within(templateCard).getByTitle('slack')).toBeTruthy();
    expect(within(templateCard).getByTitle('sentry')).toBeTruthy();
    expect(within(templateCard).getByTitle('linear')).toBeTruthy();
    expect(within(templateCard).queryByTitle('github')).toBeNull();
    expect(within(templateCard).getByTitle('1 more tag')).toBeTruthy();
    expect(within(templateCard).getByText('+1')).toBeTruthy();
    expect(templateCard.getAttribute('aria-label')).toContain('github');
  });
}

function quickstartAuth(role: string): AuthContextValue {
  return {
    account: {
      uuid: 'acct_managed_agents_test',
      email_address: 'managed-agents-test@example.com',
      memberships: [{ role, organization: { uuid: 'org_test' } }],
    },
    status: 'authenticated',
    csrfToken: 'csrf_managed_agents_test',
    refresh: async () => undefined,
    logout: async () => undefined,
  };
}
