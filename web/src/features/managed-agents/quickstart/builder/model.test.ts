import { expect, test } from 'bun:test';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import { newBuilderState, toolResult, builderStorageKey, invalidateBuilderTest, builderHistoryText } from './model';
import { proposedAgentConfig, validatedDeployment, builderTools } from './tools';
import { authenticationServers, credentialCovers, type BuilderCatalog } from './api';
import { prepareBuilderTool } from './dispatch';
import type { VaultCredentialApiResponse } from '../../types';

function initial() {
  return newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'));
}
const catalog: BuilderCatalog = {
  agents: [],
  environments: [],
  skills: [],
  providers: [],
  directory: [
    { slug: 'public', displayName: 'Public', url: 'https://public.example/mcp', authless: true, toolNames: [] },
  ],
};
test('config proposal rejects unavailable models and preserves every unspecified config field', () => {
  const state = initial();
  state.config.system = 'Preserve these instructions';
  state.config.metadata = { team: 'operations' };
  expect(() =>
    proposedAgentConfig({ id: 'x', name: 'build_agent_config', input: { model: 'unknown' } }, state, ['model']),
  ).toThrow('not available');
  const proposed = proposedAgentConfig({ id: 'x', name: 'build_agent_config', input: { name: 'Renamed' } }, state, [
    'model',
  ]);
  expect(proposed.system).toBe(state.config.system);
  expect(proposed.metadata).toEqual(state.config.metadata);
  expect(proposed.tools).toEqual(state.config.tools);
  expect(proposed.model).toEqual(state.config.model);
});
test('unknown tools and deployment schedules fail closed', async () => {
  await expect(
    prepareBuilderTool({
      state: initial(),
      tool: { id: 't', name: 'arbitrary_write', input: {} },
      catalog,
      models: ['model'],
      workspaceId: 'workspace',
    }),
  ).rejects.toThrow('Unsupported');
  expect(() =>
    validatedDeployment({
      id: 'd',
      name: 'create_deployment',
      input: { name: 'Daily', cron_expression: 'every Tuesday', timezone: 'Unknown/Zone', initial_message: 'Start' },
    }),
  ).toThrow();
  expect(
    validatedDeployment({
      id: 'd',
      name: 'create_deployment',
      input: { name: 'Daily', cron_expression: '0 9 * * *', timezone: 'Asia/Shanghai', initial_message: 'Start' },
    }).timezone,
  ).toBe('Asia/Shanghai');
});
test('only explicit authless catalog entries bypass authorization; exact active credential coverage is required', () => {
  const config = {
    ...initial().config,
    mcp_servers: [
      { type: 'url' as const, name: 'public', url: 'https://public.example/mcp' },
      { type: 'url' as const, name: 'private', url: 'https://private.example/mcp' },
    ],
  };
  expect(authenticationServers(config, catalog)).toEqual([{ name: 'private', url: 'https://private.example/mcp' }]);
  const credential = {
    auth: { type: 'static_bearer', mcp_server_url: 'https://private.example/mcp' },
    archived_at: null,
  } as VaultCredentialApiResponse;
  expect(credentialCovers(credential, 'https://private.example/mcp')).toBe(true);
  expect(credentialCovers(credential, 'https://private.example/other')).toBe(false);
  expect(credentialCovers({ ...credential, archived_at: 'today' }, 'https://private.example/mcp')).toBe(false);
  expect(
    credentialCovers({ ...credential, auth: { type: 'environment_variable' } }, 'https://private.example/mcp'),
  ).toBe(false);
});
test('tool results are single-use, preserve other pending calls and leave a readable question history', () => {
  const state = initial();
  const a = { id: 'a', name: 'ask_user_questions', input: {} };
  const b = { id: 'b', name: 'offer_next_step', input: {} };
  state.queue = [a, b];
  state.gate = a;
  const updated = toolResult(state, a, [{ question: 'Which fields?', answers: ['Company', 'Product'] }]);
  expect(updated.queue).toEqual([b]);
  expect(updated.gate).toBeNull();
  expect(toolResult(updated, a, 'duplicate')).toBe(updated);
  expect(builderHistoryText(updated.messages[0], false)).toBe('Company, Product');
  expect(builderStorageKey('account-a', 'workspace')).not.toBe(builderStorageKey('account-b', 'workspace'));
});
test('repair after explicit test close invalidates old session while retaining the decision to continue', () => {
  const state = initial();
  state.sessionID = 'session_old';
  state.testClosed = true;
  state.testSummary = 'Actual events';
  const next = invalidateBuilderTest(state);
  expect(next.sessionID).toBe('');
  expect(next.testClosed).toBe(true);
  expect(next.testSummary).toBe('Actual events');
});
test('the model receives schemas for every executable tool including packages and safe confirmation gates', () => {
  const tools = builderTools(['model']);
  expect(tools.map((tool) => tool.name)).toContain('create_deployment');
  const environment = tools.find((tool) => tool.name === 'create_environment');
  expect(JSON.stringify(environment)).toContain('packages');
  expect(tools.some((tool) => tool.name === 'web_search')).toBe(false);
});

test('discarding a pending proposal restores saved config without writing a version', async () => {
  const value = initial();
  value.savedConfig = value.config;
  value.agent = { id: 'agent', name: value.config.name, version: 1 };
  value.config = { ...value.config, name: 'Unconfirmed rename' };
  const result = await prepareBuilderTool({
    state: value,
    tool: { id: 'discard', name: 'build_agent_config', input: { name: value.savedConfig.name } },
    catalog,
    models: ['model'],
    workspaceId: 'workspace',
  });
  expect(result.gate).toBeUndefined();
  expect(result.patch?.config).toEqual(value.savedConfig);
  await expect(
    prepareBuilderTool({
      state: value,
      tool: { id: 'advance', name: 'offer_next_step', input: {} },
      catalog,
      models: ['model'],
      workspaceId: 'workspace',
    }),
  ).rejects.toThrow('Save or discard');
});
