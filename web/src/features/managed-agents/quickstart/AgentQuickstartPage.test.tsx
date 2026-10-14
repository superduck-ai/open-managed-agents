import { afterEach, expect, test, spyOn } from 'bun:test';
import {
  fireEvent,
  mockAgentsApi,
  renderManagedAgentsPage,
  resetManagedAgentsTestState,
  resetTestDom,
  screen,
  waitFor,
  cleanup,
  selectManagedComboboxOption,
  codeBlockContaining,
  act,
  jsonResponse,
} from '../ManagedAgentsPage.test-utils';

afterEach(() => {
  resetManagedAgentsTestState();
  window.sessionStorage.clear();
});

test('refresh resets progress and the test conversation while keeping saved resources reusable', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  await act(async () => runtime.reply('Reply before refresh'));
  expect(await screen.findByText('Reply before refresh')).toBeTruthy();
  cleanup();
  renderManagedAgentsPage('quickstart');
  expect(await screen.findByRole('button', { name: 'Start configuring' })).toBeTruthy();
  expect(screen.queryByText('Reply before refresh')).toBeNull();
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  expect(await screen.findByRole('heading', { name: 'Run your first conversation' })).toBeTruthy();
  expect(screen.queryByRole('link', { name: 'View session' })).toBeNull();
  expect(screen.queryByText('Reply before refresh')).toBeNull();
  expect(runtime.sent).toHaveLength(1);
  expect(api.requests.filter((request) => request.method === 'DELETE')).toHaveLength(0);
  expect(api.requests.filter((request) => request.method === 'POST' && request.url.includes('/agents?'))).toHaveLength(
    1,
  );
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.includes('/sessions?')),
  ).toHaveLength(1);
});

test('resource loading errors show the API message', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/agents?') && (!init?.method || init.method === 'GET'))
      return jsonResponse({ error: { message: 'Resource unavailable' } }, 500);
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  expect((await screen.findByRole('alert')).textContent).toContain('Resource unavailable');
});

test('Session restoration errors show the API message and keep sending disabled', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  runtime.loseNextSend();
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await screen.findByText('Sending was not confirmed. Refresh the conversation before explicitly sending again.');
  await act(async () => runtime.reply('A saved reply'));
  cleanup();
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    if (/\/v1\/sessions\/[^/]+\?/.test(String(input)) && (!init?.method || init.method === 'GET'))
      return jsonResponse({ error: { message: 'Session unavailable' } }, 500);
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  expect(await screen.findByText('Session unavailable')).toBeTruthy();
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Send', exact: true }).disabled).toBe(true);
  expect(runtime.sent).toHaveLength(1);
});

test('a rejected Agent create keeps the draft and allows an explicit retry', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  let attempts = 0;
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/agents?') && init?.method === 'POST' && ++attempts === 1)
      return jsonResponse({ error: { message: 'Name rejected' } }, 400);
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Retry Agent' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  expect((await screen.findByRole('alert')).textContent).toContain('Name rejected');
  expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Retry Agent');
  await waitFor(() =>
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  expect(await screen.findByRole('heading', { name: 'Configure the runtime environment' })).toBeTruthy();
  expect(attempts).toBe(2);
});

test('a lost Agent response is recovered by its operation ID without a second create', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    const response = await baseFetch(input, init);
    if (String(input).startsWith('/v1/agents?') && init?.method === 'POST') throw new TypeError('Response lost');
    return response;
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  expect(await screen.findByRole('button', { name: 'Check saved result' })).toBeTruthy();
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(true);
  cleanup();
  renderManagedAgentsPage('quickstart');
  expect(await screen.findByRole('button', { name: 'Check saved result' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Check saved result' }));
  expect(await screen.findByRole('heading', { name: 'Configure the runtime environment' })).toBeTruthy();
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.startsWith('/v1/agents?')),
  ).toHaveLength(1);
});

