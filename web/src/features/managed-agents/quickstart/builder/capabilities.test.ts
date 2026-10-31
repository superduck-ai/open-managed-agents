import { afterEach, expect, test } from 'bun:test';
import { assertBuilderCapabilities, builderAgentSummaries, loadBuilderCapabilities } from './capabilities';
import { configuredAgentToolPermission } from '../../agents/tools/model';
import { blankAgentTemplate, createDialogAgentConfig } from '../../agentConfig';
import type { AgentApiResponse } from '../../types';

const originalFetch = globalThis.fetch;
afterEach(() => {
  globalThis.fetch = originalFetch;
});
function agent(): AgentApiResponse {
  return {
    ...createDialogAgentConfig(blankAgentTemplate, 'en', undefined, 'model'),
    id: 'agent_test',
    version: 2,
    archived_at: null,
    created_at: '',
    updated_at: '',
    type: 'agent',
    multiagent: null,
    mcp_servers: [{ type: 'url', name: 'support', url: 'https://support.example/mcp' }],
    tools: [{ type: 'mcp_toolset', mcp_server_name: 'support', configs: [{ name: 'send', enabled: false }] }],
  };
}
function snapshot(version = 2, status = 'ready') {
  return new Response(
    JSON.stringify({
      version,
      data: [
        {
          server_name: 'support',
          status,
          tools: [{ name: 'search', description: 'Search supplied policies' }, { name: 'send' }],
        },
      ],
    }),
    { headers: { 'Content-Type': 'application/json' } },
  );
}

test('failed, stale and archived discovery cannot supply usable actions', async () => {
  for (const response of [() => snapshot(1), () => snapshot(2, 'error'), () => new Response('', { status: 503 })]) {
    globalThis.fetch = (async () => response()) as typeof fetch;
    const capabilities = await loadBuilderCapabilities('org', 'workspace', [agent()]);
    expect(capabilities[0].tools).toEqual([]);
    expect(() => assertBuilderCapabilities(agent(), capabilities)).toThrow('discovered');
  }
  let called = false;
  globalThis.fetch = (async () => {
    called = true;
    return snapshot();
  }) as typeof fetch;
  expect(await loadBuilderCapabilities('org', 'workspace', [{ ...agent(), archived_at: 'today' }])).toEqual([]);
  expect(called).toBe(false);
});

test('ready discovery exposes only permitted actual tools and rejects invented actions', async () => {
  let request = '';
  globalThis.fetch = (async (input) => {
    request = String(input);
    return snapshot();
  }) as typeof fetch;
  const capabilities = await loadBuilderCapabilities('org', 'workspace', [agent()]);
  expect(request).toContain('/organizations/org/workspaces/workspace/agents/agent_test/mcp_tool_catalogs?version=2');
  expect(capabilities[0].tools).toEqual([{ name: 'search', description: 'Search supplied policies' }]);
  for (const defaultConfig of [undefined, { enabled: true, permission_policy: { type: 'always_allow' } }]) {
    const wildcard = {
      ...agent(),
      tools: [{ type: 'mcp_toolset', mcp_server_name: 'support', default_config: defaultConfig, configs: [] }],
    };
    expect(() => assertBuilderCapabilities(wildcard, capabilities)).toThrow('default_config.enabled');
  }
  const restricted = {
    ...agent(),
    tools: [
      {
        type: 'mcp_toolset',
        mcp_server_name: 'support',
        default_config: { enabled: false },
        configs: [{ name: 'search', enabled: true }],
      },
    ],
  };
  expect(() => assertBuilderCapabilities(restricted, capabilities)).not.toThrow();
  expect(configuredAgentToolPermission(restricted, 'mcp__support__send')).toBe('always_deny');
  expect(configuredAgentToolPermission(restricted, 'mcp__support__search')).toBe('always_ask');
  const invented = {
    ...agent(),
    tools: [{ type: 'mcp_toolset', mcp_server_name: 'support', configs: [{ name: 'handoff', enabled: true }] }],
  };
  expect(() => assertBuilderCapabilities(invented, capabilities)).toThrow('not in the discovered');
});

test('agent summaries omit full instructions and resource secrets', () => {
  const resource = { ...agent(), system: 'private instructions', metadata: { token: 'private token' } };
  expect(builderAgentSummaries([resource])).toEqual([
    { id: resource.id, name: resource.name, version: resource.version, description: resource.description },
  ]);
  expect(builderAgentSummaries([{ ...resource, archived_at: 'today' }])).toEqual([]);
});
