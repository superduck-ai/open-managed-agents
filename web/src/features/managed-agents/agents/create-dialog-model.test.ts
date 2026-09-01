import { describe, expect, test } from 'bun:test';
import { createAgentConfigText, parseCreateAgentConfigText } from '../agentConfig';
import { type AgentApiResponse, type CreateAgentInput } from '../types';
import {
  addCustomTool,
  addBuiltInToolset,
  addMcpServer,
  createAgentDraftSchema,
  removeToolset,
  setToolPermission,
  setToolsetPermission,
  toggleSkill,
  toggleSubagent,
  toolsetPermission,
  updateCustomTool,
  updateDraftModelID,
  updateMcpServer,
} from './create-dialog-model';

const baseDraft: CreateAgentInput = {
  name: 'Draft agent',
  description: null,
  model: { id: 'claude-sonnet-4-6', speed: 'fast' },
  system: null,
  mcp_servers: [],
  tools: [{ type: 'agent_toolset_20260401' }],
  skills: [],
};

describe('create agent draft model', () => {
  test('round trips supported YAML and JSON fields without accepting model effort', () => {
    const input: CreateAgentInput = {
      ...baseDraft,
      metadata: { source: 'test' },
      multiagent: { type: 'coordinator', agents: [{ type: 'self' }] },
    };

    for (const format of ['YAML', 'JSON'] as const) {
      const parsed = parseCreateAgentConfigText(createAgentConfigText(input, format), format);
      expect(parsed.ok).toBe(true);
      if (parsed.ok) {
        expect(parsed.input).toEqual(input);
      }
    }

    const invalid = parseCreateAgentConfigText(
      JSON.stringify({ ...baseDraft, model: { id: 'claude-sonnet-4-6', effort: 'high' } }),
      'JSON',
    );
    expect(invalid.ok).toBe(false);
  });

  test('preserves speed while changing the rendered model id', () => {
    expect(updateDraftModelID(baseDraft.model, 'claude-opus-4-8')).toEqual({
      id: 'claude-opus-4-8',
      speed: 'fast',
    });
  });

  test('toggles unique subagents and skills with pinned defaults', () => {
    const agent = {
      id: 'agent_helper',
      name: 'Helper',
      version: 3,
      archived_at: null,
    } as AgentApiResponse;
    const withAgent = toggleSubagent(baseDraft, agent);
    expect(withAgent.multiagent).toEqual({
      type: 'coordinator',
      agents: [{ type: 'agent', id: 'agent_helper', version: 3 }],
    });
    expect(toggleSubagent(withAgent, agent).multiagent).toBeNull();

    const withSkill = toggleSkill(baseDraft, {
      id: 'skill_report',
      displayTitle: 'Report',
      latestVersion: '4',
      source: 'custom',
    });
    expect(withSkill.skills).toEqual([{ type: 'custom', skill_id: 'skill_report', version: 'latest' }]);
    expect(
      toggleSkill(withSkill, { id: 'skill_report', displayTitle: 'Report', latestVersion: '4', source: 'custom' })
        .skills,
    ).toEqual([]);
  });

  test('counts a self reference toward the 20-subagent limit', () => {
    const fullDraft: CreateAgentInput = {
      ...baseDraft,
      multiagent: {
        type: 'coordinator',
        agents: [
          { type: 'self' },
          ...Array.from({ length: 19 }, (_, index) => ({
            type: 'agent' as const,
            id: `agent_${index}`,
            version: 1,
          })),
        ],
      },
    };

    expect(
      toggleSubagent(fullDraft, {
        id: 'agent_over_limit',
        name: 'Over limit',
        version: 1,
      } as AgentApiResponse).multiagent,
    ).toEqual(fullDraft.multiagent);
  });

  test('adds and removes MCP server and toolset atomically', () => {
    const withMcp = addMcpServer(baseDraft, {
      slug: 'github',
      displayName: 'GitHub',
      url: 'https://api.githubcopilot.com/mcp/',
      toolNames: ['search_code'],
    });
    expect(withMcp.mcp_servers).toEqual([{ name: 'github', type: 'url', url: 'https://api.githubcopilot.com/mcp/' }]);
    expect(withMcp.tools[1]).toEqual({
      type: 'mcp_toolset',
      mcp_server_name: 'github',
      default_config: { enabled: true, permission_policy: { type: 'always_ask' } },
      configs: [],
    });
    expect(removeToolset(withMcp, 'github')).toEqual(baseDraft);
  });

  test('updates an MCP server and matching toolset atomically without losing permissions', () => {
    const withMcp = addMcpServer(baseDraft, {
      slug: 'tunnel_example__main',
      displayName: 'Private tools',
      url: 'https://oma.example.com/v1/mcp/tunnel_example',
      toolNames: [],
    });
    const withPermission = setToolPermission(
      withMcp,
      (tool) => tool.type === 'mcp_toolset',
      'search_records',
      'always_allow',
      'always_ask',
    );
    const updated = updateMcpServer(withPermission, 'tunnel_example__main', {
      slug: 'tunnel_example__secondary',
      displayName: 'Private tools',
      url: 'https://oma.example.com/v1/mcp/tunnel_example/secondary',
      toolNames: [],
    });

    expect(updated.mcp_servers).toEqual([
      {
        name: 'tunnel_example__secondary',
        type: 'url',
        url: 'https://oma.example.com/v1/mcp/tunnel_example/secondary',
      },
    ]);
    expect(updated.tools[1]).toEqual({
      type: 'mcp_toolset',
      mcp_server_name: 'tunnel_example__secondary',
      default_config: { enabled: true, permission_policy: { type: 'always_ask' } },
      configs: [{ name: 'search_records', enabled: true, permission_policy: { type: 'always_allow' } }],
    });
  });

  test('updates every matching MCP toolset reference without changing their permissions or order', () => {
    const withMcp = addMcpServer(baseDraft, {
      slug: 'tunnel_example__main',
      displayName: 'Private tools',
      url: 'https://oma.example.com/v1/mcp/tunnel_example',
      toolNames: [],
    });
    const duplicateReference = {
      ...withMcp,
      tools: [
        ...withMcp.tools,
        {
          type: 'mcp_toolset' as const,
          mcp_server_name: 'tunnel_example__main',
          default_config: { permission_policy: { type: 'always_allow' as const } },
          configs: [],
        },
      ],
    };

    const updated = updateMcpServer(duplicateReference, 'tunnel_example__main', {
      slug: 'tunnel_example__secondary',
      displayName: 'Private tools',
      url: 'https://oma.example.com/v1/mcp/tunnel_example/secondary',
      toolNames: [],
    });

    expect(
      updated.tools
        .filter((tool) => tool.type === 'mcp_toolset')
        .map((tool) => ({ name: tool.mcp_server_name, default_config: tool.default_config })),
    ).toEqual([
      {
        name: 'tunnel_example__secondary',
        default_config: { enabled: true, permission_policy: { type: 'always_ask' } },
      },
      {
        name: 'tunnel_example__secondary',
        default_config: { permission_policy: { type: 'always_allow' } },
      },
    ]);
  });

  test('does not update an MCP server to a duplicate or orphan its toolset', () => {
    const first = addMcpServer(baseDraft, {
      slug: 'first',
      displayName: 'First',
      url: 'https://first.example.com/mcp',
      toolNames: [],
    });
    const second = addMcpServer(first, {
      slug: 'second',
      displayName: 'Second',
      url: 'https://second.example.com/mcp',
      toolNames: [],
    });

    expect(
      updateMcpServer(second, 'first', {
        slug: 'second',
        displayName: 'Duplicate',
        url: 'https://duplicate.example.com/mcp',
        toolNames: [],
      }),
    ).toBe(second);
    expect(
      updateMcpServer(baseDraft, 'missing', {
        slug: 'replacement',
        displayName: 'Replacement',
        url: 'https://replacement.example.com/mcp',
        toolNames: [],
      }),
    ).toBe(baseDraft);
  });

  test('restores the removed built-in toolset without duplicating it', () => {
    const withoutBuiltIns = removeToolset(baseDraft, 'agent_toolset_20260401');

    expect(addBuiltInToolset(withoutBuiltIns).tools).toEqual([{ type: 'agent_toolset_20260401' }]);
    expect(addBuiltInToolset(baseDraft)).toBe(baseDraft);
  });

  test('canonicalizes group and per-tool permission writes', () => {
    const groupDenied = setToolsetPermission(baseDraft, () => true, 'always_deny');
    expect(groupDenied.tools[0].default_config).toEqual({
      enabled: false,
      permission_policy: { type: 'always_allow' },
    });
    expect(groupDenied.tools[0].configs).toEqual([]);

    const askBash = setToolPermission(baseDraft, () => true, 'bash', 'always_ask', 'always_allow');
    expect(askBash.tools[0].configs).toEqual([
      { name: 'bash', enabled: true, permission_policy: { type: 'always_ask' } },
    ]);
    expect(toolsetPermission(askBash.tools[0], ['bash', 'read'], 'always_allow')).toBe('custom');
    expect(setToolPermission(askBash, () => true, 'bash', 'always_allow', 'always_allow').tools[0].configs).toEqual([]);
  });

  test('creates unique custom tool names and makes invalid schemas observable', () => {
    const first = addCustomTool(baseDraft);
    const second = addCustomTool(first);
    expect(first.tools[1].name).toBe('new_tool');
    expect(second.tools[2].name).toBe('new_tool_2');

    const invalid = updateCustomTool(first, 1, { input_schema: '{' });
    const parsed = parseCreateAgentConfigText(JSON.stringify(invalid), 'JSON');
    expect(parsed.ok).toBe(false);
  });

  test('rejects tool contracts that the Agent API cannot normalize', () => {
    const invalidDrafts: CreateAgentInput[] = [
      {
        ...baseDraft,
        mcp_servers: [{ name: 'github', type: 'stdio', url: 'https://example.com/mcp' }],
        tools: [{ type: 'mcp_toolset', mcp_server_name: 'github' }],
      },
      {
        ...baseDraft,
        tools: [
          {
            type: 'agent_toolset_20260401',
            default_config: { enabled: true, permission_policy: { type: 'sometimes_ask' } },
          },
        ],
      },
      {
        ...baseDraft,
        tools: [{ type: 'custom', name: 'lookup', description: 'Lookup data.', input_schema: { type: 'array' } }],
      },
    ];

    for (const draft of invalidDrafts) {
      expect(createAgentDraftSchema.safeParse(draft).success).toBe(false);
    }

    expect(
      createAgentDraftSchema.safeParse({
        ...baseDraft,
        tools: [
          {
            type: 'custom',
            name: 'lookup',
            description: 'Lookup data.',
            input_schema: { type: 'object', properties: { id: { type: 'string' } } },
          },
        ],
      }).success,
    ).toBe(true);
  });
});
