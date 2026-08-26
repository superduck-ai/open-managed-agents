import type { McpTunnel } from './api';

export const MCP_TUNNEL_CHANNEL_PATTERN = /^[a-z0-9_-]{1,64}$/;
export const DEFAULT_TUNNEL_CHANNEL = 'main';

export function tunnelClientYaml(tunnel: McpTunnel, localMcpUrl: string) {
  const baseUrl = new URL(tunnel.mcp_url, window.location.origin).origin;
  return [
    'config_version: 1',
    'control_plane:',
    `  base_url: ${baseUrl}`,
    '  url_path: /connector',
    `  tunnel_id: ${tunnel.id}`,
    '  api_key: env:OMA_TUNNEL_TOKEN',
    'mcp:',
    '  server_urls:',
    '    - channel: main',
    `      url: ${localMcpUrl}`,
  ].join('\n');
}

export function visibleTunnelRefreshInterval() {
  return document.visibilityState === 'visible' ? 10_000 : false;
}

export function tunnelIdFromPath(pathname: string) {
  const encoded = pathname.match(/\/mcp-tunnels\/([^/]+)\/?$/)?.[1];
  if (!encoded) return undefined;
  try {
    return decodeURIComponent(encoded);
  } catch {
    return encoded;
  }
}

export function tunnelChannelURL(tunnel: Pick<McpTunnel, 'mcp_url'>, channel: string) {
  if (!channel || channel === DEFAULT_TUNNEL_CHANNEL) {
    return tunnel.mcp_url;
  }
  const parsed = new URL(tunnel.mcp_url, window.location.origin);
  parsed.pathname = `${parsed.pathname.replace(/\/$/, '')}/${encodeURIComponent(channel)}`;
  return parsed.toString();
}

export function tunnelChannelServerName(tunnelId: string, channel: string) {
  return `${tunnelId}__${channel}`;
}

export function tunnelChannelFromURL(tunnel: Pick<McpTunnel, 'mcp_url'>, configuredURL: string) {
  try {
    const base = new URL(tunnel.mcp_url, window.location.origin);
    const configured = new URL(configuredURL, window.location.origin);
    const basePath = base.pathname.replace(/\/$/, '');
    if (configured.origin !== base.origin || configured.search || configured.hash) {
      return null;
    }
    if (configured.pathname === basePath || configured.pathname === `${basePath}/`) {
      return DEFAULT_TUNNEL_CHANNEL;
    }
    if (!configured.pathname.startsWith(`${basePath}/`)) {
      return null;
    }
    const suffix = configured.pathname.slice(basePath.length + 1);
    const channel = decodeURIComponent(suffix);
    return !channel.includes('/') && MCP_TUNNEL_CHANNEL_PATTERN.test(channel) ? channel : null;
  } catch {
    return null;
  }
}
