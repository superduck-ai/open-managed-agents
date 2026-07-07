import { expect, test } from 'bun:test';
import { clampQuickstartInspectorPaneWidth } from './quickstart/AgentQuickstartPage';
import {
  ManagedAgentsPage,
  WorkspaceContext,
  cleanup,
  codeBlockContaining,
  createAgentRequestFixture,
  expectPageTextToContain,
  fireEvent,
  mockAgentsApi,
  mockManagedResourceApi,
  quickstartTextAndToolStream,
  quickstartTextServerToolAndToolStream,
  quickstartTextStream,
  quickstartToolStream,
  render,
  renderManagedAgentsPage,
  resetTestDom,
  screen,
  selectManagedComboboxOption,
  serverAgent,
  sessionStatusValuesFromUrl,
  setAgentConfigEditorValue,
  waitFor,
  within,
  workspaceContextValue
} from './ManagedAgentsPage.test-utils';

if (typeof globalThis.DOMRect === 'undefined') {
  class TestDOMRect {
    x: number;
    y: number;
    width: number;
    height: number;
    top: number;
    right: number;
    bottom: number;
    left: number;

    constructor(x = 0, y = 0, width = 0, height = 0) {
      this.x = x;
      this.y = y;
      this.width = width;
      this.height = height;
      this.top = y;
      this.right = x + width;
      this.bottom = y + height;
      this.left = x;
    }

    static fromRect(rect: Partial<DOMRectInit> = {}) {
      return new TestDOMRect(rect.x ?? 0, rect.y ?? 0, rect.width ?? 0, rect.height ?? 0);
    }

    toJSON() {
      return {
        x: this.x,
        y: this.y,
        width: this.width,
        height: this.height,
        top: this.top,
        right: this.right,
        bottom: this.bottom,
        left: this.left
      };
    }
  }

  Object.assign(globalThis, { DOMRect: TestDOMRect });
}

