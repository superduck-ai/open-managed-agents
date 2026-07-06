import { type ReactNode } from 'react';
import { createdFilterStartISOString } from '../api';
import { StatusPill } from '../components/common';
import { numericValueFromKeys } from '../sessions/SessionDetailPage';
import { type AgentApiResponse, type AgentDetailCreatedFilter, type AgentDetailStatusFilter, type AgentDetailTab, type AgentDetailVersionFilter, type AgentListFilters, type AgentSessionAnalyticsOverview, type AgentToolCardModel, type AgentToolPermission, type AnalyticsMetricBucket, type SessionApiResponse } from '../types';
import { objectRecord } from '../utils';

export const emptyAgents: AgentApiResponse[] = [];

export function rowFromAgent(agent: AgentApiResponse): Record<string, ReactNode> {
  return {
    ID: compactAgentId(agent.id),
    Name: agent.name || 'Untitled agent',
    Model: agentModelName(agent.model),
    Status: <StatusPill>{agent.archived_at ? 'Archived' : 'Active'}</StatusPill>,
    Created: relativeTime(agent.created_at),
    'Last updated': relativeTime(agent.updated_at)
  };
}

export function compactAgentId(id: string) {
  if (id.length <= 22) {
    return id;
  }
  return `${id.slice(0, 12)}...${id.slice(-6)}`;
}

export function agentModelName(model: AgentApiResponse['model']) {
  if (typeof model === 'string') {
    return model;
  }
  return model?.id || 'claude-sonnet-4-6';
}

export function relativeTime(value: string) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return '—';
  }
  const seconds = Math.max(0, Math.floor((Date.now() - timestamp) / 1000));
  if (seconds < 60) {
    return 'just now';
  }
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) {
    return `${minutes} minute${minutes === 1 ? '' : 's'} ago`;
  }
  const hours = Math.floor(minutes / 60);
  if (hours < 24) {
    return `${hours} hour${hours === 1 ? '' : 's'} ago`;
  }
  const days = Math.floor(hours / 24);
  return `${days} day${days === 1 ? '' : 's'} ago`;
}

export function agentSkillLabel(skill: unknown) {
  if (typeof skill === 'string') {
    return skill;
  }
  const record = objectRecord(skill);
  return String(record.skill_id || record.name || record.id || 'skill');
}

export const BUILT_IN_AGENT_TOOLSETS: Record<string, AgentToolPermission[]> = {
  agent_toolset_20260401: [
    { label: 'Bash', name: 'bash', description: 'Execute bash commands in a shell session' },
    { label: 'Read', name: 'read', description: 'Read a file from the local filesystem' },
    { label: 'Write', name: 'write', description: 'Write a file to the local filesystem' },
    { label: 'Edit', name: 'edit', description: 'Perform string replacement in a file' },
    { label: 'Glob', name: 'glob', description: 'Fast file pattern matching using glob patterns' },
    { label: 'Grep', name: 'grep', description: 'Text search using regex patterns' },
    { label: 'Web fetch', name: 'web_fetch', description: 'Fetch content from a URL' },
    { label: 'Web search', name: 'web_search', description: 'Search the web for information' }
  ]
};

export function agentMatchesClientFilters(agent: AgentApiResponse, filters: AgentListFilters, applyCreatedFilter: boolean) {
  if (filters.status !== 'all' && agent.archived_at) {
    return false;
  }
  if (applyCreatedFilter) {
    const createdAtGTE = createdFilterStartISOString(filters.created);
    if (createdAtGTE && Date.parse(agent.created_at) < Date.parse(createdAtGTE)) {
      return false;
    }
  }
  return true;
}

export function agentDetailTabFromSearch(): AgentDetailTab {
  if (typeof window === 'undefined') {
    return 'config';
  }
  const tab = new URLSearchParams(window.location.search).get('tab');
  switch (tab) {
    case 'sessions':
    case 'deployments':
    case 'observability':
      return tab;
    case 'agent':
    case 'config':
    default:
      return 'config';
  }
}