test('repeated submits create one Agent and a late response cannot bind it to another workspace', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  let release: (() => void) | undefined;
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/agents?') && init?.method === 'POST')
      await new Promise<void>((resolve) => {
        release = resolve;
      });
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  const button = screen.getByRole('button', { name: 'Create and continue' });
  fireEvent.click(button);
  fireEvent.click(button);
  await waitFor(() => expect(Boolean(release)).toBe(true));
  cleanup();
  renderManagedAgentsPage('quickstart', 'en', { workspaceId: 'another' });
  await screen.findByRole('button', { name: 'Start configuring' });
  await act(async () => release?.());
  await waitFor(() => expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(1));
  expect(Boolean(screen.queryByRole('heading', { name: 'Configure the runtime environment' }))).toBe(false);
  expect(screen.getByRole('button', { name: 'Start configuring' })).toBeTruthy();
});

test('Agent candidates include later pages and an unavailable saved model cannot be submitted', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (url.startsWith('/v1/agents?') && (!init?.method || init.method === 'GET'))
      return jsonResponse(
        new URL(url, 'https://oma.duck.ai').searchParams.get('page')
          ? {
              data: [
                {
                  id: 'agent_later',
                  type: 'agent',
                  name: 'Hello World Agent',
                  model: 'removed-model',
                  version: 2,
                  system: 'Saved prompt.',
                },
              ],
              next_page: null,
            }
          : { data: [], next_page: 'later-agents' },
      );
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Start configuring' }));
  await waitFor(() => expect(screen.getByLabelText('Model').textContent).toContain('removed-model'));
  expect(screen.getByLabelText('Model').textContent).toContain('removed-model');
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Use existing and continue' }).disabled).toBe(true);
  expect(
    screen.getByText(
      'This model is no longer available. Choose an available model for a new Agent, or update the existing Agent.',
    ),
  ).toBeTruthy();
});

test('same-name Agents default to an existing configuration and preserve an explicit new draft', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([
    { id: 'agent_savedfirst', name: 'Hello World Agent', system: 'First saved prompt.' },
    { id: 'agent_savedsecond', name: 'Hello World Agent', system: 'Second saved prompt.' },
  ]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toBe('First saved prompt.');
  expect(codeBlockContaining('/v1/agents/agent_savedfirst').textContent).not.toContain('-X POST');
  await selectManagedComboboxOption(document.body, 'Use existing', 'Create a new Agent');
  fireEvent.change(screen.getByLabelText('System prompt'), { target: { value: 'My draft prompt.' } });
  await selectManagedComboboxOption(document.body, 'Use existing', /agent_savedsecond/);
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toBe('Second saved prompt.');
  expect(codeBlockContaining('/v1/agents/agent_savedsecond').textContent).not.toContain('-X POST');
  await selectManagedComboboxOption(document.body, 'Use existing', 'Create a new Agent');
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toBe('My draft prompt.');
  cleanup();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toBe('First saved prompt.');
  fireEvent.click(screen.getByRole('button', { name: 'Use existing and continue' }));
  expect(await screen.findByRole('heading', { name: 'Configure the runtime environment' })).toBeTruthy();
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(0);
});

test('an existing Agent with no system prompt can be reused at its saved version', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([{ id: 'agent_empty', name: 'Hello World Agent', system: '', version: 3 }]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  await selectManagedComboboxOption(document.body, 'Use existing', /agent_empty/);
  expect(codeBlockContaining('/v1/agents/agent_empty')?.textContent).toContain('version=3');
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Use existing and continue' }).disabled).toBe(false);
  fireEvent.click(screen.getByRole('button', { name: 'Use existing and continue' }));
  expect(await screen.findByRole('heading', { name: 'Configure the runtime environment' })).toBeTruthy();
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(0);
});

test('refreshing during environment configuration starts over without recreating the saved Agent', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  await screen.findByRole('heading', { name: 'Configure the runtime environment' });
  cleanup();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  expect(await screen.findByRole('button', { name: 'Use existing and continue' })).toBeTruthy();
  expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Hello World Agent');
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(1);
});

