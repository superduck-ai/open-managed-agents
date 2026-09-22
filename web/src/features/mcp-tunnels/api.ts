import { consoleApi } from '../../shared/api/client';

export type TunnelConnectionState = 'connected' | 'disconnected' | 'unknown';

export type TunnelChannelConnection = {
  name: string;
  process_affinity: boolean;
  instance_count: number;
};

export type TunnelConnection = {
  state: TunnelConnectionState;
  instance_count: number;
  channels: TunnelChannelConnection[];
};

export type McpTunnel = {
  id: string;
  type: 'tunnel';
  display_name: string | null;
  domain: string;
  created_at: string;
  archived_at: string | null;
  mcp_url: string;
  connection: TunnelConnection;
};

export type TunnelToken = {
  id: string;
  type: 'tunnel_token';
  tunnel_token: string;
};

export type TunnelProbeTool = {
  name: string;
  title?: string;
  description?: string;
};

export type TunnelProbeResult = {
  status: 'ok';
  channel: string;
  protocol_version?: string;
  server_name?: string;
  server_version?: string;
  tools: TunnelProbeTool[];
};

function tunnelsPath(organizationUuid: string, workspaceId: string) {
  return `/api/console/organizations/${encodeURIComponent(organizationUuid)}/workspaces/${encodeURIComponent(workspaceId)}/mcp_tunnels`;
}

export function listMcpTunnels(organizationUuid: string, workspaceId: string, includeArchived: boolean) {
  return consoleApi<McpTunnel[]>(
    `${tunnelsPath(organizationUuid, workspaceId)}?include_archived=${includeArchived ? 'true' : 'false'}`,
  );
}

export function getMcpTunnel(organizationUuid: string, workspaceId: string, tunnelId: string) {
  return consoleApi<McpTunnel>(`${tunnelsPath(organizationUuid, workspaceId)}/${encodeURIComponent(tunnelId)}`);
}

export function createMcpTunnel(
  organizationUuid: string,
  workspaceId: string,
  displayName: string,
  csrfToken?: string,
) {
  return consoleApi<McpTunnel>(tunnelsPath(organizationUuid, workspaceId), {
    method: 'POST',
    body: JSON.stringify({ display_name: displayName || null }),
    csrfToken,
  });
}

export function revealMcpTunnelToken(
  organizationUuid: string,
  workspaceId: string,
  tunnelId: string,
  csrfToken?: string,
) {
  return consoleApi<TunnelToken>(
    `${tunnelsPath(organizationUuid, workspaceId)}/${encodeURIComponent(tunnelId)}/reveal_token`,
    {
      method: 'POST',
      csrfToken,
    },
  );
}

export function rotateMcpTunnelToken(
  organizationUuid: string,
  workspaceId: string,
  tunnelId: string,
  csrfToken?: string,
) {
  return consoleApi<TunnelToken>(
    `${tunnelsPath(organizationUuid, workspaceId)}/${encodeURIComponent(tunnelId)}/rotate_token`,
    {
      method: 'POST',
      body: JSON.stringify({}),
      csrfToken,
    },
  );
}

export function archiveMcpTunnel(organizationUuid: string, workspaceId: string, tunnelId: string, csrfToken?: string) {
  return consoleApi<McpTunnel>(
    `${tunnelsPath(organizationUuid, workspaceId)}/${encodeURIComponent(tunnelId)}/archive`,
    {
      method: 'POST',
      csrfToken,
    },
  );
}

export function probeMcpTunnel(
  organizationUuid: string,
  workspaceId: string,
  tunnelId: string,
  channel = 'main',
  csrfToken?: string,
) {
  return consoleApi<TunnelProbeResult>(
    `${tunnelsPath(organizationUuid, workspaceId)}/${encodeURIComponent(tunnelId)}/probe`,
    {
      method: 'POST',
      body: JSON.stringify({ channel }),
      csrfToken,
    },
  );
}
