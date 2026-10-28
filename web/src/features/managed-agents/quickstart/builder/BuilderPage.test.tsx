import { builderStorageKey, newBuilderState } from './model';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import { installSessionRuntime } from '../sessionRuntime.test-utils';
import { afterEach, expect, test } from 'bun:test';
import {
  fireEvent,
  mockAgentsApi,
  renderManagedAgentsPage,
  resetManagedAgentsTestState,
  resetTestDom,
  screen,
  waitFor,
  cleanup,
  act,
  jsonResponse,
  codeBlockContaining,
} from '../../ManagedAgentsPage.test-utils';

function toolStream(name: string, input: unknown) {
  return [
    {
      type: 'content_block_start',
      index: 0,
      content_block: { type: 'tool_use', id: crypto.randomUUID(), name, input },
    },
    { type: 'message_delta', delta: { stop_reason: 'tool_use' } },
    { type: 'message_stop' },
  ]
    .map((data) => `data: ${JSON.stringify(data)}\n\n`)
    .join('');
}
function setup(turns: string[]) {
  const calls: Record<string, unknown>[] = [];
  const api = mockAgentsApi([], {
    quickstartStream: (body) => {
      calls.push(body);
      return turns.shift() ?? toolStream('offer_next_step', {});
    },
  });
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) =>
    String(input).endsWith('/llm_providers') && !String(input).includes('/workspaces/')
      ? jsonResponse([])
      : String(input).endsWith('/llm_providers')
        ? jsonResponse([
            {
              id: 'provider',
              slug: 'provider',
              api_backend: 'messages',
              models: [{ id: 'claude-sonnet-4-6' }, { id: 'claude-opus-4-8' }],
            },
          ])
        : baseFetch(input, init)) as typeof fetch;
  return { ...api, calls };
}
function send(text = 'Extract company, product and quantity from orders. Missing fields should be null.') {
  fireEvent.change(screen.getByRole('textbox', { name: 'Builder message' }), { target: { value: text } });
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
}
function writes(requests: Array<{ method: string; url: string; body?: unknown }>) {
  return requests.filter((request) => request.method === 'POST' && /\/v1\/agents(?:\/[^?]+)?\?/.test(request.url));
}
afterEach(() => {
  resetManagedAgentsTestState();
  window.sessionStorage.clear();
});
test('a description proposes before writing; refresh preserves the pending proposal without replay', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([
    toolStream('build_agent_config', { name: 'Order extractor', system: 'Extract fields. Missing fields are null.' }),
  ]);
  renderManagedAgentsPage('quickstart');
  await screen.findByRole('textbox', { name: 'Builder message' });
  send();
  await screen.findByRole('button', { name: 'Confirm', exact: true });
  expect(screen.queryByRole('tablist', { name: 'Build method' })).toBeNull();
  expect(screen.queryByRole('heading', { name: 'What would you like to build?' })).toBeNull();
  expect(writes(api.requests)).toHaveLength(0);
  cleanup();
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  await screen.findByRole('button', { name: 'Confirm', exact: true });
  expect(api.calls).toHaveLength(1);
  expect(writes(api.requests)).toHaveLength(0);
});
test('template creates immediately; confirmed rename retains saved ID, instructions and version guard', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([
    toolStream('offer_next_step', {}),
    toolStream('build_agent_config', { name: 'New extractor name' }),
    toolStream('offer_next_step', {}),
  ]);
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: /Structured extractor/ }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  expect(writes(api.requests)).toHaveLength(1);
  expect(screen.getByRole('group', { name: 'Saved result' }).textContent).toContain('Structured extractor');
  expect(screen.getAllByRole('article', { name: 'User message' })).toHaveLength(1);
  expect(screen.getByRole('article', { name: 'User message' }).textContent).toBe(
    (writes(api.requests)[0].body as { description: string }).description,
  );
  expect(screen.getByRole('link', { name: 'View configuration' }).textContent).toContain('Structured extractor');
  expect(screen.getByText('No resources yet')).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Export configuration' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Back to quickstart' }));
  expect(screen.getByRole('heading', { name: 'What would you like to build?' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Continue previous build' }));
  send('Change its name to New extractor name.');
  await screen.findByRole('button', { name: 'Confirm', exact: true });
  expect(writes(api.requests)).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Confirm', exact: true }));
  await waitFor(() => expect(writes(api.requests)).toHaveLength(2));
  const [initial, update] = writes(api.requests);
  expect(update.url).toContain('/agents/agent_created123456?');
  const initialBody = initial.body as Record<string, unknown>;
  const updateBody = update.body as Record<string, unknown>;
  expect(updateBody.name).toBe('New extractor name');
  expect(updateBody.system).toBe(initialBody.system);
  expect(updateBody.version).toBe(1);
});
test('new environment uses conversational questions without a creation form', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  setup([
    toolStream('offer_next_step', {}),
    toolStream('pick_environment', {}),
    toolStream('ask_user_questions', {
      questions: [
        {
          question: 'Which websites should the environment access?',
          header: 'Network',
          options: [{ label: 'Any website' }, { label: 'Only company websites' }],
        },
      ],
    }),
  ]);
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: /Structured extractor/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Continue', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'New environment', exact: true }));
  expect(await screen.findByText('Which websites should the environment access?')).toBeTruthy();
  expect(screen.queryByRole('textbox', { name: 'Name', exact: true })).toBeNull();
  expect(screen.queryByText('Vaults')).toBeNull();
});

