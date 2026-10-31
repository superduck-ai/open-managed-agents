import type { Locale } from '../../../shared/i18n';
import {
  buildQuickstartTurnContextText,
  type QuickstartMessage,
  type QuickstartStep,
} from './platformQuickstartRequest';

export type QuickstartToolExecutionResult = { content: string; isError?: boolean };

export function quickstartChatReplyToolResult(call: { name: string }, reply: string, locale: Locale = 'en') {
  const zh = locale === 'zh-CN';
  if (call.name === 'build_agent_config' || call.name === 'await_test_run') {
    return zh ? `用户改为发送了消息："${reply}"` : `User sent a message instead: "${reply}"`;
  }
  return zh ? `用户在聊天中回复：${reply}` : `User replied in chat: ${reply}`;
}

export function toolResultBlock(toolUseId: string, result: QuickstartToolExecutionResult) {
  return {
    type: 'tool_result',
    tool_use_id: toolUseId,
    content: result.content,
    ...(result.isError ? { is_error: true } : {}),
  };
}

export function buildQuickstartToolResultMessage(blocks: Array<Record<string, unknown>>): QuickstartMessage {
  return { role: 'user', content: blocks };
}

export function quickstartTurnMessages<T extends QuickstartMessage>(
  messages: T[],
  context: {
    step: QuickstartStep;
    wantsSchedule: boolean;
    config: Record<string, unknown>;
    modelID: string;
    locale: Locale;
  },
): T[] {
  const lastMessage = messages.at(-1);
  if (!lastMessage || lastMessage.role !== 'user') return messages;
  const contextBlock = {
    type: 'text',
    text: buildQuickstartTurnContextText({
      step: context.step,
      deploymentSchedulePlanned: context.wantsSchedule,
      agentConfig: context.config,
      modelID: context.modelID,
      locale: context.locale,
    }),
  };
  return [
    ...messages.slice(0, -1),
    {
      ...lastMessage,
      content: [
        ...(typeof lastMessage.content === 'string'
          ? [{ type: 'text', text: lastMessage.content }]
          : lastMessage.content),
        contextBlock,
      ],
    },
  ];
}