export function agentDetailVersionFromSearch() {
  if (typeof window === 'undefined') {
    return null;
  }
  const rawVersion = new URLSearchParams(window.location.search).get('version_id');
  const parsed = rawVersion ? Number(rawVersion) : 0;
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

export function agentDetailDeploymentFromSearch() {
  if (typeof window === 'undefined') {
    return null;
  }
  const value = new URLSearchParams(window.location.search).get('deployment');
  return value?.trim() || null;
}

export function agentDetailSessionCreatedFromSearch(): AgentDetailCreatedFilter {
  if (typeof window === 'undefined') {
    return 'all_time';
  }
  const value = new URLSearchParams(window.location.search).get('created');
  switch (value) {
    case 'today':
    case 'last_hour':
    case 'last_day':
    case 'last_7_days':
    case 'last_30_days':
      return value;
    case 'all_time':
    case null:
    default:
      return 'all_time';
  }
}

export function agentDetailSessionVersionFromSearch(): AgentDetailVersionFilter {
  if (typeof window === 'undefined') {
    return null;
  }
  const params = new URLSearchParams(window.location.search);
  const rawVersion = params.get('agent_version') ?? params.get('version_id');
  const parsed = rawVersion ? Number(rawVersion) : 0;
  return Number.isInteger(parsed) && parsed > 0 ? parsed : null;
}

export function agentDetailSessionDeploymentFromSearch() {
  if (typeof window === 'undefined') {
    return '';
  }
  return new URLSearchParams(window.location.search).get('deployment_id')?.trim() || '';
}

export function agentDetailSessionStatusFromSearch(): AgentDetailStatusFilter {
  if (typeof window === 'undefined') {
    return 'all';
  }
  const value = new URLSearchParams(window.location.search).get('status')?.trim();
  switch (value) {
    case 'active':
    case 'idle':
    case 'running':
    case 'rescheduling':
    case 'terminated':
      return value;
    case 'running,idle,rescheduling':
    case 'rescheduling,running,idle':
      return 'active';
    case 'all':
    case null:
    default:
      return 'all';
  }
}

export function writeAgentSessionFiltersToUrl(filters: {
  created: AgentDetailCreatedFilter;
  version: AgentDetailVersionFilter;
  deploymentId: string;
  status: AgentDetailStatusFilter;
}) {
  if (typeof window === 'undefined') {
    return;
  }
  const url = new URL(window.location.href);
  if (filters.created === 'all_time') {
    url.searchParams.delete('created');
  } else {
    url.searchParams.set('created', filters.created);
  }
  if (filters.version) {
    url.searchParams.set('agent_version', String(filters.version));
  } else {
    url.searchParams.delete('agent_version');
  }
  if (filters.deploymentId) {
    url.searchParams.set('deployment_id', filters.deploymentId);
  } else {
    url.searchParams.delete('deployment_id');
  }
  if (filters.status === 'all') {
    url.searchParams.delete('status');
  } else {
    url.searchParams.set('status', filters.status);
  }
  window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`);
}

export function agentDetailCreatedRange(filter: AgentDetailCreatedFilter) {
  const now = new Date();
  const end = now.toISOString();
  switch (filter) {
    case 'today': {
      const start = new Date(now);
      start.setHours(0, 0, 0, 0);
      return { gte: start.toISOString(), lte: end };
    }
    case 'last_hour':
      return { gte: new Date(now.getTime() - 60 * 60 * 1000).toISOString(), lte: end };
    case 'last_day':
      return { gte: new Date(now.getTime() - 24 * 60 * 60 * 1000).toISOString(), lte: end };
    case 'last_7_days':
      return { gte: new Date(now.getTime() - 7 * 24 * 60 * 60 * 1000).toISOString(), lte: end };
    case 'last_30_days':
      return { gte: new Date(now.getTime() - 30 * 24 * 60 * 60 * 1000).toISOString(), lte: end };
    case 'all_time':
    default:
      return { gte: null, lte: null };
  }
}

export function agentDetailStatusValues(status: AgentDetailStatusFilter) {
  switch (status) {
    case 'active':
      return ['running', 'idle', 'rescheduling'];
    case 'idle':
    case 'running':
    case 'rescheduling':
    case 'terminated':
      return [status];
    case 'all':
    default:
      return ['rescheduling', 'running', 'idle', 'terminated'];
  }
}

export function sortAgentVersions(versions: AgentApiResponse[], current: AgentApiResponse) {
  const byVersion = new Map<number, AgentApiResponse>();
  [...versions, current].forEach((agent) => byVersion.set(agent.version, agent));
  return [...byVersion.values()].sort((left, right) => right.version - left.version);
}

export function latestAgentVersion(versions: AgentApiResponse[], current: AgentApiResponse | null) {
  return Math.max(1, current?.version ?? 1, ...versions.map((version) => version.version || 1));
}

export function uniqueVersionNumbers(versions: AgentApiResponse[], fallbackVersion: number) {
  return [...new Set([fallbackVersion, ...versions.map((version) => version.version).filter((version) => version > 0)])].sort(
    (left, right) => right - left
  );
}

export function formatDetailDate(value: string) {
  const timestamp = Date.parse(value);
  if (!Number.isFinite(timestamp)) {
    return 'recently';
  }
  return new Intl.DateTimeFormat('en', {
    month: 'short',
    day: 'numeric',
    year: 'numeric',
    hour: 'numeric',
    minute: '2-digit'
  }).format(new Date(timestamp));
}

export function agentToolCards(agent: AgentApiResponse): AgentToolCardModel[] {
  const cards: AgentToolCardModel[] = [];
  const tools = ensureArray(agent.tools);
  const mcpServers = ensureArray(agent.mcp_servers);

  tools
    .map((tool) => objectRecord(tool))
    .filter((tool) => String(tool.type || '').includes('agent_toolset'))
    .forEach((tool) => {
      const type = String(tool.type || 'agent_toolset_20260401');
      const permissions = builtInToolPermissions(type);
      cards.push({
        title: 'Built-in tools',
        subtitle: type,
        permissionCount: permissions.length || (Array.isArray(tool.configs) ? tool.configs.length : 0),
        permissions
      });
    });

  mcpServers.map((server) => objectRecord(server)).forEach((server, index) => {
    const name = String(server.name || server.display_name || `MCP server ${index + 1}`);
    const url = String(server.url || server.server_url || server.mcp_server_url || 'Configured MCP server');
    cards.push({
      title: name,
      subtitle: url,
      permissionCount: mcpPermissionCount(name, tools)
    });
  });

  if (!cards.length && tools.length) {
    tools.map((tool) => objectRecord(tool)).forEach((tool, index) => {
      cards.push({
        title: String(tool.name || tool.type || `Tool ${index + 1}`),
        subtitle: String(tool.type || 'Configured tool'),
        permissionCount: Array.isArray(tool.configs) ? tool.configs.length : 0
      });
    });
  }

  return cards.length ? cards : [{ title: 'No tools configured', subtitle: 'Add built-in tools or MCP servers when editing this agent.', permissionCount: 0 }];
}

export function builtInToolPermissions(toolsetType: string): AgentToolPermission[] {
  return BUILT_IN_AGENT_TOOLSETS[toolsetType] ?? [];
}

export function mcpPermissionCount(serverName: string, tools: unknown[]) {
  const normalized = serverName.toLowerCase();
  const tool = tools
    .map((item) => objectRecord(item))
    .find((item) => String(item.mcp_server_name || item.name || '').toLowerCase() === normalized);
  return Array.isArray(tool?.configs) ? tool.configs.length : 0;
}

export function ensureArray(value: unknown): unknown[] {
  return Array.isArray(value) ? value : [];
}

export function sessionVersionLabel(session: SessionApiResponse) {
  const agent = objectRecord(session.agent);
  const version = agent.version;
  return typeof version === 'number' && version > 0 ? `v${version}` : '-';
}

export function sessionTokenUsage(session: SessionApiResponse) {
  const usage = objectRecord(session.usage);
  const stats = objectRecord(session.stats);
  const input =
    numericValueFromKeys(usage, ['input_tokens', 'inputTokens', 'tokens_in', 'tokensIn', 'input']) ||
    numericValueFromKeys(stats, ['input_tokens', 'inputTokens', 'tokens_in', 'tokensIn', 'input']);
  const output =
    numericValueFromKeys(usage, ['output_tokens', 'outputTokens', 'tokens_out', 'tokensOut', 'output']) ||
    numericValueFromKeys(stats, ['output_tokens', 'outputTokens', 'tokens_out', 'tokensOut', 'output']);
  const cacheRead =
    numericValueFromKeys(usage, ['cache_read_input_tokens', 'cacheReadInputTokens', 'cache_read_tokens', 'cacheReadTokens']) ||
    numericValueFromKeys(stats, ['cache_read_input_tokens', 'cacheReadInputTokens', 'cache_read_tokens', 'cacheReadTokens']);
  const cacheCreation =
    numericValueFromKeys(usage, ['cache_creation_input_tokens', 'cacheCreationInputTokens', 'cache_creation_tokens', 'cacheCreationTokens']) ||
    numericValueFromKeys(stats, ['cache_creation_input_tokens', 'cacheCreationInputTokens', 'cache_creation_tokens', 'cacheCreationTokens']);
  return { input: input + cacheRead + cacheCreation, output };
}

export function emptyAgentSessionAnalyticsOverview(): AgentSessionAnalyticsOverview {
  return {
    sessions_count: { value: 0 },
    error_rate: { value: 0 },
    input_tokens: { total: 0, p50: 0, p95: 0 },
    output_tokens: { total: 0, p50: 0, p95: 0 },
    duration: { total: 0, p50: 0, p95: 0 },
    active_time: { total: 0, p50: 0, p95: 0 },
    input_tokens_per_session: { p50: 0, p95: 0 },
    output_tokens_per_session: { p50: 0, p95: 0 },
    turns_per_session: { p50: 0, p95: 0 },
    tool_call_counts: {},
    stop_reason_counts: {},
    data_as_of: null
  };
}

export function metricValue(metric?: number | AnalyticsMetricBucket) {
  if (typeof metric === 'number') {
    return Number.isFinite(metric) ? metric : 0;
  }
  if (!metric) {
    return 0;
  }
  const value = Number(metric.value ?? metric.count ?? 0);
  return Number.isFinite(value) ? value : 0;
}

export function metricTotal(metric?: AnalyticsMetricBucket) {
  const total = metric?.total;
  if (total && typeof total === 'object') {
    return metricValue(total as AnalyticsMetricBucket);
  }
  return Number(total ?? metric?.value ?? metric?.count ?? 0) || 0;
}

export function metricQuantile(metric: AnalyticsMetricBucket | undefined, quantile: 'p50' | 'p90' | 'p95') {
  const value = metric?.[quantile];
  if (value && typeof value === 'object') {
    return metricValue(value as AnalyticsMetricBucket);
  }
  const parsed = Number(value ?? 0);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function formatInteger(value: number) {
  return new Intl.NumberFormat('en', { maximumFractionDigits: 0 }).format(Number.isFinite(value) ? value : 0);
}

export function formatDecimal(value: number) {
  return new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format(Number.isFinite(value) ? value : 0);
}

export function formatPercent(value: number) {
  const normalized = value > 1 ? value / 100 : value;
  return `${new Intl.NumberFormat('en', { maximumFractionDigits: 1 }).format((Number.isFinite(normalized) ? normalized : 0) * 100)}%`;
}

export function formatDurationSeconds(value: number) {
  if (!Number.isFinite(value) || value <= 0) {
    return '0s';
  }
  if (value < 60) {
    return `${formatDecimal(value)}s`;
  }
  const minutes = value / 60;
  return `${formatDecimal(minutes)}m`;
}
