import { afterEach, expect, test } from 'bun:test';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import { newBuilderState } from './model';
import { prepareBuilderTool } from './dispatch';
import { prepareBuilderSession, prepareBuilderEnvironment, type BuilderCatalog } from './api';
import { proposedAgentConfig } from './tools';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
});
const catalog: BuilderCatalog = { agents: [], environments: [], providers: [], directory: [], skills: [] };
function state() {
  const value = newBuilderState(createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'));
  value.config.metadata = {
    quickstart_environment_packages: JSON.stringify({ type: 'packages', pip: ['polars'], npm: ['csv-parse'] }),
  };
  value.agent = { id: 'agent', name: 'Reader', version: 1 };
  value.savedConfig = value.config;
  return value;
}
const reply = (config: unknown) =>
  new Response(JSON.stringify({ id: 'env', name: 'Shared', config, archived_at: null }), {
    headers: { 'Content-Type': 'application/json' },
  });

test('reuse rejects an environment without the saved dependencies without modifying it', async () => {
  const calls: string[] = [];
  globalThis.fetch = (async (_input, init) => {
    calls.push(init?.method ?? 'GET');
    return reply({ type: 'cloud', packages: { type: 'packages', pip: [] } });
  }) as typeof fetch;
  await expect(
    prepareBuilderTool({
      state: state(),
      tool: { id: 'reuse', name: 'create_environment', input: { reuse_environment_id: 'env' } },
      catalog,
      models: ['model'],
      workspaceId: 'workspace',
    }),
  ).rejects.toThrow('polars');
  expect(calls).toEqual(['GET']);
});

test('trial rechecks actual environment dependencies before creating any session', async () => {
  const value = state();
  value.environment = { id: 'env', name: 'Changed after selection' };
  const calls: string[] = [];
  globalThis.fetch = (async (_input, init) => {
    calls.push(init?.method ?? 'GET');
    return reply({ type: 'cloud', packages: { type: 'packages' } });
  }) as typeof fetch;
  await expect(prepareBuilderSession(value, 'operation', 'workspace')).rejects.toThrow('polars');
  expect(calls).toEqual(['GET']);
});

test('a new environment proposal must include dependencies before confirmation', async () => {
  await expect(
    prepareBuilderTool({
      state: state(),
      tool: {
        id: 'new',
        name: 'create_environment',
        input: {
          name: 'New',
          config: {
            type: 'cloud',
            networking: { type: 'unrestricted' },
            packages: { type: 'packages', pip: ['polars'] },
          },
        },
      },
      catalog,
      models: ['model'],
      workspaceId: 'workspace',
    }),
  ).rejects.toThrow('csv-parse');
});

test('deliverable instructions use the platform contract without changing ordinary source edits', () => {
  const value = state();
  const config = proposedAgentConfig(
    {
      id: 'proposal',
      name: 'build_agent_config',
      input: { system: 'Write exports in the working directory and return a path.' },
    },
    value,
    ['model'],
  );
  expect(config.system).toContain('/mnt/user-data/outputs');
  expect(config.system).toContain('Ordinary source edits stay in the working directory');
  expect(config.system).toContain('take precedence over conflicting file delivery instructions');
});

test('declared dependencies are persisted by manager and survive partial config updates', () => {
  const value = state();
  const first = proposedAgentConfig(
    {
      id: 'proposal',
      name: 'build_agent_config',
      input: {
        name: 'Reader',
        system: 'Analyze user records.',
        environment_packages: { type: 'packages', pip: ['polars'], npm: ['csv-parse'] },
      },
    },
    value,
    ['model'],
  );
  const next = proposedAgentConfig(
    { id: 'rename', name: 'build_agent_config', input: { name: 'Renamed', metadata: { owner: 'team' } } },
    { ...value, config: first },
    ['model'],
  );
  expect(next.metadata?.quickstart_environment_packages).toBe(first.metadata?.quickstart_environment_packages);
  expect(next.model).toEqual(first.model);
  expect(next.system.split('# Platform file delivery contract')).toHaveLength(2);
});

test('matching dependencies allow reuse without pretending installation was verified', async () => {
  globalThis.fetch = (async () =>
    reply({ type: 'cloud', packages: { type: 'packages', pip: ['polars'], npm: ['csv-parse'] } })) as typeof fetch;
  const result = await prepareBuilderTool({
    state: state(),
    tool: { id: 'reuse', name: 'create_environment', input: { reuse_environment_id: 'env' } },
    catalog,
    models: ['model'],
    workspaceId: 'workspace',
  });
  expect(result.result).toEqual({ id: 'env', name: 'Shared', runtime_verified: false, selected: true });
});

test('new output instructions replace old output sections while preserving independent configuration', () => {
  const value = state();
  value.config.system = 'Output a report, checklist and follow-up questions.';
  const next = proposedAgentConfig(
    {
      id: 'narrow',
      name: 'build_agent_config',
      input: { system: 'Only output a checklist with assignee and date; missing entries are unspecified.' },
    },
    value,
    ['model'],
  );
  expect(next.system).not.toContain('follow-up questions');
  expect(next.system).toContain('assignee and date');
  expect(next.model).toEqual(value.config.model);
  expect(next.tools).toEqual(value.config.tools);
});

test('confirmed environment creation writes the declared packages and checks the persisted response', async () => {
  const config = {
    type: 'cloud',
    networking: { type: 'unrestricted' },
    packages: { type: 'packages', pip: ['polars'], npm: ['csv-parse'] },
  };
  let payload: Record<string, unknown> = {};
  globalThis.fetch = (async (_input, init) => {
    payload = JSON.parse(String(init?.body));
    return reply(config);
  }) as typeof fetch;
  const environment = await prepareBuilderEnvironment(
    { id: 'new', name: 'create_environment', input: { name: 'Prepared', config } },
    'operation',
    'workspace',
    { type: 'packages', pip: ['polars'], npm: ['csv-parse'] },
  )();
  expect(payload.config).toEqual(config);
  expect(environment.id).toBe('env');
  globalThis.fetch = (async () => reply({ type: 'cloud', packages: { type: 'packages' } })) as typeof fetch;
  await expect(
    prepareBuilderEnvironment(
      { id: 'new', name: 'create_environment', input: { name: 'Prepared', config } },
      'operation',
      'workspace',
      { type: 'packages', pip: ['polars'] },
    )(),
  ).rejects.toThrow('polars');
});

test('package requirements over 512 UTF-8 bytes survive proposal, saved draft and partial edits', () => {
  const value = state();
  const requirements = {
    type: 'packages',
    pip: Array.from({ length: 36 }, (_, index) => `dependency-example-${index}==1.2.3`),
    npm: ['@example/other-package@1.2.3'],
  };
  const encoded = JSON.stringify(requirements);
  expect(new TextEncoder().encode(encoded).length).toBeGreaterThan(512);
  const first = proposedAgentConfig(
    { id: 'proposal', name: 'build_agent_config', input: { name: 'Reader', environment_packages: requirements } },
    value,
    ['model'],
  );
  const next = proposedAgentConfig(
    { id: 'rename', name: 'build_agent_config', input: { name: 'Renamed' } },
    { ...value, config: first, savedConfig: first },
    ['model'],
  );
  expect(next.metadata?.quickstart_environment_packages).toBe(encoded);
  expect(newBuilderState(next).config.metadata?.quickstart_environment_packages).toBe(encoded);
});

test.each([null, '', '   '])('an explicit empty system %j remains empty without a file contract', (system) => {
  const value = state();
  value.config.system = 'Existing instructions';
  value.savedConfig = value.config;
  const next = proposedAgentConfig({ id: 'empty', name: 'build_agent_config', input: { system } }, value, ['model']);
  expect(next.system).toBeNull();
});

test('omitted system stays unchanged and a nonempty system receives a generic file delivery contract', () => {
  const value = state();
  value.config.system = 'Keep existing instructions';
  value.savedConfig = value.config;
  expect(
    proposedAgentConfig({ id: 'rename', name: 'build_agent_config', input: { name: 'New' } }, value, ['model']).system,
  ).toBe('Keep existing instructions');
  const next = proposedAgentConfig(
    { id: 'system', name: 'build_agent_config', input: { system: 'Use the user-specified output name.' } },
    value,
    ['model'],
  );
  expect(next.system).toContain('Use the user-specified output name.');
  expect(next.system).toContain('# Platform file delivery contract');
  expect(next.system).toContain('explicitly requested filename');
  expect(next.system).toContain('do not overwrite');
});

test('a changed system ending in the previous delivery contract upgrades it once and keeps explicit user rules', () => {
  const previous =
    '# Platform file delivery contract\n\nThese platform rules take precedence over conflicting file delivery instructions above. When the user requests a deliverable, write it to /mnt/user-data/outputs/<relative-path> (also mounted at /mnt/session/outputs). Only this mount publishes files as downloadable session resources; a path in /home, /tmp or the working directory is not a delivered file. Verify the written file before reporting delivery, and link it using [filename](/outputs/<relative-path>). If publishing fails, report the delivery failure separately from successful calculation. Ordinary source edits stay in the working directory unless the user asks for an exported copy. This contract does not change the requested response format when no file is requested.';
  const value = state();
  value.config.system = `Old behavior\n\n${previous}`;
  value.savedConfig = value.config;
  const first = proposedAgentConfig(
    {
      id: 'edit',
      name: 'build_agent_config',
      input: { system: `Keep an explicitly fixed user filename.\n\n${previous}` },
    },
    value,
    ['model'],
  );
  expect(first.system?.split('# Platform file delivery contract')).toHaveLength(2);
  expect(first.system).toContain('Keep an explicitly fixed user filename.');
  expect(first.system).toContain('explicitly requested filename');
  const second = proposedAgentConfig(
    { id: 'edit2', name: 'build_agent_config', input: { system: first.system } },
    { ...value, config: first },
    ['model'],
  );
  expect(second.system).toBe(first.system);
});