test('refresh discards an obsolete Agent binding without silently creating a replacement', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  await screen.findByRole('heading', { name: 'Configure the runtime environment' });
  cleanup();
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/agents?') && (!init?.method || init.method === 'GET'))
      return jsonResponse({ data: [], next_page: null });
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  expect(await screen.findByRole('heading', { name: 'Configure Agent' })).toBeTruthy();
  expect(screen.queryByRole('alert')).toBeNull();
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(1);
});

async function startAgentConfiguration() {
  fireEvent.click(await screen.findByRole('button', { name: 'Start configuring' }));
  await waitFor(() =>
    expect(
      screen.getByRole<HTMLButtonElement>('button', { name: /Create and continue|Use existing and continue/ }).disabled,
    ).toBe(false),
  );
}

test('header navigation preserves available steps after Back and cannot bypass unfinished configuration', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  expect(screen.getByRole<HTMLButtonElement>('button', { name: /Configure environment/ }).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  await screen.findByRole('heading', { name: 'Run your first conversation' });
  fireEvent.click(screen.getByRole('button', { name: 'Back', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: /Choose a starting point/ }));
  const lastStep = screen.getByRole<HTMLButtonElement>('button', { name: /First API call/ });
  expect(lastStep.disabled).toBe(false);
  fireEvent.click(lastStep);
  expect(screen.getByRole('heading', { name: 'Run your first conversation' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: /Configure environment/ }));
  fireEvent.click(screen.getByRole('radio', { name: 'Create a new environment' }));
  expect(screen.getByRole<HTMLButtonElement>('button', { name: /First API call/ }).disabled).toBe(true);
  fireEvent.click(screen.getByRole('button', { name: /Choose a starting point/ }));
  fireEvent.click(screen.getByRole('radio', { name: /Deep research/ }));
  expect(screen.getByRole<HTMLButtonElement>('button', { name: /Configure environment/ }).disabled).toBe(true);
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(1);
});

test('Session ID is requested only for follow-up application calls and shared between those calls', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  await screen.findByRole('heading', { name: 'Run your first conversation' });
  expect(screen.queryByLabelText(/Session ID/)).toBeNull();
  expect(screen.getByRole('link', { name: 'Go to API keys' }).getAttribute('href')).toBe(
    '/settings/workspaces/default/keys',
  );
  fireEvent.click(screen.getByRole('button', { name: '2. Receive replies' }));
  const input = await screen.findByRole<HTMLInputElement>('textbox', { name: 'Session ID' });
  expect(input.value).toBe('');
  fireEvent.change(input, { target: { value: 'sesn_app_result' } });
  expect(codeBlockContaining('/v1/sessions/sesn_app_result/events/stream').textContent).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '3. Send a message' }));
  expect(screen.getByRole<HTMLInputElement>('textbox', { name: 'Session ID' }).value).toBe('sesn_app_result');
  expect(codeBlockContaining('/v1/sessions/sesn_app_result/events?').textContent).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: '1. Create a session' }));
  expect(screen.queryByRole('textbox', { name: 'Session ID' })).toBeNull();
  expect(api.requests.filter((request) => request.method === 'POST' && request.url.includes('/sessions'))).toHaveLength(
    0,
  );
});

test('no Default environment is invented and archived names block new creation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/environments?') && (!init?.method || init.method === 'GET'))
      return jsonResponse({
        data: [{ id: 'env_archived', type: 'environment', name: 'Taken name', archived_at: '2026-10-01T00:00:00Z' }],
        next_page: null,
      });
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  expect(await screen.findByText('No active environments. Create one to continue.')).toBeTruthy();
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.includes('/environments')),
  ).toHaveLength(0);
  fireEvent.click(screen.getByRole('radio', { name: 'Create a new environment' }));
  fireEvent.change(screen.getByLabelText('Environment name'), { target: { value: 'Taken name' } });
  expect(
    screen.getByText(
      'An archived environment already uses this name. Choose another name or manage the existing environment.',
    ),
  ).toBeTruthy();
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('Environment name'), { target: { value: 'New runtime' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  expect(await screen.findByRole('heading', { name: 'Run your first conversation' })).toBeTruthy();
  const created = api.requests.find((request) => request.method === 'POST' && request.url.includes('/environments'));
  expect(created?.body).toMatchObject({
    name: 'New runtime',
    config: { type: 'cloud', networking: { type: 'limited' } },
  });
});