test('lost save response is recovered after refresh without repeating the write', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([toolStream('offer_next_step', {})]);
  const baseFetch = globalThis.fetch;
  let lost = false;
  globalThis.fetch = (async (input, init) => {
    const response = await baseFetch(input, init);
    if (!lost && String(input).startsWith('/v1/agents?') && init?.method === 'POST') {
      lost = true;
      throw new TypeError('Response lost');
    }
    return response;
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: /Structured extractor/ }));
  await screen.findByRole('button', { name: 'Check saved result' });
  cleanup();
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Check saved result' }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  expect(writes(api.requests)).toHaveLength(1);
});
test('test first reply does not close the session; explicit end returns actual events and empty repairs continue', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([
    toolStream('offer_next_step', {}),
    toolStream('pick_environment', {}),
    toolStream('offer_next_step', {}),
    toolStream('agent_ready', { suggested_first_message: 'Extract this order: Star Company ordered printers.' }),
    toolStream('await_test_run', { until: 'first_message' }),
    toolStream('await_test_run', { until: 'session_closed' }),
    toolStream('ask_user_questions', {
      questions: [
        {
          header: 'Repair',
          question: 'Missing quantity was invented. Choose a repair.',
          multiSelect: true,
          options: [{ label: 'Return null for missing quantity' }, { label: 'Rerun as-is' }, { label: 'Continue' }],
        },
      ],
    }),
    toolStream('offer_next_step', {}),
  ]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: /Structured extractor/ }));
  fireEvent.click(await screen.findByRole('button', { name: 'Continue', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Option environment', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Continue', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Start test', exact: true }));
  await screen.findByRole('textbox', { name: 'Test message' });
  await waitFor(() => expect(screen.getByRole('button', { name: 'End test' }).hasAttribute('disabled')).toBe(false));
  await waitFor(() =>
    expect(
      screen.getAllByRole('button', { name: 'Send', exact: true }).some((button) => !button.hasAttribute('disabled')),
    ).toBe(true),
  );
  fireEvent.click(
    screen.getAllByRole('button', { name: 'Send', exact: true }).find((button) => !button.hasAttribute('disabled'))!,
  );
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  await act(async () => runtime.reply('Quantity: 1.'));
  await waitFor(() => expect(screen.getByRole('button', { name: 'End test' }).hasAttribute('disabled')).toBe(false));
  expect(screen.queryByText('Missing quantity was invented. Choose a repair.')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'End test' }));
  await screen.findByText('Missing quantity was invented. Choose a repair.');
  fireEvent.click(screen.getByRole('button', { name: 'Skip', exact: true }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  expect(screen.queryByRole('button', { name: 'Use in an application' })).toBeNull();
  expect(api.calls.some((body) => JSON.stringify(body.messages).includes('Quantity: 1.'))).toBe(true);
  expect(writes(api.requests)).toHaveLength(1);
});

test('deployment integration uses the OMA deployment_runs endpoint with actual saved IDs', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([]);
  const value = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'claude-sonnet-4-6'));
  value.initialized = true;
  value.step = 'integrate';
  value.agent = { id: 'agent_final', name: 'Orders', version: 4 };
  value.environment = { id: 'environment_final', name: 'Restricted' };
  value.savedConfig = value.config;
  value.testClosed = true;
  value.deployment = { id: 'deployment_final', name: 'Daily orders' };
  value.gate = { id: 'exit', name: 'show_integration_exits', input: {} };
  value.queue = [value.gate];
  window.sessionStorage.setItem(builderStorageKey('acct_managed_agents_test', 'default'), JSON.stringify(value));
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  await screen.findByText('Runs', { selector: 'h3' });
  expect(codeBlockContaining('/v1/deployment_runs?deployment_id=deployment_final').textContent).toContain('beta=true');
  expect(codeBlockContaining('/v1/sessions/SESSION_ID/events/stream').textContent).toContain('x-workspace-id: default');
  expect(api.calls).toHaveLength(0);
});

