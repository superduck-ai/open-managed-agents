import { requiredEnvironmentPackages } from './environmentRequirements';
import { retrieveAgent } from '../../api';
import {
  prepareBuilderAgent,
  prepareBuilderEnvironment,
  prepareBuilderVault,
  prepareBuilderDeployment,
  prepareBuilderSession,
  savedBuilderAgent,
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
export async function prepareBuilderMutation(
  state: BuilderState,
  tool: BuilderTool,
  id: string,
  workspaceId: string,
): Promise<() => Promise<Partial<BuilderState>>> {
  if (tool.name === 'build_agent_config') {
    if (state.agent) {
      const latest = await retrieveAgent(state.agent.id, workspaceId);
      if (latest.version !== state.agent.version) throw new BuilderVersionConflictError('The agent changed elsewhere.');
    }
    return prepareBuilderAgent(state, id, workspaceId);
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
    const saveVault = prepareBuilderVault(tool, id, workspaceId);
    return async () => ({ ...invalidateBuilderTest(state), vaults: [...state.vaults, await saveVault()] });
  }
  if (tool.name === 'create_deployment') {
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
