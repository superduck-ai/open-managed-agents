import { consoleApi } from '../../../../shared/api/client';
import { normalizeMcpDirectoryServers, type McpDirectoryServer } from './model';

const MCP_DIRECTORY_CACHE_MS = 60 * 60 * 1000;
const MCP_DIRECTORY_PATH = '/api/directory/servers?type=remote&visibility=commercial&sort=popular&limit=500';

let directoryCache: { expiresAt: number; servers: McpDirectoryServer[] } | undefined;
let directoryRequest: Promise<McpDirectoryServer[]> | undefined;
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
