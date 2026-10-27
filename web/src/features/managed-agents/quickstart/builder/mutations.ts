import { retrieveAgent } from '../../api';
import {
  saveBuilderAgent,
  saveBuilderEnvironment,
  saveBuilderVault,
  saveBuilderDeployment,
  saveBuilderSession,
  savedBuilderAgent,
} from './api';
import { builderTestBinding, invalidateBuilderTest, type BuilderState, type BuilderTool } from './model';

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
export async function performBuilderMutation(
  state: BuilderState,
  tool: BuilderTool,
  id: string,
  workspaceId: string,
): Promise<Partial<BuilderState>> {
  if (tool.name === 'build_agent_config') {
    if (state.agent) {
      const latest = await retrieveAgent(state.agent.id, workspaceId);
      if (latest.version !== state.agent.version)
        throw Object.assign(new Error('The agent changed elsewhere.'), { status: 409 });
    }
    return saveBuilderAgent(state, id, workspaceId);
  }
  if (tool.name === 'create_environment')
    return { ...invalidateBuilderTest(state), environment: await saveBuilderEnvironment(tool, id, workspaceId) };
  if (tool.name === 'create_vault')
    return { ...invalidateBuilderTest(state), vaults: [await saveBuilderVault(tool, id, workspaceId)] };
  if (tool.name === 'create_deployment')
    return { deployment: await saveBuilderDeployment(state, tool, id, workspaceId) };
  if (tool.name === 'agent_ready') {
    const session = await saveBuilderSession(state, id, workspaceId);
    return {
      sessionID: session.id,
      sessionBinding: builderTestBinding(state),
      testClosed: false,
      firstMessage: '',
      testSummary: '',
    };
  }
  throw new Error('Unsupported write.');
}
export async function reloadBuilderAgent(state: BuilderState, workspaceId: string) {
  if (!state.agent) throw new Error('No saved agent to reload.');
  return savedBuilderAgent(await retrieveAgent(state.agent.id, workspaceId));
}
