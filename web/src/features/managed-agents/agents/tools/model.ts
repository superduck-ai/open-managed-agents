import { type AgentApiResponse } from '../../types';

export type ToolPermissionState = 'always_allow' | 'always_ask' | 'always_deny' | 'custom';

export type BuiltInAgentTool = {
  name: string;
  description: string;
};

export type AgentToolListItem = {
  name: string;
  description?: string;
  permission?: Exclude<ToolPermissionState, 'custom'>;
};

export type AgentToolDisplayCard = {
  key: string;
  kind: 'built-in' | 'custom' | 'mcp';
  title: string;
  subtitle: string;
  iconUrl?: string;
  aggregatePermission?: ToolPermissionState;
  tools: AgentToolListItem[];
  toolCountKnown?: boolean;
  catalogStatus?: McpToolCatalogStatus;
  catalogError?: { code: string; message: string };
  serverName?: string;
  discoveredAt?: string;
};

export type McpToolCatalogStatus = 'unknown' | 'loading' | 'ready' | 'refreshing' | 'stale' | 'auth_required' | 'error';

export type McpToolCatalog = {
  server_name: string;
  endpoint_fingerprint?: string;
  status: McpToolCatalogStatus;
  // null 表示尚无成功发现快照；[] 表示 MCP 已成功报告零个工具，两者不能合并处理。
  tools: Array<{ name: string; title?: string; description?: string }> | null;
  source?: 'anonymous_probe' | 'runtime_observation';
  protocol_version?: string;
  discovered_at?: string;
  expires_at?: string;
  last_error?: { code: string; message: string };
  generation: number;
};

export type McpDirectoryServer = {
  slug: string;
  displayName: string;
  url?: string;
  iconUrl?: string;
  toolNames: string[];
};

type ResolvedMcpServer = Omit<McpDirectoryServer, 'url'> & {
  url: string;
};

export const BUILT_IN_AGENT_TOOLSETS: Record<string, BuiltInAgentTool[]> = {
  agent_toolset_20260401: [
    { name: 'bash', description: 'Execute bash commands' },
    { name: 'read', description: 'Read files' },
    { name: 'write', description: 'Write files' },
    { name: 'edit', description: 'String replacement in files' },
    { name: 'glob', description: 'File pattern matching' },
    { name: 'grep', description: 'Text search with regex' },
    { name: 'web_fetch', description: 'Fetch URL content' },
    { name: 'web_search', description: 'Search the web' }
  ]
};

const CURRENT_BUILT_IN_TOOLSET = 'agent_toolset_20260401';

const FALLBACK_MCP_SERVERS: McpDirectoryServer[] = [
  {
    slug: 'github',
    displayName: 'GitHub',
    url: 'https://api.githubcopilot.com/mcp/',
    iconUrl: 'https://github.com/favicon.ico',
    toolNames: [
      'actions_get',
      'actions_list',
      'actions_run_trigger',
      'add_comment_to_pending_review',
      'add_issue_comment',
      'add_reply_to_pull_request_comment',
      'assign_copilot_to_issue',
      'create_branch',
      'create_gist',
      'create_or_update_file',
      'create_pull_request',
      'create_pull_request_with_copilot',
      'create_repository',
      'delete_file',
      'dismiss_notification',
      'fork_repository',
      'get_code_scanning_alert',
      'get_commit',
      'get_copilot_space',
      'get_dependabot_alert',
      'get_discussion',
      'get_discussion_comments',
      'get_file_contents',
      'get_gist',
      'get_global_security_advisory',
      'get_job_logs',
      'get_label',
      'get_latest_release',
      'get_me',
      'get_notification_details',
      'get_release_by_tag',
      'get_repository_tree',
      'get_secret_scanning_alert',
      'get_tag',
      'get_team_members',
      'get_teams',
      'github_support_docs_search',
      'issue_read',
      'issue_write',
      'label_write',
      'list_branches',
      'list_code_scanning_alerts',
      'list_commits',
      'list_copilot_spaces',
      'list_dependabot_alerts',
      'list_discussion_categories',
      'list_discussions',
      'list_gists',
      'list_global_security_advisories',
      'list_issue_types',
      'list_issues',
      'list_label',
      'list_notifications',
      'list_org_repository_security_advisories',
      'list_pull_requests',
      'list_releases',
      'list_repository_security_advisories',
      'list_secret_scanning_alerts',
      'list_starred_repositories',
      'list_tags',
      'manage_notification_subscription',
      'manage_repository_notification_subscription',
      'mark_all_notifications_read',
      'merge_pull_request',
      'projects_get',
      'projects_list',
      'projects_write',
      'pull_request_read',
      'pull_request_review_write',
      'push_files',
      'request_copilot_review',
      'search_code',
      'search_issues',
      'search_orgs',
      'search_pull_requests',
      'search_repositories',
      'search_users',
      'star_repository',
      'sub_issue_write',
      'unstar_repository',
      'update_gist',
      'update_pull_request',
      'update_pull_request_branch'
    ]
  },
  {
    slug: 'slack',
    displayName: 'Slack',
    url: 'https://mcp.slack.com/mcp',
    iconUrl: 'https://slack.com',
    toolNames: [
      'slack_create_canvas',
      'slack_read_canvas',
      'slack_read_channel',
      'slack_read_thread',
      'slack_read_user_profile',
      'slack_search_channels',
      'slack_search_public',
      'slack_search_public_and_private',
      'slack_search_users',
      'slack_send_message',
      'slack_update_canvas'
    ]
  }
];

