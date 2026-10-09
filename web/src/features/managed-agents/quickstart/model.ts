import type { Locale } from '../../../shared/i18n';
import type { AgentApiResponse, CreateAgentInput } from '../types';
import { agentModelName } from '../agents/AgentsResourcePage';

export type QuickstartScenarioID = 'hello' | 'research' | 'analysis' | 'tracking' | 'review' | 'custom';
export type QuickstartDraft = { name: string; description: string; modelID: string; system: string };

const scenarios = {
  en: [
    {
      id: 'hello',
      name: 'Hello World Agent',
      description: '',
      summary: 'A system prompt and core tools to run your first conversation.',
      system: 'You are a helpful assistant. Use tools to help users complete their tasks.',
    },
    {
      id: 'research',
      name: 'Deep research',
      description: 'Gather information and produce research with sources.',
      summary: 'Break down questions, gather sources and synthesize findings.',
      system:
        'Research the question using available tools. Compare sources, cite evidence, and distinguish verified findings from hypotheses.',
    },
    {
      id: 'analysis',
      name: 'Data analysis',
      description: 'Analyze data files and explain metrics and changes.',
      summary: 'Analyze user-provided data, calculate and explain results.',
      system:
        'Analyze user-provided data using available tools. Check data quality, calculate results, explain evidence and limitations, and never invent missing data.',
    },
    {
      id: 'tracking',
      name: 'Topic tracking',
      description: 'Summarize important changes in a topic.',
      summary: 'Gather recent information and summarize changes and trends.',
      system:
        'Track the topic, time range and sources the user provides. Use available tools to summarize changes with citations. Ask for missing information.',
    },
    {
      id: 'review',
      name: 'Code review',
      description: 'Review user-provided code changes.',
      summary: 'Check code changes and suggest concrete improvements.',
      system:
        'Review user-provided code or diffs for correctness and edge cases. Report findings with locations and actionable suggestions. Do not modify code.',
    },
    {
      id: 'custom',
      name: 'My Agent',
      description: '',
      summary: 'Start from scratch with your own model and system prompt.',
      system: '',
    },
  ],
  'zh-CN': [
    {
      id: 'hello',
      name: 'Hello World Agent',
      description: '',
      summary: '一个系统提示词和基础工具，先跑通第一次调用。',
      system: '你是一个通用助手，使用工具帮助用户完成任务。',
    },
    {
      id: 'research',
      name: '深度研究',
      description: '收集资料，形成有来源的研究结论。',
      summary: '拆解问题，整理资料和来源，形成研究结论。',
      system:
        '你是研究助手。先拆解问题，再使用可用工具收集资料、比较来源，给出结论并标注来源。区分已证实信息和待验证判断。',
    },
    {
      id: 'analysis',
      name: '数据分析',
      description: '分析数据文件，解释指标与变化。',
      summary: '读取用户提供的数据，完成分析、计算和结果解读。',
      system:
        '你是数据分析助手。先了解用户提供的数据和分析目标，再检查数据、计算并解释结果。说明依据和数据限制，不编造缺失数据。',
    },
    {
      id: 'tracking',
      name: '领域追踪',
      description: '整理指定领域的重要变化。',
      summary: '围绕一个主题整理最新资料，归纳变化与趋势。',
      system: '围绕用户指定的主题、时间范围和来源，使用可用工具整理资料，归纳变化并附上来源。资料不足时先确认。',
    },
    {
      id: 'review',
      name: '代码评审',
      description: '检查用户提供的代码变更。',
      summary: '检查代码变更，找出问题并给出具体修改建议。',
      system: '先明确评审范围，读取用户提供的代码或变更，检查正确性和边界条件。给出问题、位置与建议，不直接修改代码。',
    },
    {
      id: 'custom',
      name: '我的 Agent',
      description: '',
      summary: '从空白配置开始，定义自己的模型和系统提示词。',
      system: '',
    },
  ],
} satisfies Record<
  Locale,
  Array<{ id: QuickstartScenarioID; name: string; description: string; summary: string; system: string }>
>;

export function quickstartScenarios(locale: Locale) {
  return scenarios[locale];
}

export function quickstartDraft(scenarioID: QuickstartScenarioID, locale: Locale, modelID: string): QuickstartDraft {
  const scenario = quickstartScenarios(locale).find((item) => item.id === scenarioID)!;
  return { name: scenario.name, description: scenario.description, system: scenario.system, modelID };
}

export function quickstartAgentBody(draft: QuickstartDraft): CreateAgentInput {
  return {
    name: draft.name.trim(),
    ...(draft.description.trim() ? { description: draft.description.trim() } : {}),
    model: draft.modelID,
    system: draft.system.trim(),
    tools: [{ type: 'agent_toolset_20260401' }],
    mcp_servers: [],
    skills: [],
  };
}

export function quickstartSavedDraft(agent: AgentApiResponse): QuickstartDraft {
  return {
    name: agent.name,
    description: agent.description ?? '',
    modelID: agentModelName(agent.model),
    system: agent.system ?? '',
  };
}

export function quickstartBinding(agent: AgentApiResponse, environmentID: string) {
  return `${agent.id}:${agent.version}:${environmentID}`;
}
