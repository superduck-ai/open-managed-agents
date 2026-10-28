import { performBuilderMutation } from './mutations';
import { afterEach, expect, test } from 'bun:test';
import { newBuilderState } from './model';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import {
  loadBuilderVaults,
  saveBuilderDeployment,
  saveBuilderSession,
  recoverBuilderOperation,
  type BuilderCatalog,
} from './api';
import { prepareBuilderTool } from './dispatch';
import { setConsoleRequestContext } from '../../../../shared/api/client';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
  setConsoleRequestContext({});
});
const json = (value: unknown) =>
  new Response(JSON.stringify(value), { headers: { 'Content-Type': 'application/json' } });
function state() {
  return newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'));
}
const catalog: BuilderCatalog = { agents: [], environments: [], providers: [], directory: [], skills: [] };
test('credential coverage reads every page and returns only safe resource summaries', async () => {
  const urls: string[] = [];
  globalThis.fetch = (async (input) => {
    const url = String(input);
    urls.push(url);
    if (url.includes('/credentials?'))
      return url.includes('page=second')
        ? json({
            data: [
              {
                id: 'credential2',
                auth: {
                  type: 'static_bearer',
                  mcp_server_url: 'https://private.example/mcp',
                  token: 'MUST_NOT_ESCAPE',
                },
                archived_at: null,
              },
            ],
            next_page: null,
          })
        : json({
            data: [{ id: 'credential1', auth: { type: 'environment_variable', name: 'UNRELATED' } }],
            next_page: 'second',
          });
    return json({
      data: [{ id: 'vault_test', type: 'vault', display_name: 'Operations', archived_at: null }],
      next_page: null,
    });
  }) as typeof fetch;
  const options = await loadBuilderVaults('workspace', [{ name: 'private', url: 'https://private.example/mcp' }]);
  expect(options).toEqual([{ id: 'vault_test', name: 'Operations', covered: ['private'] }]);
  expect(urls.some((url) => url.includes('page=second'))).toBe(true);
  expect(JSON.stringify(options)).not.toContain('MUST_NOT_ESCAPE');
});
test('a previously skipped service can be explicitly authorized again and requires a real credential', async () => {
  const value = state();
  value.config.mcp_servers = [{ type: 'url', name: 'private', url: 'https://private.example/mcp' }];
  value.savedConfig = value.config;
  value.vaults = [{ id: 'vault', name: 'Operations' }];
  value.skippedServers = ['https://private.example/mcp'];
  globalThis.fetch = (async () => json({ data: [], next_page: null })) as typeof fetch;
  const result = await prepareBuilderTool({
    state: value,
    tool: { id: 'auth', name: 'create_vault_credential', input: { vault_id: 'vault', mcp_server_name: 'private' } },
    catalog,
    models: ['model'],
    workspaceId: 'workspace',
  });
  expect(result.gate).toBe(true);
});
test('deployment is blocked before testing and binds confirmed saved resources after test close', async () => {
  const value = state();
  value.agent = { id: 'agent', name: 'Orders', version: 3 };
  value.savedConfig = value.config;
  value.environment = { id: 'environment', name: 'Restricted' };
  value.vaults = [{ id: 'vault', name: 'Operations' }];
  value.wantsSchedule = true;
  value.step = 'deploy';
  const tool = {
    id: 'deploy',
    name: 'create_deployment',
    input: {
      name: 'Daily orders',
      cron_expression: '0 18 * * *',
      timezone: 'Asia/Shanghai',
      initial_message: 'Summarize today’s orders',
    },
  };
  await expect(
    prepareBuilderTool({ state: value, tool, catalog, models: ['model'], workspaceId: 'workspace' }),
  ).rejects.toThrow('finish the test');
  value.testClosed = true;
  let payload: Record<string, unknown> = {};
  let headers = new Headers();
  setConsoleRequestContext({ csrfToken: 'csrf_test' });
  globalThis.fetch = (async (_input, init) => {
    payload = JSON.parse(String(init?.body));
    headers = new Headers(init?.headers);
    return json({ id: 'deployment', name: 'Daily orders' });
  }) as typeof fetch;
  expect(await saveBuilderDeployment(value, tool, 'operation_test', 'workspace')).toEqual({
    id: 'deployment',
    name: 'Daily orders',
  });
  expect(payload.agent).toEqual({ type: 'agent', id: 'agent', version: 3 });
  expect(payload.environment_id).toBe('environment');
  expect(payload.vault_ids).toEqual(['vault']);
  expect(payload.schedule).toEqual({ type: 'cron', expression: '0 18 * * *', timezone: 'Asia/Shanghai' });
  expect(payload.metadata).toEqual({ quickstart_operation_id: 'operation_test' });
  expect(headers.get('X-Workspace-ID')).toBe('workspace');
  expect(headers.get('X-CSRF-Token')).toBe('csrf_test');
});

test('restored session creation binds the saved agent version, environment, selected vaults and recovery marker', async () => {
  const value = state();
  value.agent = { id: 'agent_saved', name: 'Research', version: 4 };
  value.environment = { id: 'environment_saved', name: 'Restricted' };
  value.vaults = [
    { id: 'vault_first', name: 'First' },
    { id: 'vault_second', name: 'Second' },
  ];
  let payload: Record<string, unknown> = {};
  setConsoleRequestContext({ csrfToken: 'csrf_test' });
  globalThis.fetch = (async (_input, init) => {
    payload = JSON.parse(String(init?.body));
    return json({ id: 'session_saved' });
  }) as typeof fetch;
  await saveBuilderSession(value, 'session_operation', 'workspace');
  expect(payload).toMatchObject({
    agent: { type: 'agent', id: 'agent_saved', version: 4 },
    environment_id: 'environment_saved',
    vault_ids: ['vault_first', 'vault_second'],
    metadata: { quickstart_operation_id: 'session_operation' },
    resources: [],
  });
});

test('creating and recovering an additional vault preserves prior selections without duplicates', async () => {
  const value = state();
  value.vaults = [{ id: 'vault_existing', name: 'Existing' }];
  setConsoleRequestContext({ csrfToken: 'csrf_test' });
  globalThis.fetch = (async (_input, init) =>
    json(
      init?.method === 'POST'
        ? { id: 'vault_new', display_name: 'New' }
        : {
            data: [
              {
                id: 'vault_new',
                type: 'vault',
                display_name: 'New',
                metadata: { quickstart_operation_id: 'vault_operation' },
              },
            ],
            next_page: null,
          },
    )) as typeof fetch;
  const patch = await performBuilderMutation(
    value,
    { id: 'create_vault', name: 'create_vault', input: { name: 'New' } },
    'vault_operation',
    'workspace',
  );
  expect(patch.vaults?.map((vault) => vault.id)).toEqual(['vault_existing', 'vault_new']);
  const recovered = await recoverBuilderOperation(
    { ...value, ...patch, operation: { id: 'vault_operation', kind: 'vault', toolId: 'create_vault' } },
    'workspace',
  );
  expect(recovered.vaults.map((vault) => vault.id)).toEqual(['vault_existing', 'vault_new']);
  expect(recovered.operation).toBeNull();
});