test('the start page follows the reviewed title, mode, composer and six-template order', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([]);
  renderManagedAgentsPage('quickstart');
  const heading = await screen.findByRole('heading', { name: 'What would you like to build?' });
  const modes = screen.getByRole('tablist', { name: 'Build method' });
  const composer = await screen.findByRole('textbox', { name: 'Builder message' });
  const templates = screen.getByLabelText('Quickstart templates');
  expect(Boolean(heading.compareDocumentPosition(modes) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
  expect(Boolean(modes.compareDocumentPosition(composer) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
  expect(Boolean(composer.compareDocumentPosition(templates) & Node.DOCUMENT_POSITION_FOLLOWING)).toBe(true);
  expect(templates.querySelectorAll('button')).toHaveLength(6);
  expect(screen.getByRole('button', { name: 'Code review', exact: true })).toBeTruthy();
  expect(screen.getByRole('button', { name: 'Data analysis', exact: true })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Incident commander', exact: true })).toBeNull();
  expect(screen.getAllByRole('textbox', { name: 'Builder message' })).toHaveLength(1);
  expect(writes(api.requests)).toHaveLength(0);
  fireEvent.click(screen.getByRole('tab', { name: 'Manual setup', exact: true }));
  expect(await screen.findByRole('button', { name: 'Start configuring' })).toBeTruthy();
  expect(screen.queryByRole('textbox', { name: 'Builder message' })).toBeNull();
});

test('both build methods share the header and templates, with manual selection separate from creation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([]);
  renderManagedAgentsPage('quickstart');
  await screen.findByRole('textbox', { name: 'Builder message' });
  const heading = screen.getByRole('heading', { name: 'What would you like to build?' });
  const modes = screen.getByRole('tablist', { name: 'Build method' });
  const names = Array.from(screen.getByLabelText('Quickstart templates').querySelectorAll('button')).map((button) =>
    button.getAttribute('aria-label'),
  );
  fireEvent.click(screen.getByRole('tab', { name: 'Manual setup', exact: true }));
  await screen.findByRole('button', { name: 'Start configuring' });
  expect(screen.getByRole('heading', { name: 'What would you like to build?' })).toHaveProperty(
    'textContent',
    heading.textContent,
  );
  expect(screen.getByRole('tablist', { name: 'Build method' })).toHaveProperty('textContent', modes.textContent);
  expect(screen.queryByRole('navigation', { name: 'Start with your first Agent' })).toBeNull();
  const radios = screen.getAllByRole('radio');
  expect(radios.slice(1, 7).map((radio) => radio.getAttribute('aria-label'))).toEqual(names);
  fireEvent.click(screen.getByRole('radio', { name: /Structured extractor/ }));
  expect(screen.getByRole('radio', { name: /Structured extractor/ }).getAttribute('aria-checked')).toBe('true');
  expect(writes(api.requests)).toHaveLength(0);
  fireEvent.click(screen.getByRole('button', { name: 'Start configuring' }));
  expect(screen.getByRole('navigation', { name: 'Start with your first Agent' })).toBeTruthy();
  expect(screen.queryByRole('tablist', { name: 'Build method' })).toBeNull();
  expect(screen.queryByRole('heading', { name: 'What would you like to build?' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Back', exact: true }));
  expect(screen.getByRole('tablist', { name: 'Build method' })).toBeTruthy();
  expect(screen.queryByRole('radio', { name: /Start from scratch/ })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Start configuring' }));
  expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Structured extractor');
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toContain('valid JSON');
  expect(writes(api.requests)).toHaveLength(0);
});

test('navigating to quickstart opens the entry and explicitly resumes saved history without replaying writes', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([toolStream('offer_next_step', {})]);
  const view = renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Deep research', exact: true }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  const saved = window.sessionStorage.getItem(builderStorageKey('acct_managed_agents_test', 'default'));
  await act(async () => view.router.navigate({ href: '/workspaces/default/agent-quickstart' }));
  expect(await screen.findByRole('heading', { name: 'What would you like to build?' })).toBeTruthy();
  expect(screen.getByRole('tablist', { name: 'Build method' })).toBeTruthy();
  expect(screen.queryByRole('group', { name: 'Saved result' })).toBeNull();
  expect(window.sessionStorage.getItem(builderStorageKey('acct_managed_agents_test', 'default'))).toBe(saved);
  fireEvent.click(screen.getByRole('button', { name: 'Continue previous build' }));
  expect(await screen.findByRole('group', { name: 'Saved result' })).toBeTruthy();
  expect(api.calls).toHaveLength(1);
  expect(writes(api.requests)).toHaveLength(1);
});

test('starting over requires an explicit choice and cancel keeps both the saved history and typed message', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([
    toolStream('offer_next_step', {}),
    toolStream('build_agent_config', { name: 'New builder', system: 'A new task.' }),
  ]);
  const view = renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Deep research', exact: true }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  await act(async () => view.router.navigate({ href: '/workspaces/default/agent-quickstart' }));
  await screen.findByRole('button', { name: 'Continue previous build' });
  send('Start a different task.');
  await screen.findByRole('alertdialog', { name: 'Start a new build?' });
  expect(api.calls).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Cancel', exact: true }));
  expect(screen.getByRole<HTMLTextAreaElement>('textbox', { name: 'Builder message' }).value).toBe(
    'Start a different task.',
  );
  expect(
    JSON.parse(window.sessionStorage.getItem(builderStorageKey('acct_managed_agents_test', 'default'))!).agent.name,
  ).toBe('Deep research');
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  fireEvent.click(await screen.findByRole('button', { name: 'Start new build', exact: true }));
  await screen.findByRole('button', { name: 'Confirm', exact: true });
  expect(screen.queryByRole('group', { name: 'Saved result' })).toBeNull();
  expect(screen.getByRole('article', { name: 'User message' }).textContent).toBe('Start a different task.');
  expect(writes(api.requests)).toHaveLength(1);
});

test('a used skill appears in resources without showing an empty state or adding unused credential items', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([]);
  const state = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'claude-sonnet-4-6'));
  state.initialized = true;
  state.agent = { id: 'agent_skilled', name: 'Skilled research', version: 1 };
  state.config.skills = [{ type: 'custom', skill_id: 'skill_used', version: '1' }];
  state.savedConfig = state.config;
  window.sessionStorage.setItem(builderStorageKey('acct_managed_agents_test', 'default'), JSON.stringify(state));
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  expect(screen.getByRole('link', { name: 'skill_used' }).getAttribute('href')).toBe(
    '/workspaces/default/skills/skill_used',
  );
  expect(screen.queryByText('No resources yet')).toBeNull();
  expect(document.querySelectorAll('a[href*="/vaults/"]')).toHaveLength(0);
  expect(writes(api.requests)).toHaveLength(0);
});

test('legacy tool continuation preserves auto results and remaining tools across a question and refresh', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const calls = [
    { id: 'schedule', name: 'flag_schedule_intent', input: { wants_schedule: true } },
    {
      id: 'question',
      name: 'ask_user_questions',
      input: {
        questions: [
          { header: 'Scope', question: 'Which sources?', options: [{ label: 'Documentation' }, { label: 'Issues' }] },
        ],
      },
    },
    { id: 'environments', name: 'list_environments', input: {} },
  ];
  const batch = [
    ...calls.map((call, index) => ({
      type: 'content_block_start',
      index,
      content_block: { type: 'tool_use', ...call },
    })),
    { type: 'message_delta', delta: { stop_reason: 'tool_use' } },
    { type: 'message_stop' },
  ]
    .map((event) => `data: ${JSON.stringify(event)}\n\n`)
    .join('');
  const done = [
    {
      type: 'content_block_start',
      index: 0,
      content_block: { type: 'text', text: 'I have the sources. Describe the report you need.' },
    },
    { type: 'message_delta', delta: { stop_reason: 'end_turn' } },
    { type: 'message_stop' },
  ]
    .map((event) => `data: ${JSON.stringify(event)}\n\n`)
    .join('');
  const api = setup([batch, done]);
  renderManagedAgentsPage('quickstart');
  await screen.findByRole('textbox', { name: 'Builder message' });
  send('Build a daily research assistant.');
  await screen.findByText('Which sources?');
  expect(api.calls).toHaveLength(1);
  fireEvent.change(screen.getByRole('textbox', { name: 'Builder message' }), {
    target: { value: 'Do not bypass this question.' },
  });
  expect(screen.getByRole('button', { name: 'Send', exact: true }).hasAttribute('disabled')).toBe(true);
  cleanup();
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  await screen.findByText('Which sources?');
  expect(api.calls).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'Skip', exact: true }));
  await screen.findByText('I have the sources. Describe the report you need.');
  expect(api.calls).toHaveLength(2);
  const messages = api.calls[1].messages as Array<{ role: string; content: unknown }>;
  const blocks = messages.flatMap((message) =>
    Array.isArray(message.content) ? (message.content as Array<Record<string, unknown>>) : [],
  );
  expect(blocks.filter((block) => block.type === 'tool_result').map((block) => block.tool_use_id)).toEqual([
    'schedule',
    'question',
    'environments',
  ]);
  expect(JSON.stringify(messages)).toContain('[Deployment schedule planned: yes.]');
  expect(screen.queryByText(/Current quickstart step/)).toBeNull();
  expect(writes(api.requests)).toHaveLength(0);
});

