import { assertBuilderCapabilities, loadBuilderCapabilities } from './capabilities';
import { loadQuickstartResources } from '../api';
import { requiredEnvironmentPackages } from './environmentRequirements';
import { retrieveAgent } from '../../api';
import {
  prepareBuilderAgent,
  prepareBuilderEnvironment,
  prepareBuilderVault,
  prepareBuilderDeployment,
  prepareBuilderSession,
  savedBuilderAgent,
  type BuilderRuntimeCapabilities,
} from './api';
import { builderTestBinding, invalidateBuilderTest, type BuilderState, type BuilderTool } from './model';

export class BuilderVersionConflictError extends Error {}

export function operationKind(tool: BuilderTool): NonNullable<BuilderState['operation']>['kind'] {
  const kinds: Record<string, NonNullable<BuilderState['operation']>['kind']> = {
    build_agent_config: 'agent',
    create_environment: 'environment',
    create_vault: 'vault',
    create_deployment: 'deployment',
    agent_ready: 'session',
  };
  const kind = kinds[tool.name];
  if (!kind) throw new Error('This tool cannot write resources.');
  return kind;
}
async function prepareAgentMutation(
  state: BuilderState,
  id: string,
  workspaceId: string,
  orgUuid?: string,
  runtimeCapabilities?: Partial<BuilderRuntimeCapabilities>,
) {
  if (runtimeCapabilities?.mcp_servers === false && state.config.mcp_servers.length > 0) {
    throw new Error('MCP servers are not supported by this workspace runtime.');
  }
  if (runtimeCapabilities?.skills === false && state.config.skills.length > 0) {
    throw new Error('Skills are not supported by this workspace runtime.');
  }
  const capabilities =
    runtimeCapabilities?.mcp_servers !== false && state.config.mcp_servers.length && orgUuid
      ? await loadBuilderCapabilities(orgUuid, workspaceId, (await loadQuickstartResources(workspaceId)).agents)
      : [];
  assertBuilderCapabilities(state.config, capabilities);
  if (state.agent) {
    const latest = await retrieveAgent(state.agent.id, workspaceId);
    if (latest.version !== state.agent.version) throw new BuilderVersionConflictError('The agent changed elsewhere.');
  }
  return prepareBuilderAgent(state, id, workspaceId);
}

export async function prepareBuilderMutation(
  state: BuilderState,
  tool: BuilderTool,
  id: string,
  workspaceId: string,
  orgUuid?: string,
  runtimeCapabilities?: Partial<BuilderRuntimeCapabilities>,
): Promise<() => Promise<Partial<BuilderState>>> {
  if (tool.name === 'build_agent_config') {
    return prepareAgentMutation(state, id, workspaceId, orgUuid, runtimeCapabilities);
  }
  if (tool.name === 'create_environment') {
    const saveEnvironment = prepareBuilderEnvironment(
      tool,
      id,
      workspaceId,
      requiredEnvironmentPackages(state.savedConfig ?? state.config),
    );
    return async () => ({ ...invalidateBuilderTest(state), environment: await saveEnvironment() });
  }
  if (tool.name === 'create_vault') {
    if (runtimeCapabilities?.credential_vaults === false) {
      throw new Error('Credential vaults are not supported by this workspace runtime.');
    }
    const saveVault = prepareBuilderVault(tool, id, workspaceId);
    return async () => ({ ...invalidateBuilderTest(state), vaults: [...state.vaults, await saveVault()] });
  }
  if (tool.name === 'create_deployment') {
    if (runtimeCapabilities?.deployments === false) {
      throw new Error('Scheduled deployments are not supported by this workspace runtime.');
    }
    const saveDeployment = await prepareBuilderDeployment(state, tool, id, workspaceId);
    return async () => ({ deployment: await saveDeployment() });
  }
  if (tool.name === 'agent_ready') {
    const saveSession = await prepareBuilderSession(state, id, workspaceId);
    return async () => {
      const session = await saveSession();
      return {
        sessionID: session.id,
        sessionBinding: builderTestBinding(state),
        testClosed: false,
        firstMessage: '',
        testSummary: '',
      };
    };
  }
  throw new Error('Unsupported write.');
}
export async function reloadBuilderAgent(state: BuilderState, workspaceId: string) {
  if (!state.agent) throw new Error('No saved agent to reload.');
  return savedBuilderAgent(await retrieveAgent(state.agent.id, workspaceId));
}

export function savedBuilderOperationResult(kind: NonNullable<BuilderState['operation']>['kind'], state: BuilderState) {
  if (kind === 'agent') return { agent: state.agent };
  if (kind === 'environment')
    return { environment: state.environment, configuration_saved: true, runtime_verified: false };
  if (kind === 'vault') return { vaults: state.vaults.slice(-1) };
  if (kind === 'deployment') return { deployment: state.deployment };
  return { session_id: state.sessionID };
}
