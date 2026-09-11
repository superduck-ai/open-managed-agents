import type { CreateWorkspaceInput, Workspace } from './api';

export const workspaceColors = [
  { name: 'Lavender', value: '#A79BD8' },
  { name: 'Pink', value: '#E8A8C9' },
  { name: 'Peach', value: '#E8C1B4' },
  { name: 'Sage', value: '#D8D2A6' },
  { name: 'Mint', value: '#8CCDB5' },
  { name: 'Purple', value: '#6B5BC7' },
  { name: 'Magenta', value: '#C45BC8' },
  { name: 'Coral', value: '#D97861' },
  { name: 'Gold', value: '#D4A04F' },
  { name: 'Emerald', value: '#2B956E' },
] as const;

export function workspaceColor(workspace?: Pick<Workspace, 'display_color' | 'color'> | null) {
  return workspace?.display_color || workspace?.color || '#9B87F5';
}

export function workspaceApiKeysPath(workspaceId: string) {
  return `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/keys`;
}

export function workspaceWebhooksPath(workspaceId: string) {
  return `/settings/workspaces/${encodeURIComponent(workspaceId || 'default')}/webhooks`;
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
  const settings = path.match(/^\/settings\/workspaces\/[^/]+\/(keys|webhooks)(?:\/|$)/);
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