export function hasConfiguredAgentTools(agent: AgentApiResponse) {
  const tools = arrayRecords(agent.tools);
  return (
    tools.some((tool) => stringValue(tool.type).startsWith('agent_toolset_')) ||
    tools.some((tool) => tool.type === 'custom') ||
    arrayRecords(agent.mcp_servers).length > 0
  );
}

export function effectiveToolPermission(
  config: Record<string, unknown> | undefined,
  fallback: Exclude<ToolPermissionState, 'custom'> = 'always_allow'
): Exclude<ToolPermissionState, 'custom'> {
  if (!config) {
    return fallback;
  }
  if (config.enabled === false) {
    return 'always_deny';
  }
  const policy = recordValue(config.permission_policy).type;
  return policy === 'always_allow' || policy === 'always_ask' ? policy : fallback;
}

export function aggregateToolPermissions(
  permissions: Array<Exclude<ToolPermissionState, 'custom'>>,
  fallback: Exclude<ToolPermissionState, 'custom'> = 'always_allow'
): ToolPermissionState {
  if (!permissions.length) {
    return fallback;
  }
  return new Set(permissions).size === 1 ? permissions[0] : 'custom';
}

export function buildAgentToolDisplayCards(
  agent: AgentApiResponse,
  directoryServers: McpDirectoryServer[] = [],
  catalogs: McpToolCatalog[] = []
): AgentToolDisplayCard[] {
  const cards: AgentToolDisplayCard[] = [];
  const tools = arrayRecords(agent.tools);
  const builtInToolset = tools.find((tool) => stringValue(tool.type).startsWith('agent_toolset_'));

  if (builtInToolset) {
    const type = CURRENT_BUILT_IN_TOOLSET;
    const definitions = BUILT_IN_AGENT_TOOLSETS[type];
    const rows = definitions.map((definition) => ({
      ...definition,
      permission: toolPermissionForName(builtInToolset, definition.name, true, 'always_allow')
    }));
    cards.push({
      key: `built-in:${type}`,
      kind: 'built-in',
      title: 'Built-in tools',
      subtitle: type,
      aggregatePermission: aggregateToolPermissions(rows.map((row) => row.permission)),
      tools: rows
    });
  }

  const customTools = tools.filter((tool) => tool.type === 'custom');
  if (customTools.length) {
    cards.push({
      key: 'custom-tools',
      kind: 'custom',
      title: 'Custom tools',
      subtitle: 'Client-handled tool definitions',
      tools: customTools.map((tool, index) => ({
        name: stringValue(tool.name) || `custom_tool_${index + 1}`,
        description: optionalStringValue(tool.description)
      }))
    });
  }

  arrayRecords(agent.mcp_servers).forEach((configuredServer, index) => {
    const serverName = stringValue(configuredServer.name) || `mcp-server-${index + 1}`;
    const configuredUrl =
      stringValue(configuredServer.url) ||
      stringValue(configuredServer.server_url) ||
      stringValue(configuredServer.mcp_server_url);
    const server = resolveMcpServer(serverName, configuredUrl, directoryServers);
    const toolset = tools.find(
      (tool) => tool.type === 'mcp_toolset' && stringValue(tool.mcp_server_name) === serverName
    );
    const catalog = catalogs.find((item) => item.server_name === serverName);
    const discoveredTools = catalog?.tools;
    // 动态 catalog 的非 null tools 是权威快照（包括真实空数组）；仅在 null/缺失时回退
    // Directory，不能与静态工具名做 union，否则已下线的工具会继续显示。
    const rows = discoveredTools !== null && discoveredTools !== undefined
      ? discoveredTools.map((tool) => ({
          name: tool.name,
          description: tool.description || tool.title,
          permission: toolPermissionForName(toolset, tool.name, false, 'always_ask')
        }))
      : server.toolNames.map((name) => ({
          name,
          permission: toolPermissionForName(toolset, name, false, 'always_ask')
        }));
    const defaultPermission = effectiveToolPermission(
      toolset ? optionalRecordValue(toolset.default_config) : undefined,
      'always_ask'
    );
    cards.push({
      key: `mcp:${serverName}:${index}`,
      kind: 'mcp',
      title: server.displayName,
      subtitle: server.url,
      iconUrl: server.iconUrl,
      aggregatePermission: aggregateToolPermissions(rows.map((row) => row.permission), defaultPermission),
      tools: rows,
      toolCountKnown: discoveredTools !== null && discoveredTools !== undefined ? true : rows.length > 0,
      catalogStatus: catalog?.status,
      catalogError: catalog?.last_error,
      serverName,
      discoveredAt: catalog?.discovered_at
    });
  });

  return cards;
}

