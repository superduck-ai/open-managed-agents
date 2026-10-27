import type { CreateWorkspaceInput, Workspace } from './api';

export const workspaceColors = [
  { name: 'Lavender', value: '#A79BD8', displayValue: 'var(--violet-7)' },
  { name: 'Pink', value: '#E8A8C9', displayValue: 'var(--pink-7)' },
  { name: 'Peach', value: '#E8C1B4', displayValue: 'var(--orange-7)' },
  { name: 'Sage', value: '#D8D2A6', displayValue: 'var(--olive-7)' },
  { name: 'Mint', value: '#8CCDB5', displayValue: 'var(--mint-7)' },
  { name: 'Purple', value: '#6B5BC7', displayValue: 'var(--violet-9)' },
  { name: 'Magenta', value: '#C45BC8', displayValue: 'var(--pink-9)' },
  { name: 'Coral', value: '#D97861', displayValue: 'var(--tomato-9)' },
  { name: 'Gold', value: '#D4A04F', displayValue: 'var(--amber-9)' },
  { name: 'Emerald', value: '#2B956E', displayValue: 'var(--green-9)' },
] as const;

export function workspaceColor(workspace?: Pick<Workspace, 'display_color' | 'color'> | null) {
  const value = (workspace?.display_color || workspace?.color || '').toUpperCase();
  return workspaceColors.find((color) => color.value === value)?.displayValue ?? 'var(--violet-9)';
}

export function workspaceApiKeysPath(workspaceId: string) {
  return `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/keys`;
}

export function workspaceWebhooksPath(workspaceId: string) {
  return `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/webhooks`;
}

export function workspaceMcpTunnelsPath(workspaceId: string) {
  return `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/mcp-tunnels`;
}

export function workspaceMcpTunnelDetailPath(workspaceId: string, tunnelId: string) {
  return `${workspaceMcpTunnelsPath(workspaceId)}/${encodeURIComponent(tunnelId)}`;
}

export function workspaceIdFromPath(pathname: string) {
  const workspaceId = pathname.match(/^\/(?:settings\/)?workspaces\/([^/]+)/)?.[1];
  if (!workspaceId) {
    return undefined;
  }
  try {
    return decodeURIComponent(workspaceId);
  } catch {
    return workspaceId;
  }
}

export function buildCreateWorkspaceInput(name: string, displayColor: string): CreateWorkspaceInput {
  return {
    name,
    display_color: displayColor,
    data_residency: {
      workspace_geo: 'us',
    },
  };
}

// 切换作用域时丢弃旧资源 ID、筛选条件和详情标签，避免请求其他工作区的资源。
export function workspaceSwitchPath(currentPath: string, workspaceId: string) {
  const path = currentPath.split(/[?#]/)[0];
  const scope = `/workspaces/${encodeURIComponent(workspaceId || 'default')}`;
  const settings = path.match(/^\/settings\/workspaces\/[^/]+\/(keys|webhooks|mcp-tunnels)(?:\/|$)/);
  if (settings) return `/settings${scope}/${settings[1]}`;
  const scoped = path.match(/^\/workspaces\/[^/]+\/([^/]+)/);
  if (scoped) return `${scope}/${scoped[1]}`;
  const section = path.split('/')[1];
  if (section === 'workbench') return '/workbench';
  const aliases: Record<string, string> = {
    'api-keys': 'keys',
    webhooks: 'webhooks',
    quickstart: 'agent-quickstart',
    'credential-vaults': 'vaults',
  };
  if (section === 'api-keys' || section === 'webhooks') return `/settings${scope}/${aliases[section]}`;
  const workspaceSections = [
    'llm-models',
    'playground',
    'files',
    'skills',
    'mcp-servers',
    'batches',
    'agents',
    'sessions',
    'observability',
    'deployments',
    'environments',
    'memory-stores',
    'dreams',
    'cost',
    'logs',
  ];
  if (aliases[section] || workspaceSections.includes(section)) return `${scope}/${aliases[section] || section}`;
  return path;
}