test('all environment pages are scanned before choosing Default or reusing a same-name environment', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  const pages: string[] = [];
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (url.startsWith('/v1/environments?') && (!init?.method || init.method === 'GET')) {
      const page = new URL(url, 'https://oma.duck.ai').searchParams.get('page');
      pages.push(page ?? 'first');
      return jsonResponse(
        page
          ? {
              data: [
                {
                  id: 'env_default',
                  type: 'environment',
                  name: 'Default',
                  archived_at: null,
                  config: { type: 'cloud' },
                },
              ],
              next_page: null,
            }
          : {
              data: [
                { id: 'env_other', type: 'environment', name: 'Other', archived_at: null, config: { type: 'cloud' } },
              ],
              next_page: 'environment_page_two',
            },
      );
    }
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  await screen.findByRole('heading', { name: 'Configure the runtime environment' });
  expect(screen.getByRole('radio', { name: /Default/ }).getAttribute('aria-checked')).toBe('true');
  expect(pages).toEqual(['first', 'environment_page_two']);
  fireEvent.click(screen.getByRole('radio', { name: 'Create a new environment' }));
  fireEvent.change(screen.getByLabelText('Environment name'), { target: { value: ' Default ' } });
  fireEvent.click(screen.getByRole('button', { name: 'Use existing and continue' }));
  await screen.findByRole('heading', { name: 'Run your first conversation' });
  expect(codeBlockContaining('/v1/sessions?').textContent).toContain('env_default');
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.includes('/environments')),
  ).toHaveLength(0);
});

test('selecting the same scenario or returning home keeps custom drafts and required-field validation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('radio', { name: /My Agent/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Start configuring' }));
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(true);
  fireEvent.change(screen.getByLabelText('System prompt'), { target: { value: 'Keep my draft.' } });
  fireEvent.click(screen.getByRole('button', { name: 'Back', exact: true }));
  fireEvent.click(screen.getByRole('radio', { name: /My Agent/ }));
  fireEvent.click(screen.getByRole('button', { name: 'Start configuring' }));
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toBe('Keep my draft.');
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(0);
  cleanup();
  renderManagedAgentsPage('quickstart');
  expect((await screen.findByRole('radio', { name: /Hello World Agent/ })).getAttribute('aria-checked')).toBe('true');
  await startAgentConfiguration();
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).not.toBe('Keep my draft.');
});

test('API previews use the current origin, safely quote form content and offer manual copy on failure', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: "O'Brien Agent" } });
  const block = codeBlockContaining('/v1/agents?');
  expect(block.textContent).toContain('https://oma.duck.ai/v1/agents?beta=true');
  expect(block.textContent).toContain("O'\"'\"'Brien Agent");
  expect(block.querySelector('code.language-bash .hljs-string')).toBeTruthy();
  const clipboard = spyOn(navigator.clipboard, 'writeText').mockRejectedValue(new Error('Permission denied'));
  fireEvent.click(screen.getByRole('button', { name: 'Copy request' }));
  expect(await screen.findByText('Select the code to copy it manually.')).toBeTruthy();
  clipboard.mockRestore();
});

test('the online test receives real event shapes, completes after idle and reuses its Session on a second turn', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent.length).toBe(1));
  expect(Boolean(screen.queryByText('First conversation completed'))).toBe(false);
  await act(async () => runtime.reply('First reply'));
  expect(await screen.findByText('First reply')).toBeTruthy();
  expect(await screen.findByText('First conversation completed')).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Test message'), { target: { value: 'A second message' } });
  fireEvent.keyDown(screen.getByLabelText('Test message'), { key: 'Enter', shiftKey: true });
  expect(runtime.sent).toHaveLength(1);
  fireEvent.keyDown(screen.getByLabelText('Test message'), { key: 'Enter' });
  await waitFor(() => expect(runtime.sent.length).toBe(2));
  await act(async () => runtime.reply('Second reply'));
  expect(await screen.findByText('Second reply')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Back', exact: true }));
  fireEvent.click(screen.getByRole('button', { name: 'Use existing and continue' }));
  expect(screen.getByText('First reply')).toBeTruthy();
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.startsWith('/v1/sessions?')),
  ).toHaveLength(1);
});

