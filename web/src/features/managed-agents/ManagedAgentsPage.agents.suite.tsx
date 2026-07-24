import { expect, test } from 'bun:test';
import {
  ManagedAgentsPage,
  WorkspaceContext,
  act,
  cleanup,
  codeBlockContaining,
  createAgentRequestFixture,
  expectPageTextToContain,
  fireEvent,
  mockAgentsApi,
  mockManagedResourceApi,
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
  workspaceContextValue,
} from './ManagedAgentsPage.test-utils';

export function registerManagedAgentsAgentsTests() {
  test('renders agent rows and creates an agent through the real v1 API', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([serverAgent]);
    render(<ManagedAgentsPage section="agents" />);

    expect(screen.getByRole('heading', { name: 'Agents' })).toBeTruthy();
    expect(screen.getByText('Create and manage autonomous agents.')).toBeTruthy();
    expect(await screen.findByText('Server agent')).toBeTruthy();
    expect(screen.getAllByText('claude-sonnet-4-6').length).toBeGreaterThan(0);
    expect(screen.queryByText('No agents yet')).toBeNull();
    expect(screen.getByRole('link', { name: 'Server agent' }).getAttribute('href')).toBe(
      '/workspaces/default/agents/agent_server123456',
    );

    fireEvent.click(screen.getByRole('button', { name: 'Create agent' }));

    const dialog = screen.getByRole('dialog', { name: 'Create agent' });
    expect(within(dialog).getByText('Start from a template or describe what you need.')).toBeTruthy();
    const startingPointTablist = within(dialog).getByRole('tablist', { name: 'Starting point' });
    expect(startingPointTablist).toBeTruthy();
    expect(startingPointTablist.dataset.slot).toBe('tabs-list');
    expect(startingPointTablist.className.includes('bg-muted/70')).toBe(false);
    expect(within(dialog).getByRole('tab', { name: 'Describe your agent' }).getAttribute('aria-selected')).toBe('true');
    expect(within(dialog).getByRole('button', { name: 'Generate' }).hasAttribute('disabled')).toBe(true);
    const descriptionInput = within(dialog).getByRole('textbox', { name: 'Describe your agent' });
    expect(descriptionInput.getAttribute('data-slot')).toBe('input-group-control');
    expect(descriptionInput.closest('[role="tabpanel"]')?.parentElement?.className).toContain('flex-col');
    expect(descriptionInput.className).toContain('overflow-y-auto');
    expect(descriptionInput.className).toContain('subtle-scrollbar');
    expect(descriptionInput.closest('[data-slot="input-group"]')?.getAttribute('data-slot')).toBe('input-group');
    const startingPointButton = within(dialog).getByRole('button', { name: /^Starting point$/i });
    expect(startingPointButton.getAttribute('data-slot')).toBe('collapsible-trigger');
    expect(startingPointButton.parentElement?.parentElement?.className).toContain('rounded-xl');
    expect(startingPointButton.className).toContain('items-center');
    expect(startingPointButton.getAttribute('aria-expanded')).toBe('true');
    expect(dialog.textContent).toContain('name: Untitled agent');
    expect(dialog.textContent).toContain('mcp_servers: []');
    expect(within(dialog).getByRole('tab', { name: 'YAML' }).getAttribute('aria-selected')).toBe('true');
    const configFormatTablist = within(dialog).getByRole('tablist', { name: 'Config format' });
    expect(configFormatTablist.dataset.slot).toBe('tabs-list');
    expect(configFormatTablist.className.includes('bg-muted/70')).toBe(false);
    const yamlEditor = within(dialog).getByRole('textbox', { name: 'Agent config YAML' });
    expect(yamlEditor.closest('.cm-editor')?.className).toContain('cm-editor');
    expect(yamlEditor.closest('.agent-config-codemirror')?.parentElement?.className).toContain('flex-1');
    expect(yamlEditor.closest('.agent-config-codemirror')?.parentElement?.className).toContain('overflow-hidden');
    expect(yamlEditor.closest('.agent-config-codemirror')?.getAttribute('style')).toContain(
      '--agent-config-editor-min-height: 0px',
    );
    // drawSelection is disabled so CodeMirror must not paint its own
    // .cm-selectionLayer, nor inject hideNativeSelection (which forces the OS
    // Highlight color on ::selection while focused and inverts the syntax
    // foreground). Regression guard for the jarring selected-text color.
    expect(yamlEditor.closest('.cm-editor')?.querySelector('.cm-selectionLayer')).toBeNull();

    fireEvent.click(within(dialog).getByRole('tab', { name: 'Template' }));

    expect(within(dialog).getByRole('tab', { name: 'Template' }).getAttribute('aria-selected')).toBe('true');
    const blankTemplateCard = within(dialog).getByRole('button', { name: /Blank agent config/i });
    const deepResearcherTemplateCard = within(dialog).getByRole('button', { name: /Deep researcher/i });
    const incidentCommanderTemplateCard = within(dialog).getByRole('button', { name: /Incident commander/i });
    expect(blankTemplateCard).toBeTruthy();
    expect(deepResearcherTemplateCard).toBeTruthy();
    expect(incidentCommanderTemplateCard).toBeTruthy();
    expect(blankTemplateCard.querySelector('[data-slot="card"]')?.getAttribute('data-slot')).toBe('card');
    expect(deepResearcherTemplateCard.querySelector('[data-slot="card"]')?.getAttribute('data-slot')).toBe('card');
    expect(incidentCommanderTemplateCard.querySelectorAll('[data-slot="badge"]').length).toBeGreaterThan(0);
    expect(within(dialog).queryByText('Contract tracker')).toBeNull();

    const templateExpectations = [
      {
        button: /Blank agent config/i,
        yaml: ['name: Untitled agent', 'description: A blank starting point with the core toolset.', 'mcp_servers: []'],
      },
      {
        button: /Deep researcher/i,
        yaml: ['name: Deep researcher', 'You are a research agent', 'confidence & gaps', 'template: deep-research'],
      },
      {
        button: /Structured extractor/i,
        yaml: [
          'name: Structured extractor',
          'No prose, no markdown fences',
          '_extraction_notes',
          'template: structured-extractor',
        ],
      },
      {
        button: /Field monitor/i,
        yaml: ['name: Field monitor', 'https://mcp.notion.com/mcp', 'mcp_server_name: notion', 'type: always_allow'],
      },
      {
        button: /Support agent/i,
        yaml: ['name: Support agent', 'https://mcp.slack.com/mcp', 'mcp_server_name: slack', '≥80% confidence'],
      },
      {
        button: /Incident commander/i,
        yaml: [
          'name: Incident commander',
          'model: claude-opus-4-8',
          'https://api.githubcopilot.com/mcp/',
          'mcp_server_name: github',
        ],
      },
    ];

    const openStartingPoint = () => {
      const trigger = within(dialog).getByRole('button', { name: /^Starting point$/i });
      if (trigger.getAttribute('aria-expanded') === 'false') {
        fireEvent.click(trigger);
      }
    };

    for (const expectation of templateExpectations) {
      openStartingPoint();
      fireEvent.click(within(dialog).getByRole('button', { name: expectation.button }));
      for (const yamlLine of expectation.yaml) {
        expect(dialog.textContent).toContain(yamlLine);
      }
    }

    openStartingPoint();
    fireEvent.click(within(dialog).getByRole('button', { name: /Deep researcher/i }));
    expect(dialog.textContent).toContain('name: Deep researcher');
    expect(
      within(dialog)
        .getByRole('button', { name: /^Starting point$/i })
        .getAttribute('aria-expanded'),
    ).toBe('false');
    const collapsedSummary = within(dialog)
      .getAllByText('Deep researcher')
      .find((node) => node.closest('[data-slot="badge"]'));
    expect(collapsedSummary?.closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
    expect(within(dialog).queryByRole('button', { name: /Structured extractor/i })).toBeNull();
    expect(dialog.className).toContain('h-[min(720px,calc(100dvh-2rem))]');
    expect(dialog.firstElementChild?.className).toContain('flex-col');
    expect(dialog.querySelector('[role="tabpanel"]')?.className).toContain('flex-1');

    fireEvent.click(within(dialog).getByRole('tab', { name: 'JSON' }));
    expect(
      within(dialog).getByRole('textbox', { name: 'Agent config JSON' }).closest('.cm-editor')?.className,
    ).toContain('cm-editor');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Create agent' }));

    await waitFor(() => expect(api.requests.some((request) => request.method === 'POST')).toBe(true));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create agent' })).toBeNull());
    expect(window.location.pathname).toBe('/workspaces/default/agents/agent_created123456');
    expect(window.location.search).toBe('?tab=config');
    expect(api.requests.some((request) => request.method === 'GET' && request.url.includes('/v1/agents?'))).toBe(true);
    const createRequest = api.requests.find((request) => request.method === 'POST');
    expect(createRequest?.url).toBe('/v1/agents?beta=true');
    expect(createRequest?.headers['x-workspace-id']).toBe('default');
    expect(createRequest?.body?.name).toBe('Deep researcher');
    expect(createRequest?.body?.model).toBe('claude-sonnet-4-6');
    expect((createRequest?.body?.metadata as Record<string, string>).template).toBe('deep-research');
    const createdToolset = (createRequest?.body?.tools as Array<Record<string, unknown>>)[0];
    expect(createdToolset.type).toBe('agent_toolset_20260401');
    expect(createdToolset.configs).toBeUndefined();

    fireEvent.click(screen.getByRole('button', { name: 'Create agent' }));
    expect(screen.getByRole('dialog', { name: 'Create agent' })).toBeTruthy();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Create agent' })).toBeNull();
  });

  test('uses client-side navigation for agent detail links', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    mockAgentsApi([serverAgent]);
    let popstateCount = 0;
    const handlePopstate = () => {
      popstateCount += 1;
    };
    window.addEventListener('popstate', handlePopstate);

    try {
      render(<ManagedAgentsPage section="agents" />);

      const agentLink = await screen.findByRole('link', { name: 'Server agent' });
      expect(agentLink.getAttribute('href')).toBe('/workspaces/default/agents/agent_server123456');

      fireEvent.click(agentLink);

      expect(window.location.pathname).toBe('/workspaces/default/agents/agent_server123456');
      expect(popstateCount).toBe(1);
    } finally {
      window.removeEventListener('popstate', handlePopstate);
    }
  });

  test('validates and generates create-agent config before navigating to the created agent', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([], {
      quickstartStream: () =>
        quickstartToolStream('build_agent_config', {
          name: 'PR digest',
          description: 'Summarizes new GitHub PRs and posts a digest to Slack.',
          model: 'claude-sonnet-4-6',
          system: 'Summarize pull requests clearly and ask before taking irreversible actions.',
          mcp_servers: [{ name: 'github', type: 'url', url: 'https://api.githubcopilot.com/mcp/' }],
          tools: [{ type: 'agent_toolset_20260401' }, { type: 'mcp_toolset', mcp_server_name: 'github' }],
          skills: [],
          metadata: { source: 'description' },
        }),
    });
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    expect(await screen.findByText('No agents yet')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Create agent' }));
    const dialog = screen.getByRole('dialog', { name: 'Create agent' });

    fireEvent.click(within(dialog).getByRole('tab', { name: 'JSON' }));
    setAgentConfigEditorValue(dialog, '{', 'Agent config JSON');
    expect(within(dialog).getByText(/JSON is not valid/i)).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Create agent' }).hasAttribute('disabled')).toBe(true);

    setAgentConfigEditorValue(
      dialog,
      JSON.stringify(createAgentRequestFixture('Temporary config'), null, 2),
      'Agent config JSON',
    );
    expect(within(dialog).queryByText(/JSON is not valid/i)).toBeNull();

    fireEvent.change(within(dialog).getByRole('textbox', { name: 'Describe your agent' }), {
      target: { value: 'Summarizes new GitHub PRs and posts a digest to Slack.' },
    });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Generate' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/api/organizations/org_test/proxy/v1/messages')).toBe(
        true,
      ),
    );
    const proxyRequest = api.requests.find(
      (request) => request.url === '/api/organizations/org_test/proxy/v1/messages',
    );
    expect(proxyRequest?.body?.tool_choice).toEqual({
      type: 'tool',
      name: 'build_agent_config',
      disable_parallel_tool_use: true,
    });
    await waitFor(() => expect(dialog.textContent).toContain('PR digest'));
    expect(
      within(dialog)
        .getByRole('button', { name: /^Starting point$/i })
        .getAttribute('aria-expanded'),
    ).toBe('true');
    expect(dialog.textContent).toContain('PR digest');

    fireEvent.click(within(dialog).getByRole('button', { name: 'Create agent' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/agents?beta=true' && request.method === 'POST')).toBe(
        true,
      ),
    );
    const createRequest = api.requests.find(
      (request) => request.url === '/v1/agents?beta=true' && request.method === 'POST',
    );
    expect(createRequest?.body?.name).toBe('PR digest');
    expect(createRequest?.headers['x-workspace-id']).toBe('default');
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Create agent' })).toBeNull());
    expect(window.location.pathname).toBe('/workspaces/default/agents/agent_created123456');
    expect(window.location.search).toBe('?tab=config');
  });

  test('renders agent detail and switches the config viewer to a historical version', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_detail123456');
    const api = mockAgentsApi(
      [
        {
          id: 'agent_detail123456',
          name: 'Current agent',
          version: 2,
          description: 'Current description',
          model: { id: 'claude-sonnet-4-6', speed: 'fast' },
          skills: [
            { type: 'anthropic', skill_id: 'triage', version: '2026-07-01' },
            { type: 'custom', skill_id: 'reporting' },
          ],
          system: 'Current system prompt',
          tools: [{ type: 'agent_toolset_20260401', configs: [{ name: 'bash' }] }],
          versions: [
            {
              id: 'agent_detail123456',
              name: 'Historical agent',
              version: 1,
              description: 'Old description',
              system: 'Old system prompt',
              tools: [],
              mcp_servers: [],
            },
          ],
        },
      ],
      {
        skills: [
          {
            id: 'triage',
            displayTitle: 'Customer triage',
            latestVersion: '20260708',
            source: 'anthropic',
            updated_at: '2026-07-08T12:00:00Z',
          },
          {
            id: 'reporting',
            displayTitle: 'Weekly reporting',
            latestVersion: '20260702',
            source: 'custom',
            updated_at: '2026-07-02T12:00:00Z',
          },
        ],
      },
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Current agent', hidden: true })).toBeTruthy();
    const breadcrumb = screen.getByRole('navigation', { name: 'Breadcrumb' });
    expect(breadcrumb.dataset.slot).toBe('breadcrumb');
    expect(within(breadcrumb).getByRole('link', { name: 'Agents' }).getAttribute('href')).toBe(
      '/workspaces/default/agents',
    );
    expect(breadcrumb.querySelector('[data-slot="breadcrumb-page"]')?.textContent).toBe('Current agent');
    expect(screen.getByText('Current system prompt')).toBeTruthy();
    expect(screen.getByText('Built-in tools')).toBeTruthy();
    expect(screen.getByText('MCPs and tools')).toBeTruthy();
    expect(screen.getByText('Skills')).toBeTruthy();
    const mcpToolsCard = screen.getByText('Built-in tools').closest('[data-slot="card"]');
    expect(mcpToolsCard?.getAttribute('data-slot')).toBe('card');
    const detailTabs = screen.getByRole('tablist', { name: 'Agent detail sections' });
    expect(detailTabs.dataset.slot).toBe('tabs-list');
    expect(within(detailTabs).getByRole('tab', { name: 'Agent' }).getAttribute('aria-selected')).toBe('true');
    expect(screen.getByRole('tabpanel').textContent).toContain('Current system prompt');
    expect(screen.getByText('New').closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
    expect(screen.getByText('Fast').closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
    expect(await screen.findByText('Customer triage')).toBeTruthy();
    expect(screen.getByText('Weekly reporting')).toBeTruthy();
    const skillsCard = screen.getByText('Customer triage').closest('[data-slot="card"]') as HTMLElement;
    expect(skillsCard).toBeTruthy();
    expect(within(skillsCard).getByText('triage').closest('code')).toBeTruthy();
    expect(within(skillsCard).getByText('reporting').closest('code')).toBeTruthy();
    expect(within(skillsCard).getByText('v2026-07-01')).toBeTruthy();
    expect(within(skillsCard).queryByText('Update available (v20260708)')).toBeNull();
    expect(within(skillsCard).getAllByText('Anthropic').length).toBeGreaterThan(0);
    expect(within(skillsCard).getAllByText('Custom').length).toBeGreaterThan(0);
    const triageHoverTarget = screen
      .getByText('Customer triage')
      .closest('[aria-label$="skill summary"]') as HTMLElement;
    expect(triageHoverTarget).toBeTruthy();
    fireEvent.click(triageHoverTarget);
    await waitFor(() => expect(screen.getByText('Source')).toBeTruthy());
    expect(screen.getByText('Latest version')).toBeTruthy();
    expect(screen.getByText('20260708')).toBeTruthy();
    expect(screen.getByText('Agent version')).toBeTruthy();
    expect(screen.getByText('2026-07-01')).toBeTruthy();
    expect(screen.getByText('Resolved')).toBeTruthy();
    expect(screen.getAllByText('Anthropic').length).toBeGreaterThan(0);
    fireEvent.click(triageHoverTarget);
    await waitFor(() => expect(screen.queryByText('Source')).toBeNull());
    expect(screen.queryByRole('columnheader', { name: 'Skill' })).toBeNull();
    expect(api.requests.some((request) => request.url === '/v1/skills/triage?beta=true')).toBe(true);
    expect(api.requests.some((request) => request.url === '/v1/skills/reporting?beta=true')).toBe(true);
    expect(screen.queryByText('No skills configured.')).toBeNull();
    const permissionsButton = screen.getByRole('button', { name: /Tool permissions\s+8/ });
    expect(permissionsButton).toBeTruthy();
    expect(permissionsButton.querySelector('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');
    fireEvent.click(permissionsButton);
    expect(screen.getByText('bash')).toBeTruthy();
    expect(screen.getByText('web_search')).toBeTruthy();
    expect(screen.getByText('Search the web')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Edit' }).hasAttribute('disabled')).toBe(false);
    const versionButton = screen.getByRole('button', { name: 'Version: v2' });
    expect(versionButton.className.includes('bg-secondary')).toBe(false);

    fireEvent.click(versionButton);
    const versionMenu = screen
      .getByRole('menuitemradio', { name: 'v1' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(versionMenu?.className).toContain('bg-popover');
    expect(versionMenu?.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'v1' }));

    await waitFor(() => expect(screen.getByText('Old system prompt')).toBeTruthy());
    expect(screen.getByText('No skills configured.')).toBeTruthy();
    expect(screen.getByText('No MCPs or tools configured.')).toBeTruthy();
    expect(screen.queryByText('Built-in tools')).toBeNull();
    expect(screen.getByRole('heading', { name: 'Current agent' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Edit' }).hasAttribute('disabled')).toBe(false);
    expect(api.requests.some((request) => request.url === '/v1/agents/agent_detail123456?beta=true&version=1')).toBe(
      true,
    );
  });

  test('shows the agent detail tools empty state without rendering a placeholder card', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_emptytools123456');
    mockAgentsApi([
      {
        id: 'agent_emptytools123456',
        name: 'No tools agent',
        tools: [],
        mcp_servers: [],
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('No MCPs or tools configured.')).toBeTruthy();
    expect(screen.queryByText('No tools configured')).toBeNull();
    const toolsHeading = screen.getByRole('heading', { name: 'MCPs and tools' });
    const toolsSection = toolsHeading.closest('section') as HTMLElement;
    expect(toolsSection.querySelector('[data-slot="card"]')).toBeNull();
  });

  test('renders coexisting built-in, custom, and directory-backed MCP tool permissions as read-only cards', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_mixedtools123456');
    const api = mockAgentsApi(
      [
        {
          id: 'agent_mixedtools123456',
          name: 'Mixed tools agent',
          mcp_servers: [{ name: 'notion', url: 'https://agent.example.com/notion' }],
          tools: [
            {
              type: 'agent_toolset_20260401',
              configs: [{ name: 'bash', enabled: false }],
            },
            {
              type: 'custom',
              name: 'lookup_customer',
              description: 'Find a customer by email',
              enabled: false,
              permission_policy: { type: 'always_ask' },
            },
            {
              type: 'mcp_toolset',
              mcp_server_name: 'notion',
              default_config: { permission_policy: { type: 'always_ask' } },
              configs: [{ name: 'search', enabled: false }],
            },
          ],
        },
      ],
      {
        mcpDirectoryServers: [
          {
            type: 'remote',
            slug: 'notion',
            name: 'Notion',
            display_name: 'Notion',
            icon_url: 'https://example.com/notion.png',
            tool_names: ['search', 'create_page'],
            remote: { url: 'https://directory.example.com/notion' },
          },
        ],
      },
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Mixed tools agent', hidden: true })).toBeTruthy();
    const section = screen.getByRole('heading', { name: 'MCPs and tools' }).closest('section') as HTMLElement;
    await waitFor(() => expect(within(section).getByRole('button', { name: /Tool permissions\s+2/ })).toBeTruthy());
    const directoryStatus = within(section).getByRole('status');
    expect(directoryStatus.textContent).toBe('MCP tool metadata loaded.');
    expect(directoryStatus.parentElement?.getAttribute('aria-busy')).toBe('false');
    const cards = Array.from(section.querySelectorAll<HTMLElement>('[data-slot="card"]'));
    expect(cards).toHaveLength(3);
    expect(cards.map((card) => card.textContent?.match(/Built-in tools|Custom tools|Notion/)?.[0])).toEqual([
      'Built-in tools',
      'Custom tools',
      'Notion',
    ]);

    const builtInCard = cards[0];
    expect(within(builtInCard).getByText('Custom')).toBeTruthy();
    fireEvent.click(within(builtInCard).getByRole('button', { name: /Tool permissions\s+8/ }));
    expect(within(builtInCard).getByText('bash')).toBeTruthy();
    expect(within(builtInCard).getByText('Always deny')).toBeTruthy();
    expect(within(builtInCard).getAllByText('Always allow').length).toBeGreaterThan(0);

    const customCard = cards[1];
    fireEvent.click(within(customCard).getByRole('button', { name: /Tools\s+1/ }));
    expect(within(customCard).getByText('lookup_customer')).toBeTruthy();
    expect(within(customCard).getByText('Find a customer by email')).toBeTruthy();
    expect(within(customCard).queryByText(/Always (allow|ask|deny)/)).toBeNull();

    const mcpCard = cards[2];
    expect(within(mcpCard).getByText('https://agent.example.com/notion')).toBeTruthy();
    expect(within(mcpCard).getByText('Custom')).toBeTruthy();
    const mcpPermissionsButton = within(mcpCard).getByRole('button', { name: /Tool permissions\s+2/ });
    expect(mcpPermissionsButton.getAttribute('aria-label')).toContain('Custom');
    fireEvent.click(mcpPermissionsButton);
    expect(within(mcpCard).getByText('search')).toBeTruthy();
    expect(within(mcpCard).getByText('create_page')).toBeTruthy();
    expect(within(mcpCard).getByText('Always deny')).toBeTruthy();
    expect(within(mcpCard).getByText('Always ask')).toBeTruthy();
    const directoryIcon = mcpCard.querySelector('img') as HTMLImageElement;
    expect(directoryIcon).toBeTruthy();
    fireEvent.error(directoryIcon);
    expect(mcpCard.querySelector('img')).toBeNull();
    expect(mcpCard.querySelector('.lucide-server')).toBeTruthy();
    expect(api.requests.some((request) => request.url.startsWith('/api/directory/servers?'))).toBe(true);
  });

  test('keeps an unknown MCP usable when the directory request fails', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_directoryfailure123456');
    mockAgentsApi(
      [
        {
          id: 'agent_directoryfailure123456',
          name: 'Private MCP agent',
          mcp_servers: [{ name: 'private_docs', url: 'https://docs.example.com/mcp' }],
          tools: [],
        },
      ],
      { mcpDirectoryErrorOnce: true },
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Private MCP agent', hidden: true })).toBeTruthy();
    const section = screen.getByRole('heading', { name: 'MCPs and tools' }).closest('section') as HTMLElement;
    await waitFor(() =>
      expect(within(section).getByRole('status').textContent).toBe('MCP tool metadata is unavailable.'),
    );
    const permissionsButton = within(section).getByRole('button', {
      name: /Private Docs Tool permissions — Always ask/,
    });
    expect(permissionsButton).toBeTruthy();
    expect(within(section).getByRole('status').parentElement?.getAttribute('aria-busy')).toBe('false');

    fireEvent.click(permissionsButton);
    expect(within(section).getByText('No tool list available.')).toBeTruthy();
  });

  test('replaces a saved MCP catalog immediately after a synchronous scoped refresh', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_livecatalog123456');
    const api = mockAgentsApi(
      [
        {
          id: 'agent_livecatalog123456',
          name: 'Saved catalog agent',
          mcp_servers: [{ name: 'weather', url: 'http://weather.local:39090/mcp' }],
          tools: [
            {
              type: 'mcp_toolset',
              mcp_server_name: 'weather',
              default_config: { permission_policy: { type: 'always_ask' } },
              configs: [{ name: 'get_forecast', enabled: false }],
            },
          ],
        },
      ],
      {
        mcpDirectoryServers: [
          {
            type: 'remote',
            slug: 'weather',
            display_name: 'Weather Service',
            tool_names: ['stale_directory_tool'],
          },
        ],
        mcpToolCatalogs: [
          {
            server_name: 'weather',
            status: 'ready',
            tools: [{ name: 'get_forecast', title: 'Forecast', description: 'Returns a weather forecast.' }],
          },
        ],
        mcpToolCatalogRefreshResult: {
          server_name: 'weather',
          status: 'ready',
          tools: [{ name: 'get_observation', title: 'Observation', description: 'Returns current conditions.' }],
        },
      },
    );
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    expect(await screen.findByRole('heading', { name: 'Saved catalog agent', hidden: true })).toBeTruthy();
    const section = screen.getByRole('heading', { name: 'MCPs and tools' }).closest('section') as HTMLElement;
    const permissionsButton = await within(section).findByRole('button', {
      name: /Weather Service Tool permissions 1 Always deny Saved tool list/,
    });
    fireEvent.click(permissionsButton);
    expect(within(section).getByText('get_forecast')).toBeTruthy();
    expect(within(section).getByText('Returns a weather forecast.')).toBeTruthy();
    expect(within(section).queryByText('stale_directory_tool')).toBeNull();

    fireEvent.click(within(section).getByRole('button', { name: 'Refresh MCP tools for Weather Service' }));
    expect(await within(section).findByText('get_observation')).toBeTruthy();
    expect(within(section).getByText('Returns current conditions.')).toBeTruthy();
    expect(within(section).queryByText('get_forecast')).toBeNull();
    expect(within(section).getByRole('status').textContent).toContain('MCP tools refreshed and saved.');
    const refresh = api.requests.find(
      (request) => request.method === 'POST' && request.url.includes('/mcp_tool_catalogs/refresh'),
    );
    expect(refresh?.url).toContain('/agent_livecatalog123456/mcp_tool_catalogs/refresh?version=1');
    expect(refresh?.body).toEqual({ server_name: 'weather' });
  });

  test('shows an error and retains the saved tools when an MCP catalog refresh fails', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_catalogrefreshfailure123456');
    mockAgentsApi(
      [
        {
          id: 'agent_catalogrefreshfailure123456',
          name: 'Catalog refresh failure agent',
          mcp_servers: [{ name: 'weather', url: 'http://weather.local:39090/mcp' }],
          tools: [
            {
              type: 'mcp_toolset',
              mcp_server_name: 'weather',
              default_config: { permission_policy: { type: 'always_ask' } },
            },
          ],
        },
      ],
      {
        mcpToolCatalogs: [
          {
            server_name: 'weather',
            status: 'ready',
            tools: [{ name: 'saved_forecast', description: 'The last successful snapshot.' }],
          },
        ],
        mcpToolCatalogRefreshErrorOnce: true,
      },
    );
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    expect(await screen.findByRole('heading', { name: 'Catalog refresh failure agent', hidden: true })).toBeTruthy();
    const section = screen.getByRole('heading', { name: 'MCPs and tools' }).closest('section') as HTMLElement;
    fireEvent.click(
      await within(section).findByRole('button', { name: /Tool permissions 1 Always ask Saved tool list/ }),
    );
    expect(within(section).getByText('saved_forecast')).toBeTruthy();
    fireEvent.click(await within(section).findByRole('button', { name: 'Refresh MCP tools for Weather' }));

    const toastTitle = await screen.findByText('Could not refresh MCP tools.');
    expect(toastTitle.closest('[data-sonner-toast]')?.getAttribute('data-type')).toBe('error');
    expect(screen.getByText('MCP catalog refresh unavailable')).toBeTruthy();
    expect(within(section).getByText('saved_forecast')).toBeTruthy();
    expect(within(section).getByText('The last successful snapshot.')).toBeTruthy();
  });

  test('keeps the old snapshot visible and disables every MCP refresh button while refreshing', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_refreshpending123456');
    let finishRefresh!: () => void;
    const refreshWait = new Promise<void>((resolve) => {
      finishRefresh = resolve;
    });
    mockAgentsApi(
      [
        {
          id: 'agent_refreshpending123456',
          name: 'Refresh pending agent',
          mcp_servers: [
            { name: 'weather', url: 'http://weather.local:39090/mcp' },
            { name: 'maps', url: 'http://maps.local:39091/mcp' },
          ],
          tools: [
            { type: 'mcp_toolset', mcp_server_name: 'weather' },
            { type: 'mcp_toolset', mcp_server_name: 'maps' },
          ],
        },
      ],
      {
        mcpToolCatalogs: [
          { server_name: 'weather', status: 'ready', tools: [{ name: 'saved_weather' }] },
          { server_name: 'maps', status: 'ready', tools: [{ name: 'saved_maps' }] },
        ],
        mcpToolCatalogRefreshResult: {
          server_name: 'weather',
          status: 'ready',
          tools: [{ name: 'new_weather' }],
        },
        mcpToolCatalogRefreshWait: refreshWait,
      },
    );
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    expect(await screen.findByRole('heading', { name: 'Refresh pending agent', hidden: true })).toBeTruthy();
    const section = screen.getByRole('heading', { name: 'MCPs and tools' }).closest('section') as HTMLElement;
    fireEvent.click(
      await within(section).findByRole('button', { name: /Weather Tool permissions 1 Always ask Saved tool list/ }),
    );
    expect(within(section).getByText('saved_weather')).toBeTruthy();
    fireEvent.click(within(section).getByRole('button', { name: 'Refresh MCP tools for Weather' }));

    await waitFor(() => {
      const buttons = within(section).getAllByRole('button', {
        name: /^Refresh MCP tools for /,
      }) as HTMLButtonElement[];
      expect(buttons).toHaveLength(2);
      expect(buttons.every((button) => button.disabled)).toBe(true);
    });
    expect(within(section).getByText('saved_weather')).toBeTruthy();

    act(() => finishRefresh());
    expect(await within(section).findByText('new_weather')).toBeTruthy();
    expect(within(section).queryByText('saved_weather')).toBeNull();
    await waitFor(() =>
      expect(
        (within(section).getAllByRole('button', { name: /^Refresh MCP tools for / }) as HTMLButtonElement[]).every(
          (button) => !button.disabled,
        ),
      ).toBe(true),
    );
  });

  test('treats a successful empty refresh as authoritative over Directory tools', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_emptycatalog123456');
    mockAgentsApi(
      [
        {
          id: 'agent_emptycatalog123456',
          name: 'Empty catalog agent',
          mcp_servers: [{ name: 'weather', url: 'http://weather.local:39090/mcp' }],
          tools: [
            {
              type: 'mcp_toolset',
              mcp_server_name: 'weather',
              default_config: { permission_policy: { type: 'always_ask' } },
            },
          ],
        },
      ],
      {
        mcpDirectoryServers: [
          {
            type: 'remote',
            slug: 'weather',
            display_name: 'Weather Service',
            tool_names: ['directory_forecast'],
          },
        ],
        mcpToolCatalogRefreshResult: {
          server_name: 'weather',
          status: 'ready',
          tools: [],
        },
      },
    );
    render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    expect(await screen.findByRole('heading', { name: 'Empty catalog agent', hidden: true })).toBeTruthy();
    const section = screen.getByRole('heading', { name: 'MCPs and tools' }).closest('section') as HTMLElement;
    fireEvent.click(
      await within(section).findByRole('button', {
        name: /Weather Service Tool permissions 1 Always ask Tool list not refreshed/,
      }),
    );
    expect(within(section).getByText('directory_forecast')).toBeTruthy();

    fireEvent.click(within(section).getByRole('button', { name: 'Refresh MCP tools for Weather Service' }));

    expect(await within(section).findByText('This server reported no tools.')).toBeTruthy();
    expect(within(section).queryByText('directory_forecast')).toBeNull();
    expect(
      within(section).getByRole('button', {
        name: /Weather Service Tool permissions 0 Always ask Saved tool list/,
      }),
    ).toBeTruthy();
  });

  test('renders agent sessions tab and refetches with version, deployment, and status filters', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_detail123456');
    const api = mockAgentsApi(
      [
        {
          id: 'agent_detail123456',
          name: 'Current agent',
          version: 2,
          versions: [{ id: 'agent_detail123456', name: 'Current agent v1', version: 1 }],
        },
      ],
      {
        deployments: [
          {
            id: 'dep_detail123456',
            agentId: 'agent_detail123456',
            version: 2,
            name: 'Nightly run',
          },
        ],
        sessions: [
          {
            id: 'sesn_detail123456',
            agentId: 'agent_detail123456',
            version: 2,
            deploymentId: 'dep_detail123456',
            inputTokens: 1234,
            outputTokens: 56,
            title: null,
            status: 'running',
          },
          {
            id: 'sesn_old123456',
            agentId: 'agent_detail123456',
            version: 1,
            title: 'Old session',
            status: 'idle',
            archived_at: new Date().toISOString(),
          },
        ],
      },
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Current agent', hidden: true })).toBeTruthy();
    fireEvent.click(screen.getByRole('tab', { name: 'Sessions' }));

    expect(await screen.findByText('sesn_detail123456')).toBeTruthy();
    expect(screen.getAllByText('v2').length).toBeGreaterThan(0);
    expect(screen.getByText('1,234 / 56')).toBeTruthy();
    const sessionsCard = screen.getByText('sesn_detail123456').closest('[data-slot="card"]');
    expect(sessionsCard?.getAttribute('data-slot')).toBe('card');
    const sessionRow = screen.getByText('sesn_detail123456').closest('tr');
    expect(sessionRow).toBeTruthy();
    const viewSessionLink = within(sessionRow as HTMLElement).getByRole('link', { name: 'View session' });
    expect(viewSessionLink.dataset.slot).toBe('button');
    expect(viewSessionLink.getAttribute('href')).toBe('/workspaces/default/sessions/sesn_detail123456');
    const initialSessionRequest = api.requests.find(
      (request) =>
        request.url.includes('/v1/sessions?') &&
        request.url.includes('agent_id=agent_detail123456') &&
        !request.url.includes('agent_version=') &&
        !request.url.includes('deployment_id='),
    );
    expect(initialSessionRequest).toBeTruthy();
    const initialSessionParams = new URL(initialSessionRequest?.url ?? '', 'https://oma.duck.ai').searchParams;
    expect(initialSessionParams.get('limit')).toBe('8');
    expect(initialSessionParams.get('include_archived')).toBe('true');
    expect(sessionStatusValuesFromUrl(initialSessionRequest?.url ?? '')).toEqual([
      'idle',
      'rescheduling',
      'running',
      'terminated',
    ]);
    expect(
      api.requests.some(
        (request) =>
          request.url === '/v1/deployments?beta=true&limit=20&agent_id=agent_detail123456&include_archived=true',
      ),
    ).toBe(true);
    expect(screen.getByRole('button', { name: 'Previous page' }).className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('button', { name: 'Next page' }).className.includes('bg-secondary')).toBe(false);

    const sessionVersionButton = screen.getByRole('button', { name: /Version\s+All/ });
    expect(sessionVersionButton.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(sessionVersionButton);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'v2' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url.includes('/v1/sessions?') &&
            request.url.includes('agent_id=agent_detail123456') &&
            request.url.includes('agent_version=2'),
        ),
      ).toBe(true),
    );

    const deploymentButton = screen.getByRole('button', { name: /deployment\s+All/i });
    expect(deploymentButton.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(deploymentButton);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Nightly run' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url.includes('/v1/sessions?') &&
            request.url.includes('agent_version=2') &&
            request.url.includes('deployment_id=dep_detail123456'),
        ),
      ).toBe(true),
    );

    const statusButton = screen.getByRole('button', { name: /Status\s+All/ });
    expect(statusButton.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(statusButton);
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Running' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.url.includes('/v1/sessions?') &&
            request.url.includes('deployment_id=dep_detail123456') &&
            sessionStatusValuesFromUrl(request.url).join(',') === 'running',
        ),
      ).toBe(true),
    );
  });

  test('opens agent deployment creation from the detail query and locks the current agent', async () => {
    resetTestDom(
      'https://oma.duck.ai/workspaces/default/agents/agent_detail123456?tab=deployments&create_deployment=1',
    );
    const api = mockAgentsApi([
      {
        id: 'agent_detail123456',
        name: 'Current agent',
        version: 2,
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Current agent', hidden: true })).toBeTruthy();
    expect(screen.getByRole('tab', { name: 'Deployments', hidden: true }).getAttribute('aria-selected')).toBe('true');
    const dialog = await screen.findByRole('dialog', { name: 'Create deployment' });
    expect(within(dialog).getByText('Current agent')).toBeTruthy();
    expect(within(dialog).queryByLabelText('Agent')).toBeNull();

    fireEvent.change(within(dialog).getByLabelText('Name'), { target: { value: 'Agent detail deployment' } });
    fireEvent.change(within(dialog).getByLabelText('Initial message'), { target: { value: 'Run the detail flow.' } });
    await waitFor(() =>
      expect(within(dialog).getByRole('combobox', { name: 'Environment' }).textContent).toContain(
        'Select an environment',
      ),
    );
    await selectManagedComboboxOption(dialog, 'Environment', 'Option environment');
    await selectManagedComboboxOption(dialog, 'Trigger', 'Manual');
    fireEvent.click(within(dialog).getByRole('button', { name: 'Create' }));

    await waitFor(() =>
      expect(
        api.requests.some((request) => request.url === '/v1/deployments?beta=true' && request.method === 'POST'),
      ).toBe(true),
    );
    const createRequest = api.requests.find(
      (request) => request.url === '/v1/deployments?beta=true' && request.method === 'POST',
    );
    expect(createRequest?.body?.name).toBe('Agent detail deployment');
    expect(createRequest?.body?.agent).toEqual({ type: 'agent', id: 'agent_detail123456', version: 2 });
    expect(createRequest?.body?.environment_id).toBe('env_option123456');
    expect(createRequest?.body?.initial_events).toEqual([
      {
        type: 'user.message',
        content: [{ type: 'text', text: 'Run the detail flow.' }],
      },
    ]);
  });

  test('renders agent observability and requests agent-scoped analytics', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_detail123456?tab=observability');
    const api = mockAgentsApi(
      [
        {
          id: 'agent_detail123456',
          name: 'Current agent',
          version: 2,
        },
      ],
      {
        analyticsOverview: {
          sessions_count: 3,
          error_rate: 0.025,
          input_tokens: { total: 12345, p50: 100, p95: 200 },
          output_tokens: { total: 678, p50: 50, p95: 75 },
          duration: { p50: 30, p90: 40, p95: 45 },
          active_time: { p50: 6, p90: 9, p95: 12 },
          turns_per_session: { p50: 2, p90: 3, p95: 4 },
          tool_call_counts: { bash: 2 },
          stop_reason_counts: { end_turn: 3 },
          data_as_of: '2026-07-03T00:00:00Z',
        },
        analyticsTimeseries: [{ outcome_category: 'success', sessions_count: 3 }],
      },
    );
    renderManagedAgentsPage('agents');

    expect(await screen.findByRole('heading', { name: 'Current agent' })).toBeTruthy();
    expect((await screen.findAllByText('Sessions')).length).toBeGreaterThan(1);
    expect(await screen.findByText('12,345')).toBeTruthy();
    expect(await screen.findByText('2.5%')).toBeTruthy();
    expect(screen.getByText('Tool usage')).toBeTruthy();
    expect(screen.getByText('Bash')).toBeTruthy();
    const sessionActivityCard = screen.getByText('Session activity').closest('[data-slot="card"]');
    expect(sessionActivityCard?.getAttribute('data-slot')).toBe('card');
    const groupByTrigger = screen.getByRole('combobox', { name: 'Group by' });
    expect(groupByTrigger.dataset.slot).toBe('select-trigger');
    expect(groupByTrigger.className.includes('bg-secondary')).toBe(false);
    const turnsCard = screen.getByText('Turns').closest('[data-slot="card"]');
    expect(turnsCard).toBeTruthy();
    expect(turnsCard?.querySelector('[data-slot="tabs-list"]')?.getAttribute('data-slot')).toBe('tabs-list');
    expect(
      within(turnsCard as HTMLElement)
        .getByRole('tab', { name: 'p50' })
        .getAttribute('aria-selected'),
    ).toBe('true');
    expect(within(turnsCard as HTMLElement).getByRole('tabpanel').textContent).toContain('2');

    fireEvent.click(within(turnsCard as HTMLElement).getByRole('tab', { name: 'p95' }));

    expect(
      within(turnsCard as HTMLElement)
        .getByRole('tab', { name: 'p95' })
        .getAttribute('aria-selected'),
    ).toBe('true');
    expect(within(turnsCard as HTMLElement).getByRole('tabpanel').textContent).toContain('4');
    expect(
      api.requests.some(
        (request) =>
          request.url === '/api/organizations/org_test/analytics/sessions/overview?agent_id=agent_detail123456',
      ),
    ).toBe(true);
    expect(
      api.requests.some(
        (request) =>
          request.url ===
          '/api/organizations/org_test/analytics/sessions/timeseries?agent_id=agent_detail123456&group_by=agent_version',
      ),
    ).toBe(true);
  });

  test('renders missing agent and missing version states with shared alerts', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_missing123456');
    mockAgentsApi([]);
    render(<ManagedAgentsPage section="agents" />);

    const missingAgentBreadcrumb = await screen.findByRole('navigation', { name: 'Breadcrumb' });
    expect(missingAgentBreadcrumb.dataset.slot).toBe('breadcrumb');
    expect(within(missingAgentBreadcrumb).getByRole('link', { name: 'Agents' }).getAttribute('href')).toBe(
      '/workspaces/default/agents',
    );
    expect(missingAgentBreadcrumb.querySelector('[data-slot="breadcrumb-page"]')?.textContent).toBe('Error');
    const missingAgentAlert = await screen.findByRole('alert');
    expect(missingAgentAlert.dataset.slot).toBe('alert');
    expect(missingAgentAlert.textContent).toContain('not found');

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_detail123456?version_id=99');
    mockAgentsApi([
      {
        id: 'agent_detail123456',
        name: 'Current agent',
        version: 2,
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Current agent' })).toBeTruthy();
    const missingVersionAlert = await screen.findByRole('alert');
    expect(missingVersionAlert.dataset.slot).toBe('alert');
    expect(missingVersionAlert.textContent).toContain('Agent version not found');
  });

  test('uses a shared collapsible trigger for agent deployment rows', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_detail123456?tab=deployments');
    mockAgentsApi(
      [
        {
          id: 'agent_detail123456',
          name: 'Current agent',
          version: 2,
        },
      ],
      {
        deployments: [
          {
            id: 'dep_detail123456',
            agentId: 'agent_detail123456',
            version: 2,
            name: 'Nightly run',
          },
        ],
      },
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Current agent' })).toBeTruthy();
    const deploymentTrigger = await screen.findByRole('button', { name: /Nightly run/i });
    expect(deploymentTrigger.dataset.slot).toBe('collapsible-trigger');
    expect(deploymentTrigger.getAttribute('aria-expanded')).toBe('false');
    expect(deploymentTrigger.getAttribute('aria-controls')).toBe('agent-deployment-panel-dep_detail123456');
    expect(screen.getByRole('button', { name: 'Previous page' }).className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('button', { name: 'Next page' }).className.includes('bg-secondary')).toBe(false);

    fireEvent.click(deploymentTrigger);

    expect(deploymentTrigger.getAttribute('aria-expanded')).toBe('true');
    expect(new URL(window.location.href).searchParams.get('deployment')).toBe('dep_detail123456');
    const deploymentsCard = deploymentTrigger.closest('[data-slot="card"]');
    expect(deploymentsCard?.getAttribute('data-slot')).toBe('card');
    const viewSessions = await screen.findByRole('link', { name: 'View sessions' });
    expect(viewSessions.getAttribute('href')).toBe('/workspaces/default/sessions?deployment_id=dep_detail123456');
    expect(viewSessions.dataset.slot).toBe('button');
    const deploymentVersionBadge = screen.getAllByText('v2').find((node) => node.closest('[data-slot="badge"]'));
    expect(deploymentVersionBadge?.closest('[data-slot="badge"]')?.getAttribute('data-slot')).toBe('badge');

    fireEvent.click(deploymentTrigger);
    expect(deploymentTrigger.getAttribute('aria-expanded')).toBe('false');
    expect(new URL(window.location.href).searchParams.get('deployment')).toBeNull();
  });

  test('opens the edit modal as a YAML config editor', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_edit123456');
    mockAgentsApi([
      {
        id: 'agent_edit123456',
        name: 'Editable agent',
        version: 2,
        description: 'Before',
        system: 'Original prompt',
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Editable agent' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));

    const dialog = screen.getByRole('dialog', { name: 'Edit agent' });
    expect(within(dialog).getByRole('combobox', { name: 'Code format' })).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Close' })).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Copy code' })).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Save new version' })).toBeTruthy();
    const configTextbox = within(dialog).getByRole('textbox', { name: 'Agent configuration' });
    expect(configTextbox.closest('.cm-editor')?.className).toContain('cm-editor');
    const configCard = configTextbox.closest('[data-slot="card"]') as HTMLElement | null;
    expect(configCard?.dataset.slot).toBe('card');
    expect(configCard?.className).toContain('bg-card');
    expect(configCard?.className).not.toContain('bg-muted');
    expect(dialog.textContent).toContain('name: Editable agent');
    fireEvent.click(within(dialog).getByRole('combobox', { name: 'Code format' }));
    expect(screen.getByRole('option', { name: 'YAML' }).getAttribute('aria-selected')).toBe('true');

    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Edit agent' })).toBeNull();
  });

  test('closes the edit modal from the close button and backdrop', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_edit_close');
    mockAgentsApi([
      {
        id: 'agent_edit_close',
        name: 'Closable agent',
        version: 2,
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Closable agent' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    fireEvent.click(screen.getByRole('button', { name: 'Close' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Edit agent' })).toBeNull());

    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(screen.getByRole('dialog', { name: 'Edit agent' })).toBeTruthy();
    const overlay = document.querySelector('[data-slot="dialog-overlay"]');
    expect(overlay).toBeTruthy();
    fireEvent.click(overlay as Element);
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Edit agent' })).toBeNull());
  });

  test('validates JSON edit config and saves a canonicalized new version body', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_edit123456');
    const api = mockAgentsApi([
      {
        id: 'agent_edit123456',
        name: 'Editable agent',
        version: 2,
        description: 'Before',
        model: { id: 'claude-sonnet-4-6', speed: 'standard' },
        system: 'Original prompt',
        metadata: { team: 'ops' },
        tools: [{ type: 'agent_toolset_20250301', configs: [{ type: 'always_allow', tool_name: 'bash' }] }],
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Editable agent' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));

    const dialog = screen.getByRole('dialog', { name: 'Edit agent' });
    await selectManagedComboboxOption(dialog, 'Code format', 'JSON');
    await waitFor(() =>
      expect(within(dialog).getByRole('combobox', { name: 'Code format' }).textContent).toContain('JSON'),
    );

    setAgentConfigEditorValue(dialog, '{', 'Agent configuration');

    expect(within(dialog).getByText(/JSON is not valid/i)).toBeTruthy();
    expect(within(dialog).getByRole('button', { name: 'Save new version' }).hasAttribute('disabled')).toBe(true);

    setAgentConfigEditorValue(
      dialog,
      JSON.stringify(
        {
          name: 'Updated agent',
          description: 'After',
          model: { id: 'claude-opus-4-8', speed: 'fast' },
          system: 'Updated prompt',
          mcp_servers: [],
          tools: [{ type: 'agent_toolset_20250301', configs: [{ type: 'always_allow', tool_name: 'bash' }] }],
          skills: [],
          metadata: { team: 'support' },
          multiagent: null,
        },
        null,
        2,
      ),
      'Agent configuration',
    );
    expect(within(dialog).queryByText(/JSON is not valid/i)).toBeNull();

    fireEvent.click(within(dialog).getByRole('button', { name: 'Save new version' }));

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === 'POST' && request.url === '/v1/agents/agent_edit123456?beta=true',
        ),
      ).toBe(true),
    );
    const updateRequest = api.requests.find(
      (request) => request.method === 'POST' && request.url === '/v1/agents/agent_edit123456?beta=true',
    );
    expect(updateRequest?.body?.version).toBe(2);
    expect(updateRequest?.body?.name).toBe('Updated agent');
    expect(updateRequest?.body?.description).toBe('After');
    expect(updateRequest?.body?.model).toEqual({ id: 'claude-opus-4-8', speed: 'fast' });
    expect((updateRequest?.body?.metadata as Record<string, string>).team).toBe('support');
    const updatedToolset = (updateRequest?.body?.tools as Array<Record<string, unknown>>)[0];
    expect(updatedToolset.type).toBe('agent_toolset_20260401');
    expect((updatedToolset.configs as Array<Record<string, unknown>>)[0]).toEqual({
      type: 'always_allow',
      name: 'bash',
    });
    expect(await screen.findByRole('heading', { name: 'Updated agent' })).toBeTruthy();
  });

  test('shows friendly edit save errors for conflict and invalid configuration responses', async () => {
    for (const [status, message] of [
      [
        409,
        'This agent was updated elsewhere while you were editing. Close and reopen the editor to start from the latest version.',
      ],
      [400, 'Invalid agent configuration. Check your editor for errors.'],
    ] as const) {
      resetTestDom(`https://oma.duck.ai/workspaces/default/agents/agent_edit_error${status}`);
      mockAgentsApi(
        [
          {
            id: `agent_edit_error${status}`,
            name: `Editable agent ${status}`,
            version: 2,
          },
        ],
        { agentUpdateErrorStatus: status },
      );
      render(<ManagedAgentsPage section="agents" />);

      expect(await screen.findByRole('heading', { name: `Editable agent ${status}` })).toBeTruthy();
      fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
      const dialog = screen.getByRole('dialog', { name: 'Edit agent' });
      fireEvent.click(within(dialog).getByRole('button', { name: 'Save new version' }));

      expect(await within(dialog).findByText(message)).toBeTruthy();
      cleanup();
    }
  });

  test('saves the edit modal with Cmd+S', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents/agent_edit_shortcut');
    const api = mockAgentsApi([
      {
        id: 'agent_edit_shortcut',
        name: 'Shortcut agent',
        version: 3,
      },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByRole('heading', { name: 'Shortcut agent' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Edit' }));
    expect(screen.getByRole('dialog', { name: 'Edit agent' })).toBeTruthy();

    fireEvent.keyDown(document, { key: 's', metaKey: true });

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.method === 'POST' && request.url === '/v1/agents/agent_edit_shortcut?beta=true',
        ),
      ).toBe(true),
    );
    const updateRequest = api.requests.find(
      (request) => request.method === 'POST' && request.url === '/v1/agents/agent_edit_shortcut?beta=true',
    );
    expect(updateRequest?.body?.version).toBe(3);
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Edit agent' })).toBeNull());
  });

  test('queries agents for the active workspace and refetches when it changes', async () => {
    resetTestDom('https://oma.duck.ai/agents');
    const api = mockAgentsApi([]);
    const { rerender } = render(
      <WorkspaceContext.Provider value={workspaceContextValue('default')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.method === 'GET' &&
            request.url === '/v1/agents?beta=true&limit=20&include_archived=false' &&
            request.headers['x-workspace-id'] === 'default',
        ),
      ).toBe(true),
    );

    rerender(
      <WorkspaceContext.Provider value={workspaceContextValue('wrkspc_foo')}>
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.method === 'GET' &&
            request.url === '/v1/agents?beta=true&limit=20&include_archived=false' &&
            request.headers['x-workspace-id'] === 'wrkspc_foo',
        ),
      ).toBe(true),
    );
  });

  test('uses the agents route workspace before stored workspace state catches up', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/wrkspc_foo/agents');
    const api = mockAgentsApi([]);
    const selectedWorkspaceIds: string[] = [];
    render(
      <WorkspaceContext.Provider
        value={{
          ...workspaceContextValue('default'),
          selectWorkspace: (workspaceId) => selectedWorkspaceIds.push(workspaceId),
        }}
      >
        <ManagedAgentsPage section="agents" />
      </WorkspaceContext.Provider>,
    );

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) =>
            request.method === 'GET' &&
            request.url === '/v1/agents?beta=true&limit=20&include_archived=false' &&
            request.headers['x-workspace-id'] === 'wrkspc_foo',
        ),
      ).toBe(true),
    );
    await waitFor(() => expect(selectedWorkspaceIds).toContain('wrkspc_foo'));
  });

  test('paginates agents twenty rows at a time with the backend page cursor', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi(
      Array.from({ length: 21 }, (_, index) => ({
        id: `agent_page${String(index + 1).padStart(2, '0')}123456`,
        name: index === 0 ? 'First agent' : index === 20 ? 'Twenty first agent' : `Agent ${index + 1}`,
      })),
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('First agent')).toBeTruthy();
    expect(screen.queryByText('Twenty first agent')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByText('Twenty first agent')).toBeTruthy();
    expect(screen.queryByText('First agent')).toBeNull();
    expect(api.requests.some((request) => request.method === 'GET' && request.url.includes('page=next_cursor'))).toBe(
      true,
    );

    fireEvent.click(screen.getByRole('button', { name: 'Previous page' }));

    expect(await screen.findByText('First agent')).toBeTruthy();
  });

  test('shows a clear button after typing in the agents search field', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([
      { id: 'agent_one123456', name: 'First agent' },
      { id: 'agent_two123456', name: 'Second agent' },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('First agent')).toBeTruthy();

    const searchInput = screen.getByPlaceholderText('Search by name or exact ID') as HTMLInputElement;
    fireEvent.change(searchInput, { target: { value: 'second' } });

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/agents:search?beta=true')).toBe(true),
    );
    const searchRequest = api.requests.find((request) => request.url === '/v1/agents:search?beta=true');
    expect(searchRequest?.method).toBe('POST');
    expect(searchRequest?.body?.name).toBe('second');
    expect(searchRequest?.body?.limit).toBe(100);
    expect(await screen.findByText('Second agent')).toBeTruthy();
    await waitFor(() => expect(screen.queryByText('First agent')).toBeNull());

    fireEvent.click(screen.getByRole('button', { name: 'Clear Search by name or exact ID' }));

    expect(searchInput.value).toBe('');
    expect(screen.queryByRole('button', { name: 'Clear Search by name or exact ID' })).toBeNull();
    expect(await screen.findByText('First agent')).toBeTruthy();
    expect(document.activeElement).toBe(searchInput);

    fireEvent.change(searchInput, { target: { value: 'first' } });
    expect(screen.getByRole('button', { name: 'Clear Search by name or exact ID' })).toBeTruthy();
    fireEvent.keyDown(searchInput, { key: 'Escape' });
    expect(searchInput.value).toBe('');
  });

  test('retrieves an exact agent ID from the search field', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([
      { id: 'agent_exact123456789012345', name: 'Exact match agent' },
      { id: 'agent_other123456789012345', name: 'Other agent' },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Exact match agent')).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('Search by name or exact ID'), {
      target: { value: 'agent_exact123456789012345' },
    });

    await waitFor(() =>
      expect(
        api.requests.some(
          (request) => request.url === '/v1/agents/agent_exact123456789012345?beta=true' && request.method === 'GET',
        ),
      ).toBe(true),
    );
    expect(api.requests.some((request) => request.url === '/v1/agents:search?beta=true')).toBe(false);
    expect(await screen.findByText('Exact match agent')).toBeTruthy();
    await waitFor(() => expect(screen.queryByText('Other agent')).toBeNull());
  });

  test('treats short agent-like text as a name search', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([{ id: 'agent_shortlookup123456', name: 'agent_short123456 lookup' }]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('agent_short123456 lookup')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('Search by name or exact ID'), {
      target: { value: 'agent_short123456' },
    });

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/agents:search?beta=true')).toBe(true),
    );
    expect(api.requests.some((request) => request.url === '/v1/agents/agent_short123456?beta=true')).toBe(false);
    expect(await screen.findByText('agent_short123456 lookup')).toBeTruthy();
  });

  test('aggregates name search pages and paginates results locally', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi(
      Array.from({ length: 101 }, (_, index) => ({
        id: `agent_aggregate${String(index + 1).padStart(2, '0')}123456`,
        name: `Aggregate agent ${index + 1}`,
      })),
      { agentsSearchPageSize: 40 },
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Aggregate agent 1')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('Search by name or exact ID'), { target: { value: 'aggregate' } });

    await waitFor(() => {
      expect(api.requests.filter((request) => request.url === '/v1/agents:search?beta=true').length).toBe(3);
    });
    const searchRequests = api.requests.filter((request) => request.url === '/v1/agents:search?beta=true');
    expect(searchRequests[0]?.body?.limit).toBe(100);
    expect(searchRequests[1]?.body?.limit).toBe(60);
    expect(searchRequests[1]?.body?.page).toBe('search_40');
    expect(searchRequests[2]?.body?.limit).toBe(20);
    expect(searchRequests[2]?.body?.page).toBe('search_80');
    const truncatedAlert = await screen.findByRole('alert');
    expect(truncatedAlert.getAttribute('data-slot')).toBe('alert');
    expect(truncatedAlert.textContent).toContain(
      "Couldn't search every agent. Narrow the search or paste an exact ID.",
    );
    expect(screen.getByText('Aggregate agent 20')).toBeTruthy();
    expect(screen.queryByText('Aggregate agent 21')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));

    expect(await screen.findByText('Aggregate agent 21')).toBeTruthy();
    expect(screen.queryByText('Aggregate agent 1')).toBeNull();
    expect(api.requests.filter((request) => request.url === '/v1/agents:search?beta=true').length).toBe(3);
  });

  test('shows the incomplete-search empty state when aggregated search is truncated before date filtering', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    mockAgentsApi(
      Array.from({ length: 101 }, (_, index) => ({
        id: `agent_ancient${String(index + 1).padStart(3, '0')}123456`,
        name: `Ancient agent ${index + 1}`,
        created_at: '2020-01-01T00:00:00.000Z',
        updated_at: '2020-01-01T00:00:00.000Z',
      })),
    );
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Ancient agent 1')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Created All time' }));
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Last 7 days' }));
    expect(await screen.findByText('No matching agents')).toBeTruthy();

    fireEvent.change(screen.getByPlaceholderText('Search by name or exact ID'), { target: { value: 'ancient' } });

    expect(
      await screen.findByText("Couldn't search every agent. Narrow the search or paste an exact ID."),
    ).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Reset filters' })).toBeNull();
  });

  test('shows agents-specific empty and filtered states', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    mockAgentsApi([]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('No agents yet')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Get started with agents' }));
    expect(screen.getByRole('dialog', { name: 'Create agent' })).toBeTruthy();
    fireEvent.keyDown(document, { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Create agent' })).toBeNull();

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    mockAgentsApi([{ id: 'agent_visible123456', name: 'Visible agent' }]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Visible agent')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('Search by name or exact ID'), { target: { value: 'missing' } });
    expect(await screen.findByText('No matching agents')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Reset filters' }));
    expect(await screen.findByText('Visible agent')).toBeTruthy();
  });

  test('shows retry states for list and search failures', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const listApi = mockAgentsApi([{ id: 'agent_retry123456', name: 'Retry agent' }], { agentsListErrorOnce: true });
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Could not load agents')).toBeTruthy();
    expect(screen.getByText('agents list failed')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Retry agent')).toBeTruthy();
    expect(listApi.requests.filter((request) => request.url.startsWith('/v1/agents?')).length).toBeGreaterThanOrEqual(
      2,
    );

    cleanup();
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const searchApi = mockAgentsApi([{ id: 'agent_searchretry123456', name: 'Search retry agent' }], {
      agentsSearchErrorOnce: true,
    });
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Search retry agent')).toBeTruthy();
    fireEvent.change(screen.getByPlaceholderText('Search by name or exact ID'), { target: { value: 'retry' } });
    expect(await screen.findByText('Search failed')).toBeTruthy();
    expect(screen.getByText('agents search failed')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Retry' }));
    expect(await screen.findByText('Search retry agent')).toBeTruthy();
    expect(
      searchApi.requests.filter((request) => request.url === '/v1/agents:search?beta=true').length,
    ).toBeGreaterThanOrEqual(2);
  });

  test('opens created and status filters and refetches agents with selected values', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const recentCreatedAt = new Date(Date.now() + 60_000).toISOString();
    const api = mockAgentsApi([
      { id: 'agent_recent123456', name: 'Recent active agent', created_at: recentCreatedAt },
      {
        id: 'agent_archived123456',
        name: 'Archived agent',
        archived_at: recentCreatedAt,
        created_at: recentCreatedAt,
      },
      { id: 'agent_old123456', name: 'Old active agent', created_at: '2020-01-01T00:00:00.000Z' },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Recent active agent')).toBeTruthy();
    expect(screen.getByText('Old active agent')).toBeTruthy();
    expect(screen.queryByText('Archived agent')).toBeNull();

    fireEvent.click(screen.getByRole('button', { name: 'Status Active' }));
    const statusMenu = screen
      .getByRole('menuitemradio', { name: 'Active' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(statusMenu?.className).toContain('bg-popover');
    expect(statusMenu?.className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('menuitemradio', { name: 'Active' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'All' }));

    expect(await screen.findByText('Archived agent')).toBeTruthy();
    expect(api.requests.some((request) => request.url.includes('include_archived=true'))).toBe(true);

    fireEvent.click(screen.getByRole('button', { name: 'Created All time' }));
    const createdMenu = screen
      .getByRole('menuitemradio', { name: 'All time' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(createdMenu?.className).toContain('bg-popover');
    expect(createdMenu?.className.includes('bg-secondary')).toBe(false);
    expect(screen.getByRole('menuitemradio', { name: 'All time' }).getAttribute('aria-checked')).toBe('true');
    fireEvent.click(screen.getByRole('menuitemradio', { name: 'Last 7 days' }));

    await waitFor(() => {
      expect(screen.queryByText('Old active agent')).toBeNull();
      expect(screen.getByText('Recent active agent')).toBeTruthy();
    });
    expect(api.requests.some((request) => request.url.includes('created_at%5Bgte%5D='))).toBe(true);
  }, 10_000);

  test('does not render selection controls or a batch archive bar', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    mockAgentsApi([
      { id: 'agent_one123456', name: 'First agent' },
      { id: 'agent_two123456', name: 'Second agent' },
      { id: 'agent_three123456', name: 'Third agent' },
    ]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('First agent')).toBeTruthy();
    expect(screen.queryByRole('checkbox', { name: 'Select all agents' })).toBeNull();
    expect(screen.queryByText('3 selected')).toBeNull();
    expect(screen.getAllByRole('button', { name: 'More actions' }).length).toBe(3);
  });

  test('archives a single agent from the row action menu', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([{ id: 'agent_menu123456', name: 'Menu agent' }]);
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Menu agent')).toBeTruthy();

    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    const actionMenu = screen
      .getByRole('menuitem', { name: 'Archive agent' })
      .closest('[data-slot="dropdown-menu-content"]');
    expect(actionMenu?.className).toContain('bg-popover');
    expect(actionMenu?.className.includes('bg-secondary')).toBe(false);
    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive agent' }));

    const dialog = screen.getByRole('alertdialog', { name: 'Archive agent' });
    expect(dialog).toBeTruthy();
    expect(
      screen.getByText('This agent will be hidden from the default view. Sessions that reference it keep working.'),
    ).toBeTruthy();
    fireEvent.click(within(dialog).getByRole('button', { name: 'Archive' }));

    await waitFor(() =>
      expect(api.requests.some((request) => request.url === '/v1/agents/agent_menu123456/archive?beta=true')).toBe(
        true,
      ),
    );
    await waitFor(() => expect(screen.queryByText('Menu agent')).toBeNull());
  });

  test('shows a shared alert when archiving an agent fails', async () => {
    resetTestDom('https://oma.duck.ai/workspaces/default/agents');
    const api = mockAgentsApi([{ id: 'agent_archiveerror123456', name: 'Archive error agent' }], {
      agentArchiveErrorOnce: true,
    });
    render(<ManagedAgentsPage section="agents" />);

    expect(await screen.findByText('Archive error agent')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'More actions' }));
    fireEvent.click(screen.getByRole('menuitem', { name: 'Archive agent' }));
    fireEvent.click(
      within(screen.getByRole('alertdialog', { name: 'Archive agent' })).getByRole('button', { name: 'Archive' }),
    );

    const alert = await screen.findByRole('alert');
    expect(alert.getAttribute('data-slot')).toBe('alert');
    expect(alert.textContent).toContain('agent archive failed');
    expect(screen.getByText('Archive error agent')).toBeTruthy();
    expect(
      api.requests.some((request) => request.url === '/v1/agents/agent_archiveerror123456/archive?beta=true'),
    ).toBe(true);
  });
}