test('legacy free-text reply completes a pending proposal with the actual request before asking the model again', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([
    toolStream('build_agent_config', { name: 'Support', system: 'Draft replies.' }),
    toolStream('build_agent_config', { name: 'Read-only support', system: 'Only read tickets. Never send replies.' }),
  ]);
  renderManagedAgentsPage('quickstart');
  await screen.findByRole('textbox', { name: 'Builder message' });
  send('Build a support assistant.');
  await screen.findByRole('button', { name: 'Confirm', exact: true });
  send('Only read tickets. Never send replies.');
  await waitFor(() => expect(api.calls).toHaveLength(2));
  await screen.findByText('Read-only support', { selector: 'h1' });
  const messages = JSON.stringify(api.calls[1].messages);
  expect(messages).toContain('User sent a message instead:');
  expect(messages).toContain('Only read tickets. Never send replies.');
  expect(messages).toContain('[Current quickstart step:');
  expect(writes(api.requests)).toHaveLength(0);
});

test('legacy usage question and schedule-intent tool control the deployment branch without a second UI choice', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([
    toolStream('ask_user_questions', {
      questions: [
        {
          header: 'Usage',
          question: 'How will you use it?',
          options: [{ label: 'Schedule it' }, { label: 'Call from an application' }],
        },
      ],
    }),
    toolStream('flag_schedule_intent', { wants_schedule: true }),
    toolStream('offer_next_step', {}),
    toolStream('create_deployment', {
      name: 'Daily acceptance',
      cron_expression: '0 9 * * *',
      timezone: 'Asia/Shanghai',
      initial_message: 'Extract the supplied order',
    }),
  ]);
  const value = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'claude-sonnet-4-6'));
  value.initialized = true;
  value.step = 'session';
  value.agent = { id: 'agent_saved', name: 'Acceptance', version: 2 };
  value.savedConfig = value.config;
  value.environment = { id: 'environment_saved', name: 'Restricted' };
  value.testClosed = true;
  value.messages = [{ role: 'user', content: 'The test is closed and its result was correct.' }];
  window.sessionStorage.setItem(builderStorageKey('acct_managed_agents_test', 'default'), JSON.stringify(value));
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Continue', exact: true }));
  await screen.findByText('How will you use it?');
  fireEvent.click(screen.getByRole('radio', { name: /Schedule it/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Confirm', exact: true }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  expect(screen.queryByRole('button', { name: 'Run on a schedule' })).toBeNull();
  expect(screen.queryByRole('button', { name: 'Use in an application' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: 'Continue', exact: true }));
  await screen.findByRole('heading', { name: 'Create scheduled deployment' });
  expect(api.calls).toHaveLength(4);
  expect(api.requests.some((request) => request.method === 'POST' && request.url.includes('/deployments'))).toBe(false);
});

test('chat reply completes only the active gate and executes remaining tools with real results', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([toolStream('offer_next_step', {})]);
  const value = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'claude-sonnet-4-6'));
  const calls = [
    { id: 'proposal', name: 'build_agent_config', input: { name: 'Draft' } },
    { id: 'list', name: 'list_environments', input: {} },
    { id: 'pick', name: 'pick_environment', input: {} },
  ];
  value.initialized = true;
  value.agent = { id: 'saved', name: 'Saved', version: 1 };
  value.savedConfig = value.config;
  value.queue = calls;
  value.gate = calls[0];
  value.messages = [{ role: 'assistant', content: calls.map((call) => ({ type: 'tool_use', ...call })) }];
  window.sessionStorage.setItem(builderStorageKey('acct_managed_agents_test', 'default'), JSON.stringify(value));
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  send('Change the requested name');
  await screen.findByRole('button', { name: 'New environment', exact: true });
  const stored = JSON.parse(window.sessionStorage.getItem(builderStorageKey('acct_managed_agents_test', 'default'))!);
  expect(stored.queue.map((tool: { id: string }) => tool.id)).toEqual(['pick']);
  const results = stored.messages
    .flatMap((message: { content: unknown }) => (Array.isArray(message.content) ? message.content : []))
    .filter((block: { type: string }) => block.type === 'tool_result');
  expect(results.map((block: { tool_use_id: string }) => block.tool_use_id)).toEqual(['proposal', 'list']);
  expect(results[1].content).not.toContain('User replied');
  expect(api.calls).toHaveLength(0);
  expect(writes(api.requests)).toHaveLength(0);
});

