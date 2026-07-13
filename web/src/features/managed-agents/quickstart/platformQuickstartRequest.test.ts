import { describe, expect, test } from 'bun:test';
import { platformQuickstartOfficialRequest } from './platformQuickstartOfficialRequest.generated';
import {
  buildPlatformQuickstartRequest,
  buildQuickstartTurnContextText,
  platformQuickstartToolNames,
} from './platformQuickstartRequest';
import { quickstartToolResultText } from './quickstartPromptText';

const blankAgentConfig = {
  name: 'Untitled agent',
  description: 'A blank starting point with the core toolset.',
  model: 'claude-sonnet-4-6',
  system:
    "You are a general-purpose agent that can research, write code, run commands, and use connected tools to complete the user's task end to end.",
  mcp_servers: [],
  tools: [
    {
      type: 'agent_toolset_20260401',
    },
  ],
  skills: [],
};

describe('platform quickstart request builder', () => {
  test('matches the captured Platform request shape for the initial environment turn', () => {
    const request = buildPlatformQuickstartRequest({
      step: 'environment',
      deploymentSchedulePlanned: false,
      agentConfig: blankAgentConfig,
    });

    expect(Object.keys(request)).toEqual([
      'messages',
      'system',
      'model',
      'max_tokens',
      'tools',
      'tool_choice',
      'stream',
    ]);
    expect(request.model).toBe(platformQuickstartOfficialRequest.model);
    expect(request.max_tokens).toBe(platformQuickstartOfficialRequest.max_tokens);
    expect(request.stream).toBe(true);
    expect(request.tool_choice).toEqual({
      type: 'auto',
      disable_parallel_tool_use: true,
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
      'web_search',
    ]);
  });

  test('builds the official turn context text for continuation messages', () => {
    const text = buildQuickstartTurnContextText({
      step: 'session',
      deploymentSchedulePlanned: true,
      agentConfig: blankAgentConfig,
    });

    expect(text).toContain(
      '[Current quickstart step: "session". Follow this step\'s instructions from the system prompt.]',
    );
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
      agentConfig: blankAgentConfig,
    });

    expect(text).toContain("I'm building an agent. Here's my description:");
    expect(text).toContain('"Build an invoice tracker that summarizes inbound invoice emails."');
    expect(text).not.toContain("I'm building a new agent.\n\nHere's the current config:");
    expect(text).toContain('"description": "A blank starting point with the core toolset."');
  });

  test('keeps block 0 English but localizes the Agent Builder block for the zh-CN locale', () => {
    const request = buildPlatformQuickstartRequest({
      step: 'environment',
      deploymentSchedulePlanned: false,
      agentConfig: blankAgentConfig,
      locale: 'zh-CN',
    });

    // Block 0 (API reference) stays byte-identical to the English source.
    expect(request.system[0]).toEqual(platformQuickstartOfficialRequest.system[0]);
    expect(request.system.map((block) => block.cache_control)).toEqual([{ type: 'ephemeral' }, { type: 'ephemeral' }]);

    const builderText = request.system[1].text as string;
    const englishBuilderText = platformQuickstartOfficialRequest.system[1].text as string;
    expect(builderText).not.toBe(englishBuilderText);
    // Localized prose.
    expect(builderText).toContain('你是一位专业的 agent 构建助手');
    expect(builderText).toContain('build_agent_config 规则');
    // Technical tokens must survive translation.
    expect(builderText).toContain('agent_toolset_20260401');
    expect(builderText).toContain('claude-sonnet-4-6');
    expect(builderText).toContain('claude-opus-4-8');
    expect(builderText).toContain('build_agent_config');
    expect(builderText).toContain('https://platform.claude.com');
    // User-visible choices must follow the Builder language instead of leaking
    // the English examples from the source prompt into structured questions.
    expect(builderText).toContain('“创建新环境”');
    expect(builderText).toContain('“不受限制”');
    expect(builderText).toContain('“按原配置重新运行”');
    expect(builderText).toContain('“暂时跳过”');
    expect(builderText).toContain('“其他”选项');
    expect(builderText).toContain('在右侧的测试运行面板中发送你的第一条消息');
    expect(builderText).toContain('增加一个“计划部署”步骤');
    expect(builderText).toContain('然后“计划部署”步骤');
    expect(builderText).toContain('“计划部署”步骤会成为下一步');
    expect(builderText).toContain('STEP: 计划部署（key "deploy"）');
    expect(builderText).not.toContain('"Create a new one"');
    expect(builderText).not.toContain('"Rerun as-is"');
    expect(builderText).not.toContain('"Skip for now"');
    expect(builderText).not.toContain('"Run it on demand instead"');
    expect(builderText).not.toContain('"Other" 选项');
    // The MCP catalog is sliced from the English block, so its identifiers appear verbatim.
    const catalogStart = englishBuilderText.indexOf('Known servers (URLs on file):\n');
    const catalogEnd = englishBuilderText.indexOf('\n  If the user names a service not in this list');
    const catalog = englishBuilderText.slice(catalogStart, catalogEnd);
    const firstCatalogLine = catalog.split('\n').find((line) => line.trim().startsWith('-'));
    expect(firstCatalogLine).toBeTruthy();
    expect(builderText).toContain((firstCatalogLine as string).trim());
  });

  test('localizes the zh-CN turn context while keeping bracketed state lines in English', () => {
    const text = buildQuickstartTurnContextText({
      step: 'session',
      deploymentSchedulePlanned: true,
      agentConfig: blankAgentConfig,
      locale: 'zh-CN',
    });

    expect(text).toContain(
      '[Current quickstart step: "session". Follow this step\'s instructions from the system prompt.]',
    );
    expect(text).toContain('[Deployment schedule planned: yes.]');
    expect(text).toContain('这是当前的配置：');
    expect(text).toContain('"model": "claude-sonnet-4-6"');
    expect(text.endsWith('请从当前 quickstart 步骤开始（见 turn context）。')).toBe(true);
  });

  test('localizes the zh-CN freeform description turn context', () => {
    const text = buildQuickstartTurnContextText({
      step: 'agent',
      deploymentSchedulePlanned: false,
      agentDescription: '构建一个汇总收件箱发票邮件的发票跟踪器。',
      agentConfig: blankAgentConfig,
      locale: 'zh-CN',
    });

    expect(text).toContain('我正在构建一个 agent。这是我的描述：');
    expect(text).toContain('"构建一个汇总收件箱发票邮件的发票跟踪器。"');
  });

  test('localizes model-facing tool results without translating technical identifiers', () => {
    const english = quickstartToolResultText('en');
    const chinese = quickstartToolResultText('zh-CN');

    expect(english.environmentCreated('env_123')).toBe('Environment created (id: env_123).');
    expect(chinese.environmentCreated('env_123')).toBe('已创建环境（id: env_123）。');
    expect(chinese.deploymentCreated('deployment_123')).toContain('deployment_123');
    expect(chinese.sessionCreatedWithMessage('session_123', 'Run the report')).toContain('session_123');
    expect(chinese.sessionCreatedWithMessage('session_123', 'Run the report')).toContain('Run the report');
    expect(chinese.webSearchUpstream).toContain('web_search');
    expect(english.questionSkipped).toBe('Skipped.');
    expect(chinese.questionSkipped).toBe('已跳过。');
    expect(chinese.agentCreated).toBe('Agent 已创建。');
    expect(chinese.refineAgentConfig).toBe('我想在创建前继续调整配置。');
    expect(chinese.keepRefining).toBe('继续调整。');
  });
});