function installSessionRuntime() {
  const baseFetch = globalThis.fetch;
  const events: Record<string, unknown>[] = [];
  const sent: Record<string, unknown>[] = [];
  let session: Record<string, unknown> | null = null;
  let stream: ReadableStreamDefaultController<Uint8Array> | null = null;
  const emit = (event: Record<string, unknown>) => {
    events.push(event);
    stream?.enqueue(new TextEncoder().encode(`data: ${JSON.stringify(event)}\n\n`));
  };
  let sequence = 0;
  let loseSend = false;
  let sendResponse: Promise<void> | null = null;
  const event = (type: string, fields: Record<string, unknown> = {}) => ({
    id: `sevt_test_${++sequence}`,
    type,
    processed_at: new Date(Date.UTC(2026, 9, 8, 12, 0, sequence)).toISOString(),
    ...fields,
  });
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    const method = init?.method ?? 'GET';
    if (url.includes('/events/stream?'))
      return new Response(
        new ReadableStream<Uint8Array>({
          start(controller) {
            stream = controller;
            controller.enqueue(new TextEncoder().encode(': connected\n\n'));
            init?.signal?.addEventListener(
              'abort',
              () => {
                controller.close();
                if (stream === controller) stream = null;
              },
              { once: true },
            );
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      );
    if (url.includes('/events?') && method === 'GET') return jsonResponse({ data: events, next_page: null });
    if (url.includes('/events?') && method === 'POST') {
      const body = JSON.parse(String(init?.body));
      const inputEvent = body.events[0];
      sent.push(inputEvent);
      const accepted = event(inputEvent.type, inputEvent);
      emit(accepted);
      if (session) session.status = 'running';
      if (sendResponse) {
        await sendResponse;
        sendResponse = null;
      }
      if (loseSend) {
        loseSend = false;
        throw new TypeError('Sending response lost');
      }
      return jsonResponse({ data: [accepted] });
    }
    if (/\/v1\/sessions\/[^/]+\?/.test(url) && session) return jsonResponse(session);
    const response = await baseFetch(input, init);
    if (url.startsWith('/v1/sessions?') && method === 'POST') session = await response.clone().json();
    return response;
  }) as typeof fetch;
  return {
    sent,
    holdNextSendResponse() {
      let release!: () => void;
      sendResponse = new Promise<void>((resolve) => {
        release = resolve;
      });
      return release;
    },
    loseNextSend() {
      loseSend = true;
    },
    emit(type: string, fields: Record<string, unknown>) {
      emit(event(type, fields));
    },
    reply(text: string) {
      emit(event('agent.message', { content: [{ type: 'text', text }] }));
      emit(event('session.status_idle', { stop_reason: { type: 'end_turn' } }));
      if (session) session.status = 'idle';
    },
  };
}

test('a pending send shows progress without reporting an unconfirmed failure', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  const release = runtime.holdNextSendResponse();
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  const warning = Boolean(
    screen.queryByText('Sending was not confirmed. Refresh the conversation before explicitly sending again.'),
  );
  await act(async () => release());
  expect(warning).toBe(false);
});

test('an unconfirmed send survives refresh and is never resent automatically', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.change(screen.getByLabelText('Test message'), { target: { value: 'Original custom message' } });
  runtime.loseNextSend();
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  expect(
    await screen.findByText('Sending was not confirmed. Refresh the conversation before explicitly sending again.'),
  ).toBeTruthy();
  await act(async () => runtime.reply('Saved despite a lost response'));
  cleanup();
  renderManagedAgentsPage('quickstart');
  expect(await screen.findByText('Saved despite a lost response')).toBeTruthy();
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Send', exact: true }).disabled).toBe(true);
  expect(runtime.sent).toHaveLength(1);
  fireEvent.click(screen.getByRole('button', { name: 'I reviewed the conversation; enable sending' }));
  await waitFor(() =>
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Send', exact: true }).disabled).toBe(false),
  );
  expect(runtime.sent).toHaveLength(1);
  expect(screen.getByLabelText<HTMLTextAreaElement>('Test message').value).toBe('Original custom message');
});