test('pending writes can be abandoned only after a complete empty lookup, without replaying a write', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = setup([]);
  const baseFetch = globalThis.fetch;
  let lookup: 'network' | 'duplicate' | 'empty' = 'network';
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/sessions?')) {
      if (lookup === 'network') throw new TypeError('Lookup unavailable');
      return jsonResponse({
        data:
          lookup === 'duplicate'
            ? ['first', 'second'].map((id) => ({
                id,
                type: 'session',
                metadata: { quickstart_operation_id: 'unknown' },
              }))
            : [],
        next_page: null,
      });
    }
    return baseFetch(input, init);
  }) as typeof fetch;
  const value = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'claude-sonnet-4-6'));
  const call = { id: 'ready', name: 'agent_ready', input: {} };
  value.initialized = true;
  value.queue = [call];
  value.gate = call;
  value.operation = { id: 'unknown', kind: 'session', toolId: call.id };
  window.sessionStorage.setItem(builderStorageKey('acct_managed_agents_test', 'default'), JSON.stringify(value));
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Continue previous build' }));
  expect(screen.queryByRole('button', { name: 'Abandon pending save' })).toBeNull();
  fireEvent.click(await screen.findByRole('button', { name: 'Check saved result' }));
  await screen.findByText('Connection error.');
  expect(screen.queryByRole('button', { name: 'Abandon pending save' })).toBeNull();
  lookup = 'duplicate';
  fireEvent.click(screen.getByRole('button', { name: 'Check saved result' }));
  await screen.findByText(/No unique saved session/);
  expect(screen.queryByRole('button', { name: 'Abandon pending save' })).toBeNull();
  lookup = 'empty';
  fireEvent.click(screen.getByRole('button', { name: 'Check saved result' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Abandon pending save' }));
  await screen.findByRole('button', { name: 'Continue', exact: true });
  const stored = JSON.parse(window.sessionStorage.getItem(builderStorageKey('acct_managed_agents_test', 'default'))!);
  expect(stored.operation).toBeNull();
  expect(stored.queue).toEqual([]);
  expect(stored.messages.at(-1).content[0]).toMatchObject({ tool_use_id: 'ready', is_error: true });
  expect(api.calls).toHaveLength(0);
  expect(api.requests.some((request) => request.method === 'POST')).toBe(false);
});
