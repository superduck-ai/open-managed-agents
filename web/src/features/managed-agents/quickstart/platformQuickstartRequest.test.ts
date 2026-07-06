import { describe, expect, test } from 'bun:test';
import { platformQuickstartOfficialRequest } from './platformQuickstartOfficialRequest.generated';
import {
  buildPlatformQuickstartRequest,
  buildQuickstartTurnContextText,
  platformQuickstartToolNames
} from './platformQuickstartRequest';

const blankAgentConfig = {
  name: 'Untitled agent',
  description: 'A blank starting point with the core toolset.',
  model: 'claude-sonnet-4-6',
  system: "You are a general-purpose agent that can research, write code, run commands, and use connected tools to complete the user's task end to end.",
  mcp_servers: [],
  tools: [
    {
      type: 'agent_toolset_20260401'
    }
  ],
  skills: []
};

describe('platform quickstart request builder', () => {
  test('matches the captured Platform request shape for the initial environment turn', () => {
    const request = buildPlatformQuickstartRequest({
      step: 'environment',
      deploymentSchedulePlanned: false,
      agentConfig: blankAgentConfig
    });

    expect(Object.keys(request)).toEqual(['messages', 'system', 'model', 'max_tokens', 'tools', 'tool_choice', 'stream']);
    expect(request.model).toBe(platformQuickstartOfficialRequest.model);
    expect(request.max_tokens).toBe(platformQuickstartOfficialRequest.max_tokens);
    expect(request.stream).toBe(true);
    expect(request.tool_choice).toEqual({
      type: 'auto',
      disable_parallel_tool_use: true
    });
    expect(request.system).toEqual(platformQuickstartOfficialRequest.system);
    expect(request.tools).toEqual(platformQuickstartOfficialRequest.tools);
    expect(request.messages[0]).toEqual(platformQuickstartOfficialRequest.messages[0]);
    expect(request.system.map((block) => block.cache_control)).toEqual([{ type: 'ephemeral' }, { type: 'ephemeral' }]);
    expect(platformQuickstartToolNames).toEqual([
      'ask_user_questions',
      'vault_sharing_notice',
      'build_agent_config',
      'list_environments',
      'create_environment',
      'list_vaults',
      'select_vault',
      'create_vault',
      'create_vault_credential',
      'agent_ready',
      'await_test_run',
      'offer_next_step',
      'show_integration_exits',
      'flag_schedule_intent',
      'create_deployment',
      'web_search'
    ]);
  });

  test('builds the official turn context text for continuation messages', () => {
    const text = buildQuickstartTurnContextText({
      step: 'session',
      deploymentSchedulePlanned: true,
      agentConfig: blankAgentConfig
    });

    expect(text).toContain('[Current quickstart step: "session". Follow this step\'s instructions from the system prompt.]');
    expect(text).toContain('[Deployment schedule planned: yes.]');
    expect(text).toContain("Here's the current config:");
    expect(text).toContain('"model": "claude-sonnet-4-6"');
    expect(text.endsWith('Start from the current quickstart step (see turn context).')).toBe(true);
  });

  test('builds the official freeform description turn context before template-free agent creation', () => {
    const text = buildQuickstartTurnContextText({
      step: 'agent',
      deploymentSchedulePlanned: false,
      agentDescription: 'Build an invoice tracker that summarizes inbound invoice emails.',
      agentConfig: blankAgentConfig
    });

    expect(text).toContain("I'm building an agent. Here's my description:");
    expect(text).toContain('"Build an invoice tracker that summarizes inbound invoice emails."');
    expect(text).not.toContain("I'm building a new agent.\n\nHere's the current config:");
    expect(text).toContain('"description": "A blank starting point with the core toolset."');
  });
});