test.each([
  {
    toolType: 'agent.tool_use',
    decision: 'Deny',
    accepted: { type: 'user.tool_confirmation', tool_use_id: 'sevt_pending_tool', result: 'deny' },
  },
  {
    toolType: 'agent.tool_use',
    decision: 'Approve',
    accepted: { type: 'user.tool_confirmation', tool_use_id: 'sevt_pending_tool', result: 'allow' },
  },
  {
    toolType: 'agent.custom_tool_use',
    decision: 'Deny',
    accepted: { type: 'user.custom_tool_result', custom_tool_use_id: 'sevt_pending_tool', is_error: true },
  },
  {
    toolType: 'agent.custom_tool_use',
    decision: 'Approve',
    accepted: { type: 'user.custom_tool_result', custom_tool_use_id: 'sevt_pending_tool', is_error: false },
  },
])('tools wait for a new status (%j); refresh resets progress', async ({ toolType, decision, accepted }) => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  await act(async () => {
    runtime.emit(toolType, {
      id: 'sevt_pending_tool',
      name: 'Write',
      input: { file_path: '/tmp/quickstart.txt', content: 'test' },
    });
    runtime.emit('session.status_idle', {
      stop_reason: { type: 'requires_action', event_ids: ['sevt_pending_tool'] },
    });
  });
  fireEvent.click(await screen.findByRole('button', { name: decision, exact: true }));
  await waitFor(() => expect(runtime.sent[1]).toMatchObject(accepted));
  fireEvent.change(screen.getByLabelText('Test message'), { target: { value: 'Wait for the resumed turn' } });
  await act(async () => fireEvent.keyDown(screen.getByLabelText('Test message'), { key: 'Enter' }));
  expect(runtime.sent).toHaveLength(2);
  expect(screen.getByRole('button', { name: 'Stop', exact: true })).toBeTruthy();
  fireEvent.change(screen.getByLabelText('Test message'), { target: { value: 'Still waiting for a new status' } });
  await act(async () => fireEvent.keyDown(screen.getByLabelText('Test message'), { key: 'Enter' }));
  expect(runtime.sent).toHaveLength(2);
  await act(async () => runtime.emit('session.status_running', {}));
  fireEvent.click(screen.getByRole('button', { name: 'Stop', exact: true }));
  await waitFor(() => expect(runtime.sent[2]).toMatchObject({ type: 'user.interrupt' }));
  await act(async () => runtime.emit('session.status_idle', { stop_reason: { type: 'end_turn' } }));
  fireEvent.click(await screen.findByRole('button', { name: 'Send', exact: true }));
  await waitFor(() =>
    expect(runtime.sent[3]).toMatchObject({
      type: 'user.message',
      content: [{ type: 'text', text: 'Still waiting for a new status' }],
    }),
  );
  cleanup();
  renderManagedAgentsPage('quickstart');
  expect(await screen.findByRole('button', { name: 'Start configuring' })).toBeTruthy();
  expect(screen.queryByRole('button', { name: 'Stop', exact: true })).toBeNull();
  expect(runtime.sent).toHaveLength(4);
});

test('Thinking followed by idle is not reported as a completed first reply', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  await act(async () => {
    runtime.emit('agent.message', { content: [{ type: 'thinking', thinking: 'Checking the question' }] });
    runtime.emit('session.status_idle', { stop_reason: { type: 'end_turn' } });
  });
  expect(Boolean(screen.queryByText('First conversation completed'))).toBe(false);
});