export function normalizeMcpDirectoryServers(payload: unknown): McpDirectoryServer[] {
  const root = recordValue(payload);
  const rawServers = Array.isArray(payload) ? payload : Array.isArray(root.servers) ? root.servers : [];
  return arrayRecords(rawServers).flatMap((server) => {
    const serverType = stringValue(server.type);
    const visibility = Array.isArray(server.visibility)
      ? server.visibility.filter((value): value is string => typeof value === 'string')
      : [];
    // Directory 路由目前不执行查询参数过滤，因此前端再次筛选 remote/commercial；
    // 租户型条目即使没有固定 URL 也必须保留其 slug 和工具元数据。
    if ((serverType && serverType !== 'remote') || (visibility.length && !visibility.includes('commercial'))) {
      return [];
    }
    const slug = stringValue(server.slug) || stringValue(server.name).toLowerCase();
    const remote = recordValue(server.remote);
    const urlOption = arrayRecords(remote.url_options)[0];
    const rawUrl = stringValue(remote.url) || stringValue(urlOption?.url) || stringValue(server.url);
    const url = rawUrl && !rawUrl.includes('{') ? rawUrl : undefined;
    if (!slug) {
      return [];
    }
    const displayName =
      stringValue(server.display_name) ||
      stringValue(server.displayName) ||
      stringValue(server.name) ||
      humanizeMcpName(slug);
    const rawToolNames = Array.isArray(server.tool_names)
      ? server.tool_names
      : Array.isArray(server.toolNames)
        ? server.toolNames
        : [];
    return [
      {
        slug,
        displayName,
        ...(url ? { url } : {}),
        iconUrl: optionalStringValue(server.icon_url) ?? optionalStringValue(server.iconUrl),
        toolNames: rawToolNames.filter((name): name is string => typeof name === 'string' && Boolean(name))
      }
    ];
  });
}

function toolPermissionForName(
  toolset: Record<string, unknown> | undefined,
  toolName: string,
  allowLegacyToolName: boolean,
  fallback: Exclude<ToolPermissionState, 'custom'>
): Exclude<ToolPermissionState, 'custom'> {
  if (!toolset) {
    return fallback;
  }
  // 与运行时保持 first-wins：按配置顺序取首个同名项。内置工具兼容 legacy tool_name，
  // MCP 只匹配 name；未命中时再使用 default_config。
  const override = arrayRecords(toolset.configs).find(
    (config) =>
      (stringValue(config.name) || (allowLegacyToolName ? stringValue(config.tool_name) : '')) === toolName
  );
  return effectiveToolPermission(override ?? optionalRecordValue(toolset.default_config), fallback);
}

function resolveMcpServer(name: string, url: string, directoryServers: McpDirectoryServer[]): ResolvedMcpServer {
  if (name.startsWith('tunnel:')) {
    return {
      slug: name,
      displayName: urlHost(url) || name.slice('tunnel:'.length),
      url,
      toolNames: []
    };
  }

  const metadata =
    directoryServers.find((server) => server.slug === name) ??
    FALLBACK_MCP_SERVERS.find((server) => server.slug === name);
  // 展示元数据优先级：在线 Directory > 内置 GitHub/Slack fallback > 名称格式化；
  // 实际 URL 始终以当前 Agent 版本配置为准。
  if (metadata) {
    return { ...metadata, slug: name, url };
  }
  return {
    slug: name,
    displayName: humanizeMcpName(name),
    url,
    toolNames: []
  };
}

function humanizeMcpName(value: string) {
  return value
    .split(/[-_\s]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function urlHost(value: string) {
  if (!value) {
    return '';
  }
  try {
    return new URL(value).host;
  } catch {
    return '';
  }
}

function arrayRecords(value: unknown): Array<Record<string, unknown>> {
  return Array.isArray(value)
    ? value.filter((item) => Boolean(item) && typeof item === 'object' && !Array.isArray(item)).map(recordValue)
    : [];
}

function recordValue(value: unknown): Record<string, unknown> {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : {};
}

function optionalRecordValue(value: unknown) {
  return value && typeof value === 'object' && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
}

function stringValue(value: unknown) {
  return typeof value === 'string' ? value : '';
}

function optionalStringValue(value: unknown) {
  return typeof value === 'string' && value ? value : undefined;
}
