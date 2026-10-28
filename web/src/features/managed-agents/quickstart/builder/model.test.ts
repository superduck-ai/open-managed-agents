import { expect, test } from 'bun:test';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import {
  newBuilderState,
  builderExportConfig,
  toolResult,
  builderStorageKey,
  invalidateBuilderTest,
  builderHistoryText,
} from './model';
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
test('advancing an unsaved draft explains the missing confirmation tool without changing state', async () => {
  const state = initial();
  const before = structuredClone(state);
  await expect(
    prepareBuilderTool({
      state,
      tool: { id: 'advance', name: 'offer_next_step', input: {} },
      catalog,
      models: ['model'],
      workspaceId: 'workspace',
    }),
  ).rejects.toThrow('Call build_agent_config');
  expect(state).toEqual(before);
});
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
  const next = invalidateBuilderTest(state, true);
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

test('configuration export contains the current agent and real session references without history or placeholder resources', () => {
  const state = initial();
  state.config = { ...state.config, name: 'Exported research', system: 'Cite primary sources.' };
  state.messages = [{ role: 'user', content: 'Private build discussion' }];
  state.agent = { id: 'agent_saved', name: 'Exported research', version: 3 };
  state.savedConfig = state.config;
  const empty = builderExportConfig(state);
  expect(empty.session).not.toHaveProperty('environment_id');
  state.environment = { id: 'env_selected', name: 'Research environment' };
  state.vaults = [{ id: 'vault_linked', name: 'Connected service' }];
  const exported = builderExportConfig(state);
  expect(exported.agent.system).toBe('Cite primary sources.');
  expect(exported.session).toEqual({
    agent: { type: 'agent', id: 'agent_saved', version: 3 },
    environment_id: 'env_selected',
    vault_ids: ['vault_linked'],
  });
  expect(JSON.stringify(exported)).not.toContain('Private build discussion');
  expect(JSON.stringify(exported)).not.toContain('ENVIRONMENT_ID');
});

test('dependency changes invalidate completed test evidence and block deployment', () => {
  const state = initial();
  state.testClosed = true;
  state.testSummary = 'Old environment result';
  state.sessionID = 'old';
  const next = invalidateBuilderTest(state);
  expect(next.testClosed).toBe(false);
  expect(next.testSummary).toBe('');
  expect(next.sessionID).toBe('');
});

test('remaining tool results precede chat replies in model history', () => {
  const state = initial();
  const first = { id: 'first', name: 'build_agent_config', input: {} };
  const second = { id: 'second', name: 'list_environments', input: {} };
  state.queue = [first, second];
  state.messages = [{ role: 'assistant', content: [first, second].map((call) => ({ type: 'tool_use', ...call })) }];
  const replied = toolResult(state, first, 'User requested a revision');
  replied.messages.push({ role: 'user', content: 'Revise the name' });
  const completed = toolResult(replied, second, []);
  expect(completed.messages.at(-1)?.content).toBe('Revise the name');
  expect(
    completed.messages.slice(1, -1).map((message) => Array.isArray(message.content) && message.content[0].tool_use_id),
  ).toEqual(['first', 'second']);
});

test('export keeps pending draft separate from the version used by sessions', () => {
  const state = initial();
  state.savedConfig = { ...state.config, name: 'Saved', system: 'Saved instructions' };
  state.agent = { id: 'agent_saved', name: 'Saved', version: 2 };
  state.config = { ...state.savedConfig, name: 'Pending', system: 'Unconfirmed instructions' };
  const exported = builderExportConfig(state);
  expect(exported.agent).toEqual(state.savedConfig);
  expect(exported.draft).toEqual(state.config);
  expect(exported.configuration_status).toBe('unsaved_changes');
  expect(exported.session.agent).toEqual({ type: 'agent', id: 'agent_saved', version: 2 });
});

test('changing dependencies detaches the old deployment from the current flow without rewriting history', () => {
  const state = initial();
  state.step = 'integrate';
  state.wantsSchedule = true;
  state.testClosed = true;
  state.deployment = { id: 'deployment_old', name: 'Old pinned deployment' };
  state.messages = [{ role: 'user', content: 'Original deployment exists.' }];
  const changed = invalidateBuilderTest(state);
  expect(changed.deployment).toBeNull();
  expect(changed.step).toBe('session');
  expect(changed.testClosed).toBe(false);
  expect(changed.messages).toEqual(state.messages);
  const repairAndContinue = invalidateBuilderTest(state, true);
  expect(repairAndContinue.step).toBe('deploy');
  expect(repairAndContinue.testClosed).toBe(true);
});

test('restating the same model ID in a full proposal preserves saved model options', () => {
  const state = initial();
  state.config.model = { id: 'model', speed: 'fast' };
  const proposed = proposedAgentConfig(
    { id: 'rename', name: 'build_agent_config', input: { name: 'Renamed', model: 'model' } },
    state,
    ['model'],
  );
  expect(proposed.model).toEqual({ id: 'model', speed: 'fast' });
});

for (const step of ['agent', 'environment', 'session', 'integrate'] as const) {
  test(`schedule intent at ${step} does not create or bypass testing and confirmation`, async () => {
    const state = initial();
    state.step = step;
    const prepared = await prepareBuilderTool({
      state,
      tool: { id: 'schedule', name: 'flag_schedule_intent', input: { wants_schedule: true } },
      catalog,
      models: ['model'],
      workspaceId: 'workspace',
    });
    expect(prepared.patch?.step ?? step).toBe(step === 'integrate' ? 'deploy' : step);
    expect(prepared.patch?.wantsSchedule).toBe(true);
    const planned = {
      ...state,
      ...prepared.patch,
      agent: { id: 'agent', name: 'Agent', version: 1 },
      environment: { id: 'environment', name: 'Environment' },
    };
    await expect(
      prepareBuilderTool({
        state: planned,
        tool: {
          id: 'deploy',
          name: 'create_deployment',
          input: {
            name: 'Daily report',
            cron_expression: '0 9 * * *',
            timezone: 'Asia/Shanghai',
            initial_message: 'Report',
          },
        },
        catalog,
        models: ['model'],
        workspaceId: 'workspace',
      }),
    ).rejects.toThrow('finish the test');
    expect(planned.deployment).toBeNull();
  });
}