test('an interrupted partial reply is stopped rather than reported as completed', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  await act(async () => {
    runtime.emit('agent.message', { content: [{ type: 'text', text: 'Partial output' }] });
    runtime.emit('user.interrupt', {});
    runtime.emit('session.status_idle', { stop_reason: { type: 'end_turn' } });
  });
  expect(Boolean(screen.queryByText('First conversation completed'))).toBe(false);
  expect(await screen.findByText('Test stopped')).toBeTruthy();
});

test('selecting a quickstart scenario only edits a draft until the user confirms creation', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');

  expect(await screen.findByRole('heading', { name: 'Start with your first Agent' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'Start configuring' }));

  expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Hello World Agent');
  expect(screen.getByLabelText<HTMLTextAreaElement>('System prompt').value).toContain('helpful assistant');
  expect(api.requests.filter((request) => request.method === 'POST')).toHaveLength(0);
});

test('confirming an Agent saves the displayed configuration and advances only after success', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Start configuring' }));
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'First real Agent' } });
  fireEvent.change(screen.getByLabelText('System prompt'), { target: { value: 'Reply briefly.' } });
  await waitFor(() =>
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));

  expect(await screen.findByRole('heading', { name: 'Configure the runtime environment' })).toBeTruthy();
  const saved = api.requests.find((request) => request.method === 'POST' && request.url.startsWith('/v1/agents?'));
  expect(saved?.body).toMatchObject({ name: 'First real Agent', model: 'claude-sonnet-4-6', system: 'Reply briefly.' });
  expect(saved?.body).not.toHaveProperty('description');
  expect(screen.getByText('Option environment')).toBeTruthy();
});

test('the online test creates a Session lazily and connects its event stream before sending', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  const order: string[] = [];
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (url.includes('/events/stream?')) {
      order.push('stream');
      return new Response(
        new ReadableStream({
          start(controller) {
            controller.enqueue(new TextEncoder().encode(': connected\n\n'));
            init?.signal?.addEventListener('abort', () => controller.close(), { once: true });
          },
        }),
        { headers: { 'Content-Type': 'text/event-stream' } },
      );
    }
    if (url.includes('/events?') && init?.method === 'POST') order.push('send');
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Start configuring' }));
  await waitFor(() =>
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(false),
  );
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.startsWith('/v1/sessions?')),
  ).toHaveLength(0);
  fireEvent.change(screen.getByLabelText('Test message'), { target: { value: 'Hello from quickstart' } });
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(order).toEqual(['stream', 'send']));
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.startsWith('/v1/sessions?')),
  ).toHaveLength(1);
  expect(screen.getByText('Uses your signed-in account. No API key needed.')).toBeTruthy();
});

test('a lost Session create response is recovered after refresh without creating or sending again', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  installSessionRuntime();
  const baseFetch = globalThis.fetch;
  let saved: Record<string, unknown> | null = null;
  globalThis.fetch = (async (input, init) => {
    const url = String(input);
    if (url.startsWith('/v1/sessions?') && (!init?.method || init.method === 'GET')) {
      expect(new URL(url, 'https://oma.duck.ai').searchParams.get('agent_id')).toBeTruthy();
      return jsonResponse({ data: saved ? [saved] : [], next_page: null });
    }
    const response = await baseFetch(input, init);
    if (url.startsWith('/v1/sessions?') && init?.method === 'POST') {
      saved = await response.clone().json();
      throw new TypeError('Session response lost');
    }
    return response;
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await screen.findByRole('button', { name: 'Check saved result' });
  cleanup();
  renderManagedAgentsPage('quickstart');
  fireEvent.click(await screen.findByRole('button', { name: 'Check saved result' }));
  await waitFor(() => expect(Boolean(screen.queryByRole('button', { name: 'Check saved result' }))).toBe(false));
  expect(await screen.findByRole('link', { name: 'View session' })).toBeTruthy();
  expect(
    api.requests.filter((request) => request.method === 'POST' && request.url.startsWith('/v1/sessions?')),
  ).toHaveLength(1);
  expect(api.requests.filter((request) => request.method === 'POST' && request.url.includes('/events?'))).toHaveLength(
    0,
  );
});

test('API key links are available beside requests in steps two through four and open a new tab', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  const expectKeyLink = () => {
    for (const link of screen.getAllByRole('link', { name: 'Go to API keys' })) {
      expect(link.getAttribute('href')).toBe('/settings/workspaces/default/keys');
      expect(link.getAttribute('target')).toBe('_blank');
      expect(link.getAttribute('rel')).toContain('noopener');
    }
  };
  expectKeyLink();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  await screen.findByRole('heading', { name: 'Configure the runtime environment' });
  expectKeyLink();
  fireEvent.click(screen.getByRole('button', { name: 'Use existing and continue' }));
  await screen.findByRole('heading', { name: 'Run your first conversation' });
  expectKeyLink();
});

