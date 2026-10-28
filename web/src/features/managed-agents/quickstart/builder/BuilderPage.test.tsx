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
    String(input).endsWith('/llm_providers')
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
  expect(writes(api.requests)).toHaveLength(0);
  cleanup();
  renderManagedAgentsPage('quickstart');
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
  expect(await screen.findByText('Which websites should the environment access?', { selector: 'label' })).toBeTruthy();
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
  await screen.findByText('Missing quantity was invented. Choose a repair.', { selector: 'label' });
  fireEvent.click(screen.getByRole('button', { name: 'Continue', exact: true }));
  await screen.findByRole('button', { name: 'Use in an application' });
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
  expect(screen.getByRole('heading', { name: 'What would you like to build?' })).toBe(heading);
  expect(screen.getByRole('tablist', { name: 'Build method' })).toBe(modes);
  expect(screen.queryByRole('navigation', { name: 'Start with your first Agent' })).toBeNull();
  const radios = screen.getAllByRole('radio');
  expect(radios.slice(0, 6).map((radio) => radio.getAttribute('aria-label'))).toEqual(names);
  fireEvent.click(screen.getByRole('radio', { name: /Structured extractor/ }));
  expect(screen.getByRole('radio', { name: /Structured extractor/ }).getAttribute('aria-checked')).toBe('true');
  expect(writes(api.requests)).toHaveLength(0);
  fireEvent.click(screen.getByRole('button', { name: 'Start configuring' }));
  expect(screen.getByRole('navigation', { name: 'Start with your first Agent' })).toBeTruthy();
  expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Structured extractor');
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toContain('valid JSON');
  expect(writes(api.requests)).toHaveLength(0);
});
