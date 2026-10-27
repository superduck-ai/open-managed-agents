import { z } from 'zod';
import { anthropicBetaApi } from '../../../../shared/api/anthropic';
import { listLLMProviders } from '../../../llm-providers/api';
import { listCreateAgentSkills, loadMcpDirectoryServers } from '../../agents/create-dialog-api';
import { createAgentDraftSchema, normalizeCreateAgentDraft } from '../../agents/create-dialog-model';
import { buildAgentUpdateInput } from '../../agentConfig';
import { createAgent, updateAgentDetail, retrieveAgent, listManagedEntities, listVaultCredentials } from '../../api';
import type {
  AgentApiResponse,
  CreateAgentInput,
  EnvironmentApiResponse,
  VaultApiResponse,
  VaultCredentialApiResponse,
  SessionApiResponse,
  DeploymentApiResponse,
} from '../../types';
import { allPages, loadQuickstartResources, findQuickstartSession } from '../api';
import { environmentInputSchema, validatedDeployment } from './tools';
import { invalidateBuilderTest, type BuilderState, type BuilderTool } from './model';

export async function loadBuilderCatalog(orgUuid: string, workspaceId: string) {
  const [resources, directory, skills, providers] = await Promise.all([
    loadQuickstartResources(workspaceId),
    loadMcpDirectoryServers(),
    listCreateAgentSkills(workspaceId),
    listLLMProviders(orgUuid, workspaceId),
  ]);
  return { ...resources, directory, skills, providers };
}
export type BuilderCatalog = Awaited<ReturnType<typeof loadBuilderCatalog>>;
export function builderBackend(model: string, catalog: BuilderCatalog) {
  const candidates = catalog.providers.filter((provider) =>
    provider.models.some(
      (item) => model === `${provider.id}/${item.id}` || model === `${provider.slug}/${item.id}` || model === item.id,
    ),
  );
  if (candidates.length !== 1)
    throw new Error('Unable to resolve the builder model provider. Refresh the model catalog.');
  return candidates[0].api_backend;
}
const mcpSchema = z.object({ name: z.string(), url: z.string() });
const credentialAuthSchema = z.object({ type: z.enum(['static_bearer', 'mcp_oauth']), mcp_server_url: z.string() });
export function authenticationServers(config: CreateAgentInput, catalog: BuilderCatalog) {
  return config.mcp_servers
    .map((server) => mcpSchema.parse(server))
    .filter((server) => !catalog.directory.some((entry) => entry.url === server.url && entry.authless === true));
}
export function credentialCovers(credential: VaultCredentialApiResponse, url: string) {
  const auth = credentialAuthSchema.safeParse(credential.auth);
  return !credential.archived_at && auth.success && auth.data.mcp_server_url === url;
}
export async function loadBuilderVaults(workspaceId: string, servers: Array<{ name: string; url: string }>) {
  const vaults = await allPages<VaultApiResponse>(async (page) => {
    const response = await listManagedEntities('credential-vaults', workspaceId, page);
    return {
      ...response,
      data: response.data.filter((item): item is VaultApiResponse => item.type === 'vault' && !item.archived_at),
    };
  });
  return Promise.all(
    vaults.map(async (vault) => {
      const credentials = await allPages((page) => listVaultCredentials(vault.id, workspaceId, page));
      return {
        id: vault.id,
        name: vault.display_name,
        covered: servers
          .filter((server) => credentials.some((credential) => credentialCovers(credential, server.url)))
          .map((server) => server.name),
      };
    }),
  );
}
export async function missingBuilderCredentials(state: BuilderState, catalog: BuilderCatalog, workspaceId: string) {
  const servers = authenticationServers(state.savedConfig ?? state.config, catalog);
  if (!servers.length) return [];
  const credentials = (
    await Promise.all(
      state.vaults.map((vault) => allPages((page) => listVaultCredentials(vault.id, workspaceId, page))),
    )
  ).flat();
  return servers.filter(
    (server) =>
      !state.skippedServers.includes(server.url) &&
      !credentials.some((credential) => credentialCovers(credential, server.url)),
  );
}
export function savedBuilderAgent(agent: AgentApiResponse) {
  const config = normalizeCreateAgentDraft(
    createAgentDraftSchema.parse({
      name: agent.name,
      description: agent.description,
      model: agent.model,
      system: agent.system,
      mcp_servers: agent.mcp_servers,
      tools: agent.tools,
      skills: agent.skills,
      metadata: agent.metadata,
      ...(agent.multiagent ? { multiagent: agent.multiagent } : {}),
    }),
  );
  return {
    agent: { id: agent.id, name: agent.name, version: agent.version },
    savedConfig: createAgentDraftSchema.parse(config),
    config: createAgentDraftSchema.parse(config),
  };
}
export async function saveBuilderAgent(state: BuilderState, operationId: string, workspaceId: string) {
  const config = { ...state.config, metadata: { ...state.config.metadata, quickstart_operation_id: operationId } };
  const saved = state.agent
    ? await updateAgentDetail(state.agent.id, buildAgentUpdateInput(state.agent.version, config), workspaceId)
    : await createAgent(config, workspaceId);
  return { ...invalidateBuilderTest(state), ...savedBuilderAgent(saved), operation: null };
}
export async function selectBuilderEnvironment(id: string, workspaceId: string) {
  const environment = await anthropicBetaApi.environments.retrieve<EnvironmentApiResponse>(id, workspaceId);
  if (environment.archived_at) throw new Error('This environment is archived. Choose an active environment.');
  return { id: environment.id, name: environment.name };
}
export async function saveBuilderEnvironment(tool: BuilderTool, operationId: string, workspaceId: string) {
  const input = environmentInputSchema.parse(tool.input);
  const environment = await anthropicBetaApi.environments.create<EnvironmentApiResponse>(
    {
      name: input.name,
      description: input.description ?? '',
      config: input.config,
      metadata: { quickstart_operation_id: operationId },
    },
    workspaceId,
  );
  return { id: environment.id, name: environment.name };
}
export async function saveBuilderVault(tool: BuilderTool, operationId: string, workspaceId: string) {
  const name = z.string().trim().min(1).parse(tool.input.name);
  const vault = await anthropicBetaApi.vaults.create<VaultApiResponse>(
    { display_name: name, metadata: { quickstart_operation_id: operationId } },
    workspaceId,
  );
  return { id: vault.id, name: vault.display_name };
}
export async function saveBuilderSession(state: BuilderState, operationId: string, workspaceId: string) {
  if (!state.agent || !state.environment) throw new Error('Save an agent and choose an environment first.');
  return anthropicBetaApi.sessions.create<SessionApiResponse>(
    {
      agent: { type: 'agent', id: state.agent.id, version: state.agent.version },
      environment_id: state.environment.id,
      vault_ids: state.vaults.map((vault) => vault.id),
      metadata: { quickstart_operation_id: operationId },
      resources: [],
    },
    workspaceId,
  );
}
export async function saveBuilderDeployment(
  state: BuilderState,
  tool: BuilderTool,
  operationId: string,
  workspaceId: string,
) {
  if (!state.agent || !state.environment || !state.testClosed)
    throw new Error('Finish the test before creating a deployment.');
  const input = validatedDeployment(tool);
  const deployment = await anthropicBetaApi.deployments.create<DeploymentApiResponse>(
    {
      name: input.name,
      agent: { type: 'agent', id: state.agent.id, version: state.agent.version },
      environment_id: state.environment.id,
      vault_ids: state.vaults.map((vault) => vault.id),
      resources: [],
      metadata: { quickstart_operation_id: operationId },
      initial_events: [{ type: 'user.message', content: [{ type: 'text', text: input.initial_message }] }],
      schedule: { type: 'cron', expression: input.cron_expression, timezone: input.timezone },
    },
    workspaceId,
  );
  return { id: deployment.id, name: deployment.name };
}
const resourceIdentity = z
  .object({
    id: z.string(),
    name: z.string().optional(),
    display_name: z.string().optional(),
    metadata: z.record(z.string(), z.string()).optional(),
  })
  .passthrough();