test('an unmatched create can only be abandoned after a successful empty recovery check', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  const api = mockAgentsApi([]);
  const baseFetch = globalThis.fetch;
  let failCheck = false;
  let createAttempts = 0;
  globalThis.fetch = (async (input, init) => {
    if (String(input).startsWith('/v1/agents?')) {
      if (init?.method === 'POST') {
        createAttempts++;
        throw new TypeError('Request failed before saving');
      }
      if (failCheck) return jsonResponse({ error: { message: 'Lookup unavailable' } }, 500);
    }
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.change(screen.getByLabelText('Name'), { target: { value: 'Recoverable draft' } });
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  await screen.findByRole('button', { name: 'Check saved result' });
  expect(screen.queryByRole('button', { name: 'I checked my resources; abandon this request' })).toBeNull();
  failCheck = true;
  fireEvent.click(screen.getByRole('button', { name: 'Check saved result' }));
  await screen.findByText('Lookup unavailable');
  expect(screen.queryByRole('button', { name: 'I checked my resources; abandon this request' })).toBeNull();
  failCheck = false;
  fireEvent.click(screen.getByRole('button', { name: 'Check saved result' }));
  const abandon = await screen.findByRole('button', { name: 'I checked my resources; abandon this request' });
  expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(true);
  expect(screen.getByText(/The original request may still finish/)).toBeTruthy();
  fireEvent.click(abandon);
  await waitFor(() =>
    expect(screen.getByRole<HTMLButtonElement>('button', { name: 'Create and continue' }).disabled).toBe(false),
  );
  expect(screen.getByLabelText<HTMLInputElement>('Name').value).toBe('Recoverable draft');
  expect(screen.queryByRole('button', { name: 'Check saved result' })).toBeNull();
  expect(window.sessionStorage.length).toBe(0);
  expect(createAttempts).toBe(1);
  expect(api.requests.filter((request) => request.method === 'POST' || request.method === 'DELETE')).toHaveLength(0);
});

test('a completed Session has no recurring retrievals while visible or on an earlier step', async () => {
  resetTestDom('https://oma.duck.ai/workspaces/default/agent-quickstart');
  mockAgentsApi([]);
  const runtime = installSessionRuntime();
  let retrievals = 0;
  const baseFetch = globalThis.fetch;
  globalThis.fetch = (async (input, init) => {
    if (/\/v1\/sessions\/[^/]+\?/.test(String(input)) && (!init?.method || init.method === 'GET')) retrievals++;
    return baseFetch(input, init);
  }) as typeof fetch;
  renderManagedAgentsPage('quickstart');
  await startAgentConfiguration();
  fireEvent.click(screen.getByRole('button', { name: 'Create and continue' }));
  fireEvent.click(await screen.findByRole('button', { name: 'Use existing and continue' }));
  fireEvent.click(screen.getByRole('button', { name: 'Send', exact: true }));
  await waitFor(() => expect(runtime.sent).toHaveLength(1));
  await act(async () => runtime.reply('Finished reply'));
  await screen.findByText('First conversation completed');
  const before = retrievals;
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 2100));
  });
  expect(retrievals).toBe(before);
  fireEvent.click(screen.getByRole('button', { name: 'Back', exact: true }));
  await screen.findByRole('heading', { name: 'Configure the runtime environment' });
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 2100));
  });
  expect(retrievals).toBe(before);
}, 8000);
