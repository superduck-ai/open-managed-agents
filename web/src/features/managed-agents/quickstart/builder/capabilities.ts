import { z } from 'zod';
import { loadAgentMcpToolCatalogs } from '../../agents/tools/api';
import { configuredAgentToolPermission, type McpDirectoryServer } from '../../agents/tools/model';
import type { AgentApiResponse, CreateAgentInput } from '../../types';

const serverSchema = z.object({ name: z.string(), url: z.string().url() });
export type BuilderCapability = {
  agentId: string;
  agentVersion: number;
  name: string;
  url: string;
  status: 'ready' | 'unknown' | 'error';
  tools: Array<{ name: string; description?: string }>;
};

export async function loadBuilderCapabilities(orgUuid: string, workspaceId: string, agents: AgentApiResponse[]) {
  const results = await Promise.all(
    agents
      .filter((agent) => !agent.archived_at && agent.mcp_servers.length)
      .map(async (agent) => {
        const response = await loadAgentMcpToolCatalogs(orgUuid, workspaceId, agent.id, agent.version).catch(
          () => null,
        );
        return agent.mcp_servers.flatMap((value): BuilderCapability[] => {
          const parsed = serverSchema.safeParse(value);
          if (!parsed.success) return [];
          const server = parsed.data;
          const snapshot =
            response?.version === agent.version
              ? response.data.find((item) => item.server_name === server.name)
              : undefined;
          return [
            {
              agentId: agent.id,
              agentVersion: agent.version,
              ...server,
              status: response === null ? 'error' : (snapshot?.status ?? 'unknown'),
              tools:
                snapshot?.status === 'ready'
                  ? (snapshot.tools ?? [])
                      .filter(
                        (tool) =>
                          configuredAgentToolPermission(agent, `mcp__${server.name}__${tool.name}`) !== 'always_deny',
                      )
                      .map(({ name, description }) => ({ name, description }))
                  : [],
            },
          ];
        });
      }),
  );
  return results.flat();
}

export function builderCapabilityDirectory(capabilities: BuilderCapability[]): McpDirectoryServer[] {
  return capabilities
    .filter((item) => item.status === 'ready' && item.tools.length > 0)
    .map((item) => ({
      slug: item.name,
      displayName: item.name,
      url: item.url,
      toolNames: item.tools.map((tool) => tool.name),
    }));
}

export function assertBuilderCapabilities(config: CreateAgentInput, capabilities: BuilderCapability[]) {
  for (const value of config.mcp_servers) {
    const server = serverSchema.parse(value);
    const available = capabilities.filter((item) => item.url === server.url && item.status === 'ready');
    const names = new Set(available.flatMap((item) => item.tools.map((tool) => tool.name)));
    if (!names.size)
      throw new Error(
        `No usable tools have been discovered in this workspace for ${server.name}. Do not propose this service; explain the limitation or offer a text-only alternative.`,
      );
    const toolset = config.tools.find((tool) => tool.type === 'mcp_toolset' && tool.mcp_server_name === server.name);
    const parsed = z
      .object({ configs: z.array(z.object({ name: z.string(), enabled: z.boolean().optional() })).optional() })
      .parse(toolset);
    if (parsed.configs?.some((tool) => tool.enabled !== false && !names.has(tool.name)))
      throw new Error(`The proposed tool is not in the discovered workspace tools for ${server.name}.`);
  }
  if (config.tools.some((tool) => tool.type === 'custom'))
    throw new Error(
      'Custom tool definitions have no executable handler in Builder sessions. Use discovered workspace tools.',
    );
}

export function builderAgentSummaries(agents: AgentApiResponse[]) {
  return agents
    .filter((agent) => !agent.archived_at)
    .map(({ id, name, description, version }) => ({ id, name, description, version }));
}

const actionContract = `# Platform capability boundary
External actions require an actually available tool with the corresponding capability. Instructions, service names, credentials, and shell access alone do not establish an integration. Never promise or claim a handoff, message, retrieval, or other external action when no corresponding tool is available. If the requested action is unavailable, state that limitation and offer an alternative without claiming to perform it. Suggesting that the user contact a human is not transferring them. Report an external action as completed only after its successful tool result. Preserve the requested output format for tasks that do not require external actions.`;

export function builderActionSystem(system: string) {
  if (system.includes(actionContract)) return system;
  const delivery = system.indexOf('# Platform file delivery contract');
  return delivery < 0
    ? `${system}\n\n${actionContract}`
    : `${system.slice(0, delivery)}${actionContract}\n\n${system.slice(delivery)}`;
}