export async function recoverBuilderOperation(state: BuilderState, workspaceId: string) {
  const operation = state.operation;
  if (!operation) throw new Error('There is no pending operation.');
  if (operation.kind === 'agent') {
    const resources = state.agent
      ? [await retrieveAgent(state.agent.id, workspaceId)]
      : (await loadQuickstartResources(workspaceId)).agents;
    const found = resources.filter((agent) => agent.metadata.quickstart_operation_id === operation.id);
    if (found.length !== 1)
      throw new Error(
        found.length
          ? 'Multiple saved results require review.'
          : 'No saved result found yet. Check again before retrying.',
      );
    return { ...state, ...savedBuilderAgent(found[0]), operation: null };
  }
  if (operation.kind === 'session') {
    const found = await findQuickstartSession(operation.id, workspaceId, state.agent?.id ?? '');
    if (found.length !== 1) throw new Error('No unique saved session found yet. Check again before retrying.');
    return { ...state, sessionID: found[0].id, operation: null };
  }
  const section =
    operation.kind === 'vault' ? 'credential-vaults' : operation.kind === 'deployment' ? 'deployments' : 'environments';
  const resources = await allPages((page) =>
    listManagedEntities(section, workspaceId, page, { includeArchived: true }),
  );
  const matches = resources
    .map((item) => resourceIdentity.parse(item))
    .filter((item) => item.metadata?.quickstart_operation_id === operation.id);
  if (matches.length !== 1) throw new Error('No unique saved result found yet. Check again before retrying.');
  const reference = { id: matches[0].id, name: matches[0].name ?? matches[0].display_name ?? matches[0].id };
  if (operation.kind === 'environment')
    return { ...invalidateBuilderTest(state), environment: reference, operation: null };
  if (operation.kind === 'deployment') return { ...state, deployment: reference, operation: null };
  return { ...invalidateBuilderTest(state), vaults: [reference], operation: null };
}
