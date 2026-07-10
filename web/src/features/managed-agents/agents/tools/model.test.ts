import { describe, expect, test } from 'bun:test';
import { type AgentApiResponse } from '../../types';
import {
  aggregateToolPermissions,
  buildAgentToolDisplayCards,
  effectiveToolPermission,
  hasConfiguredAgentTools,
  normalizeMcpDirectoryServers
} from './model';

describe('agent tool display model', () => {
  test('does not create content from an orphaned mcp_toolset config', () => {
    const agent = agentFixture({
      tools: [{ type: 'mcp_toolset', mcp_server_name: 'notion' }],
      mcp_servers: []
    });

    expect(hasConfiguredAgentTools(agent)).toBe(false);
    expect(buildAgentToolDisplayCards(agent)).toEqual([]);
  });

  test('keeps an unknown MCP expandable without deriving tool names from permission configs', () => {
    const agent = agentFixture({
      mcp_servers: [{ name: 'private_docs', url: 'https://docs.example.com/mcp' }],
      tools: [{
        type: 'mcp_toolset',
        mcp_server_name: 'private_docs',
        configs: [{ name: 'must_not_be_a_tool_row', enabled: false }]
      }]
    });

    const [card] = buildAgentToolDisplayCards(agent);
    expect(card.title).toBe('Private Docs');
    expect(card.tools).toEqual([]);
    expect(card.aggregatePermission).toBe('always_ask');
  });

  test('derives deny from enabled false and only aggregates known tools', () => {
    expect(effectiveToolPermission({ enabled: false, permission_policy: { type: 'always_allow' } })).toBe('always_deny');
    expect(effectiveToolPermission({ permission_policy: { type: 'always_ask' } })).toBe('always_ask');
    expect(effectiveToolPermission({ permission_policy: { type: 'invalid' } })).toBe('always_allow');
    expect(aggregateToolPermissions(['always_allow', 'always_deny'])).toBe('custom');
    expect(aggregateToolPermissions([])).toBe('always_allow');
    expect(aggregateToolPermissions([], 'always_ask')).toBe('always_ask');
  });

  test('uses built-in legacy name precedence and the first duplicate config while MCP requires name', () => {
    const agent = agentFixture({
      mcp_servers: [{ name: 'notion', url: 'https://mcp.notion.com/mcp' }],
      tools: [
        {
          type: 'agent_toolset_20250301',
          configs: [
            { name: 'read', tool_name: 'bash', enabled: false },
            { name: 'bash', permission_policy: { type: 'always_ask' } },
            { name: 'bash', enabled: false }
          ]
        },
        {
          type: 'mcp_toolset',
          mcp_server_name: 'notion',
          configs: [{ tool_name: 'search', enabled: false }]
        }
      ]
    });
    const cards = buildAgentToolDisplayCards(agent, [{
      slug: 'notion',
      displayName: 'Notion',
      url: 'https://mcp.notion.com/mcp',
      toolNames: ['search']
    }]);

    expect(cards[0].subtitle).toBe('agent_toolset_20260401');
    expect(cards[0].tools.find((tool) => tool.name === 'read')?.permission).toBe('always_deny');
    expect(cards[0].tools.find((tool) => tool.name === 'bash')?.permission).toBe('always_ask');
    expect(cards[1].tools[0].permission).toBe('always_ask');
  });

  test('uses the MCP runtime default when directory tools have no matching toolset', () => {
    const [card] = buildAgentToolDisplayCards(
      agentFixture({
        mcp_servers: [{ name: 'snowflake', url: 'https://tenant.snowflake.example/mcp' }],
        tools: []
      }),
      [{ slug: 'snowflake', displayName: 'Snowflake', toolNames: ['search', 'query'] }]
    );

    expect(card.subtitle).toBe('https://tenant.snowflake.example/mcp');
    expect(card.tools.map((tool) => [tool.name, tool.permission])).toEqual([
      ['search', 'always_ask'],
      ['query', 'always_ask']
    ]);
    expect(card.aggregatePermission).toBe('always_ask');
  });

  test('uses an explicit MCP default for an unknown server with no known tool rows', () => {
    const [card] = buildAgentToolDisplayCards(agentFixture({
      mcp_servers: [{ name: 'private_docs', url: 'https://docs.example.com/mcp' }],
      tools: [{
        type: 'mcp_toolset',
        mcp_server_name: 'private_docs',
        default_config: { permission_policy: { type: 'always_allow' } }
      }]
    }));

    expect(card.tools).toEqual([]);
    expect(card.aggregatePermission).toBe('always_allow');
  });

  test('uses discovered tools as authoritative, including a confirmed empty catalog', () => {
    const agent = agentFixture({
      mcp_servers: [{ name: 'weather', url: 'https://weather.example/mcp' }],
      tools: [{
        type: 'mcp_toolset',
        mcp_server_name: 'weather',
        default_config: { permission_policy: { type: 'always_ask' } },
        configs: [{ name: 'get_forecast', enabled: false }]
      }]
    });
    const directory = [{
      slug: 'weather',
      displayName: 'Weather',
      toolNames: ['stale_directory_tool']
    }];
    const [ready] = buildAgentToolDisplayCards(agent, directory, [{
      server_name: 'weather',
      status: 'ready',
      tools: [{ name: 'get_forecast', title: 'Forecast', description: 'Returns a forecast.' }],
      generation: 1
    }]);
    const [empty] = buildAgentToolDisplayCards(agent, directory, [{
      server_name: 'weather',
      status: 'ready',
      tools: [],
      generation: 2
    }]);

    expect(ready.tools).toEqual([{ name: 'get_forecast', description: 'Returns a forecast.', permission: 'always_deny' }]);
    expect(ready.toolCountKnown).toBe(true);
    expect(empty.tools).toEqual([]);
    expect(empty.toolCountKnown).toBe(true);
    expect(empty.aggregatePermission).toBe('always_ask');
  });

  test('uses the GitHub and Slack fallback catalogs when directory metadata is unavailable', () => {
    const cards = buildAgentToolDisplayCards(agentFixture({
      mcp_servers: [
        { name: 'github', url: 'https://github.example/mcp' },
        { name: 'slack', url: 'https://slack.example/mcp' }
      ],
      tools: []
    }));

    expect(cards.map((card) => card.title)).toEqual(['GitHub', 'Slack']);
    expect(cards.map((card) => card.subtitle)).toEqual([
      'https://github.example/mcp',
      'https://slack.example/mcp'
    ]);
    expect(cards[0].tools.some((tool) => tool.name === 'search_repositories')).toBe(true);
    expect(cards[1].tools.some((tool) => tool.name === 'slack_send_message')).toBe(true);
    expect(cards.every((card) => card.aggregatePermission === 'always_ask')).toBe(true);
  });

  test('renders built-in, custom, and MCP cards together in their required order', () => {
    const agent = agentFixture({
      mcp_servers: [{ name: 'notion', url: 'https://agent.example.com/notion' }],
      tools: [
        {
          type: 'agent_toolset_20260401',
          default_config: { permission_policy: { type: 'always_ask' } },
          configs: [
            { tool_name: 'bash', enabled: false },
            { name: 'read', permission_policy: { type: 'always_allow' } }
          ]
        },
        { type: 'agent_toolset_future', default_config: { enabled: false } },
        { type: 'custom', name: 'lookup_customer', description: 'Find a customer' },
        {
          type: 'mcp_toolset',
          mcp_server_name: 'notion',
          default_config: { permission_policy: { type: 'always_ask' } },
          configs: [{ name: 'search', enabled: false }]
        }
      ]
    });
    const directory = [{
      slug: 'notion',
      displayName: 'Notion',
      url: 'https://directory.example.com/notion',
      iconUrl: 'https://example.com/notion.png',
      toolNames: ['search', 'create_page']
    }];

    const cards = buildAgentToolDisplayCards(agent, directory);
    expect(cards.map((card) => card.kind)).toEqual(['built-in', 'custom', 'mcp']);
    expect(cards[0].subtitle).toBe('agent_toolset_20260401');
    expect(cards[0].aggregatePermission).toBe('custom');
    expect(cards[0].tools.find((tool) => tool.name === 'bash')?.permission).toBe('always_deny');
    expect(cards[0].tools.find((tool) => tool.name === 'write')?.permission).toBe('always_ask');
    expect(cards[1].tools).toEqual([{ name: 'lookup_customer', description: 'Find a customer' }]);
    expect(cards[2].title).toBe('Notion');
    expect(cards[2].subtitle).toBe('https://agent.example.com/notion');
    expect(cards[2].aggregatePermission).toBe('custom');
    expect(cards[2].tools.map((tool) => [tool.name, tool.permission])).toEqual([
      ['search', 'always_deny'],
      ['create_page', 'always_ask']
    ]);
  });

  test('normalizes directory fields and resolves tunnel display names from the configured URL', () => {
    const directory = normalizeMcpDirectoryServers({
      servers: [{
        type: 'remote',
        slug: 'notion',
        display_name: 'Notion',
        icon_url: 'https://example.com/icon.png',
        tool_names: ['search', null, 12],
        remote: { url: 'https://directory.example.com/mcp' }
      }]
    });
    expect(directory).toEqual([{
      slug: 'notion',
      displayName: 'Notion',
      iconUrl: 'https://example.com/icon.png',
      toolNames: ['search'],
      url: 'https://directory.example.com/mcp'
    }]);

    const [tunnel] = buildAgentToolDisplayCards(agentFixture({
      mcp_servers: [{ name: 'tunnel:fallback-id', url: 'https://wiki.example.com/mcp' }]
    }));
    expect(tunnel.title).toBe('wiki.example.com');
    expect(tunnel.tools).toEqual([]);
  });

  test('uses a directory URL option when the canonical remote URL is absent', () => {
    expect(normalizeMcpDirectoryServers({
      servers: [{
        type: 'remote',
        slug: 'datadog',
        display_name: 'Datadog',
        tool_names: ['search_datadog'],
        remote: { url: null, url_options: [{ url: 'https://example.datadoghq.com/mcp' }] }
      }]
    })).toEqual([{
      slug: 'datadog',
      displayName: 'Datadog',
      url: 'https://example.datadoghq.com/mcp',
      toolNames: ['search_datadog']
    }]);
  });

  test('keeps remote tenant metadata without a concrete URL and filters local entries', () => {
    expect(normalizeMcpDirectoryServers({
      servers: [
        {
          type: 'remote',
          visibility: ['commercial'],
          slug: 'snowflake',
          display_name: 'Snowflake',
          tool_names: ['search', 'query'],
          remote: {
            url: null,
            url_regex: '^https://.+\\.snowflakecomputing\\.com/mcp$'
          }
        },
        {
          type: 'local',
          name: 'Desktop Commander',
          tool_names: ['execute_command']
        }
      ]
    })).toEqual([{
      slug: 'snowflake',
      displayName: 'Snowflake',
      toolNames: ['search', 'query']
    }]);
  });
});

function agentFixture(overrides: Partial<AgentApiResponse>): AgentApiResponse {
  return {
    id: 'agent_fixture1234567890',
    archived_at: null,
    created_at: '2026-07-10T00:00:00Z',
    description: null,
    mcp_servers: [],
    metadata: {},
    model: 'claude-sonnet-4-6',
    multiagent: null,
    name: 'Fixture agent',
    skills: [],
    system: null,
    tools: [],
    type: 'agent',
    updated_at: '2026-07-10T00:00:00Z',
    version: 1,
    ...overrides
  };
}
