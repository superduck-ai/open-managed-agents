import {
  workspaceSwitchPath,
  workspaceApiKeysPath,
  workspaceWebhooksPath,
  workspaceTelemetryPath,
  workspaceMcpTunnelsPath,
} from '../workspaces/presentation';
const workspacePathByHref: Record<string, string> = {
  '/quickstart': 'agent-quickstart',
  '/agents': 'agents',
  '/sessions': 'sessions',
  '/observability': 'observability',
  '/deployments': 'deployments',
  '/environments': 'environments',
  '/channels': 'channels',
  '/credential-vaults': 'vaults',
  '/memory-stores': 'memory-stores',
  '/dreams': 'dreams',
  '/llm-models': 'llm-models',
  '/playground': 'playground',
  '/files': 'files',
  '/skills': 'skills',
  '/mcp-servers': 'mcp-servers',
  '/batches': 'batches',
};

const workspaceSettingsResourceByHref: Record<string, string> = {
  '/api-keys': 'keys',
  '/webhooks': 'webhooks',
  '/telemetry': 'telemetry',
  '/mcp-tunnels': 'mcp-tunnels',
};

export function navigationHref(href: string, workspaceId: string) {
  if (href === '/logs') {
    return workspaceSwitchPath(href, workspaceId);
  }
  if (href === '/api-keys') {
    return workspaceApiKeysPath(workspaceId);
  }
  if (href === '/webhooks') {
    return workspaceWebhooksPath(workspaceId);
  }
  if (href === '/telemetry') {
    return workspaceTelemetryPath(workspaceId);
  }
  if (href === '/mcp-tunnels') {
    return workspaceMcpTunnelsPath(workspaceId);
  }

  const workspacePath = workspacePathByHref[href];
  if (workspacePath) {
    return `/workspaces/${encodeURIComponent(workspaceId || 'default')}/${workspacePath}`;
  }

  return href;
}

export function isActivePath(currentPath: string, href: string) {
  if (href === '/dashboard') {
    return currentPath === '/' || currentPath === '/dashboard';
  }
  const workspaceSettingsResource = workspaceSettingsResourceByHref[href];
  if (workspaceSettingsResource) {
    return isWorkspaceSettingsNavActive(currentPath, href, workspaceSettingsResource);
  }
  if (href === '/usage') {
    return currentPath === '/usage';
  }
  if (href === '/usage/cache') {
    return currentPath === '/usage/cache' || currentPath === '/caching';
  }
  if (href === '/usage/limits') {
    return currentPath === '/usage/limits' || currentPath === '/rate-limits';
  }
  if (href === '/cost') {
    return currentPath === '/cost' || /^\/workspaces\/[^/]+\/cost(\/|$)/.test(currentPath);
  }
  if (href === '/logs') {
    return currentPath === '/logs' || /^\/workspaces\/[^/]+\/logs(\/|$)/.test(currentPath);
  }
  const workspacePath = workspacePathByHref[href];
  if (workspacePath) {
    return currentPath === href || new RegExp(`^/workspaces/[^/]+/${workspacePath}(/|$)`).test(currentPath);
  }
  return currentPath === href || currentPath.startsWith(`${href}/`);
}

export function isWideConsolePath(currentPath: string) {
  return (
    ['/api-keys', '/members', '/settings/members'].includes(currentPath) ||
    /^\/settings\/workspaces\/[^/]+\/keys/.test(currentPath) ||
    currentPath === '/webhooks' ||
    /^\/settings\/workspaces\/[^/]+\/(webhooks|telemetry)/.test(currentPath) ||
    isMcpTunnelsPath(currentPath) ||
    isBuildPath(currentPath) ||
    isAnalyticsPath(currentPath) ||
    isManagedAgentsPath(currentPath)
  );
}

export function isSessionDetailPath(currentPath: string) {
  return /^\/workspaces\/[^/]+\/sessions\/[^/]+\/?$/.test(currentPath);
}

function isBuildPath(currentPath: string) {
  return (
    ['/workbench', '/llm-models', '/playground', '/files', '/skills', '/mcp-servers', '/batches'].includes(
      currentPath,
    ) ||
    currentPath.startsWith('/workbench/') ||
    /^\/workspaces\/[^/]+\/(?:llm-models|playground|files|skills|mcp-servers|batches)(\/|$)/.test(currentPath)
  );
}

function isAnalyticsPath(currentPath: string) {
  return (
    ['/usage', '/usage/cache', '/usage/limits', '/caching', '/rate-limits', '/cost', '/logs'].includes(currentPath) ||
    /^\/workspaces\/[^/]+\/(?:cost|logs)(\/|$)/.test(currentPath)
  );
}

function isMcpTunnelsPath(currentPath: string) {
  return /^\/settings\/workspaces\/[^/]+\/mcp-tunnels(\/|$)/.test(currentPath);
}

function isWorkspaceSettingsNavActive(currentPath: string, href: string, resource: string) {
  const canonicalPath = new RegExp(`^/settings/workspaces/[^/]+/${resource}`).test(currentPath);
  return href === '/mcp-tunnels' ? canonicalPath : currentPath === href || canonicalPath;
}

function isManagedAgentsPath(currentPath: string) {
  return (
    [
      '/quickstart',
      '/agents',
      '/sessions',
      '/observability',
      '/deployments',
      '/environments',
      '/channels',
      '/credential-vaults',
      '/memory-stores',
      '/dreams',
    ].includes(currentPath) ||
    /^\/workspaces\/[^/]+\/(agent-quickstart|agents|sessions|observability|deployments|environments|channels|vaults|memory-stores|dreams)(\/|$)/.test(
      currentPath,
    )
  );
}
