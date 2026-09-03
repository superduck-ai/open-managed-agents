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
  test('round trips supported YAML and JSON fields including model effort', () => {
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

    const withEffort = parseCreateAgentConfigText(
      JSON.stringify({ ...baseDraft, model: { id: 'claude-sonnet-4-6', effort: 'high' } }),
      'JSON',
    );
    expect(withEffort.ok).toBe(true);
    if (withEffort.ok) {
      expect(withEffort.input.model).toEqual({ id: 'claude-sonnet-4-6', effort: 'high' });
    }
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
