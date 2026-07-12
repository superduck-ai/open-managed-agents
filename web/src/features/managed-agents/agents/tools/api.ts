import { consoleApi } from '../../../../shared/api/client';
import { normalizeMcpDirectoryServers, type McpDirectoryServer, type McpToolCatalog } from './model';

const MCP_DIRECTORY_CACHE_MS = 60 * 60 * 1000;
const MCP_DIRECTORY_PATH = '/api/directory/servers?type=remote&visibility=commercial&sort=popular&limit=500';

let directoryCache: { expiresAt: number; servers: McpDirectoryServer[] } | undefined;
let directoryRequest: Promise<McpDirectoryServer[]> | undefined;
// Directory 是全局静态展示元数据：成功结果缓存一小时并复用进行中的 Promise；
// generation 防止测试 reset 后已在途的旧请求回填新缓存。
let directoryGeneration = 0;

export function loadMcpDirectoryServers() {
  if (directoryCache && directoryCache.expiresAt > Date.now()) {
    return Promise.resolve(directoryCache.servers);
  }
  if (directoryRequest) {
    return directoryRequest;
  }

  const generation = directoryGeneration;
  const request = consoleApi<unknown>(MCP_DIRECTORY_PATH)
    .then((payload) => {
      const servers = normalizeMcpDirectoryServers(payload);
      if (generation === directoryGeneration && directoryRequest === request) {
        directoryCache = { expiresAt: Date.now() + MCP_DIRECTORY_CACHE_MS, servers };
      }
      return servers;
    })
    .finally(() => {
      if (directoryRequest === request) {
        directoryRequest = undefined;
      }
    });
  directoryRequest = request;
  return request;
}

export function resetMcpDirectoryCacheForTests() {
  directoryGeneration += 1;
  directoryCache = undefined;
  directoryRequest = undefined;
}

export function loadAgentMcpToolCatalogs(
  orgUuid: string,
  workspaceId: string,
  agentId: string,
  version: number,
  signal?: AbortSignal
) {
  const versionQuery = version > 0 ? `?version=${encodeURIComponent(String(version))}` : '';
  return consoleApi<{ data: McpToolCatalog[]; version: number }>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(agentId)}/mcp_tool_catalogs${versionQuery}`,
    { signal }
  );
}

export function refreshAgentMcpToolCatalogs(
  orgUuid: string,
  workspaceId: string,
  agentId: string,
  version: number,
  serverName: string,
  csrfToken?: string
) {
  const versionQuery = version > 0 ? `?version=${encodeURIComponent(String(version))}` : '';
  return consoleApi<{ data: McpToolCatalog; version: number }>(
    `/api/console/organizations/${encodeURIComponent(orgUuid)}/workspaces/${encodeURIComponent(workspaceId)}/agents/${encodeURIComponent(agentId)}/mcp_tool_catalogs/refresh${versionQuery}`,
    {
      method: 'POST',
      body: JSON.stringify({ server_name: serverName }),
      csrfToken
    }
  );
}
