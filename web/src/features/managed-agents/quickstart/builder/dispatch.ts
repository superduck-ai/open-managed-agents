import { z } from 'zod';
import {
  authenticationServers,
  loadBuilderVaults,
  missingBuilderCredentials,
  selectBuilderEnvironment,
  type BuilderCatalog,
} from './api';
import { builderNextStep, invalidateBuilderTest, type BuilderState, type BuilderTool } from './model';
import { proposedAgentConfig, validateBuilderTool, environmentInputSchema, validatedDeployment } from './tools';
import { stableStringify } from '../platformQuickstartRequest';

export type BuilderVaultOption = { id: string; name: string; covered: string[] };
export type PreparedTool = {
  result?: unknown;
  patch?: Partial<BuilderState>;
  gate?: boolean;
  vaults?: BuilderVaultOption[];
};
type ToolContext = {
  state: BuilderState;
  tool: BuilderTool;
  catalog: BuilderCatalog;
  models: string[];
  workspaceId: string;
};
function savedResources(state: BuilderState) {
  if (!state.agent || !state.environment) throw new Error('Save the agent and choose an environment first.');
  return { agent: state.agent, environment: state.environment };
}
function beforeTest(state: BuilderState) {
  if (state.sessionID && !state.testClosed)
    throw new Error('End the current test before changing its configuration or dependencies.');
}
async function vaultOptions(context: ToolContext) {
  const servers = authenticationServers(context.state.savedConfig ?? context.state.config, context.catalog);
  if (!servers.length) throw new Error('No configured MCP server needs credentials. Skip the vault flow.');
  return loadBuilderVaults(context.workspaceId, servers);
}
const handlers: Record<string, (context: ToolContext) => Promise<PreparedTool> | PreparedTool> = {
  ask_user_questions: () => ({ gate: true }),
  build_agent_config: ({ state, tool, models, catalog }) => {
    beforeTest(state);
    const config = proposedAgentConfig(tool, state, models);
    for (const skill of config.skills)
      if (!catalog.skills.some((option) => option.id === skill.skill_id))
        throw new Error('The proposed skill is not available in this workspace.');
    if (state.agent && stableStringify(config) === stableStringify(state.savedConfig))
      return { result: 'Configuration is unchanged; no new version was created.', patch: { config } };
    return { gate: true, patch: { config } };
  },
  list_environments: ({ catalog }) => ({
    result: catalog.environments
      .filter((environment) => !environment.archived_at)
      .map((environment) => ({ id: environment.id, name: environment.name, config: environment.config })),
  }),
  pick_environment: ({ state }) => {
    beforeTest(state);
    if (!state.agent) throw new Error('Save the agent first.');
    return { gate: true };
  },
  create_environment: async ({ state, tool, workspaceId }) => {
    beforeTest(state);
    if (!state.agent) throw new Error('Save the agent first.');
    const input = environmentInputSchema.parse(tool.input);
    if (!input.reuse_environment_id) return { gate: true };
    const environment = await selectBuilderEnvironment(input.reuse_environment_id, workspaceId);
    return {
      result: { ...environment, runtime_verified: false, selected: true },
      patch: { ...invalidateBuilderTest(state), environment },
    };
  },
  list_vaults: async (context) => ({ result: await vaultOptions(context) }),
  pick_vault: async (context) => {
    beforeTest(context.state);
    return { gate: true, vaults: await vaultOptions(context) };
  },
  select_vault: async (context) => {
    beforeTest(context.state);
    const vaults = await vaultOptions(context);
    const ids = z.array(z.string()).parse(context.tool.input.vault_ids);
    if (ids.some((id) => !vaults.some((vault) => vault.id === id)))
      throw new Error('A selected vault is not available in this workspace.');
    return { gate: true, vaults };
  },
  create_vault: (context) => {
    beforeTest(context.state);
    savedResources(context.state);
    if (!authenticationServers(context.state.config, context.catalog).length)
      throw new Error('No configured service requires a vault.');
    return { gate: true };
  },
  create_vault_credential: async ({ state, tool, catalog, workspaceId }) => {
    beforeTest(state);
    if (!state.vaults.some((vault) => vault.id === tool.input.vault_id))
      throw new Error('Choose this vault before adding its credential.');
    const required = authenticationServers(state.savedConfig ?? state.config, catalog);
    const server = required.find((item) => item.name === tool.input.mcp_server_name);
    if (!server) throw new Error('This configured service does not require a credential.');
    const missing = await missingBuilderCredentials(
      { ...state, skippedServers: state.skippedServers.filter((url) => url !== server.url) },
      catalog,
      workspaceId,
    );
    return missing.some((item) => item.url === server.url)
      ? { gate: true }
      : { result: `Credentials already cover ${server.name}.` };
  },
  agent_ready: async ({ state, tool, catalog, workspaceId }) => {
    savedResources(state);
    if (stableStringify(state.config) !== stableStringify(state.savedConfig))
      throw new Error('Save or discard the proposed agent changes before testing.');
    const missing = await missingBuilderCredentials(state, catalog, workspaceId);
    if (missing.length)
      throw new Error(
        `Choose a vault and authorize or explicitly skip these services: ${missing.map((server) => server.name).join(', ')}`,
      );
    if (state.sessionID && !state.testClosed)
      return { result: { session_id: state.sessionID, message: 'A test is already open. Wait for it to finish.' } };
    return { gate: true, patch: { suggestedMessage: String(tool.input.suggested_first_message) } };
  },
  await_test_run: ({ state, tool }) => {
    if (!state.sessionID) throw new Error('Call agent_ready and start a test first.');
    if (tool.input.until === 'first_message' && state.firstMessage)
      return { result: { session_id: state.sessionID, first_message: state.firstMessage } };
    if (tool.input.until === 'session_closed' && state.testClosed)
      return { result: state.testSummary || 'The test was closed. No transcript summary is available.' };
    return { gate: true };
  },
  offer_next_step: ({ state }) => {
    if (!state.agent)
      throw new Error(
        'No agent has been saved. Call build_agent_config to propose the configuration and wait for user confirmation before advancing.',
      );
    if (stableStringify(state.config) !== stableStringify(state.savedConfig))
      throw new Error('Save or discard the proposed configuration before advancing.');
    if (state.step !== 'agent' && !state.environment) throw new Error('Choose an environment before advancing.');
    if (state.step === 'session' && !state.testClosed)
      throw new Error('End the test and analyze its events before advancing.');
    if (state.step === 'deploy' && state.wantsSchedule && !state.deployment)
      throw new Error('Create the planned deployment, or withdraw the schedule, before advancing.');
    if (builderNextStep(state) === state.step) throw new Error('Already on the final step. Show integration instead.');
    return { gate: true };
  },
  flag_schedule_intent: ({ state, tool }) => ({
    result: 'Schedule intent recorded; this did not create a deployment.',
    patch: {
      wantsSchedule: tool.input.wants_schedule === true,
      ...(state.step === 'deploy' && !tool.input.wants_schedule ? { step: 'integrate' as const } : {}),
      ...(state.step === 'integrate' && tool.input.wants_schedule ? { step: 'deploy' as const } : {}),
    },
  }),
  create_deployment: ({ state, tool }) => {
    beforeTest(state);
    savedResources(state);
    if (state.step !== 'deploy' || !state.wantsSchedule || !state.testClosed)
      throw new Error('Plan a schedule and finish the test before creating a deployment.');
    validatedDeployment(tool);
    return { gate: true };
  },
  show_integration_exits: ({ state, tool }) => {
    savedResources(state);
    if (state.step !== 'integrate' || !state.testClosed)
      throw new Error('Finish testing and advance to integration first.');
    if (tool.input.agent_id && tool.input.agent_id !== state.agent?.id)
      throw new Error('Integration must use the saved agent.');
    if (tool.input.environment_id && tool.input.environment_id !== state.environment?.id)
      throw new Error('Integration must use the selected environment.');
    return { gate: true };
  },
};
export async function prepareBuilderTool(context: ToolContext): Promise<PreparedTool> {
  validateBuilderTool(context.tool);
  const handler = handlers[context.tool.name];
  if (!handler) throw new Error('Unsupported builder tool.');
  return handler(context);
}
