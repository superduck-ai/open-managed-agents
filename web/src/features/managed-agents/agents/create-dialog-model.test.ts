import { describe, expect, test } from 'bun:test';
import { createAgentConfigText, parseCreateAgentConfigText } from '../agentConfig';
import { type AgentApiResponse, type CreateAgentInput } from '../types';
import {
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
  test('rejects duplicate built-in and MCP toolsets', () => {
    const duplicateBuiltIns: CreateAgentInput = {
      ...baseDraft,
      tools: [{ type: 'agent_toolset_20260401' }, { type: 'agent_toolset_20260401' }],
    };
    const duplicateMcpToolsets: CreateAgentInput = {
      ...baseDraft,
      mcp_servers: [{ name: 'github', type: 'url', url: 'https://api.githubcopilot.com/mcp/' }],
      tools: [
        ...baseDraft.tools,
        { type: 'mcp_toolset', mcp_server_name: 'github' },
        { type: 'mcp_toolset', mcp_server_name: 'github' },
      ],
    };

    expect(createAgentDraftSchema.safeParse(duplicateBuiltIns).success).toBe(false);
    expect(createAgentDraftSchema.safeParse(duplicateMcpToolsets).success).toBe(false);
  });

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

  test('rejects invalid MCP inputs without changing the draft', () => {
    const invalid = addMcpServer(baseDraft, { name: ' ', url: 'ftp://internal.example/mcp' });
    expect(invalid).toEqual({ ok: false, errors: { name: 'required', url: 'invalid' } });
    expect(baseDraft.mcp_servers).toEqual([]);
    expect(baseDraft.tools).toEqual([{ type: 'agent_toolset_20260401' }]);

    expect(addMcpServer(baseDraft, { name: 'x'.repeat(256), url: `https://example.com/${'x'.repeat(2049)}` })).toEqual({
      ok: false,
      errors: { name: 'too_long', url: 'too_long' },
    });

    for (const url of ['https://user:secret@example.com/mcp', 'https://example.com/mcp#tools']) {
      expect(addMcpServer(baseDraft, { name: 'invalid-runtime-url', url })).toEqual({
        ok: false,
        errors: { url: 'invalid' },
      });
    }
  });

  test('rejects duplicate, conflicting, and over-limit MCP servers', () => {
    const configuredDraft: CreateAgentInput = {
      ...baseDraft,
      mcp_servers: [{ name: 'github', type: 'url', url: 'https://api.githubcopilot.com/mcp/' }],
      tools: [...baseDraft.tools, { type: 'mcp_toolset', mcp_server_name: 'github' }],
    };
    expect(addMcpServer(configuredDraft, { name: ' github ', url: 'https://example.com/mcp' })).toEqual({
      ok: false,
      errors: { name: 'duplicate' },
    });

    const conflictDraft: CreateAgentInput = {
      ...baseDraft,
      tools: [...baseDraft.tools, { type: 'mcp_toolset', mcp_server_name: 'reserved' }],
    };
    expect(addMcpServer(conflictDraft, { name: 'reserved', url: 'https://example.com/mcp' })).toEqual({
      ok: false,
      errors: { name: 'duplicate' },
    });

    const fullDraft: CreateAgentInput = {
      ...baseDraft,
      mcp_servers: Array.from({ length: 20 }, (_, index) => ({
        name: `server-${index}`,
        type: 'url',
        url: `https://server-${index}.example.com/mcp`,
      })),
      tools: [
        ...baseDraft.tools,
        ...Array.from({ length: 20 }, (_, index) => ({
          type: 'mcp_toolset',
          mcp_server_name: `server-${index}`,
        })),
      ],
    };
    expect(addMcpServer(fullDraft, { name: 'overflow', url: 'https://overflow.example.com/mcp' })).toEqual({
      ok: false,
      errors: { form: 'limit' },
    });
  });

  test('adds and removes MCP server and toolset atomically', () => {
    const result = addMcpServer(baseDraft, {
      name: ' github ',
      url: 'https://api.githubcopilot.com/mcp/',
    });
    expect(result.ok).toBe(true);
    if (!result.ok) {
      return;
    }
    const withMcp = result.draft;
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

  test('keeps invalid custom tool schemas observable without offering a create helper', () => {
    const withCustomTool: CreateAgentInput = {
      ...baseDraft,
      tools: [
        ...baseDraft.tools,
        {
          type: 'custom',
          name: 'lookup',
          description: 'Lookup data.',
          input_schema: { type: 'object', properties: {} },
        },
      ],
    };
    const invalid = updateCustomTool(withCustomTool, 1, { input_schema: '{' });
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