export function registerManagedAgentsQuickstartTests() {
  test('renders the quickstart template browser in Chinese', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    renderManagedAgentsPage('quickstart', 'zh-CN');

    await waitFor(() => expect(document.documentElement.lang).toBe('zh-CN'));
    expect(screen.getByText('快速开始')).toBeTruthy();
    expect(screen.getByText('创建 Agent')).toBeTruthy();
    expect(screen.getByText('你想构建什么？')).toBeTruthy();
    expect(screen.getByRole('heading', { name: '浏览模板' })).toBeTruthy();
    expect(screen.getByPlaceholderText('搜索模板')).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('搜索模板'), { target: { value: '事故' } });

    expect(screen.getByRole('button', { name: /事故指挥官/i })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /数据分析师/i })).toBeNull();
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
    fireEvent.click(within(dialog).getByRole('button', { name: /深度研究员/i }));

    expect(within(dialog).getByRole('button', { name: /^起点$/i }).getAttribute('aria-expanded')).toBe('false');
    const collapsedSummary = within(dialog)
      .getAllByText('深度研究员')
      .find((node) => node.closest('[data-slot="badge"]'));
    expect(collapsedSummary?.closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
  });

  test('renders the quickstart template browser and opens a template detail view', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(<ManagedAgentsPage section="quickstart" />);

    expect(screen.getByText('Quickstart')).toBeTruthy();
    expect(screen.getByText('Create agent')).toBeTruthy();
    expect(screen.getByText('What do you want to build?')).toBeTruthy();
    const browserHeading = screen.getByRole('heading', { name: 'Browse templates' });
    const browserCard = browserHeading.closest('[data-slot="card"]') as HTMLElement | null;
    expect(browserHeading).toBeTruthy();
    expect(browserCard?.getAttribute('data-slot')).toBe('card');
    expect(browserCard?.className).toContain('h-full');
    expect(screen.queryByRole('button', { name: 'Browse templates' })).toBeNull();
    expect(screen.getByRole('button', { name: /Deep researcher/i })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Send message/i }).hasAttribute('disabled')).toBe(true);
    const fieldMonitorTemplate = screen.getByRole('button', { name: /Field monitor/i });
    const templateGrid = fieldMonitorTemplate.parentElement as HTMLElement | null;
    const fieldMonitorDescription = within(fieldMonitorTemplate).getByText(/Scans software blogs for a topic/i);
    expect(fieldMonitorTemplate.className).toContain('min-h-[118px]');
    expect(fieldMonitorTemplate.className).toContain('h-auto');
    expect(fieldMonitorTemplate.className).toContain('self-start');
    expect(fieldMonitorTemplate.className).toContain('overflow-hidden');
    expect(templateGrid?.className).toContain('items-start');
    expect(fieldMonitorDescription.className).toContain('min-h-[54px]');
    expect(fieldMonitorTemplate.querySelector('[title="notion"]')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));

    expect(screen.getByRole('button', { name: 'Back to templates' })).toBeTruthy();
    expect(screen.getByRole('combobox', { name: 'YAML' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Copy code' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Use template' })).toBeTruthy();
    expect(screen.getByText(/name:/)).toBeTruthy();
    const detailHeading = screen.getByRole('heading', { name: /Structured extractor.*Template/i });
    const detailCard = detailHeading.closest('[data-slot="card"]') as HTMLElement | null;
    expect(detailHeading).toBeTruthy();
    expect(detailCard?.getAttribute('data-slot')).toBe('card');
    expect(detailCard?.className).toContain('h-full');
    expect(screen.getByText(/agent_toolset_20260401/)).toBeTruthy();
    const promptInput = screen.getByLabelText('Describe your agent') as HTMLTextAreaElement;
    const promptGroup = promptInput.closest('[data-slot="input-group"]') as HTMLElement | null;
    const promptSendButton = screen.getByRole('button', { name: /Send message/i });
    const promptAddon = promptSendButton.closest('[data-slot="input-group-addon"]') as HTMLElement | null;
    expect(promptInput.value).toBe('');
    expect(promptInput.className).toContain('overflow-y-auto');
    expect(promptInput.className).toContain('px-4');
    expect(promptInput.className).toContain('pt-4');
    expect(promptInput.className).toContain('pb-2');
    expect(promptInput.className).toContain('max-h-52');
    expect(promptGroup?.className).toContain('rounded-lg');
    expect(promptGroup?.className).toContain('border-input');
    expect(promptGroup?.className).toContain('shadow-xs');
    expect(promptGroup?.className).toContain('dark:bg-input/30');
    expect(promptGroup?.className).not.toContain('bg-popover');
    expect(promptGroup?.className).not.toContain('dark:bg-[rgb(56_56_53)]');
    expect(promptGroup?.className).toContain('ring-ring/50');
    expect(promptGroup?.className).toContain('gap-0');
    expect(promptGroup?.className).toContain('p-0');
    expect(promptAddon?.dataset.align).toBe('block-end');
    expect(promptSendButton.className).toContain('rounded-md');
    expect(promptSendButton.className).toContain('bg-primary');
    expect(promptSendButton.className).toContain('text-primary-foreground');
    expect(promptSendButton.className).not.toContain('rounded-full');
  });

  test('autosizes the quickstart composer for multiline input', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(<ManagedAgentsPage section="quickstart" />);

    const promptInput = screen.getByLabelText('Describe your agent') as HTMLTextAreaElement;
    Object.defineProperty(promptInput, 'scrollHeight', { configurable: true, value: 112 });

    fireEvent.change(promptInput, { target: { value: 'first line\nsecond line\nthird line' } });

    await waitFor(() => expect(promptInput.style.height).toBe('112px'));
    expect(promptInput.className).toContain('overflow-y-auto');
  });

  test('anchors the initial quickstart composer inside a full-height primary pane', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(<ManagedAgentsPage section="quickstart" />);

    const promptInput = screen.getByLabelText('Describe your agent') as HTMLTextAreaElement;
    const promptForm = promptInput.closest('form');
    const promptPane = promptForm?.parentElement;

    expect(promptForm?.className).toContain('absolute inset-x-0 bottom-0');
    expect(promptForm?.className).toContain('p-3');
    expect(promptForm?.className).toContain('pt-0');
    expect(promptPane?.className).toContain('h-full');
    expect(promptPane?.className).toContain('min-h-0');
    expect(promptPane?.className).toContain('flex-col');
  });

  test('uses the shared resizable handle for the quickstart work areas', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(<ManagedAgentsPage section="quickstart" />);

    const layout = screen.getByTestId('quickstart-layout');
    const divider = screen.getByRole('separator', { name: 'Resize quickstart panels' });

    expect(layout.dataset.inspectorWidth).toBe('720');
    expect(screen.getByTestId('quickstart-resizable-panels')).toBeTruthy();
    expect(divider.getAttribute('data-slot')).toBe('resizable-handle');
    expect(divider.getAttribute('aria-orientation')).toBe('vertical');
    expect(divider.querySelector('svg')).toBeTruthy();
  });

  test('clamps quickstart inspector widths against the container bounds', () => {
    expect(clampQuickstartInspectorPaneWidth(720, 1600)).toBe(720);
    expect(clampQuickstartInspectorPaneWidth(120, 1600)).toBe(440);
    expect(clampQuickstartInspectorPaneWidth(1400, 1600)).toBe(1240);
    expect(clampQuickstartInspectorPaneWidth(900, 600)).toBe(440);
  });

  test('lets the quickstart chat content fill the resized work area', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    mockAgentsApi([]);
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));

    expect(await screen.findByText('Agent created')).toBeTruthy();
    expect(screen.getByTestId('quickstart-layout').dataset.inspectorWidth).toBe('720');
    const chatStream = screen.getByTestId('quickstart-chat-stream');
    const chatContent = screen.getByTestId('quickstart-chat-content');
    expect(chatStream.getAttribute('data-slot')).toBe('message-scroller-viewport');
    expect(chatContent.getAttribute('data-slot')).toBe('message-scroller-content');
    expect(chatContent.className).toContain('w-full');
    expect(chatContent.className).toContain('px-4');
    expect(chatContent.className).not.toContain('w-[432px]');
    expect(chatContent.className).not.toContain('max-w-');
    expect(chatContent.className).not.toContain('px-6');

    const userMessage = within(chatContent)
      .getByText('Parses unstructured text into a typed JSON schema.')
      .closest('[data-slot="message"]') as HTMLElement | null;
    expect(userMessage).toBeTruthy();
    expect(userMessage?.getAttribute('data-align')).toBe('end');
    expect(within(userMessage as HTMLElement).getByText('You')).toBeTruthy();
    expect((userMessage as HTMLElement).querySelector('[data-slot="message-avatar"]')).toBeTruthy();

    const userBubble = within(userMessage as HTMLElement)
      .getByText('Parses unstructured text into a typed JSON schema.')
      .closest('[data-slot="bubble"]') as HTMLElement | null;
    expect(userBubble).toBeTruthy();
    expect(userBubble?.getAttribute('data-align')).toBe('end');
    expect(userBubble?.getAttribute('data-variant')).toBe('secondary');
    expect(userBubble?.className).toContain('w-fit');
    expect(userBubble?.className).toContain('max-w-[85%]');
    const userBubbleContent = within(userMessage as HTMLElement)
      .getByText('Parses unstructured text into a typed JSON schema.')
      .closest('[data-slot="bubble-content"]') as HTMLElement | null;
    expect(userBubbleContent?.className).toContain('rounded-3xl');
  });

  test('renders assistant quickstart replies with chat bubble chrome', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('build_agent_config', {
            name: 'Invoice tracker',
            description: 'Tracks inbound invoice emails and files weekly summaries.',
            model: 'claude-sonnet-4-6',
            system: 'Track invoices, summarize status, and ask before taking irreversible actions.',
            mcp_servers: [],
            tools: [
              {
                type: 'agent_toolset_20260401',
                default_config: { enabled: true, permission_policy: { type: 'always_allow' } },
                configs: [{ name: 'bash', enabled: false }]
              }
            ],
            skills: [],
            metadata: { template: 'blank-agent', source: 'description' }
          });
        }
        return quickstartTextStream('Ready to configure the environment.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.change(screen.getByLabelText('Describe your agent'), {
      target: { value: 'Build an invoice tracker that summarizes inbound invoice emails.' }
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByRole('button', { name: 'Create this agent' })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Create this agent' }));

    const assistantText = await screen.findByText('Ready to configure the environment.');
    const assistantMessage = assistantText.closest('[data-slot="message"]') as HTMLElement | null;
    expect(assistantMessage).toBeTruthy();
    expect(assistantMessage?.getAttribute('data-align')).toBe('start');
    expect(within(assistantMessage as HTMLElement).getByText('Quickstart')).toBeTruthy();
    expect((assistantMessage as HTMLElement).querySelector('[data-slot="message-avatar"]')).toBeTruthy();
    const assistantBubble = assistantText.closest('[data-slot="bubble"]') as HTMLElement | null;
    expect(assistantBubble).toBeTruthy();
    expect(assistantBubble?.getAttribute('data-variant')).toBe('outline');
    expect(assistantBubble?.className).toContain('max-w-[85%]');
  });

  test('renders assistant quickstart action cards with shared chat chrome', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('build_agent_config', {
            name: 'Invoice tracker',
            description: 'Tracks inbound invoice emails and files weekly summaries.',
            model: 'claude-sonnet-4-6',
            system: 'Track invoices, summarize status, and ask before taking irreversible actions.',
            mcp_servers: [],
            tools: [{ type: 'agent_toolset_20260401' }],
            skills: []
          });
        }
        return quickstartTextStream('Ready to configure the environment.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.change(screen.getByLabelText('Describe your agent'), {
      target: { value: 'Build an invoice tracker that summarizes inbound invoice emails.' }
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    const createButton = await screen.findByRole('button', { name: 'Create this agent' });
    const buildConfigBubble = createButton.closest('[data-slot="bubble"]') as HTMLElement | null;
    expect(buildConfigBubble).toBeTruthy();
    expect(buildConfigBubble?.getAttribute('data-variant')).toBe('ghost');
    expect(buildConfigBubble?.className).toContain('w-full');
    expect(buildConfigBubble?.className).toContain('max-w-full');
    const buildConfigMessage = createButton.closest('[data-slot="message"]') as HTMLElement | null;
    expect(buildConfigMessage).toBeTruthy();
    expect(buildConfigMessage?.getAttribute('data-align')).toBe('start');
    expect(within(buildConfigMessage as HTMLElement).getByText('Quickstart')).toBeTruthy();
    expect((buildConfigMessage as HTMLElement).querySelector('[data-slot="message-avatar"]')).toBeTruthy();

    fireEvent.click(createButton);

    const createResultText = await screen.findByText('Your agent is created. Here’s the call that made it:');
    const createResultBubble = createResultText.closest('[data-slot="bubble"]') as HTMLElement | null;
    expect(createResultBubble).toBeTruthy();
    expect(createResultBubble?.getAttribute('data-variant')).toBe('ghost');
    expect(createResultBubble?.className).toContain('w-full');
    expect(createResultBubble?.className).toContain('max-w-full');
    const createResultMessage = createResultText.closest('[data-slot="message"]') as HTMLElement | null;
    expect(createResultMessage).toBeTruthy();
    expect(createResultMessage?.getAttribute('data-align')).toBe('start');
    expect(within(createResultMessage as HTMLElement).getByText('Quickstart')).toBeTruthy();
    expect((createResultMessage as HTMLElement).querySelector('[data-slot="message-avatar"]')).toBeTruthy();
  });

  test('renders the official quickstart config for every advanced template', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(<ManagedAgentsPage section="quickstart" />);

    const pageText = () => document.body.textContent?.replace(/\u00a0/g, ' ') ?? '';
    const expectations = [
      {
        button: /Contract tracker/i,
        yaml: [
          'model: claude-opus-4-8',
          'https://mcp.box.com',
          'https://mcp.asana.com/sse',
          'urgent ≤30 days / medium 31–90 days',
          'template: contract-clause-extraction'
        ]
      },
      {
        button: /Sprint retro facilitator/i,
        yaml: [
          'https://mcp.linear.app/mcp',
          'https://mcp.slack.com/mcp',
          'nice" / 🎉 reactions',
          'skill_id: docx',
          'template: sprint-retro-facilitator'
        ]
      },
      {
        button: /Support-to-eng escalator/i,
        yaml: [
          'https://mcp.intercom.com/mcp',
          'https://mcp.atlassian.com/v1/mcp',
          "If you can't repro, say so explicitly",
          'template: support-to-eng-escalator'
        ]
      },
      {
        button: /Data analyst/i,
        yaml: [
          'https://mcp.amplitude.com/mcp',
          'correlation-vs-causation',
          'A clear bar chart usually beats a dense heatmap.',
          'template: data-analyst'
        ]
      }
    ];

    for (const expectation of expectations) {
      fireEvent.click(screen.getByRole('button', { name: expectation.button }));
      for (const yamlLine of expectation.yaml) {
        expect(pageText()).toContain(yamlLine);
      }
      fireEvent.click(screen.getByRole('button', { name: 'Back to templates' }));
    }
  });

  test('switches template code format and creates an agent from a template through the real quickstart flow', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    const api = mockAgentsApi([]);
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    await selectManagedComboboxOption(document.body, 'YAML', 'JSON');
    await waitFor(() => expect(screen.getByRole('combobox', { name: 'JSON' })).toBeTruthy());
    expectPageTextToContain('"metadata":');
    const templateJsonBlock = codeBlockContaining('"metadata":');
    expect(templateJsonBlock?.querySelector('code.language-json')).toBeTruthy();
    expect(templateJsonBlock?.querySelector('.hljs-attr')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));

    await waitFor(() => expect(api.requests.some((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST')).toBe(true));
    expect(api.requests.some((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')).toBe(false);
    expect(screen.getByText('Agent created')).toBeTruthy();
    expect(screen.getByText('Your agent is created. Here’s the call that made it:')).toBeTruthy();
    expect(screen.getByText('/v1/agents')).toBeTruthy();
    const createCodeBlock = codeBlockContaining('ant beta:agents create');
    expect(createCodeBlock?.className).toContain('whitespace-pre-wrap');
    expect(createCodeBlock?.className).toContain('break-words');
    expect(createCodeBlock?.className).toContain('overflow-y-auto');
    expect(createCodeBlock?.className).toContain('overflow-x-hidden');
    expect(createCodeBlock?.querySelector('code.language-bash')).toBeTruthy();
    expect(createCodeBlock?.querySelector('.hljs-attr')).toBeTruthy();
    expect(createCodeBlock?.textContent).not.toContain('mcp_servers:');
    expect(createCodeBlock?.textContent).not.toContain('skills:');
    expect(screen.getByRole('button', { name: /Next: Configure environment/i })).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Next: Configure environment/i }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')).toBe(true)
    );
    expect(screen.getByRole('button', { name: 'Test run' }).hasAttribute('disabled')).toBe(true);
    expect(await screen.findByText("Let's configure the environment.")).toBeTruthy();
    const panelTabs = screen.getByRole('tablist', { name: 'Agent panel views' });
    const agentPanel = panelTabs.closest('aside') as HTMLElement | null;
    expect(within(panelTabs).getByRole('tab', { name: 'Config', selected: true })).toBeTruthy();
    expect(agentPanel?.className).toContain('h-full');
    const previewTab = within(panelTabs).getByRole('tab', { name: 'Preview', selected: false });

    fireEvent.click(previewTab);
    expect(within(panelTabs).getByRole('tab', { name: 'Preview', selected: true })).toBeTruthy();
    expect(screen.getByRole('button', { name: /Select an environment/i })).toBeTruthy();
    const noEnvironmentCard = screen.getByText('No environments yet').closest('[data-slot="card"]') as HTMLElement | null;
    expect(noEnvironmentCard?.getAttribute('data-slot')).toBe('card');
    expect(within(noEnvironmentCard as HTMLElement).getByRole('button', { name: /^Configure environment$/i }).dataset.slot).toBe('button');

    const createRequest = api.requests.find((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST');
    expect(createRequest?.headers['x-workspace-id']).toBe('default');
    expect(createRequest?.body?.name).toBe('Structured extractor');
    expect((createRequest?.body?.metadata as Record<string, string>).template).toBe('structured-extractor');

    const proxyRequest = api.requests.find((request) => request.url === '/api/organizations/org_test/proxy/v1/messages');
    expect(Object.keys(proxyRequest?.body ?? {})).toEqual(['messages', 'system', 'model', 'max_tokens', 'tools', 'tool_choice', 'stream']);
    expect(proxyRequest?.body?.model).toBe('claude-sonnet-4-6');
    expect(proxyRequest?.body?.max_tokens).toBe(4096);
    expect(proxyRequest?.body?.stream).toBe(true);
  });

  test('starts the environment step from the preview empty-state action', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    const api = mockAgentsApi([]);
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));

    expect(await screen.findByText('Agent created')).toBeTruthy();
    const panelTabs = screen.getByRole('tablist', { name: 'Agent panel views' });
    fireEvent.click(within(panelTabs).getByRole('tab', { name: 'Preview', selected: false }));
    const previewPanel = screen.getByRole('tabpanel');
    fireEvent.click(within(previewPanel).getByRole('button', { name: /^Configure environment$/i }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')).toBe(true)
    );
    expect(await screen.findByText("Let's configure the environment.")).toBeTruthy();
  });

  test('routes the initial freeform prompt through official build_agent_config before creating the agent', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('build_agent_config', {
            name: 'Invoice tracker',
            description: 'Tracks inbound invoice emails and files weekly summaries.',
            model: 'claude-sonnet-4-6',
            system: 'Track invoices, summarize status, and ask before taking irreversible actions.',
            mcp_servers: [],
            tools: [
              {
                type: 'agent_toolset_20260401',
                default_config: { enabled: true, permission_policy: { type: 'always_allow' } },
                configs: [{ name: 'bash', enabled: false }]
              }
            ],
            skills: [],
            metadata: { template: 'blank-agent', source: 'description' }
          });
        }
        return quickstartTextStream('Ready to configure the environment.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.change(screen.getByLabelText('Describe your agent'), {
      target: { value: 'Build an invoice tracker that summarizes inbound invoice emails.' }
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')).toBe(true)
    );
    const firstProxyRequest = api.requests.find((request) => request.url === '/api/organizations/org_test/proxy/v1/messages');
    const firstProxyContent = firstProxyRequest?.body?.messages?.[0]?.content as string;
    expect(firstProxyContent).toContain("I'm building an agent. Here's my description:");
    expect(firstProxyContent).toContain('"Build an invoice tracker that summarizes inbound invoice emails."');
    expect(firstProxyContent).toContain('"description": "A blank starting point with the core toolset."');
    expect(firstProxyContent).not.toContain('"metadata"');
    expect(api.requests.some((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST')).toBe(false);
    expect(screen.getByRole('button', { name: 'Create this agent' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Keep refining' })).toBeTruthy();
    expect(screen.queryByText('/v1/agents')).toBeNull();
    expectPageTextToContain('Invoice tracker');

    fireEvent.click(screen.getByRole('button', { name: 'Create this agent' }));

    await waitFor(() => expect(api.requests.some((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST')).toBe(true));
    const createRequest = api.requests.find((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST');
    expect(createRequest?.body?.name).toBe('Invoice tracker');
    expect(createRequest?.body?.system).toBe('Track invoices, summarize status, and ask before taking irreversible actions.');
    expect(createRequest?.body?.tools).toEqual([
      {
        type: 'agent_toolset_20260401',
        default_config: { enabled: true, permission_policy: { type: 'always_allow' } },
        configs: [{ name: 'bash', enabled: false }]
      }
    ]);
    expect((createRequest?.body?.metadata as Record<string, string>).source).toBe('description');
    expect(await screen.findByText('Agent created')).toBeTruthy();
    expect(await screen.findByText('/v1/agents')).toBeTruthy();
    expect(screen.getByRole('button', { name: /Next: Configure environment/i })).toBeTruthy();

    await waitFor(() =>
      expect(api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').length).toBeGreaterThanOrEqual(2)
    );
    const latestProxyRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    expect(Object.keys(latestProxyRequest?.body ?? {})).toEqual(['messages', 'system', 'model', 'max_tokens', 'tools', 'tool_choice', 'stream']);
    expect(JSON.stringify(latestProxyRequest?.body?.messages)).toContain('Agent created.');
  });

  test('routes freeform build-config chat replies through the official tool result wording', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('build_agent_config', {
            name: 'Invoice tracker',
            description: 'Tracks inbound invoice emails.',
            model: 'claude-sonnet-4-6',
            system: 'Track invoices.',
            mcp_servers: [],
            tools: [{ type: 'agent_toolset_20260401' }],
            skills: []
          });
        }
        return quickstartTextStream('I updated the config.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.change(screen.getByLabelText('Describe your agent'), {
      target: { value: 'Build an invoice tracker.' }
    });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByRole('button', { name: 'Create this agent' })).toBeTruthy();
    const reply = screen.getByLabelText('Reply…') as HTMLTextAreaElement;
    fireEvent.change(reply, { target: { value: 'Call it Invoice Copilot.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    await waitFor(() =>
      expect(api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').length).toBeGreaterThanOrEqual(2)
    );
    const latestProxyRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    const latestContent = latestProxyRequest?.body?.messages?.at(-1)?.content as Array<Record<string, string>>;
    expect(latestContent[0]?.content).toBe('User sent a message instead: "Call it Invoice Copilot."');
    expect(api.requests.some((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST')).toBe(false);
  });

  test('sends chat replies and renders official quickstart question tool calls', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('ask_user_questions', {
            questions: [
              {
                question: 'Does this agent need to access external services?',
                header: 'Networking',
                multiSelect: false,
                options: [
                  { label: 'Limited networking', description: 'Run without open internet access.' },
                  { label: 'Unrestricted networking', description: 'Allow external API calls.' }
                ]
              }
            ]
          });
        }
        return quickstartTextStream("Thanks, I'll continue from there.");
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText('Does this agent need to access external services?')).toBeTruthy();
    const pinnedInteraction = screen.getByTestId('quickstart-pinned-interaction');
    expect(within(pinnedInteraction).getByText('Does this agent need to access external services?')).toBeTruthy();
    expect(within(pinnedInteraction).getByTestId('quickstart-question-card').className).toContain('border-border');
    const networkingChoices = within(pinnedInteraction).getByRole('radiogroup', {
      name: 'Does this agent need to access external services?'
    });
    expect(within(networkingChoices).getByRole('radio', { name: /Limited networking/i })).toBeTruthy();
    expect(within(networkingChoices).getByRole('radio', { name: /Unrestricted networking/i })).toBeTruthy();
    expect(within(screen.getByTestId('quickstart-chat-stream')).queryByText('Does this agent need to access external services?')).toBeNull();
    fireEvent.click(screen.getByRole('radio', { name: /Limited networking/i }));

    expect(await screen.findByText("Thanks, I'll continue from there.")).toBeTruthy();
    await waitFor(() => expect(screen.queryByTestId('quickstart-pinned-interaction')).toBeNull());
    expect(within(screen.getByTestId('quickstart-chat-stream')).getByText('Limited networking')).toBeTruthy();
    expect(screen.queryByText(/\{"answers"/)).toBeNull();

    const reply = screen.getByLabelText('Reply…') as HTMLTextAreaElement;
    fireEvent.change(reply, { target: { value: 'Use limited networking.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    await waitFor(() => expect(api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').length).toBeGreaterThanOrEqual(3));
    const latestProxyRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    expect(JSON.stringify(latestProxyRequest?.body?.messages)).toContain('Use limited networking.');
  });

  test('supports multi-select, Other, Skip, and submitted question display', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('ask_user_questions', {
            questions: [
              {
                question: 'Which services should this agent use?',
                header: 'Services',
                multiSelect: true,
                options: [
                  { label: 'Slack', description: 'Post updates to channels.' },
                  { label: 'Notion', description: 'Read project pages.' }
                ]
              }
            ]
          });
        }
        if (proxyCalls === 2) {
          return quickstartToolStream('ask_user_questions', {
            questions: [
              {
                question: 'Can we skip this decision?',
                header: 'Skip',
                multiSelect: false,
                options: [{ label: 'Decide later', description: 'Leave this for the next step.' }]
              }
            ]
          });
        }
        return quickstartTextStream('Question flow complete.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText('Which services should this agent use?')).toBeTruthy();
    const servicesGroup = screen.getByRole('group', { name: 'Which services should this agent use?' });
    fireEvent.click(within(servicesGroup).getByRole('checkbox', { name: /Slack/i }));
    fireEvent.click(within(servicesGroup).getByRole('checkbox', { name: /Notion/i }));
    fireEvent.change(screen.getByPlaceholderText('Something else'), { target: { value: 'Linear' } });
    fireEvent.click(screen.getByRole('button', { name: 'Submit answer' }));

    expect(await screen.findByText('Slack, Notion, Linear')).toBeTruthy();
    expect(await screen.findByText('Can we skip this decision?')).toBeTruthy();
    expect(screen.getByRole('radiogroup', { name: 'Can we skip this decision?' })).toBeTruthy();
    const skipButton = screen.getAllByRole('button', { name: 'Skip' }).at(-1)!;
    expect(skipButton.className).toContain('ml-auto');
    fireEvent.click(skipButton);

    await waitFor(() => expect(screen.getAllByText('Skipped.').length).toBeGreaterThan(0));
    expect(await screen.findByText('Question flow complete.')).toBeTruthy();
    const questionResultRequest = api.requests
      .filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')
      .find((request) => JSON.stringify(request.body?.messages).includes('Slack'));
    const questionResultMessages = JSON.stringify(questionResultRequest?.body?.messages);
    expect(questionResultMessages).toContain('Slack');
    expect(questionResultMessages).toContain('Notion');
    expect(questionResultMessages).toContain('Linear');
  });

  test('removes model thinking tags from visible quickstart text and continuation messages', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartTextAndToolStream(
            '<think>I should not be visible or persisted.</think>An environment is a reusable compute template.',
            'list_environments',
            {}
          );
        }
        return quickstartTextStream('Environment options are ready.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Field monitor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText('An environment is a reusable compute template.')).toBeTruthy();
    expect(screen.queryByText(/I should not be visible/)).toBeNull();
    await waitFor(() =>
      expect(api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').length).toBeGreaterThanOrEqual(2)
    );

    const continuationRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    const continuationMessages = JSON.stringify(continuationRequest?.body?.messages);
    expect(continuationMessages).toContain('An environment is a reusable compute template.');
    expect(continuationMessages).not.toContain('<think>');
    expect(continuationMessages).not.toContain('I should not be visible');
  });

  test('renders official quickstart status lines and preserves visible search-result text', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    const officialSearchResult =
      "Search results for query: An environment is a reusable template for the container where your agent's tools execute — things like networking policy and package access. Let me check what environments you already have.";
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartTextAndToolStream('We need a vault for Notion access.', 'vault_sharing_notice', {});
        }
        if (proxyCalls === 2) {
          return quickstartTextAndToolStream(officialSearchResult, 'list_vaults', {});
        }
        return quickstartTextStream('Vault options are ready.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Field monitor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    await waitFor(() =>
      expect(screen.getAllByRole('alert').some((node) => node.textContent?.includes('Vaults are shared across this workspace'))).toBe(true)
    );
    const vaultSharingNotice = screen.getAllByRole('alert').find((node) => node.textContent?.includes('Vaults are shared across this workspace'));
    expect(vaultSharingNotice?.getAttribute('role')).toBe('alert');
    expect(screen.queryByText('A vault')).toBeNull();
    expect(screen.getByRole('link', { name: /here/ }).getAttribute('href')).toBe('/docs/en/managed-agents/vaults');
    expect(await screen.findByText(officialSearchResult)).toBeTruthy();
    expect(await screen.findByText('Vaults loaded')).toBeTruthy();
    expect(await screen.findByText('Vault options are ready.')).toBeTruthy();
    expect(screen.queryByText('Vaults loaded.')).toBeNull();
    expect(screen.queryByText('List vaults')).toBeNull();
    expect(screen.getByLabelText('Reply…')).toBeTruthy();
    expect(api.requests.some((request) => request.url.startsWith('/v1/vaults?') && request.method === 'GET')).toBe(true);
  });

  test('handles quickstart web_search tool calls without inventing a local API side effect', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    const environmentSearchQuery =
      "An environment is a reusable template for the container where your agent's tools execute — things like networking policy and package access. Let me check what environments you already have.";
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartTextAndToolStream('Search results for query:', 'web_search', {
            query: environmentSearchQuery
          });
        }
        return quickstartTextStream('Search complete.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText(`Search results for query: ${environmentSearchQuery}`)).toBeTruthy();
    expect(await screen.findByText('Search web')).toBeTruthy();
    expect(screen.getByText('web_search')).toBeTruthy();
    expect(await screen.findByText('web_search is handled by the upstream model.')).toBeTruthy();
    expect(await screen.findByText('Search complete.')).toBeTruthy();
    const continuationRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    expect(JSON.stringify(continuationRequest?.body?.messages)).toContain(`Search results for query: ${environmentSearchQuery}`);
    expect(JSON.stringify(continuationRequest?.body?.messages)).toContain('web_search is handled by the upstream model.');
    expect(api.requests.some((request) => request.url.includes('/v1/search'))).toBe(false);
  });

  test('renders server-side quickstart web search queries without an empty prefix row', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    const environmentSearchQuery =
      "An environment is a reusable template for the container where your agent's tools execute — things like networking policy and package access. Let me check what environments you already have.";
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartTextServerToolAndToolStream(
            'Search results for query:',
            '',
            'list_environments',
            {}
          );
        }
        return quickstartTextStream('Environment options are ready.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText(`Search results for query: ${environmentSearchQuery}`)).toBeTruthy();
    expect(screen.queryByText('Search results for query:')).toBeNull();
    expect(await screen.findByText('Environments loaded')).toBeTruthy();
    expect(await screen.findByText('Environment options are ready.')).toBeTruthy();
    expect(screen.queryByText('Search web')).toBeNull();
    const continuationRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    expect(JSON.stringify(continuationRequest?.body?.messages)).toContain(environmentSearchQuery);
    expect(JSON.stringify(continuationRequest?.body?.messages)).not.toContain('server_tool_use');
    expect(api.requests.some((request) => request.url.includes('/v1/search'))).toBe(false);
  });

  test('creates quickstart environments with the official package defaults in the POST body and CLI', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('create_environment', {
            name: 'structured-extractor-env-5',
            config: {
              type: 'cloud',
              networking: { type: 'unrestricted' }
            }
          });
        }
        return quickstartTextStream('Environment is ready.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    await waitFor(() => expect(api.requests.some((request) => request.url === '/v1/environments?beta=true' && request.method === 'POST')).toBe(true));
    await waitFor(() => expect(codeBlockContaining('ant beta:environments create')).toBeTruthy());
    const createEnvironmentRequest = api.requests.find(
      (request) => request.url === '/v1/environments?beta=true' && request.method === 'POST'
    );
    expect(Object.keys(createEnvironmentRequest?.body ?? {})).toEqual(['name', 'metadata', 'scope', 'config']);
    expect(createEnvironmentRequest?.body?.name).toBe('structured-extractor-env-5');
    const config = createEnvironmentRequest?.body?.config as Record<string, unknown>;
    expect(Object.keys(config)).toEqual(['type', 'packages', 'networking']);
    expect(config.packages).toEqual({ pip: [], npm: [], apt: [], cargo: [], gem: [], go: [] });
    expect(config.networking).toEqual({ type: 'unrestricted' });

    const environmentCliBlock = codeBlockContaining('ant beta:environments create');
    expect(environmentCliBlock?.textContent).toContain('metadata: {}');
    expect(environmentCliBlock?.textContent).toContain('packages:');
    expect(environmentCliBlock?.textContent).toContain('pip: []');
    expect(environmentCliBlock?.textContent).toContain('npm: []');
    expect(environmentCliBlock?.textContent).toContain('apt: []');
    expect(environmentCliBlock?.textContent).toContain('cargo: []');
    expect(environmentCliBlock?.textContent).toContain('gem: []');
    expect(environmentCliBlock?.textContent).toContain('go: []');
    expect(environmentCliBlock?.textContent).not.toContain('description: Created from the managed agent quickstart.');

    const environmentToast = screen.getByText(/Environment created - env_created123456/i).closest('[data-sonner-toast]');
    expect(environmentToast).toBeTruthy();
    expect(within(environmentToast as HTMLElement).getByRole('button', { name: 'Close' })).toBeTruthy();
  });

  test('routes freeform chat replies through pending quickstart tool results', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('await_test_run', { until: 'first_message' });
        }
        return quickstartTextStream('Thanks, I captured that reply.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText('Waiting for first message...')).toBeTruthy();
    const reply = screen.getByLabelText('Reply…') as HTMLTextAreaElement;
    expect(reply.className).toContain('focus-visible:ring-0');
    expect(reply.parentElement?.dataset.slot).toBe('input-group');
    fireEvent.change(reply, { target: { value: 'Keep the current setup and continue.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send message' }));

    expect(await screen.findByText('Thanks, I captured that reply.')).toBeTruthy();
    const latestProxyRequest = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1);
    const messages = latestProxyRequest?.body?.messages as Array<{ role: string; content: unknown }>;
    const latestMessage = messages.at(-1);
    expect(latestMessage?.role).toBe('user');
    expect(Array.isArray(latestMessage?.content)).toBe(true);
    expect(JSON.stringify(latestMessage?.content)).toContain('"type":"tool_result"');
    expect(JSON.stringify(latestMessage?.content)).toContain('User replied in chat: Keep the current setup and continue.');
  });

  test('creates a real test session and renders session events in the preview panel', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('create_environment', { reuse_environment_id: 'env_option123456' });
        }
        if (proxyCalls === 2) {
          return quickstartToolStream('select_vault', { vault_ids: ['vault_option123456'] });
        }
        if (proxyCalls === 3) {
          return quickstartToolStream('flag_schedule_intent', { wants_schedule: false });
        }
        if (proxyCalls === 4) {
          return quickstartToolStream('create_vault_credential', {
            vault_id: 'vault_option123456',
            mcp_server_name: 'notion',
            reason: 'Notion access is needed for this agent.'
          });
        }
        if (proxyCalls === 5) {
          return quickstartToolStream('agent_ready', { suggested_first_message: 'Say hello from the test run.' });
        }
        if (proxyCalls === 6) {
          return quickstartToolStream('await_test_run', { until: 'first_message' });
        }
        if (proxyCalls === 7) {
          return quickstartToolStream('show_integration_exits', {
            agent_id: 'agent_model_wrong123456',
            environment_id: 'env_option123456'
          });
        }
        return quickstartTextStream('Integration selected.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));

    expect(await screen.findByText('Environment selected')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Next: Start session/i }));

    expect(await screen.findByText('Selected:')).toBeTruthy();
    const sessionStepProxyRequest = api.requests
      .filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')
      .at(-1);
    const sessionStepMessages = sessionStepProxyRequest?.body?.messages as Array<{ role: string; content: unknown }>;
    const sessionStepUserMessage = sessionStepMessages.at(-1);
    expect(sessionStepUserMessage?.role).toBe('user');
    expect(Array.isArray(sessionStepUserMessage?.content)).toBe(true);
    const sessionStepContent = sessionStepUserMessage?.content as Array<Record<string, unknown>>;
    expect(sessionStepContent[0]?.type).toBe('tool_result');
    expect((sessionStepContent[1]?.text as string)).toContain('[Current quickstart step: "session". Follow this step');
    expect((sessionStepContent[1]?.text as string)).toContain("Here's the current config:");
    expect(screen.getByText('vault_option123456')).toBeTruthy();
    const confirmVaultButton = screen.getByRole('button', { name: 'Confirm' });
    expect(confirmVaultButton.hasAttribute('disabled')).toBe(true);
    const vaultAckCheckbox = screen.getByRole('checkbox', { name: /I own or am authorized to use this vault/ });
    const vaultAckLabel = screen.getByText(/I own or am authorized to use this vault/).closest('[data-slot="label"]');
    expect(vaultAckLabel).toBeTruthy();
    fireEvent.click(vaultAckLabel!);
    expect(vaultAckCheckbox.getAttribute('aria-checked')).toBe('true');
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm' }).hasAttribute('disabled')).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    await waitFor(() => expect(screen.queryByText('Selected:')).toBeNull());
    expect(screen.queryByText('Deployment schedule intent cleared.')).toBeNull();
    expect(await screen.findByText('Authorization required to use this MCP')).toBeTruthy();
    expect(screen.getByText('Notion access is needed for this agent.')).toBeTruthy();
    const accessTokenDisclosure = screen.getByRole('button', { name: /Access token/ });
    const oauthDisclosure = screen.getByRole('button', { name: /OAuth client credentials/ });
    expect(accessTokenDisclosure).toBeTruthy();
    expect(accessTokenDisclosure.dataset.slot).toBe('collapsible-trigger');
    expect(oauthDisclosure).toBeTruthy();
    expect(oauthDisclosure.dataset.slot).toBe('collapsible-trigger');
    const credentialSharingNotice = screen.getAllByRole('alert').find((node) => node.textContent?.includes('This credential will be shared across this workspace'));
    expect(credentialSharingNotice).toBeTruthy();
    expect(credentialSharingNotice?.getAttribute('role')).toBe('alert');
    const credentialAckCheckbox = screen.getByRole('checkbox', { name: /I acknowledge this credential is shared/ });
    const credentialAckLabel = screen.getByText(/I acknowledge this credential is shared/).closest('[data-slot="label"]');
    expect(credentialAckLabel).toBeTruthy();
    fireEvent.click(credentialAckLabel!);
    expect(credentialAckCheckbox.getAttribute('aria-checked')).toBe('true');
    expect(screen.getByRole('button', { name: 'Authorize Notion credential' }).hasAttribute('disabled')).toBe(true);
    fireEvent.click(accessTokenDisclosure);
    const accessTokenInput = screen.getByPlaceholderText('OAuth access token');
    expect(accessTokenInput).toBeTruthy();
    expect(accessTokenInput.closest('[data-slot="collapsible-content"]')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Skip for now' }));
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Test run' }).length).toBeGreaterThan(1));
    fireEvent.click(screen.getAllByRole('button', { name: 'Test run' }).at(-1)!);

    expect(await screen.findByText('Session created')).toBeTruthy();
    expect(screen.getByText('/v1/sessions')).toBeTruthy();
    const environmentLink = await screen.findByRole('link', { name: 'Option environment' });
    expect(environmentLink.dataset.slot).toBe('button');
    const viewSessionLink = await screen.findByRole('link', { name: 'View session' });
    expect(viewSessionLink.dataset.slot).toBe('button');
    expect(await screen.findByText('Waiting for first message...')).toBeTruthy();

    const previewMessage = screen.getByPlaceholderText('Send a message to the agent') as HTMLTextAreaElement;
    const previewComposer = previewMessage.parentElement as HTMLElement | null;
    const previewSendButton = screen.getByRole('button', { name: 'Send' });
    expect(previewMessage.className).toContain('focus-visible:ring-0');
    expect(previewMessage.parentElement?.dataset.slot).toBe('input-group');
    expect(previewComposer?.className).toContain('rounded-lg');
    expect(previewComposer?.className).toContain('border-input');
    expect(previewComposer?.className).toContain('shadow-xs');
    expect(previewComposer?.className).toContain('dark:bg-input/30');
    expect(previewComposer?.className).not.toContain('bg-popover');
    expect(previewComposer?.className).not.toContain('dark:bg-[rgb(56_56_53)]');
    expect(previewComposer?.className).toContain('ring-ring/50');
    expect(previewComposer?.className).toContain('gap-2');
    expect(previewSendButton.className).toContain('rounded-md');
    expect(previewSendButton.className).toContain('bg-primary');
    expect(previewSendButton.className).toContain('text-primary-foreground');
    expect(previewSendButton.className).not.toContain('rounded-full');
    expect(previewMessage.value).toBe('Say hello from the test run.');
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/sessions/sesn_created123456/events?beta=true' && request.method === 'POST')).toBe(true)
    );
    await waitFor(() => {
      const postIndex = api.requests.findIndex((request) => request.url === '/v1/sessions/sesn_created123456/events?beta=true' && request.method === 'POST');
      expect(postIndex).toBeGreaterThanOrEqual(0);
      expect(
        api.requests
          .slice(postIndex + 1)
          .some((request) => request.url.startsWith('/v1/sessions/sesn_created123456/events?beta=true') && request.method === 'GET')
      ).toBe(true);
    }, { timeout: 3000 });
    expect(screen.getByRole('tab', { name: 'Transcript' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.queryByRole('button', { name: /^Env / })).toBeNull();
    expect(await screen.findByRole('button', { name: /^System System message/ })).toBeTruthy();
    expect(await screen.findByRole('button', { name: /^Idle Session idle/ })).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: 'Debug' }));
    expect(screen.getByRole('tab', { name: 'Debug' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.queryByRole('button', { name: /^Env / })).toBeNull();
    expect(await screen.findByRole('button', { name: /^System System message/ })).toBeTruthy();
    expect(await screen.findByRole('button', { name: /^Idle Session idle/ })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /^User / }).length).toBe(2);
    fireEvent.click(screen.getByRole('button', { name: 'All events' }));
    expect(await screen.findByRole('menuitemcheckbox', { name: /agent\.message/ })).toBeTruthy();
    expect(screen.queryByRole('menuitemcheckbox', { name: /env_manager_log/ })).toBeNull();
    expect(screen.getByRole('menuitemcheckbox', { name: /session\.status_idle/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /system\.message/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /user\.message/ })).toBeTruthy();
    fireEvent.pointerDown(document.body);
    fireEvent.click(screen.getAllByRole('button', { name: /^User / })[0]);
    let detail = await screen.findByTestId('session-trace-detail');
    expect(within(detail).getByText('Message')).toBeTruthy();
    expect(detail.textContent).toContain('"type": "user.message"');
    const debugCode = within(detail).getByTestId('session-trace-code-block');
    expect(debugCode.className).toContain('overflow-visible');
    expect(debugCode.className).not.toContain('overflow-y-auto');
    expect(debugCode.className).not.toContain('max-h-');
    fireEvent.click(screen.getByRole('button', { name: 'Close detail panel' }));
    fireEvent.click(screen.getByRole('tab', { name: 'Transcript' }));
    expect(screen.getByRole('tab', { name: 'Transcript' }).getAttribute('aria-selected')).toBe('true');
    fireEvent.click(screen.getByRole('button', { name: 'All events' }));
    expect(await screen.findByRole('menuitemcheckbox', { name: /All events/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /User/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /Agent/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /Tool/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /Error/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /Status/ })).toBeTruthy();
    expect(screen.getByRole('menuitemcheckbox', { name: /System/ })).toBeTruthy();
    expect(screen.queryByRole('menuitemcheckbox', { name: /session\.status_idle/ })).toBeNull();
    fireEvent.pointerDown(document.body);
    expect(screen.queryByRole('button', { name: /^Env / })).toBeNull();
    expect(await screen.findByRole('button', { name: /^System System message/ })).toBeTruthy();
    expect(await screen.findByRole('button', { name: /^Idle Session idle/ })).toBeTruthy();
    expect(screen.getAllByRole('button', { name: /^User / }).length).toBe(2);
    const transcriptRows = screen
      .getAllByRole('button')
      .map((button) => button.getAttribute('aria-label') || '')
      .filter((label) => /^(Env|User|System|Agent|Thinking) /.test(label));
    expect(transcriptRows.map((label) => label.split(' ')[0])).toEqual(['User', 'User', 'System', 'Thinking', 'Agent']);
    expect(screen.getByRole('button', { name: /^Thinking Thinking\.\.\./ })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /^Agent ```json/ }));
    detail = await screen.findByTestId('session-trace-detail');
    expect(within(detail).getByText('Message')).toBeTruthy();
    expect(await within(detail).findByText('Content')).toBeTruthy();
    const detailCode = within(detail).getByTestId('session-trace-code-block');
    expect(detailCode.textContent).toContain('"order_id": "ORD-7742"');
    expect(detailCode.textContent).toContain('"total_amount": 142.50');
    expect(detailCode.textContent).not.toContain('```json');
    fireEvent.click(screen.getByRole('button', { name: 'Close detail panel' }));
    expect(await screen.findByText('Sample code')).toBeTruthy();
    expect(screen.getAllByText(/ant beta:sessions create/).length).toBeGreaterThan(1);
    const exitQuickstartButton = screen.getByRole('button', { name: /Exit quickstart/ });
    expect(exitQuickstartButton.dataset.slot).toBe('button');
    const integrationBubble = exitQuickstartButton.closest('[data-slot="bubble"]') as HTMLElement | null;
    expect(integrationBubble).toBeTruthy();
    expect(integrationBubble?.getAttribute('data-variant')).toBe('ghost');
    expect(integrationBubble?.className).toContain('w-full');
    expect(integrationBubble?.className).toContain('max-w-full');
    const integrationMessage = exitQuickstartButton.closest('[data-slot="message"]') as HTMLElement | null;
    expect(integrationMessage).toBeTruthy();
    expect(integrationMessage?.getAttribute('data-align')).toBe('start');
    expect(within(integrationMessage as HTMLElement).getByText('Quickstart')).toBeTruthy();
    expect((integrationMessage as HTMLElement).querySelector('[data-slot="message-avatar"]')).toBeTruthy();
    expect(screen.getAllByText(/agent_created123456/).length).toBeGreaterThan(0);
    expect(screen.queryAllByText(/agent_model_wrong123456/)).toHaveLength(0);
    fireEvent.click(screen.getByRole('tab', { name: 'Python' }));
    expectPageTextToContain('from anthropic import Anthropic');
    expectPageTextToContain('client.beta.sessions.events.stream');
    const pythonCodeBlock = codeBlockContaining('from anthropic import Anthropic');
    expect(pythonCodeBlock?.querySelector('code.language-python')).toBeTruthy();
    expect(pythonCodeBlock?.querySelector('.hljs-keyword')).toBeTruthy();
    expect(await screen.findByRole('button', { name: 'Scaffold in Claude Code' })).toBeTruthy();
    const proxyCallCountBeforeCopy = api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').length;
    fireEvent.click(screen.getByRole('button', { name: 'Scaffold in Claude Code' }));
    expect(await screen.findByRole('button', { name: 'Prompt copied' })).toBeTruthy();
    expect(api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').length).toBe(
      proxyCallCountBeforeCopy
    );

    expect(api.requests.some((request) => request.url === '/v1/sessions?beta=true' && request.method === 'POST')).toBe(true);
    expect(api.requests.find((request) => request.url === '/v1/sessions?beta=true' && request.method === 'POST')?.body?.vault_ids).toEqual([
      'vault_option123456'
    ]);
    expect(api.requests.some((request) => request.url === '/v1/sessions/sesn_created123456/events/stream?beta=true')).toBe(true);
    expect(JSON.stringify(api.requests.filter((request) => request.url === '/api/organizations/org_test/proxy/v1/messages').at(-1)?.body?.messages)).not.toContain(
      'User chose scaffold.'
    );
  });

  test('enables header-created idle test sessions to send messages without an await_test_run tool call', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('create_environment', { reuse_environment_id: 'env_option123456' });
        }
        if (proxyCalls === 2) {
          return quickstartToolStream('agent_ready', { suggested_first_message: 'Say hello from the test run.' });
        }
        return quickstartTextStream('Session started.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));
    expect(await screen.findByText('Environment selected')).toBeTruthy();
    fireEvent.click(await screen.findByRole('button', { name: /Next: Start session/i }));
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Test run' }).length).toBeGreaterThan(1));
    fireEvent.click(screen.getAllByRole('button', { name: 'Test run' }).at(-1)!);

    const composer = await screen.findByPlaceholderText('Send a message to the agent') as HTMLTextAreaElement;
    await waitFor(() => expect(composer.hasAttribute('disabled')).toBe(false));
    fireEvent.change(composer, { target: { value: 'Extract structured data from this email.' } });
    fireEvent.click(screen.getByRole('button', { name: 'Send' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/sessions/sesn_created123456/events?beta=true' &&
            request.method === 'POST' &&
            request.body?.events?.[0]?.type === 'user.message' &&
            request.body?.events?.[0]?.content?.[0]?.text === 'Extract structured data from this email.'
        )
      ).toBe(true)
    );
    expect((await screen.findAllByRole('button', { name: /^User Extract structured data from this email\./ })).length).toBeGreaterThan(1);
  });

  test('creates vault credentials through the real vault credential endpoint', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('create_environment', { reuse_environment_id: 'env_option123456' });
        }
        if (proxyCalls === 2) {
          return quickstartToolStream('select_vault', { vault_ids: ['vault_option123456'], vault_names: ['Option vault'] });
        }
        if (proxyCalls === 3) {
          return quickstartToolStream('create_vault_credential', {
            vault_id: 'vault_option123456',
            mcp_server_name: 'notion',
            reason: 'Notion access is needed for this agent.'
          });
        }
        return quickstartTextStream('Credential stored.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Field monitor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Start session/i }));

    expect(await screen.findByText('Selected:')).toBeTruthy();
    const vaultAckLabel = screen.getByText(/I own or am authorized to use this vault/).closest('[data-slot="label"]');
    expect(vaultAckLabel).toBeTruthy();
    fireEvent.click(vaultAckLabel!);
    await waitFor(() => expect(screen.getByRole('button', { name: 'Confirm' }).hasAttribute('disabled')).toBe(false));
    fireEvent.click(screen.getByRole('button', { name: 'Confirm' }));
    expect(await screen.findByText('Authorization required to use this MCP')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: /Access token/ }));
    fireEvent.change(screen.getByPlaceholderText('OAuth access token'), { target: { value: 'token_test123' } });
    const credentialAckLabel = screen.getByText(/I acknowledge this credential is shared/).closest('[data-slot="label"]');
    expect(credentialAckLabel).toBeTruthy();
    fireEvent.click(credentialAckLabel!);
    fireEvent.click(screen.getByRole('button', { name: 'Authorize Notion credential' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/vaults/vault_option123456/credentials?beta=true' &&
            request.method === 'POST' &&
            request.body?.display_name === 'Notion credential'
        )
      ).toBe(true)
    );
    const credentialRequest = api.requests.find((request) => request.url === '/v1/vaults/vault_option123456/credentials?beta=true');
    expect(credentialRequest?.body?.auth).toEqual({
      type: 'static_bearer',
      mcp_server_url: 'https://mcp.notion.com/mcp',
      token: 'token_test123'
    });
    expect(await screen.findByText('Credential stored.')).toBeTruthy();
  });

  test('creates vaults and deployments through real quickstart tool endpoints', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('create_environment', { reuse_environment_id: 'env_option123456' });
        }
        if (proxyCalls === 2) {
          return quickstartToolStream('create_vault', { display_name: 'Research vault' });
        }
        if (proxyCalls === 3) {
          return quickstartToolStream('create_deployment', {
            name: 'Weekly research report',
            cron_expression: '0 9 * * 1',
            timezone: 'America/New_York',
            initial_message: 'Run the scheduled research report.'
          });
        }
        return quickstartTextStream('Deployment ready.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Start session/i }));

    expect(await screen.findByText('Vault created')).toBeTruthy();
    expect(await screen.findByText('Deployment created')).toBeTruthy();
    expect(await screen.findByText('Deployment ready.')).toBeTruthy();

    const vaultRequest = api.requests.find((request) => request.url === '/v1/vaults?beta=true' && request.method === 'POST');
    expect(vaultRequest?.body).toEqual({ display_name: 'Research vault', metadata: {} });
    const deploymentRequest = api.requests.find((request) => request.url === '/v1/deployments?beta=true' && request.method === 'POST');
    expect(deploymentRequest?.body?.agent).toEqual({ type: 'agent', id: 'agent_created123456', version: 1 });
    expect(deploymentRequest?.body?.environment_id).toBe('env_option123456');
    expect(deploymentRequest?.body?.vault_ids).toEqual(['vault_created123456']);
    expect(deploymentRequest?.body?.schedule).toEqual({
      type: 'cron',
      expression: '0 9 * * 1',
      timezone: 'America/New_York'
    });
  });

  test('stops a quickstart test run with a real session interrupt and shows rerun state', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    let proxyCalls = 0;
    const api = mockAgentsApi([], {
      quickstartStream: () => {
        proxyCalls += 1;
        if (proxyCalls === 1) {
          return quickstartToolStream('create_environment', { reuse_environment_id: 'env_option123456' });
        }
        if (proxyCalls === 2) {
          return quickstartToolStream('agent_ready', { suggested_first_message: 'Say hello from the test run.' });
        }
        if (proxyCalls === 3) {
          return quickstartToolStream('await_test_run', { until: 'session_closed' });
        }
        return quickstartTextStream('Test run stopped.');
      }
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="quickstart" />
      </WorkspaceContext.Provider>
    );

    fireEvent.click(screen.getByRole('button', { name: /Structured extractor/i }));
    fireEvent.click(screen.getByRole('button', { name: 'Use template' }));
    fireEvent.click(await screen.findByRole('button', { name: /Next: Configure environment/i }));
    expect(await screen.findByText('Environment selected')).toBeTruthy();
    fireEvent.click(await screen.findByRole('button', { name: /Next: Start session/i }));
    await waitFor(() => expect(screen.getAllByRole('button', { name: 'Test run' }).length).toBeGreaterThan(1));
    fireEvent.click(screen.getAllByRole('button', { name: 'Test run' }).at(-1)!);

    expect(await screen.findByText('Session created')).toBeTruthy();
    expect(await screen.findByText('Waiting for session to close...')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Stop session' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url === '/v1/sessions/sesn_created123456/events?beta=true' &&
            request.method === 'POST' &&
            request.body?.events?.[0]?.type === 'user.interrupt'
        )
      ).toBe(true)
    );
    expect(await screen.findByText('Test run stopped.')).toBeTruthy();
    expect(screen.getAllByRole('button', { name: 'Test run' }).length).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: 'Rerun' })).toBeTruthy();
  });

  test('filters quickstart templates by search', () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
    render(<ManagedAgentsPage section="quickstart" />);

    fireEvent.change(screen.getByPlaceholderText('Search templates'), { target: { value: 'incident' } });

    expect(screen.getByRole('button', { name: /Incident commander/i })).toBeTruthy();
    expect(screen.queryByRole('button', { name: /Data analyst/i })).toBeNull();
  });

}
